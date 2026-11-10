package netsim

import (
	"errors"
	"net"
	"sync"
	"time"
)

type Kind int

const (
	// Cone reuses one external mapping for all destinations.
	Cone Kind = iota
	// Symmetric allocates a new external port per destination.
	Symmetric
)

// NAT is a userspace UDP NAT for deterministic tests. It is not a
// general network emulator. Hairpinning is off unless enabled.
type NAT struct {
	Kind    Kind
	Hairpin bool

	wan *net.UDPConn

	mu      sync.Mutex
	cone    *net.UDPConn
	sym     map[string]*net.UDPConn // dest -> socket
	closed  chan struct{}
	deliver chan packet
}

type packet struct {
	addr net.Addr
	data []byte
}

func NewNAT(kind Kind) (*NAT, error) {
	wan, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	n := &NAT{
		Kind:    kind,
		wan:     wan,
		sym:     make(map[string]*net.UDPConn),
		closed:  make(chan struct{}),
		deliver: make(chan packet, 64),
	}
	if kind == Cone {
		n.cone = wan
		go n.readWAN(wan)
	}
	return n, nil
}

func (n *NAT) WANAddr() *net.UDPAddr {
	return n.wan.LocalAddr().(*net.UDPAddr)
}

func (n *NAT) Close() error {
	select {
	case <-n.closed:
	default:
		close(n.closed)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	_ = n.wan.Close()
	for _, c := range n.sym {
		_ = c.Close()
	}
	return nil
}

func (n *NAT) readWAN(c *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		num, addr, err := c.ReadFrom(buf)
		if err != nil {
			return
		}
		// Hairpin is not implemented unless Hairpin is set and dest is us;
		// incoming packets from the WAN are delivered to the host.
		data := make([]byte, num)
		copy(data, buf[:num])
		if !n.Hairpin && n.isSelf(addr) {
			continue
		}
		select {
		case n.deliver <- packet{addr: addr, data: data}:
		case <-n.closed:
			return
		default:
		}
	}
}

func (n *NAT) isSelf(addr net.Addr) bool {
	u, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	wan := n.WANAddr()
	return u.IP.Equal(wan.IP) && u.Port == wan.Port
}

func (n *NAT) socketFor(dest net.Addr) (*net.UDPConn, error) {
	if n.Kind == Cone {
		return n.cone, nil
	}
	key := dest.String()
	n.mu.Lock()
	defer n.mu.Unlock()
	if c, ok := n.sym[key]; ok {
		return c, nil
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	n.sym[key] = c
	go n.readWAN(c)
	return c, nil
}

// Host returns a PacketConn representing the internal host. Writes are
// source-NATed onto the WAN. The apparent local address is the current
// cone WAN address (or a placeholder for symmetric until the first send).
func (n *NAT) Host() net.PacketConn {
	return &hostConn{n: n}
}

type hostConn struct {
	n        *NAT
	mu       sync.Mutex
	deadline time.Time
}

func (h *hostConn) ReadFrom(b []byte) (int, net.Addr, error) {
	h.mu.Lock()
	dl := h.deadline
	h.mu.Unlock()
	var timeout <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return 0, nil, timeoutErr{}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case <-h.n.closed:
		return 0, nil, errors.New("nat closed")
	case <-timeout:
		return 0, nil, timeoutErr{}
	case p := <-h.n.deliver:
		n := copy(b, p.data)
		return n, p.addr, nil
	}
}

func (h *hostConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if !h.n.Hairpin && h.n.isSelf(addr) {
		return len(b), nil // drop hairpin
	}
	c, err := h.n.socketFor(addr)
	if err != nil {
		return 0, err
	}
	return c.WriteTo(b, addr)
}

func (h *hostConn) Close() error        { return nil }
func (h *hostConn) LocalAddr() net.Addr { return h.n.WANAddr() }
func (h *hostConn) SetDeadline(t time.Time) error {
	h.mu.Lock()
	h.deadline = t
	h.mu.Unlock()
	return nil
}
func (h *hostConn) SetReadDeadline(t time.Time) error  { return h.SetDeadline(t) }
func (h *hostConn) SetWriteDeadline(t time.Time) error { return nil }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

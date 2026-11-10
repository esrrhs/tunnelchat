package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"tunnelchat/pkg/protocol"
)

const (
	TypeAlloc   uint8 = 1
	TypeAllocOK uint8 = 2
	TypeForward uint8 = 3
	TypeError   uint8 = 4

	defaultMaxSessions = 32
	defaultIdle        = 60 * time.Second
	maxPayload         = protocol.MaxPacketSize
)

var (
	ErrUnauthorized = errors.New("relay token rejected")
	ErrNoSession    = errors.New("relay allocation not found")
	ErrCapacity     = errors.New("relay at capacity")
)

// Config for an opt-in local relay. The listen address should stay on
// loopback unless the operator explicitly chooses otherwise.
type Config struct {
	Listen      string
	Token       string
	MaxSessions int
	Idle        time.Duration
}

// Server forwards opaque datagrams between at most two endpoints that
// share an allocation id. It cannot decrypt application ciphertext.
type Server struct {
	cfg  Config
	conn net.PacketConn

	mu     sync.Mutex
	allocs map[[16]byte]*allocation
	closed chan struct{}
	wg     sync.WaitGroup
}

type allocation struct {
	id   [16]byte
	a, b *net.UDPAddr
	last time.Time
}

func NewServer(cfg Config, conn net.PacketConn) *Server {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = defaultMaxSessions
	}
	if cfg.Idle <= 0 {
		cfg.Idle = defaultIdle
	}
	s := &Server{cfg: cfg, conn: conn, allocs: make(map[[16]byte]*allocation), closed: make(chan struct{})}
	s.wg.Add(2)
	go s.readLoop()
	go s.gcLoop()
	return s
}

func Listen(cfg Config) (*Server, error) {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	addr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	return NewServer(cfg, conn), nil
}

func (s *Server) Addr() net.Addr { return s.conn.LocalAddr() }

func (s *Server) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	err := s.conn.Close()
	s.wg.Wait()
	return err
}

func (s *Server) readLoop() {
	defer s.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		udp, _ := addr.(*net.UDPAddr)
		if udp == nil {
			continue
		}
		s.handle(buf[:n], udp)
	}
}

func (s *Server) gcLoop() {
	defer s.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-t.C:
			now := time.Now()
			s.mu.Lock()
			for id, al := range s.allocs {
				if now.Sub(al.last) > s.cfg.Idle {
					delete(s.allocs, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) handle(pkt []byte, from *net.UDPAddr) {
	typ, id, mac, payload, err := parse(pkt)
	if err != nil {
		return
	}
	if !validMAC(s.cfg.Token, typ, id, payload, mac) {
		s.reply(from, TypeError, id, []byte("unauthorized"))
		return
	}
	switch typ {
	case TypeAlloc:
		s.mu.Lock()
		al, ok := s.allocs[id]
		if !ok {
			if len(s.allocs) >= s.cfg.MaxSessions {
				s.mu.Unlock()
				s.reply(from, TypeError, id, []byte("capacity"))
				return
			}
			al = &allocation{id: id, last: time.Now()}
			s.allocs[id] = al
		}
		switch {
		case al.a == nil || sameAddr(al.a, from):
			al.a = clone(from)
		case al.b == nil || sameAddr(al.b, from):
			al.b = clone(from)
		default:
			s.mu.Unlock()
			s.reply(from, TypeError, id, []byte("busy"))
			return
		}
		al.last = time.Now()
		s.mu.Unlock()
		s.reply(from, TypeAllocOK, id, nil)
	case TypeForward:
		if len(payload) == 0 || len(payload) > maxPayload {
			return
		}
		s.mu.Lock()
		al := s.allocs[id]
		if al == nil {
			s.mu.Unlock()
			s.reply(from, TypeError, id, []byte("no session"))
			return
		}
		var dest *net.UDPAddr
		if sameAddr(al.a, from) {
			dest = al.b
		} else if sameAddr(al.b, from) {
			dest = al.a
		}
		al.last = time.Now()
		s.mu.Unlock()
		if dest == nil {
			return // peer not yet allocated; drop
		}
		_, _ = s.conn.WriteTo(frame(TypeForward, id, s.cfg.Token, payload), dest)
	}
}

func (s *Server) reply(to *net.UDPAddr, typ uint8, id [16]byte, payload []byte) {
	_, _ = s.conn.WriteTo(frame(typ, id, s.cfg.Token, payload), to)
}

func (s *Server) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.allocs)
}

func sameAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return false
	}
	return a.IP.Equal(b.IP) && a.Port == b.Port
}

func clone(a *net.UDPAddr) *net.UDPAddr {
	ip := make(net.IP, len(a.IP))
	copy(ip, a.IP)
	return &net.UDPAddr{IP: ip, Port: a.Port}
}

func frame(typ uint8, id [16]byte, token string, payload []byte) []byte {
	mac := macOf(token, typ, id, payload)
	out := make([]byte, 4+1+16+32+len(payload))
	copy(out[:4], protocol.RelayMagic[:])
	out[4] = typ
	copy(out[5:21], id[:])
	copy(out[21:53], mac)
	copy(out[53:], payload)
	return out
}

func parse(pkt []byte) (typ uint8, id [16]byte, mac []byte, payload []byte, err error) {
	if !protocol.HasRelayMagic(pkt) || len(pkt) < 53 {
		return 0, id, nil, nil, errors.New("not a relay packet")
	}
	typ = pkt[4]
	copy(id[:], pkt[5:21])
	mac = append([]byte(nil), pkt[21:53]...)
	payload = append([]byte(nil), pkt[53:]...)
	return typ, id, mac, payload, nil
}

func macOf(token string, typ uint8, id [16]byte, payload []byte) []byte {
	h := hmac.New(sha256.New, []byte(token))
	h.Write([]byte{typ})
	h.Write(id[:])
	h.Write(payload)
	return h.Sum(nil)
}

func validMAC(token string, typ uint8, id [16]byte, payload, mac []byte) bool {
	want := macOf(token, typ, id, payload)
	return hmac.Equal(want, mac)
}

// AllocID is a rendezvous on the relay derived from the operator token
// and the two pinned identities (order-independent).
func AllocID(token string, a, b []byte) [16]byte {
	h := hmac.New(sha256.New, []byte(token))
	if bytesLess(a, b) {
		h.Write(a)
		h.Write(b)
	} else {
		h.Write(b)
		h.Write(a)
	}
	var id [16]byte
	copy(id[:], h.Sum(nil))
	return id
}

func bytesLess(a, b []byte) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// Client sends allocations and wraps opaque packets through a relay.
type Client struct {
	conn  net.PacketConn
	relay net.Addr
	token string
	id    [16]byte

	mu    sync.Mutex
	ready bool
}

func NewClient(conn net.PacketConn, relay net.Addr, token string, id [16]byte) *Client {
	return &Client{conn: conn, relay: relay, token: token, id: id}
}

func (c *Client) Allocate() error {
	pkt := frame(TypeAlloc, c.id, c.token, nil)
	if _, err := c.conn.WriteTo(pkt, c.relay); err != nil {
		return err
	}
	buf := make([]byte, 256)
	_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := c.conn.ReadFrom(buf)
	if err != nil {
		return err
	}
	typ, id, mac, payload, err := parse(buf[:n])
	if err != nil {
		return err
	}
	if !validMAC(c.token, typ, id, payload, mac) {
		return ErrUnauthorized
	}
	if typ == TypeError {
		return fmt.Errorf("relay error: %s", payload)
	}
	if typ != TypeAllocOK {
		return errors.New("unexpected relay reply")
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	return nil
}

func (c *Client) Wrap(opaque []byte) []byte {
	return frame(TypeForward, c.id, c.token, opaque)
}

func (c *Client) WriteOpaque(opaque []byte) error {
	_, err := c.conn.WriteTo(c.Wrap(opaque), c.relay)
	return err
}

func (c *Client) Unwrap(pkt []byte) ([]byte, bool) {
	typ, id, mac, payload, err := parse(pkt)
	if err != nil || typ != TypeForward || id != c.id {
		return nil, false
	}
	if !validMAC(c.token, typ, id, payload, mac) {
		return nil, false
	}
	return payload, true
}

func RandomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b[:]), nil
}

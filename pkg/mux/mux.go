package mux

import (
	"errors"
	"net"
	"sync"
	"time"

	"tunnelchat/pkg/protocol"
)

const bufSize = 32

type packet struct {
	addr net.Addr
	data []byte
}

// Mux demultiplexes STUN, DHT KRPC, application, and relay packets on
// a single PacketConn so they share the same local UDP port (and thus
// the same NAT mapping).
type Mux struct {
	conn net.PacketConn
	stun chan packet
	dht  chan packet
	app  chan packet
	rel  chan packet

	closeOnce sync.Once
	closed    chan struct{}
	err       error
}

func New(conn net.PacketConn) *Mux {
	m := &Mux{
		conn:   conn,
		stun:   make(chan packet, bufSize),
		dht:    make(chan packet, bufSize),
		app:    make(chan packet, bufSize),
		rel:    make(chan packet, bufSize),
		closed: make(chan struct{}),
	}
	go m.readLoop()
	return m
}

func (m *Mux) LocalAddr() net.Addr { return m.conn.LocalAddr() }

func (m *Mux) WriteTo(p []byte, addr net.Addr) (int, error) {
	return m.conn.WriteTo(p, addr)
}

func (m *Mux) Close() error {
	m.closeOnce.Do(func() {
		m.err = m.conn.Close()
		close(m.closed)
	})
	return m.err
}

func (m *Mux) STUN() net.PacketConn { return &port{m: m, ch: m.stun} }
func (m *Mux) DHT() net.PacketConn  { return &port{m: m, ch: m.dht} }
func (m *Mux) App() net.PacketConn  { return &port{m: m, ch: m.app} }
func (m *Mux) Relay() net.PacketConn {
	return &port{m: m, ch: m.rel}
}

func (m *Mux) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := m.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		var ch chan packet
		switch {
		case protocol.IsSTUN(data):
			ch = m.stun
		case protocol.HasRelayMagic(data):
			ch = m.rel
		case protocol.HasMagic(data):
			ch = m.app
		case protocol.IsKRPC(data):
			ch = m.dht
		default:
			continue
		}
		select {
		case ch <- packet{addr: addr, data: data}:
		default:
			// Drop rather than block the demuxer.
		case <-m.closed:
			return
		}
	}
}

type port struct {
	m        *Mux
	ch       chan packet
	mu       sync.Mutex
	deadline time.Time
}

func (p *port) ReadFrom(b []byte) (int, net.Addr, error) {
	p.mu.Lock()
	dl := p.deadline
	p.mu.Unlock()
	var timer *time.Timer
	var timeout <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return 0, nil, newTimeout("i/o timeout")
		}
		timer = time.NewTimer(d)
		timeout = timer.C
		defer timer.Stop()
	}
	select {
	case <-p.m.closed:
		return 0, nil, errors.New("mux closed")
	case pkt := <-p.ch:
		n := copy(b, pkt.data)
		return n, pkt.addr, nil
	case <-timeout:
		return 0, nil, newTimeout("i/o timeout")
	}
}

func (p *port) WriteTo(b []byte, addr net.Addr) (int, error) {
	return p.m.conn.WriteTo(b, addr)
}

func (p *port) Close() error { return nil } // closing a virtual port does not close the mux

func (p *port) LocalAddr() net.Addr { return p.m.conn.LocalAddr() }

func (p *port) SetDeadline(t time.Time) error {
	p.mu.Lock()
	p.deadline = t
	p.mu.Unlock()
	return nil
}
func (p *port) SetReadDeadline(t time.Time) error  { return p.SetDeadline(t) }
func (p *port) SetWriteDeadline(t time.Time) error { return nil }

type timeoutError struct{ s string }

func newTimeout(s string) error         { return &timeoutError{s} }
func (e *timeoutError) Error() string   { return e.s }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

package relay

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestRelayForwardsOpaqueAndRejectsBadToken(t *testing.T) {
	token := "test-token-please-change"
	srv, err := Listen(Config{Listen: "127.0.0.1:0", Token: token, MaxSessions: 2, Idle: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	id := AllocID(token, []byte("alice-key"), []byte("bob-key"))
	c1, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	a := NewClient(c1, srv.Addr(), token, id)
	b := NewClient(c2, srv.Addr(), token, id)
	if err := a.Allocate(); err != nil {
		t.Fatal(err)
	}
	if err := b.Allocate(); err != nil {
		t.Fatal(err)
	}

	plainLooksLike := []byte("this is ciphertext, not a chat command")
	if err := a.WriteOpaque(plainLooksLike); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := c2.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := b.Unwrap(buf[:n])
	if !ok || !bytes.Equal(got, plainLooksLike) {
		t.Fatalf("forward mismatch ok=%v %q", ok, got)
	}

	// Wrong token cannot allocate.
	c3, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer c3.Close()
	bad := NewClient(c3, srv.Addr(), "wrong-token", id)
	if err := bad.Allocate(); err == nil {
		t.Fatal("bad token should fail")
	}
}

func TestRelayIdleCleanup(t *testing.T) {
	token := "tok"
	srv, err := Listen(Config{Listen: "127.0.0.1:0", Token: token, MaxSessions: 2, Idle: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c1, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer c1.Close()
	id := AllocID(token, []byte("a"), []byte("b"))
	if err := NewClient(c1, srv.Addr(), token, id).Allocate(); err != nil {
		t.Fatal(err)
	}
	if srv.SessionCount() != 1 {
		t.Fatalf("count %d", srv.SessionCount())
	}
	time.Sleep(6 * time.Second) // gc ticker is 5s
	if srv.SessionCount() != 0 {
		t.Fatalf("expected cleanup, still %d", srv.SessionCount())
	}
}

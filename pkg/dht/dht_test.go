package dht

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
)

func startNode(t *testing.T) (*Node, *net.UDPConn) {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	n := NewNode(c, NodeID{})
	t.Cleanup(func() { n.Close() })
	return n, c
}

func TestAnnounceImpliedPortAndGetPeers(t *testing.T) {
	_, storeConn := startNode(t)
	pub, pubConn := startNode(t)
	pub.AddBootstrap(storeConn.LocalAddr().(*net.UDPAddr))

	secret := bytes.Repeat([]byte{0x42}, 32)
	ih := InfoHashFromSecret(secret)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pub.Announce(ctx, ih); err != nil {
		t.Fatal(err)
	}

	lookup, _ := startNode(t)
	lookup.AddBootstrap(storeConn.LocalAddr().(*net.UDPAddr))
	peers, err := lookup.GetPeers(ctx, ih)
	if err != nil {
		t.Fatal(err)
	}
	wantPort := pubConn.LocalAddr().(*net.UDPAddr).Port
	found := false
	for _, p := range peers {
		if p.Port == wantPort && p.IP.IsLoopback() {
			found = true
		}
		// Public store must not invent private addresses beyond what
		// the source IP of the announce already is (loopback in tests).
		if candidate.IsPrivateIP(p.IP) && !p.IP.IsLoopback() {
			t.Fatalf("unexpected private peer %v", p)
		}
	}
	if !found {
		t.Fatalf("expected implied port %d in %+v", wantPort, peers)
	}
}

func TestMutableHintsHidePrivateAddresses(t *testing.T) {
	_, storeConn := startNode(t)
	pub, _ := startNode(t)
	pub.AddBootstrap(storeConn.LocalAddr().(*net.UDPAddr))

	id, err := identity.Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	secret := bytes.Repeat([]byte{9}, 32)
	cands := []candidate.Candidate{
		candidate.New(candidate.TypeHost, "192.168.0.5", 1111),
		candidate.New(candidate.TypeSrflx, "203.0.113.9", 2222),
	}
	blob, err := SealHints(secret, cands)
	if err != nil {
		t.Fatal(err)
	}
	// Private address must not be present in the sealed-then-opened set,
	// and must not appear in the ciphertext as plaintext.
	if bytes.Contains(blob, []byte("192.168.0.5")) {
		t.Fatal("private address leaked into public hint blob")
	}
	opened, err := OpenHints(secret, blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0].IP != "203.0.113.9" {
		t.Fatalf("expected only public hint, got %+v", opened)
	}
	if _, err := OpenHints(bytes.Repeat([]byte{8}, 32), blob); err == nil {
		t.Fatal("wrong secret must fail")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	salt := []byte(hintSalt)
	if err := pub.PutMutable(ctx, id.Ed25519Private(), salt, 1, blob); err != nil {
		t.Fatal(err)
	}
	lookup, _ := startNode(t)
	lookup.AddBootstrap(storeConn.LocalAddr().(*net.UDPAddr))
	_, v, err := lookup.GetMutable(ctx, id.Ed25519Public(), salt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenHints(secret, v)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Port != 2222 {
		t.Fatalf("lookup hints %+v", got)
	}
}

func TestBencodeRoundTrip(t *testing.T) {
	msg := map[string]any{
		"t": "aa",
		"y": "q",
		"q": "ping",
		"a": map[string]any{"id": "abcdefghij0123456789"},
	}
	b, err := encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := asDict(v)
	if q, _ := asString(d["q"]); q != "ping" {
		t.Fatalf("got %+v", d)
	}
}

func TestResolveSettings(t *testing.T) {
	on, boots := ResolveSettings(false, true, nil, nil)
	if !on {
		t.Fatal("defaults should enable DHT")
	}
	if len(boots) != len(DefaultMainlineBootstraps) {
		t.Fatalf("expected default bootstrap list, got %v", boots)
	}
	off, boots := ResolveSettings(true, true, nil, nil)
	if off || boots != nil {
		t.Fatalf("disabled should be off, got %v %v", off, boots)
	}
	on, boots = ResolveSettings(false, true, []string{"127.0.0.1:1"}, nil)
	if !on || len(boots) != 1 || boots[0] != "127.0.0.1:1" {
		t.Fatalf("explicit override failed: %v %v", on, boots)
	}
	on, boots = ResolveSettings(false, false, nil, nil)
	if on {
		t.Fatal("without useDefaults, empty list must not contact public DHT")
	}
	on, boots = ResolveSettings(false, false, nil, []string{"10.0.0.1:1"})
	if !on || boots[0] != "10.0.0.1:1" {
		t.Fatalf("config bootstrap: %v %v", on, boots)
	}
}

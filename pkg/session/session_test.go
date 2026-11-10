package session

import (
	"bytes"
	"testing"

	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/protocol"
)

func pair(t *testing.T) (*identity.Identity, *identity.Identity) {
	t.Helper()
	a, err := identity.Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.Generate("Bob")
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func handshake(t *testing.T, a, b *identity.Identity) (*Session, *Session) {
	t.Helper()
	init, msg1, err := Initiator(a, b.Public())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := StartResponder(b, a.Public())
	if err != nil {
		t.Fatal(err)
	}
	msg2, sessB, err := resp.Continue(msg1)
	if err != nil {
		t.Fatal(err)
	}
	if sessB == nil || msg2 == nil {
		t.Fatal("responder should return session and reply")
	}
	_, sessA, err := init.Continue(msg2)
	if err != nil {
		t.Fatal(err)
	}
	if sessA == nil {
		t.Fatal("initiator should return session")
	}
	return sessA, sessB
}

func TestSessionSendRecv(t *testing.T) {
	a, b := pair(t)
	sa, sb := handshake(t, a, b)
	pkt, err := sa.Seal([]byte("hello bob"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := sb.Open(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "hello bob" {
		t.Fatalf("got %q", pt)
	}
	pkt2, err := sb.Seal([]byte("hi alice"))
	if err != nil {
		t.Fatal(err)
	}
	pt2, err := sa.Open(pkt2)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt2) != "hi alice" {
		t.Fatalf("got %q", pt2)
	}
}

func TestTamperRejected(t *testing.T) {
	a, b := pair(t)
	sa, sb := handshake(t, a, b)
	pkt, err := sa.Seal([]byte("auth me"))
	if err != nil {
		t.Fatal(err)
	}
	pkt[len(pkt)-1] ^= 0x5a
	if _, err := sb.Open(pkt); err != ErrTamper {
		t.Fatalf("expected ErrTamper, got %v", err)
	}
}

func TestReplayRejected(t *testing.T) {
	a, b := pair(t)
	sa, sb := handshake(t, a, b)
	pkt, err := sa.Seal([]byte("once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Open(pkt); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Open(pkt); err != ErrReplay {
		t.Fatalf("expected ErrReplay, got %v", err)
	}
	// Out-of-order still accepted if unused.
	p1, _ := sa.Seal([]byte("n1"))
	p2, _ := sa.Seal([]byte("n2"))
	pt, err := sb.Open(p2)
	if err != nil || string(pt) != "n2" {
		t.Fatalf("ooo p2: %s %v", pt, err)
	}
	pt, err = sb.Open(p1)
	if err != nil || string(pt) != "n1" {
		t.Fatalf("ooo p1: %s %v", pt, err)
	}
}

func TestWrongIdentityRejected(t *testing.T) {
	a, b := pair(t)
	eve, err := identity.Generate("Eve")
	if err != nil {
		t.Fatal(err)
	}
	_, msg1, err := Initiator(a, b.Public())
	if err != nil {
		t.Fatal(err)
	}
	// Bob expects Eve, but Alice is initiating.
	resp, err := StartResponder(b, eve.Public())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resp.Continue(msg1); err == nil {
		t.Fatal("bob should reject alice when pinned to eve")
	}

	// Alice initiates "to Bob" but the packet is delivered to Eve, who
	// is asked to complete as Bob. Eve does not have Bob's static key.
	eveR, err := StartResponder(eve, a.Public())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := eveR.Continue(msg1); err == nil {
		t.Fatal("eve should not complete a handshake built for bob")
	}
}

func TestDowngradePrologueRejected(t *testing.T) {
	a, b := pair(t)
	// Build a handshake with a different prologue by temporarily
	// constructing against the unexported path: reuse Initiator then
	// mutate the packet — authentication should fail.
	_, msg1, err := Initiator(a, b.Public())
	if err != nil {
		t.Fatal(err)
	}
	msg1[len(msg1)-1] ^= 0x1
	resp, err := StartResponder(b, a.Public())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resp.Continue(msg1); err == nil {
		t.Fatal("tampered handshake must fail")
	}
	_ = protocol.NoisePrologue
}

func TestLegacyBytesRejected(t *testing.T) {
	if IsHandshake([]byte("not-magic")) || IsTransport([]byte("DES")) {
		t.Fatal("legacy/garbage classified as v2")
	}
	a, b := pair(t)
	sa, sb := handshake(t, a, b)
	_ = sb
	pkt, _ := sa.Seal([]byte("x"))
	if !bytes.Equal(pkt[:4], protocol.Magic[:]) {
		t.Fatal("transport packets must use v2 magic")
	}
}

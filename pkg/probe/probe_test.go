package probe

import (
	"testing"

	"tunnelchat/pkg/identity"
)

func TestProbeRoundTripAndAuth(t *testing.T) {
	a, err := identity.Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.Generate("Bob")
	if err != nil {
		t.Fatal(err)
	}
	eve, err := identity.Generate("Eve")
	if err != nil {
		t.Fatal(err)
	}
	pkt, nonce, err := Encode(a, b.Public(), false, [16]byte{})
	if err != nil {
		t.Fatal(err)
	}
	v := NewVerifier()
	m, err := v.CheckFor(pkt, a.Public(), b.Public())
	if err != nil {
		t.Fatal(err)
	}
	if m.Ack || m.Nonce != nonce {
		t.Fatalf("unexpected message: %+v", m)
	}
	if _, err := v.CheckFor(pkt, a.Public(), b.Public()); err != ErrReplay {
		t.Fatalf("expected replay error, got %v", err)
	}

	if _, err := NewVerifier().CheckFor(pkt, eve.Public(), b.Public()); err != ErrUnauthenticated {
		t.Fatalf("eve pin should fail, got %v", err)
	}

	// Ack echoing the nonce, signed by Bob, addressed to Alice.
	ack, _, err := Encode(b, a.Public(), true, nonce)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewVerifier().CheckFor(ack, b.Public(), a.Public())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ack || got.Echo != nonce {
		t.Fatalf("ack mismatch: %+v", got)
	}

	tampered := append([]byte(nil), pkt...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := NewVerifier().CheckFor(tampered, a.Public(), b.Public()); err != ErrUnauthenticated {
		t.Fatalf("tamper should fail, got %v", err)
	}
}

func TestProbeWrongRecipient(t *testing.T) {
	a, _ := identity.Generate("Alice")
	b, _ := identity.Generate("Bob")
	c, _ := identity.Generate("Carol")
	pkt, _, err := Encode(a, b.Public(), false, [16]byte{})
	if err != nil {
		t.Fatal(err)
	}
	// Signed for Bob, verified as if addressed to Carol.
	if _, err := NewVerifier().CheckFor(pkt, a.Public(), c.Public()); err != ErrUnauthenticated {
		t.Fatalf("expected unauthenticated, got %v", err)
	}
}

package identity

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

func TestGenerateUniqueAndStableDerivation(t *testing.T) {
	a, err := Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	if a.Seed == b.Seed {
		t.Fatal("seeds must be unique")
	}

	a2, err := FromSeed(a.Name, a.Seed[:])
	if err != nil {
		t.Fatal(err)
	}
	if a.X25519Public() != a2.X25519Public() {
		t.Fatal("X25519 derivation is not stable")
	}
	if !bytes.Equal(a.Ed25519Public(), a2.Ed25519Public()) {
		t.Fatal("Ed25519 derivation is not stable")
	}

	msg := []byte("pin-me")
	sig := ed25519.Sign(a.Ed25519Private(), msg)
	if !ed25519.Verify(a.Ed25519Public(), msg, sig) {
		t.Fatal("ed25519 sign/verify failed")
	}

	pa := a.Public()
	if pa.Fingerprint() == "" || pa.Fingerprint() == b.Public().Fingerprint() {
		t.Fatal("fingerprints should uniquely identify keys")
	}
	nk := a.NoiseKey()
	if len(nk.Private) != 32 || len(nk.Public) != 32 {
		t.Fatalf("unexpected noise key sizes %d %d", len(nk.Private), len(nk.Public))
	}
	if !bytes.Equal(nk.Public, pa.X25519[:]) {
		t.Fatal("noise public key should match X25519 public")
	}
}

func TestGenerateRejectsEmptyName(t *testing.T) {
	if _, err := Generate("  "); err == nil {
		t.Fatal("expected error")
	}
}

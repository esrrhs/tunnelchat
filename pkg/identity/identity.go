package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/flynn/noise"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const SeedSize = 32

// Identity is a long-term local account. The 32-byte seed is the only
// secret; Ed25519 (signing) and X25519 (Noise static) keys are derived
// from it. Ed25519 is not used as a key-exchange primitive.
type Identity struct {
	Name string
	Seed [SeedSize]byte
}

// Public is the pin-able identity of a peer.
type Public struct {
	Name    string
	Ed25519 [ed25519.PublicKeySize]byte
	X25519  [32]byte
}

func Generate(name string) (*Identity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 64 {
		return nil, fmt.Errorf("name too long")
	}
	id := &Identity{Name: name}
	if _, err := rand.Read(id.Seed[:]); err != nil {
		return nil, fmt.Errorf("generate seed: %w", err)
	}
	return id, nil
}

func FromSeed(name string, seed []byte) (*Identity, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("seed must be %d bytes", SeedSize)
	}
	id := &Identity{Name: strings.TrimSpace(name)}
	copy(id.Seed[:], seed)
	return id, nil
}

func (id *Identity) Ed25519Private() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(id.Seed[:])
}

func (id *Identity) Ed25519Public() ed25519.PublicKey {
	return id.Ed25519Private().Public().(ed25519.PublicKey)
}

// x25519Private derives a clamped X25519 scalar from the seed via HKDF.
func (id *Identity) x25519Private() [32]byte {
	r := hkdf.New(sha256.New, id.Seed[:], []byte("tunnelchat"), []byte("x25519-v2"))
	var key [32]byte
	if _, err := io.ReadFull(r, key[:]); err != nil {
		panic("hkdf x25519: " + err.Error())
	}
	// Standard X25519 clamping.
	key[0] &= 248
	key[31] &= 127
	key[31] |= 64
	return key
}

func (id *Identity) X25519Public() [32]byte {
	priv := id.x25519Private()
	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)
	return pub
}

// NoiseKey returns the static DH key used for Noise_IK.
func (id *Identity) NoiseKey() noise.DHKey {
	priv := id.x25519Private()
	pub := id.X25519Public()
	return noise.DHKey{
		Private: append([]byte(nil), priv[:]...),
		Public:  append([]byte(nil), pub[:]...),
	}
}

func (id *Identity) Public() Public {
	var p Public
	p.Name = id.Name
	copy(p.Ed25519[:], id.Ed25519Public())
	p.X25519 = id.X25519Public()
	return p
}

func (p Public) Fingerprint() string {
	h := sha256.New()
	h.Write(p.Ed25519[:])
	h.Write(p.X25519[:])
	return strings.ToLower(hex.EncodeToString(h.Sum(nil)[:16]))
}

func (p Public) Equal(other Public) bool {
	return p.Ed25519 == other.Ed25519 && p.X25519 == other.X25519
}

func (p Public) MatchesEd25519(pub []byte) bool {
	return len(pub) == len(p.Ed25519) && bytes.Equal(p.Ed25519[:], pub)
}

func (p Public) MatchesX25519(pub []byte) bool {
	return len(pub) == len(p.X25519) && bytes.Equal(p.X25519[:], pub)
}

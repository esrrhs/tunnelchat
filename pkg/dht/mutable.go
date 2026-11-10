package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"tunnelchat/pkg/candidate"
)

// BEP 44 mutable item stored by this node. v is opaque to the DHT besides
// size and signature checks. Expiration follows the 2-hour guidance in
// BEP 44; this is not durable storage.
type mutableItem struct {
	K      []byte
	Salt   []byte
	Seq    int64
	Sig    []byte
	V      []byte
	Expiry time.Time
}

func MutableTarget(pub ed25519.PublicKey, salt []byte) InfoHash {
	h := sha1.New()
	h.Write(pub)
	h.Write(salt)
	var out InfoHash
	copy(out[:], h.Sum(nil))
	return out
}

func signMutable(sk ed25519.PrivateKey, salt []byte, seq int64, v []byte) []byte {
	return ed25519.Sign(sk, mutableSignBuf(salt, seq, v))
}

func mutableSignBuf(salt []byte, seq int64, v []byte) []byte {
	var buf bytes.Buffer
	if len(salt) > 0 {
		fmt.Fprintf(&buf, "4:salt%d:%s", len(salt), salt)
	}
	fmt.Fprintf(&buf, "3:seqi%de1:v%d:", seq, len(v))
	buf.Write(v)
	return buf.Bytes()
}

func (n *Node) handleGet(args map[string]any, from *net.UDPAddr) map[string]any {
	target := targetOf(args["target"])
	tok := n.issueToken(from)
	r := map[string]any{"id": string(n.id[:]), "token": tok, "nodes": string(n.closestCompact(target))}
	n.mu.Lock()
	item, ok := n.mutable[target]
	n.mu.Unlock()
	if ok && time.Now().Before(item.Expiry) {
		r["k"] = string(item.K)
		r["seq"] = item.Seq
		r["sig"] = string(item.Sig)
		r["v"] = string(item.V)
		if len(item.Salt) > 0 {
			r["salt"] = string(item.Salt)
		}
	}
	return r
}

func (n *Node) handlePut(args map[string]any, from *net.UDPAddr) error {
	tok, _ := asString(args["token"])
	n.mu.Lock()
	ent, ok := n.issued[tok]
	n.mu.Unlock()
	if !ok || time.Now().After(ent.Expiry) || ent.IP != from.IP.String() {
		return errors.New("bad token")
	}
	k, _ := asString(args["k"])
	if len(k) != ed25519.PublicKeySize {
		return errors.New("invalid public key")
	}
	sig, _ := asString(args["sig"])
	if len(sig) != ed25519.SignatureSize {
		return errors.New("invalid signature")
	}
	v, _ := asString(args["v"])
	if len(v) > 1000 {
		return errors.New("message (v field) too big")
	}
	salt, _ := asString(args["salt"])
	if len(salt) > 64 {
		return errors.New("salt too big")
	}
	seq, _ := asInt(args["seq"])
	if seq < 0 {
		return errors.New("negative sequence")
	}
	if !ed25519.Verify([]byte(k), mutableSignBuf([]byte(salt), seq, []byte(v)), []byte(sig)) {
		return errors.New("invalid signature")
	}
	target := MutableTarget([]byte(k), []byte(salt))
	n.mu.Lock()
	defer n.mu.Unlock()
	if existing, ok := n.mutable[target]; ok && time.Now().Before(existing.Expiry) {
		if seq < existing.Seq {
			return errors.New("sequence number less than current")
		}
		if seq == existing.Seq && v != string(existing.V) {
			return errors.New("sequence number less than current")
		}
	}
	n.mutable[target] = mutableItem{
		K:      []byte(k),
		Salt:   []byte(salt),
		Seq:    seq,
		Sig:    []byte(sig),
		V:      []byte(v),
		Expiry: time.Now().Add(mutableTTL),
	}
	return nil
}

// PutMutable publishes a BEP 44-style signed item to known nodes.
func (n *Node) PutMutable(ctx context.Context, sk ed25519.PrivateKey, salt []byte, seq int64, v []byte) error {
	if len(v) > 1000 {
		return errors.New("value too large for BEP 44")
	}
	pk := sk.Public().(ed25519.PublicKey)
	sig := signMutable(sk, salt, seq, v)
	target := MutableTarget(pk, salt)
	var last error
	okN := 0
	for _, c := range n.snapshotTable() {
		if c.Addr == nil {
			continue
		}
		r, err := n.query(ctx, c.Addr, "get", map[string]any{"target": string(target[:])})
		if err != nil {
			last = err
			continue
		}
		tok, ok := asString(r["token"])
		if !ok {
			last = errors.New("no write token")
			continue
		}
		args := map[string]any{
			"k":     string(pk),
			"seq":   seq,
			"sig":   string(sig),
			"v":     string(v),
			"token": tok,
		}
		if len(salt) > 0 {
			args["salt"] = string(salt)
		}
		if _, err := n.query(ctx, c.Addr, "put", args); err != nil {
			last = err
			continue
		}
		okN++
	}
	if okN == 0 {
		if last == nil {
			last = errors.New("no DHT nodes accepted mutable put")
		}
		return last
	}
	return nil
}

func (n *Node) GetMutable(ctx context.Context, pub ed25519.PublicKey, salt []byte) (seq int64, v []byte, err error) {
	target := MutableTarget(pub, salt)
	var last error
	for _, c := range n.snapshotTable() {
		if c.Addr == nil {
			continue
		}
		r, e := n.query(ctx, c.Addr, "get", map[string]any{"target": string(target[:])})
		if e != nil {
			last = e
			continue
		}
		raw, ok := asString(r["v"])
		if !ok {
			continue
		}
		k, _ := asString(r["k"])
		sig, _ := asString(r["sig"])
		s, _ := asInt(r["seq"])
		if !ed25519.Verify([]byte(k), mutableSignBuf(salt, s, []byte(raw)), []byte(sig)) {
			last = errors.New("mutable signature invalid")
			continue
		}
		gotTarget := MutableTarget([]byte(k), salt)
		if gotTarget != target {
			last = errors.New("mutable key does not match target")
			continue
		}
		if s >= seq {
			seq = s
			v = []byte(raw)
		}
	}
	if v == nil {
		if last == nil {
			last = errors.New("mutable item not found")
		}
		return 0, nil, last
	}
	return seq, v, nil
}

const hintSalt = "tc2-cands"

// HintKey derives a symmetric key from the rendezvous secret. Only
// holders of the secret can read candidate hints.
func HintKey(secret []byte) []byte {
	r := hkdf.New(sha256.New, secret, []byte("tunnelchat"), []byte("cand-hints-v2"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(r, key); err != nil {
		panic(err)
	}
	return key
}

func SealHints(secret []byte, cands []candidate.Candidate) ([]byte, error) {
	// Public records must not include private addresses.
	safe := candidate.PublicOnly(cands)
	body, err := json.Marshal(safe)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(HintKey(secret))
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, body, []byte("tc2-hints"))...), nil
}

func OpenHints(secret, blob []byte) ([]candidate.Candidate, error) {
	aead, err := chacha20poly1305.New(HintKey(secret))
	if err != nil {
		return nil, err
	}
	if len(blob) < aead.NonceSize() {
		return nil, errors.New("hint too short")
	}
	nonce, ct := blob[:aead.NonceSize()], blob[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, ct, []byte("tc2-hints"))
	if err != nil {
		return nil, err
	}
	var cands []candidate.Candidate
	if err := json.Unmarshal(pt, &cands); err != nil {
		return nil, err
	}
	return candidate.PublicOnly(cands), nil
}

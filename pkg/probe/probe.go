package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/protocol"
)

const maxSkew = 2 * time.Minute

var (
	ErrUnauthenticated = errors.New("probe signature does not match the pinned identity")
	ErrReplay          = errors.New("replayed probe rejected")
	ErrExpired         = errors.New("probe timestamp outside allowed window")
)

// Message is a signed connectivity probe or acknowledgement. It does not
// establish a session; it only proves that a pinned identity can be
// reached at a given UDP path.
type Message struct {
	Ack       bool
	Timestamp int64
	Nonce     [16]byte
	Sender    [32]byte
	Echo      [16]byte
	Sig       []byte
}

func signBytes(ack bool, ts int64, nonce, echo [16]byte, sender, peerEd [32]byte) []byte {
	kind := byte(protocol.TypeProbe)
	if ack {
		kind = protocol.TypeProbeAck
	}
	buf := make([]byte, 0, 16+len(protocol.ProbeContext)+1+8+16+16+32+32)
	buf = append(buf, protocol.ProbeContext...)
	buf = append(buf, kind)
	var tsB [8]byte
	binary.BigEndian.PutUint64(tsB[:], uint64(ts))
	buf = append(buf, tsB[:]...)
	buf = append(buf, nonce[:]...)
	buf = append(buf, echo[:]...)
	buf = append(buf, sender[:]...)
	buf = append(buf, peerEd[:]...)
	return buf
}

func Encode(id *identity.Identity, peer identity.Public, ack bool, echo [16]byte) ([]byte, [16]byte, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nonce, err
	}
	if !ack {
		echo = [16]byte{}
	}
	ts := time.Now().Unix()
	var sender [32]byte
	copy(sender[:], id.Ed25519Public())
	toSign := signBytes(ack, ts, nonce, echo, sender, peer.Ed25519)
	sig := ed25519.Sign(id.Ed25519Private(), toSign)
	pkt := make([]byte, 4+1+1+8+16+16+32+64)
	copy(pkt[:4], protocol.Magic[:])
	if ack {
		pkt[4] = protocol.TypeProbeAck
	} else {
		pkt[4] = protocol.TypeProbe
	}
	pkt[5] = protocol.Version
	binary.BigEndian.PutUint64(pkt[6:14], uint64(ts))
	copy(pkt[14:30], nonce[:])
	copy(pkt[30:46], echo[:])
	copy(pkt[46:78], sender[:])
	copy(pkt[78:], sig)
	return pkt, nonce, nil
}

func Parse(pkt []byte) (Message, error) {
	var m Message
	if !protocol.HasMagic(pkt) || len(pkt) < 78+64 {
		return m, errors.New("not a probe packet")
	}
	switch pkt[4] {
	case protocol.TypeProbe:
	case protocol.TypeProbeAck:
		m.Ack = true
	default:
		return m, errors.New("not a probe packet")
	}
	if pkt[5] != protocol.Version {
		return m, errors.New("unsupported probe version")
	}
	m.Timestamp = int64(binary.BigEndian.Uint64(pkt[6:14]))
	copy(m.Nonce[:], pkt[14:30])
	copy(m.Echo[:], pkt[30:46])
	copy(m.Sender[:], pkt[46:78])
	m.Sig = append([]byte(nil), pkt[78:78+64]...)
	return m, nil
}

func IsProbe(pkt []byte) bool {
	return protocol.HasMagic(pkt) && len(pkt) > 5 && (pkt[4] == protocol.TypeProbe || pkt[4] == protocol.TypeProbeAck)
}

// Verifier checks signatures against a pinned identity and rejects replays.
type Verifier struct {
	mu   sync.Mutex
	seen map[string]int64
}

func NewVerifier() *Verifier {
	return &Verifier{seen: make(map[string]int64)}
}

// CheckFor verifies a probe addressed to local and signed by pinned.
func (v *Verifier) CheckFor(pkt []byte, pinned, local identity.Public) (Message, error) {
	m, err := Parse(pkt)
	if err != nil {
		return m, err
	}
	skew := time.Since(time.Unix(m.Timestamp, 0))
	if skew > maxSkew || skew < -maxSkew {
		return m, ErrExpired
	}
	if !pinned.MatchesEd25519(m.Sender[:]) {
		return m, ErrUnauthenticated
	}
	toSign := signBytes(m.Ack, m.Timestamp, m.Nonce, m.Echo, m.Sender, local.Ed25519)
	if !ed25519.Verify(pinned.Ed25519[:], toSign, m.Sig) {
		return m, ErrUnauthenticated
	}
	key := string(append(append([]byte{}, m.Sender[:]...), m.Nonce[:]...))
	v.mu.Lock()
	defer v.mu.Unlock()
	v.gcLocked()
	if _, ok := v.seen[key]; ok {
		return m, ErrReplay
	}
	v.seen[key] = m.Timestamp
	return m, nil
}

func (v *Verifier) gcLocked() {
	cut := time.Now().Add(-maxSkew * 2).Unix()
	for k, ts := range v.seen {
		if ts < cut {
			delete(v.seen, k)
		}
	}
}

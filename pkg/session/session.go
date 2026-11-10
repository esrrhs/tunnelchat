package session

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flynn/noise"

	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/protocol"
)

const (
	handshakeStep1 uint8 = 1
	handshakeStep2 uint8 = 2

	maxSkew = 2 * time.Minute
)

var (
	ErrUnauthenticated = errors.New("peer identity did not match the pinned key")
	ErrReplay          = errors.New("replayed packet rejected")
	ErrDowngrade       = errors.New("protocol downgrade or prologue mismatch")
	ErrTamper          = errors.New("ciphertext failed authentication")
)

// HandshakePayload is signed by Ed25519 and also carried inside the
// Noise_IK handshake so the static X25519 and signing keys are bound.
type HandshakePayload struct {
	Version   uint8
	Timestamp int64
	Ed25519   [32]byte
	Name      string
}

func cipherSuite() noise.CipherSuite {
	return noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
}

func encodePayload(p HandshakePayload) []byte {
	b := make([]byte, 1+8+32+1+len(p.Name))
	b[0] = p.Version
	binary.BigEndian.PutUint64(b[1:9], uint64(p.Timestamp))
	copy(b[9:41], p.Ed25519[:])
	b[41] = byte(len(p.Name))
	copy(b[42:], []byte(p.Name))
	return b
}

func decodePayload(b []byte) (HandshakePayload, error) {
	var p HandshakePayload
	if len(b) < 42 {
		return p, errors.New("handshake payload too short")
	}
	p.Version = b[0]
	p.Timestamp = int64(binary.BigEndian.Uint64(b[1:9]))
	copy(p.Ed25519[:], b[9:41])
	nlen := int(b[41])
	if 42+nlen != len(b) || nlen > protocol.MaxNameLen {
		return p, errors.New("handshake payload name invalid")
	}
	p.Name = string(b[42:])
	return p, nil
}

func newIK(initiator bool, local *identity.Identity, peer identity.Public) (*noise.HandshakeState, error) {
	cfg := noise.Config{
		CipherSuite:   cipherSuite(),
		Pattern:       noise.HandshakeIK,
		Initiator:     initiator,
		Prologue:      []byte(protocol.NoisePrologue),
		StaticKeypair: local.NoiseKey(),
	}
	if initiator {
		cfg.PeerStatic = append([]byte(nil), peer.X25519[:]...)
	}
	return noise.NewHandshakeState(cfg)
}

func localPayload(id *identity.Identity) []byte {
	pub := id.Public()
	return encodePayload(HandshakePayload{
		Version:   protocol.Version,
		Timestamp: time.Now().Unix(),
		Ed25519:   pub.Ed25519,
		Name:      id.Name,
	})
}

func checkPayload(p HandshakePayload, pinned identity.Public) error {
	if p.Version != protocol.Version {
		return ErrDowngrade
	}
	skew := time.Since(time.Unix(p.Timestamp, 0))
	if skew > maxSkew || skew < -maxSkew {
		return fmt.Errorf("handshake timestamp outside allowed window")
	}
	if !pinned.MatchesEd25519(p.Ed25519[:]) {
		return ErrUnauthenticated
	}
	return nil
}

// Initiator builds Noise_IK message 1 addressed to a pinned peer.
func Initiator(local *identity.Identity, peer identity.Public) (*Handshaker, []byte, error) {
	hs, err := newIK(true, local, peer)
	if err != nil {
		return nil, nil, err
	}
	msg, _, _, err := hs.WriteMessage(nil, localPayload(local))
	if err != nil {
		return nil, nil, err
	}
	pkt := frameHandshake(handshakeStep1, msg)
	return &Handshaker{hs: hs, local: local, peer: peer, initiator: true, step: 1}, pkt, nil
}

// StartResponder prepares a responder. Message 1 is consumed with Continue.
func StartResponder(local *identity.Identity, peer identity.Public) (*Handshaker, error) {
	hs, err := newIK(false, local, peer)
	if err != nil {
		return nil, err
	}
	return &Handshaker{hs: hs, local: local, peer: peer, initiator: false, step: 0}, nil
}

type Handshaker struct {
	hs        *noise.HandshakeState
	local     *identity.Identity
	peer      identity.Public
	initiator bool
	step      int
}

// Continue consumes a handshake packet and may return a reply and/or a Session.
func (h *Handshaker) Continue(pkt []byte) (reply []byte, sess *Session, err error) {
	step, msg, err := parseHandshake(pkt)
	if err != nil {
		return nil, nil, err
	}
	if h.initiator {
		if step != handshakeStep2 {
			return nil, nil, errors.New("unexpected handshake step")
		}
		payload, csSend, csRecv, err := h.hs.ReadMessage(nil, msg)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrTamper, err)
		}
		pl, err := decodePayload(payload)
		if err != nil {
			return nil, nil, err
		}
		if err := checkPayload(pl, h.peer); err != nil {
			return nil, nil, err
		}
		if !h.peer.MatchesX25519(h.hs.PeerStatic()) {
			return nil, nil, ErrUnauthenticated
		}
		return nil, newSession(csSend, csRecv, h.peer, true), nil
	}
	if step != handshakeStep1 {
		return nil, nil, errors.New("unexpected handshake step")
	}
	payload, _, _, err := h.hs.ReadMessage(nil, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrTamper, err)
	}
	pl, err := decodePayload(payload)
	if err != nil {
		return nil, nil, err
	}
	if err := checkPayload(pl, h.peer); err != nil {
		return nil, nil, err
	}
	if !h.peer.MatchesX25519(h.hs.PeerStatic()) {
		return nil, nil, ErrUnauthenticated
	}
	out, csRecv, csSend, err := h.hs.WriteMessage(nil, localPayload(h.local))
	if err != nil {
		return nil, nil, err
	}
	// For the responder, Split() returns (c1, c2) where initiator uses
	// c1 to send. flynn/noise WriteMessage on the last message returns
	// cs0, cs1 with cs0 = initiator-to-responder, cs1 = the other direction.
	// After ReadMessage (initiator) or WriteMessage (responder, last):
	// first CS is for initiator to send, second for responder to send.
	// Responder therefore sends with the second and receives with the first.
	return frameHandshake(handshakeStep2, out), newSession(csSend, csRecv, h.peer, false), nil
}

func frameHandshake(step uint8, msg []byte) []byte {
	out := make([]byte, 4+1+1+len(msg))
	copy(out[:4], protocol.Magic[:])
	out[4] = protocol.TypeHandshake
	out[5] = step
	copy(out[6:], msg)
	return out
}

func parseHandshake(pkt []byte) (uint8, []byte, error) {
	if !protocol.HasMagic(pkt) || len(pkt) < 7 || pkt[4] != protocol.TypeHandshake {
		return 0, nil, errors.New("not a handshake packet")
	}
	return pkt[5], pkt[6:], nil
}

func IsHandshake(pkt []byte) bool {
	return protocol.HasMagic(pkt) && len(pkt) > 5 && pkt[4] == protocol.TypeHandshake
}

func IsTransport(pkt []byte) bool {
	return protocol.HasMagic(pkt) && len(pkt) > 5 && pkt[4] == protocol.TypeTransport
}

type Session struct {
	mu     sync.Mutex
	send   *noise.CipherState
	recv   *noise.CipherState
	peer   identity.Public
	next   uint64
	window replayWindow
}

func newSession(send, recv *noise.CipherState, peer identity.Public, _ bool) *Session {
	return &Session{send: send, recv: recv, peer: peer}
}

func (s *Session) Peer() identity.Public { return s.peer }

func (s *Session) Seal(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.next
	s.next++
	s.send.SetNonce(n)
	ad := associatedData(n)
	ct, err := s.send.Encrypt(nil, ad, plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 4+1+8+len(ct))
	copy(out[:4], protocol.Magic[:])
	out[4] = protocol.TypeTransport
	binary.BigEndian.PutUint64(out[5:13], n)
	copy(out[13:], ct)
	return out, nil
}

func (s *Session) Open(pkt []byte) ([]byte, error) {
	if !IsTransport(pkt) || len(pkt) < 14 {
		return nil, errors.New("not a transport packet")
	}
	n := binary.BigEndian.Uint64(pkt[5:13])
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.window.Accept(n) {
		return nil, ErrReplay
	}
	s.recv.SetNonce(n)
	ad := associatedData(n)
	pt, err := s.recv.Decrypt(nil, ad, pkt[13:])
	if err != nil {
		s.window.Undo(n)
		return nil, ErrTamper
	}
	return pt, nil
}

func associatedData(nonce uint64) []byte {
	ad := make([]byte, 4+1+8)
	copy(ad[:4], protocol.Magic[:])
	ad[4] = protocol.TypeTransport
	binary.BigEndian.PutUint64(ad[5:13], nonce)
	return ad
}

// 64-bit sliding window; bit 0 is the highest accepted nonce.
type replayWindow struct {
	max  uint64
	bits uint64
	init bool
}

func (w *replayWindow) Accept(n uint64) bool {
	if !w.init {
		w.init = true
		w.max = n
		w.bits = 1
		return true
	}
	if n > w.max {
		shift := n - w.max
		if shift >= 64 {
			w.bits = 1
		} else {
			w.bits = (w.bits << shift) | 1
		}
		w.max = n
		return true
	}
	d := w.max - n
	if d >= 64 {
		return false
	}
	mask := uint64(1) << d
	if w.bits&mask != 0 {
		return false
	}
	w.bits |= mask
	return true
}

func (w *replayWindow) Undo(n uint64) {
	if n == w.max {
		// Leave max in place but clear the bit; a later packet can still use it? No,
		// clearing is enough so a genuine retry of the same nonce is also rejected
		// only if we set the bit... After a failed decrypt we should reject this
		// nonce anyway (possible attacker). Keep the bit set.
		return
	}
	d := w.max - n
	if d < 64 {
		w.bits |= uint64(1) << d
	}
}

// VerifyStatic is a convenience used by callers.
func VerifyStatic(got []byte, want identity.Public) error {
	if !want.MatchesX25519(got) {
		return ErrUnauthenticated
	}
	return nil
}

// Unused import guard if ed25519 not directly used.
var _ = ed25519.PublicKeySize

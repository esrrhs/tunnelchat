package presence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/protocol"
)

const (
	payloadVersion = 1
	MaxAge         = 5 * time.Minute
	maxCandidates  = 8
)

var (
	ErrInvalid   = errors.New("invalid presence record")
	ErrSignature = errors.New("presence signature is invalid")
	ErrExpired   = errors.New("presence timestamp outside allowed window")
	ErrDowngrade = errors.New("unsupported presence version")
	ErrReplay    = errors.New("replayed presence rejected")
)

// Record is a signed online advertisement. Name and public keys are
// bound together by the Ed25519 signature. Only public candidates should
// be included when the record may be widely visible.
type Record struct {
	Name       string
	Public     identity.Public
	Candidates []candidate.Candidate
	Timestamp  int64
	Nonce      [16]byte
}

func IsQuery(pkt []byte) bool {
	return protocol.HasMagic(pkt) && len(pkt) >= 6 && pkt[4] == protocol.TypePresenceQuery && pkt[5] == protocol.Version
}

func IsRecord(pkt []byte) bool {
	return protocol.HasMagic(pkt) && len(pkt) >= 6 && pkt[4] == protocol.TypePresence
}

func EncodeQuery() []byte {
	return []byte{protocol.Magic[0], protocol.Magic[1], protocol.Magic[2], protocol.Magic[3], protocol.TypePresenceQuery, protocol.Version}
}

func Encode(id *identity.Identity, cands []candidate.Candidate) ([]byte, error) {
	if id == nil {
		return nil, errors.New("identity required")
	}
	pub := id.Public()
	safe := candidate.PublicOnly(cands)
	if len(safe) > maxCandidates {
		safe = safe[:maxCandidates]
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	ts := time.Now().Unix()
	payload, err := marshal(pub.Name, pub, safe, ts, nonce)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(id.Ed25519Private(), signInput(payload))
	out := make([]byte, 6+len(payload)+ed25519.SignatureSize)
	copy(out[:4], protocol.Magic[:])
	out[4] = protocol.TypePresence
	out[5] = protocol.Version
	copy(out[6:], payload)
	copy(out[6+len(payload):], sig)
	return out, nil
}

func Verify(pkt []byte) (Record, error) {
	var rec Record
	if !IsRecord(pkt) {
		return rec, ErrInvalid
	}
	if pkt[5] != protocol.Version {
		return rec, ErrDowngrade
	}
	if len(pkt) < 6+ed25519.SignatureSize+1 {
		return rec, ErrInvalid
	}
	sig := pkt[len(pkt)-ed25519.SignatureSize:]
	payload := pkt[6 : len(pkt)-ed25519.SignatureSize]
	rec, err := unmarshal(payload)
	if err != nil {
		return rec, err
	}
	if !ed25519.Verify(rec.Public.Ed25519[:], signInput(payload), sig) {
		return rec, ErrSignature
	}
	skew := time.Since(time.Unix(rec.Timestamp, 0))
	if skew > MaxAge || skew < -MaxAge {
		return rec, ErrExpired
	}
	if rec.Public.Ed25519 == ([32]byte{}) || rec.Public.X25519 == ([32]byte{}) {
		return rec, ErrInvalid
	}
	return rec, nil
}

func signInput(payload []byte) []byte {
	return append([]byte(protocol.PresenceContext), payload...)
}

func marshal(name string, pub identity.Public, cands []candidate.Candidate, ts int64, nonce [16]byte) ([]byte, error) {
	if name == "" || len(name) > protocol.MaxNameLen {
		return nil, errors.New("invalid name")
	}
	var buf bytes.Buffer
	buf.WriteByte(payloadVersion)
	var tsB [8]byte
	binary.BigEndian.PutUint64(tsB[:], uint64(ts))
	buf.Write(tsB[:])
	buf.WriteByte(byte(len(name)))
	buf.WriteString(name)
	buf.Write(pub.Ed25519[:])
	buf.Write(pub.X25519[:])
	buf.Write(nonce[:])
	// Pre-filter so the count matches the written entries.
	var kept []candidate.Candidate
	for _, c := range cands {
		ip := net.ParseIP(c.IP)
		if ip == nil || c.Port <= 0 {
			continue
		}
		kept = append(kept, c)
	}
	buf.WriteByte(byte(len(kept)))
	for _, c := range kept {
		ip := net.ParseIP(c.IP)
		ipb := ip.To4()
		if ipb == nil {
			ipb = ip.To16()
		}
		if ipb == nil {
			continue
		}
		typ := byte(0)
		if c.Type == candidate.TypeSrflx {
			typ = 1
		} else if c.Type == candidate.TypeRelay {
			typ = 2
		}
		buf.WriteByte(typ)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], uint16(c.Port))
		buf.Write(port[:])
		buf.WriteByte(byte(len(ipb)))
		buf.Write(ipb)
	}
	return buf.Bytes(), nil
}

func unmarshal(b []byte) (Record, error) {
	var rec Record
	if len(b) < 1+8+1+32+32+16+1 {
		return rec, ErrInvalid
	}
	if b[0] != payloadVersion {
		return rec, ErrDowngrade
	}
	off := 1
	rec.Timestamp = int64(binary.BigEndian.Uint64(b[off : off+8]))
	off += 8
	nlen := int(b[off])
	off++
	if nlen == 0 || nlen > protocol.MaxNameLen || off+nlen+32+32+16+1 > len(b) {
		return rec, ErrInvalid
	}
	rec.Name = string(b[off : off+nlen])
	rec.Public.Name = rec.Name
	off += nlen
	copy(rec.Public.Ed25519[:], b[off:off+32])
	off += 32
	copy(rec.Public.X25519[:], b[off:off+32])
	off += 32
	copy(rec.Nonce[:], b[off:off+16])
	off += 16
	nc := int(b[off])
	off++
	for i := 0; i < nc; i++ {
		if off+4 > len(b) {
			return rec, ErrInvalid
		}
		typB := b[off]
		port := int(binary.BigEndian.Uint16(b[off+1 : off+3]))
		iplen := int(b[off+3])
		off += 4
		if iplen == 0 || off+iplen > len(b) {
			return rec, ErrInvalid
		}
		ip := net.IP(b[off : off+iplen]).String()
		off += iplen
		typ := candidate.TypeHost
		switch typB {
		case 1:
			typ = candidate.TypeSrflx
		case 2:
			typ = candidate.TypeRelay
		}
		c := candidate.New(typ, ip, port)
		if !c.Private {
			rec.Candidates = append(rec.Candidates, c)
		}
	}
	if off != len(b) {
		return rec, ErrInvalid
	}
	return rec, nil
}

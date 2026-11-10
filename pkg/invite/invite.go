package invite

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/protocol"
)

const prefix = "TC2INV1."

// Invite is an out-of-band, Ed25519-signed introduction. It is not a
// public DHT record. Optional candidates may include LAN addresses for
// same-network setup; they must not be copied into public announcements.
type Invite struct {
	Name       string
	Public     identity.Public
	Rendezvous [32]byte
	Candidates []candidate.Candidate
}

func Encode(id *identity.Identity, rendezvous [32]byte, cands []candidate.Candidate) (string, error) {
	if id == nil {
		return "", fmt.Errorf("identity required")
	}
	inv := Invite{
		Name:       id.Name,
		Public:     id.Public(),
		Rendezvous: rendezvous,
		Candidates: append([]candidate.Candidate(nil), cands...),
	}
	payload, err := marshalPayload(inv)
	if err != nil {
		return "", err
	}
	ctx := append([]byte(protocol.InviteContext), payload...)
	sig := ed25519.Sign(id.Ed25519Private(), ctx)
	return prefix + b64(payload) + "." + b64(sig), nil
}

func Decode(s string) (Invite, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, prefix) {
		return Invite{}, fmt.Errorf("not a tunnelchat v2 invite (legacy DES info strings are not supported)")
	}
	rest := strings.TrimPrefix(s, prefix)
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return Invite{}, fmt.Errorf("malformed invite")
	}
	payload, err := unb64(parts[0])
	if err != nil {
		return Invite{}, fmt.Errorf("invite payload: %w", err)
	}
	sig, err := unb64(parts[1])
	if err != nil {
		return Invite{}, fmt.Errorf("invite signature: %w", err)
	}
	inv, err := unmarshalPayload(payload)
	if err != nil {
		return Invite{}, err
	}
	ctx := append([]byte(protocol.InviteContext), payload...)
	if !ed25519.Verify(inv.Public.Ed25519[:], ctx, sig) {
		return Invite{}, fmt.Errorf("invite signature is invalid")
	}
	return inv, nil
}

func marshalPayload(inv Invite) ([]byte, error) {
	if len(inv.Name) == 0 || len(inv.Name) > protocol.MaxNameLen {
		return nil, fmt.Errorf("invalid name")
	}
	var cands []candidate.Candidate
	for _, c := range inv.Candidates {
		if net.ParseIP(c.IP) == nil || c.Port <= 0 {
			continue
		}
		cands = append(cands, c)
		if len(cands) == 16 {
			break
		}
	}
	var buf bytes.Buffer
	buf.WriteByte(1) // payload version
	buf.WriteByte(byte(len(inv.Name)))
	buf.WriteString(inv.Name)
	buf.Write(inv.Public.Ed25519[:])
	buf.Write(inv.Public.X25519[:])
	buf.Write(inv.Rendezvous[:])
	buf.WriteByte(byte(len(cands)))
	for _, c := range cands {
		ip := net.ParseIP(c.IP)
		var typ byte
		switch c.Type {
		case candidate.TypeSrflx:
			typ = 1
		case candidate.TypeRelay:
			typ = 2
		default:
			typ = 0
		}
		priv := byte(0)
		if c.Private {
			priv = 1
		}
		ipb := []byte(ip)
		if v4 := ip.To4(); v4 != nil {
			ipb = v4
		}
		buf.WriteByte(typ)
		buf.WriteByte(priv)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], uint16(c.Port))
		buf.Write(port[:])
		buf.WriteByte(byte(len(ipb)))
		buf.Write(ipb)
	}
	return buf.Bytes(), nil
}

func unmarshalPayload(b []byte) (Invite, error) {
	if len(b) < 1+1+32+32+32+1 {
		return Invite{}, fmt.Errorf("invite payload too short")
	}
	if b[0] != 1 {
		return Invite{}, fmt.Errorf("unsupported invite payload version %d", b[0])
	}
	off := 1
	nlen := int(b[off])
	off++
	if nlen == 0 || nlen > protocol.MaxNameLen || off+nlen+32+32+32+1 > len(b) {
		return Invite{}, fmt.Errorf("invalid invite name")
	}
	var inv Invite
	inv.Name = string(b[off : off+nlen])
	inv.Public.Name = inv.Name
	off += nlen
	copy(inv.Public.Ed25519[:], b[off:off+32])
	off += 32
	copy(inv.Public.X25519[:], b[off:off+32])
	off += 32
	copy(inv.Rendezvous[:], b[off:off+32])
	off += 32
	nc := int(b[off])
	off++
	for i := 0; i < nc; i++ {
		if off+5 > len(b) {
			return Invite{}, fmt.Errorf("truncated candidate")
		}
		typB := b[off]
		priv := b[off+1] == 1
		port := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
		iplen := int(b[off+4])
		off += 5
		if iplen == 0 || off+iplen > len(b) {
			return Invite{}, fmt.Errorf("truncated candidate ip")
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
		c := candidate.Candidate{Type: typ, IP: ip, Port: port, Private: priv}
		inv.Candidates = append(inv.Candidates, c)
	}
	if off != len(b) {
		return Invite{}, fmt.Errorf("invite payload trailing bytes")
	}
	return inv, nil
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func unb64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

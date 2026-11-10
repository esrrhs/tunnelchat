package protocol

import "encoding/binary"

// Magic identifies tunnelchat v2 datagrams. There is no fallback to the
// legacy DES/MD5 protocol.
var Magic = [4]byte{'T', 'C', '2', 0x01}

// RelayMagic identifies optional relay framing. Relay payloads are opaque
// ciphertext belonging to the application protocol.
var RelayMagic = [4]byte{'T', 'C', '2', 'R'}

const (
	Version = 2

	TypeHandshake     uint8 = 1
	TypeTransport     uint8 = 2
	TypeProbe         uint8 = 3
	TypeProbeAck      uint8 = 4
	TypePresenceQuery uint8 = 5
	TypePresence      uint8 = 6

	// Noise prologue binds both parties to this protocol revision.
	NoisePrologue = "tunnelchat-v2-noise-ik"

	InviteContext   = "tunnelchat-invite-v2"
	ProbeContext    = "tunnelchat-probe-v2"
	PresenceContext = "tunnelchat-presence-v2"

	MaxPacketSize = 1200
	MaxNameLen    = 64
)

func HasMagic(b []byte) bool {
	return len(b) >= 4 && b[0] == Magic[0] && b[1] == Magic[1] && b[2] == Magic[2] && b[3] == Magic[3]
}

func HasRelayMagic(b []byte) bool {
	return len(b) >= 4 && b[0] == RelayMagic[0] && b[1] == RelayMagic[1] && b[2] == RelayMagic[2] && b[3] == RelayMagic[3]
}

// IsSTUN reports whether b looks like an RFC 5389 STUN packet
// (leading type bits and magic cookie).
func IsSTUN(b []byte) bool {
	if len(b) < 20 {
		return false
	}
	// Most significant 2 bits of message type must be 0.
	if b[0]&0xc0 != 0 {
		return false
	}
	return binary.BigEndian.Uint32(b[4:8]) == 0x2112A442
}

// IsKRPC reports whether b looks like a bencoded KRPC dictionary
// used by BitTorrent DHT (BEP 5).
func IsKRPC(b []byte) bool {
	return len(b) > 0 && b[0] == 'd'
}

func PutU64(n uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	return b[:]
}

func U64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b[:8])
}

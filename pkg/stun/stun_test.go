package stun

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildAndParseSTUN(t *testing.T) {
	var txID [12]byte
	for i := range txID {
		txID[i] = byte(i + 1)
	}

	req := BuildBindingRequest(txID)
	if len(req) != 20 {
		t.Fatalf("expected 20 bytes request, got %d", len(req))
	}
	if binary.BigEndian.Uint16(req[0:2]) != BindingRequest {
		t.Errorf("expected BindingRequest type, got 0x%04X", binary.BigEndian.Uint16(req[0:2]))
	}

	// Construct a synthetic BindingResponse with MappedAddress attribute
	resp := make([]byte, 20+12)
	binary.BigEndian.PutUint16(resp[0:2], BindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], 12) // attribute length
	binary.BigEndian.PutUint32(resp[4:8], MagicCookie)
	copy(resp[8:20], txID[:])

	// Attribute: MappedAddress (type 0x0001, len 8)
	binary.BigEndian.PutUint16(resp[20:22], AttrMappedAddress)
	binary.BigEndian.PutUint16(resp[22:24], 8)
	resp[24] = 0x00 // reserved
	resp[25] = 0x01 // IPv4 family
	binary.BigEndian.PutUint16(resp[26:28], 54321)
	copy(resp[28:32], net.ParseIP("198.51.100.42").To4())

	addr, err := ParseMappedAddress(resp, txID)
	if err != nil {
		t.Fatalf("ParseMappedAddress failed: %v", err)
	}
	if addr.Port != 54321 {
		t.Errorf("expected port 54321, got %d", addr.Port)
	}
	if addr.IP.String() != "198.51.100.42" {
		t.Errorf("expected IP 198.51.100.42, got %s", addr.IP.String())
	}
}

func TestLocalOutboundIP(t *testing.T) {
	ip := GetLocalOutboundIP()
	if ip == "" {
		t.Fatal("expected valid IP address, got empty")
	}
	t.Logf("Discovered local outbound IP: %s", ip)
}

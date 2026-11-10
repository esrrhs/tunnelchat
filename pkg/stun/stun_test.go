package stun

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"tunnelchat/pkg/protocol"
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

	resp, err := BuildMappedResponse(txID, &Address{IP: net.ParseIP("198.51.100.42"), Port: 54321})
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.IsSTUN(resp) {
		t.Fatal("response should be classified as STUN")
	}
	addr, err := ParseMappedAddress(resp, txID)
	if err != nil {
		t.Fatalf("ParseMappedAddress failed: %v", err)
	}
	if addr.Port != 54321 {
		t.Errorf("expected port 54321, got %d", addr.Port)
	}
	if addr.IP.To4().String() != "198.51.100.42" {
		t.Errorf("expected IP 198.51.100.42, got %s", addr.IP.String())
	}
}

func TestSharedSocketUsesMappedPort(t *testing.T) {
	// Fake STUN reports a mapped port that is deliberately different
	// from the client's bind port. The client must keep that port.
	stunConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer stunConn.Close()

	mapped := &Address{IP: net.ParseIP("203.0.113.77"), Port: 41234}
	go func() { _ = ServeFake(stunConn, mapped) }()

	clientConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	bindPort := clientConn.LocalAddr().(*net.UDPAddr).Port
	if bindPort == mapped.Port {
		t.Fatal("test requires bind port != mapped port")
	}

	c := NewClient()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := clientConn.ReadFrom(buf)
			if err != nil {
				return
			}
			c.HandlePacket(addr, buf[:n])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := c.Discover(ctx, func(p []byte, addr net.Addr) (int, error) {
		return clientConn.WriteTo(p, addr)
	}, []string{stunConn.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != mapped.Port {
		t.Fatalf("expected mapped port %d, got %d (bind was %d)", mapped.Port, got.Port, bindPort)
	}
	if got.IP.To4().String() != "203.0.113.77" {
		t.Fatalf("expected mapped IP 203.0.113.77, got %s", got.IP)
	}
	_ = clientConn.SetReadDeadline(time.Now())
	<-done
}

func TestLocalOutboundIP(t *testing.T) {
	ip := GetLocalOutboundIP()
	if ip == "" {
		t.Fatal("expected valid IP address, got empty")
	}
}

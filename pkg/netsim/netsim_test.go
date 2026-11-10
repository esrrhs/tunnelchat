package netsim

import (
	"context"
	"net"
	"testing"
	"time"

	"tunnelchat/pkg/mux"
	"tunnelchat/pkg/stun"
)

func TestConeNATSharedSocketMappedPort(t *testing.T) {
	nat, err := NewNAT(Cone)
	if err != nil {
		t.Fatal(err)
	}
	defer nat.Close()

	stunConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer stunConn.Close()
	go func() { _ = stun.ServeFake(stunConn, nil) }() // reflect source

	host := nat.Host()
	m := mux.New(host)
	defer m.Close()

	client := stun.NewClient()
	go func() {
		buf := make([]byte, 2048)
		for {
			_ = m.STUN().SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := m.STUN().ReadFrom(buf)
			if err != nil {
				return
			}
			client.HandlePacket(addr, buf[:n])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	mapped, err := client.Discover(ctx, func(p []byte, addr net.Addr) (int, error) {
		return m.WriteTo(p, addr)
	}, []string{stunConn.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	wan := nat.WANAddr()
	if mapped.Port != wan.Port {
		t.Fatalf("expected STUN mapped port %d (WAN), got %d", wan.Port, mapped.Port)
	}
	if !mapped.IP.Equal(wan.IP) {
		t.Fatalf("expected mapped IP %s, got %s", wan.IP, mapped.IP)
	}
}

func TestSymmetricNATDifferentMappedPorts(t *testing.T) {
	nat, err := NewNAT(Symmetric)
	if err != nil {
		t.Fatal(err)
	}
	defer nat.Close()

	s1, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	s2, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	go func() { _ = stun.ServeFake(s1, nil) }()
	go func() { _ = stun.ServeFake(s2, nil) }()

	host := nat.Host()
	client := stun.NewClient()
	go func() {
		buf := make([]byte, 2048)
		for {
			_ = host.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := host.ReadFrom(buf)
			if err != nil {
				return
			}
			client.HandlePacket(addr, buf[:n])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a1, err := client.Discover(ctx, host.WriteTo, []string{s1.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := client.Discover(ctx, host.WriteTo, []string{s2.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if a1.Port == a2.Port {
		t.Fatalf("symmetric NAT should use different ports, both %d", a1.Port)
	}
}

func TestNoHairpinDropsSelf(t *testing.T) {
	nat, err := NewNAT(Cone)
	if err != nil {
		t.Fatal(err)
	}
	defer nat.Close()
	host := nat.Host()
	if _, err := host.WriteTo([]byte("hello"), nat.WANAddr()); err != nil {
		t.Fatal(err)
	}
	_ = host.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 16)
	if _, _, err := host.ReadFrom(buf); err == nil {
		t.Fatal("hairpin packet should have been dropped")
	}
}

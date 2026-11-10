package mux

import (
	"net"
	"testing"
	"time"

	"tunnelchat/pkg/protocol"
)

func TestDemuxSTUNAndApp(t *testing.T) {
	c1, c2 := localPair(t)
	defer c1.Close()
	defer c2.Close()
	m := New(c1)
	defer m.Close()

	stunP := make([]byte, 20)
	stunP[4] = 0x21
	stunP[5] = 0x12
	stunP[6] = 0xA4
	stunP[7] = 0x42
	if !protocol.IsSTUN(stunP) {
		t.Fatal("fixture is not STUN")
	}
	appP := append(protocol.Magic[:], []byte{protocol.TypeTransport, 1, 2, 3}...)
	dhtP := []byte("d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t2:aa1:y1:qe")

	if _, err := c2.WriteTo(stunP, c1.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.WriteTo(appP, c1.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.WriteTo(dhtP, c1.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	_ = m.STUN().SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := m.STUN().ReadFrom(buf)
	if err != nil || n != len(stunP) {
		t.Fatalf("stun: n=%d err=%v", n, err)
	}
	_ = m.App().SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = m.App().ReadFrom(buf)
	if err != nil || n != len(appP) {
		t.Fatalf("app: n=%d err=%v", n, err)
	}
	_ = m.DHT().SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = m.DHT().ReadFrom(buf)
	if err != nil || n != len(dhtP) {
		t.Fatalf("dht: n=%d err=%v", n, err)
	}
}

func localPair(t *testing.T) (*net.UDPConn, *net.UDPConn) {
	t.Helper()
	a, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	b, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	return a, b
}

package stun

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSTUNPort = 3478

	// STUN message types
	BindingRequest  = 0x0001
	BindingResponse = 0x0101

	// Magic Cookie (RFC 5389)
	MagicCookie = 0x2112A442

	// Attributes
	AttrMappedAddress    = 0x0001
	AttrResponseAddress  = 0x0002
	AttrChangeRequest    = 0x0003
	AttrSourceAddress    = 0x0004
	AttrChangedAddress   = 0x0005
	AttrXorMappedAddress = 0x0020
	AttrXorMappedAddressOld = 0x8020
)

type NatType int

const (
	NatTypeUnknown NatType = iota
	NatTypeOpen
	NatTypeFullCone
	NatTypeRestrictedCone
	NatTypePortRestrictedCone
	NatTypeSymmetric
	NatTypeBlocked
)

func (n NatType) String() string {
	switch n {
	case NatTypeOpen:
		return "Open Internet"
	case NatTypeFullCone:
		return "Full Cone NAT"
	case NatTypeRestrictedCone:
		return "Restricted Cone NAT"
	case NatTypePortRestrictedCone:
		return "Port Restricted Cone NAT"
	case NatTypeSymmetric:
		return "Symmetric NAT"
	case NatTypeBlocked:
		return "UDP Blocked"
	default:
		return "Unknown NAT"
	}
}

// StunAddress holds resolved IP and Port.
type StunAddress struct {
	IP   net.IP
	Port int
}

func (a StunAddress) String() string {
	return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
}

// BuildBindingRequest constructs a standard STUN binding request packet.
// It includes the RFC 5389 magic cookie while staying compatible with RFC 3489 servers.
func BuildBindingRequest(txID [12]byte) []byte {
	packet := make([]byte, 20)
	binary.BigEndian.PutUint16(packet[0:2], BindingRequest)
	binary.BigEndian.PutUint16(packet[2:4], 0) // Attribute length = 0
	binary.BigEndian.PutUint32(packet[4:8], MagicCookie)
	copy(packet[8:20], txID[:])
	return packet
}

// ParseMappedAddress extracts the mapped IP and port from a STUN binding response.
func ParseMappedAddress(data []byte, txID [12]byte) (*StunAddress, error) {
	if len(data) < 20 {
		return nil, errors.New("STUN packet too short")
	}

	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != BindingResponse {
		return nil, fmt.Errorf("unexpected STUN message type: 0x%04X", msgType)
	}

	msgLen := int(binary.BigEndian.Uint16(data[2:4]))
	if len(data) < 20+msgLen {
		return nil, errors.New("truncated STUN packet")
	}

	cookie := binary.BigEndian.Uint32(data[4:8])
	pos := 20
	end := 20 + msgLen

	var mappedAddr *StunAddress

	for pos+4 <= end {
		attrType := binary.BigEndian.Uint16(data[pos : pos+2])
		attrLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4

		if pos+attrLen > end {
			break
		}

		attrVal := data[pos : pos+attrLen]
		// Align to 4-byte boundary
		pos += (attrLen + 3) &^ 3

		switch attrType {
		case AttrMappedAddress:
			if len(attrVal) >= 8 && attrVal[1] == 0x01 { // IPv4
				port := int(binary.BigEndian.Uint16(attrVal[2:4]))
				ip := net.IPv4(attrVal[4], attrVal[5], attrVal[6], attrVal[7])
				mappedAddr = &StunAddress{IP: ip, Port: port}
			}

		case AttrXorMappedAddress, AttrXorMappedAddressOld:
			if len(attrVal) >= 8 && attrVal[1] == 0x01 { // IPv4
				xorPort := binary.BigEndian.Uint16(attrVal[2:4])
				port := int(xorPort ^ uint16(cookie>>16))

				xorIP := binary.BigEndian.Uint32(attrVal[4:8])
				rawIP := xorIP ^ cookie
				ipBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(ipBytes, rawIP)
				ip := net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3])

				mappedAddr = &StunAddress{IP: ip, Port: port}
			}
		}
	}

	if mappedAddr == nil {
		return nil, errors.New("no mapped address found in STUN response")
	}
	return mappedAddr, nil
}

// DiscoverPublicAddress queries a list of STUN servers to discover the mapped public IP and port.
// If localPort > 0, the query originates from that local UDP port.
func DiscoverPublicAddress(stunServers []string, localPort int) (*StunAddress, error) {
	var laddr *net.UDPAddr
	if localPort > 0 {
		laddr = &net.UDPAddr{Port: localPort}
	}

	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP on port %d: %w", localPort, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	var lastErr error
	for _, server := range stunServers {
		serverHost := server
		if !strings.Contains(serverHost, ":") {
			serverHost = net.JoinHostPort(serverHost, strconv.Itoa(DefaultSTUNPort))
		}

		raddr, err := net.ResolveUDPAddr("udp4", serverHost)
		if err != nil {
			lastErr = err
			continue
		}

		var txID [12]byte
		_, _ = rand.Read(txID[:])
		req := BuildBindingRequest(txID)

		if _, err := conn.WriteToUDP(req, raddr); err != nil {
			lastErr = err
			continue
		}

		buf := make([]byte, 1024)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				lastErr = err
				break
			}

			addr, err := ParseMappedAddress(buf[:n], txID)
			if err == nil && addr != nil {
				return addr, nil
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("failed to query any STUN server")
}

// GetLocalOutboundIP returns the local IP address used for outbound connections,
// providing a reliable fallback for LAN environments or when STUN servers are unreachable.
func GetLocalOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}

	// Fallback to checking local network interfaces
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

package stun

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"tunnelchat/pkg/protocol"
)

const (
	DefaultSTUNPort = 3478

	BindingRequest  = 0x0001
	BindingResponse = 0x0101

	MagicCookie = 0x2112A442

	AttrMappedAddress       = 0x0001
	AttrXorMappedAddress    = 0x0020
	AttrXorMappedAddressOld = 0x8020
)

// Address is a STUN mapped public endpoint.
type Address struct {
	IP   net.IP
	Port int
}

func (a Address) String() string {
	return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
}

func (a Address) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: a.IP, Port: a.Port}
}

func BuildBindingRequest(txID [12]byte) []byte {
	packet := make([]byte, 20)
	binary.BigEndian.PutUint16(packet[0:2], BindingRequest)
	binary.BigEndian.PutUint16(packet[2:4], 0)
	binary.BigEndian.PutUint32(packet[4:8], MagicCookie)
	copy(packet[8:20], txID[:])
	return packet
}

func ParseMappedAddress(data []byte, txID [12]byte) (*Address, error) {
	if len(data) < 20 {
		return nil, errors.New("STUN packet too short")
	}
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != BindingResponse {
		return nil, fmt.Errorf("unexpected STUN message type: 0x%04X", msgType)
	}
	if len(txID) == 12 && data[8:20] != nil {
		if string(data[8:20]) != string(txID[:]) {
			return nil, errors.New("STUN transaction id mismatch")
		}
	}
	msgLen := int(binary.BigEndian.Uint16(data[2:4]))
	if len(data) < 20+msgLen {
		return nil, errors.New("truncated STUN packet")
	}
	cookie := binary.BigEndian.Uint32(data[4:8])
	pos := 20
	end := 20 + msgLen
	var mapped, xorMapped *Address
	for pos+4 <= end {
		attrType := binary.BigEndian.Uint16(data[pos : pos+2])
		attrLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4
		if pos+attrLen > end {
			break
		}
		attrVal := data[pos : pos+attrLen]
		pos += (attrLen + 3) &^ 3
		switch attrType {
		case AttrMappedAddress:
			if addr, ok := parseClassicAddr(attrVal); ok {
				mapped = addr
			}
		case AttrXorMappedAddress, AttrXorMappedAddressOld:
			if addr, ok := parseXORAddr(attrVal, cookie, txID); ok {
				xorMapped = addr
			}
		}
	}
	if xorMapped != nil {
		return xorMapped, nil
	}
	if mapped != nil {
		return mapped, nil
	}
	return nil, errors.New("no mapped address found in STUN response")
}

func parseClassicAddr(attrVal []byte) (*Address, bool) {
	if len(attrVal) >= 8 && attrVal[1] == 0x01 {
		port := int(binary.BigEndian.Uint16(attrVal[2:4]))
		ip := net.IPv4(attrVal[4], attrVal[5], attrVal[6], attrVal[7])
		return &Address{IP: ip, Port: port}, true
	}
	if len(attrVal) >= 20 && attrVal[1] == 0x02 {
		port := int(binary.BigEndian.Uint16(attrVal[2:4]))
		ip := make(net.IP, 16)
		copy(ip, attrVal[4:20])
		return &Address{IP: ip, Port: port}, true
	}
	return nil, false
}

func parseXORAddr(attrVal []byte, cookie uint32, txID [12]byte) (*Address, bool) {
	if len(attrVal) >= 8 && attrVal[1] == 0x01 {
		xorPort := binary.BigEndian.Uint16(attrVal[2:4])
		port := int(xorPort ^ uint16(cookie>>16))
		xorIP := binary.BigEndian.Uint32(attrVal[4:8])
		rawIP := xorIP ^ cookie
		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, rawIP)
		return &Address{IP: net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3]), Port: port}, true
	}
	if len(attrVal) >= 20 && attrVal[1] == 0x02 {
		xorPort := binary.BigEndian.Uint16(attrVal[2:4])
		port := int(xorPort ^ uint16(cookie>>16))
		var mask [16]byte
		binary.BigEndian.PutUint32(mask[0:4], cookie)
		copy(mask[4:], txID[:])
		ip := make(net.IP, 16)
		for i := 0; i < 16; i++ {
			ip[i] = attrVal[4+i] ^ mask[i]
		}
		return &Address{IP: ip, Port: port}, true
	}
	return nil, false
}

// Writer is used to send a STUN request on an already-owned socket.
type Writer func(p []byte, addr net.Addr) (int, error)

// Client matches Binding Responses delivered by a shared demultiplexer
// to outstanding Discover calls. It never opens its own UDP socket.
type Client struct {
	mu      sync.Mutex
	pending map[[12]byte]chan *Address
}

func NewClient() *Client {
	return &Client{pending: make(map[[12]byte]chan *Address)}
}

func (c *Client) HandlePacket(_ net.Addr, data []byte) bool {
	if !protocol.IsSTUN(data) {
		return false
	}
	if len(data) < 20 {
		return false
	}
	var tx [12]byte
	copy(tx[:], data[8:20])
	addr, err := ParseMappedAddress(data, tx)
	if err != nil {
		return true
	}
	c.mu.Lock()
	ch := c.pending[tx]
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- addr:
		default:
		}
	}
	return true
}

// Discover queries servers using write (the shared socket). The mapped
// address — including the mapped port — belongs to that socket.
func (c *Client) Discover(ctx context.Context, write Writer, servers []string) (*Address, error) {
	var lastErr error
	for _, server := range servers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		addr, err := c.query(ctx, write, server)
		if err == nil {
			return addr, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no STUN servers configured")
	}
	return nil, lastErr
}

func (c *Client) query(ctx context.Context, write Writer, server string) (*Address, error) {
	host := server
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, strconv.Itoa(DefaultSTUNPort))
	}
	network := "udp"
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		// bare IPv6 without port
		host = net.JoinHostPort(host, strconv.Itoa(DefaultSTUNPort))
	}
	raddr, err := net.ResolveUDPAddr(network, host)
	if err != nil {
		return nil, err
	}
	var tx [12]byte
	if _, err := rand.Read(tx[:]); err != nil {
		return nil, err
	}
	ch := make(chan *Address, 1)
	c.mu.Lock()
	c.pending[tx] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, tx)
		c.mu.Unlock()
	}()

	req := BuildBindingRequest(tx)
	deadline := time.Now().Add(2 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	for time.Now().Before(deadline) {
		if _, err := write(req, raddr); err != nil {
			return nil, err
		}
		wait := 400 * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case addr := <-ch:
			return addr, nil
		case <-time.After(wait):
		}
	}
	return nil, fmt.Errorf("STUN timeout querying %s", server)
}

// GetLocalOutboundIP is a last-resort host hint when STUN is unavailable.
func GetLocalOutboundIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
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

// ServeFake is a test helper STUN server. If mapTo is nil, it reflects
// the request source address (XOR-MAPPED-ADDRESS).
func ServeFake(conn net.PacketConn, mapTo *Address) error {
	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		data := buf[:n]
		if !protocol.IsSTUN(data) || len(data) < 20 {
			continue
		}
		if binary.BigEndian.Uint16(data[0:2]) != BindingRequest {
			continue
		}
		var tx [12]byte
		copy(tx[:], data[8:20])
		mapped := mapTo
		if mapped == nil {
			udp, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}
			mapped = &Address{IP: udp.IP, Port: udp.Port}
		}
		resp, err := BuildMappedResponse(tx, mapped)
		if err != nil {
			continue
		}
		_, _ = conn.WriteTo(resp, addr)
	}
}

func BuildMappedResponse(txID [12]byte, addr *Address) ([]byte, error) {
	if addr == nil || addr.IP == nil {
		return nil, errors.New("no address")
	}
	ip4 := addr.IP.To4()
	var attr []byte
	if ip4 != nil {
		attr = make([]byte, 12)
		binary.BigEndian.PutUint16(attr[0:2], AttrXorMappedAddress)
		binary.BigEndian.PutUint16(attr[2:4], 8)
		attr[5] = 0x01
		xorPort := uint16(addr.Port) ^ uint16(MagicCookie>>16)
		binary.BigEndian.PutUint16(attr[6:8], xorPort)
		ipnum := binary.BigEndian.Uint32(ip4)
		binary.BigEndian.PutUint32(attr[8:12], ipnum^MagicCookie)
	} else {
		ip16 := addr.IP.To16()
		if ip16 == nil {
			return nil, errors.New("invalid IP")
		}
		attr = make([]byte, 24)
		binary.BigEndian.PutUint16(attr[0:2], AttrXorMappedAddress)
		binary.BigEndian.PutUint16(attr[2:4], 20)
		attr[5] = 0x02
		xorPort := uint16(addr.Port) ^ uint16(MagicCookie>>16)
		binary.BigEndian.PutUint16(attr[6:8], xorPort)
		var mask [16]byte
		binary.BigEndian.PutUint32(mask[0:4], MagicCookie)
		copy(mask[4:], txID[:])
		for i := 0; i < 16; i++ {
			attr[8+i] = ip16[i] ^ mask[i]
		}
	}
	resp := make([]byte, 20+len(attr))
	binary.BigEndian.PutUint16(resp[0:2], BindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(resp[4:8], MagicCookie)
	copy(resp[8:20], txID[:])
	copy(resp[20:], attr)
	return resp, nil
}

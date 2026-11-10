package candidate

import (
	"fmt"
	"net"
	"strings"
)

const (
	TypeHost  = "host"
	TypeSrflx = "srflx"
	TypeRelay = "relay"
)

// Candidate is a possible transport address for a peer.
type Candidate struct {
	Type    string `json:"type"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Private bool   `json:"private"`
}

func (c Candidate) Addr() string {
	return net.JoinHostPort(c.IP, fmt.Sprintf("%d", c.Port))
}

func (c Candidate) UDPAddr() (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp", c.Addr())
}

func (c Candidate) String() string {
	vis := "public"
	if c.Private {
		vis = "private"
	}
	return fmt.Sprintf("%s/%s/%s", c.Type, c.Addr(), vis)
}

func (c Candidate) Equal(other Candidate) bool {
	return c.Type == other.Type && c.IP == other.IP && c.Port == other.Port
}

// IsPrivateIP reports RFC1918, loopback, link-local, ULA, and unspecified addresses.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	// IPv6 Unique Local Address fc00::/7 (IsPrivate covers this in recent Go).
	return false
}

func New(typ, ip string, port int) Candidate {
	parsed := net.ParseIP(ip)
	return Candidate{
		Type:    typ,
		IP:      ip,
		Port:    port,
		Private: IsPrivateIP(parsed),
	}
}

// GatherHost collects local interface addresses for the bound port.
// Link-local and unspecified addresses are omitted; loopback is included
// so same-host tests and local chats work. Callers must not publish
// private candidates in public DHT records.
func GatherHost(port int) []Candidate {
	var out []Candidate
	seen := map[string]struct{}{}
	add := func(ip net.IP) {
		if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			return
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		typ := TypeHost
		out = append(out, New(typ, s, port))
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil {
					continue
				}
				if v4 := ip.To4(); v4 != nil {
					add(v4)
				} else {
					add(ip)
				}
			}
		}
	}
	if len(out) == 0 {
		add(net.ParseIP("127.0.0.1"))
	}
	return out
}

// PublicOnly returns candidates that may be placed in a public record.
func PublicOnly(in []Candidate) []Candidate {
	var out []Candidate
	for _, c := range in {
		if !c.Private && c.Port > 0 && c.IP != "" {
			out = append(out, c)
		}
	}
	return out
}

// Merge deduplicates candidates, preserving order.
func Merge(lists ...[]Candidate) []Candidate {
	var out []Candidate
	seen := map[string]struct{}{}
	for _, list := range lists {
		for _, c := range list {
			if c.IP == "" || c.Port <= 0 {
				continue
			}
			key := c.Type + "|" + c.Addr()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

func FormatList(cs []Candidate) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, ", ")
}

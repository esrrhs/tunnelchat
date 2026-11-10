package dht

import (
	"crypto/sha1"
	"strings"
)

// DefaultMainlineBootstraps are well-known public BitTorrent Mainline DHT
// routers. They are used only when the user goes online with DHT enabled
// and does not override the list. Tests must not apply this list unless
// they explicitly opt in, and must not contact these hosts.
var DefaultMainlineBootstraps = []string{
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"router.utorrent.com:6881",
	"dht.libtorrent.org:25401",
}

// PublicSwarmHash is the well-known BEP 5 infohash for tunnelchat v2
// stranger discovery. Announcing here advertises only the DHT-observed
// source IP and port (implied_port). Identity is established later with
// a signed presence exchange, not by trusting this IP alone.
func PublicSwarmHash() InfoHash {
	return sha1.Sum([]byte("tunnelchat-v2-public-swarm"))
}

// NormalizeBootstrap trims and drops empty entries.
func NormalizeBootstrap(in []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ResolveSettings decides whether DHT should run and which bootstraps to
// use. Public default routers are applied only when useDefaults is true
// and no explicit list was provided. Unit tests must leave useDefaults
// false so they never contact the internet.
func ResolveSettings(disabled bool, useDefaults bool, explicit, fromConfig []string) (enabled bool, bootstraps []string) {
	if disabled {
		return false, nil
	}
	bootstraps = NormalizeBootstrap(explicit)
	if len(bootstraps) == 0 {
		bootstraps = NormalizeBootstrap(fromConfig)
	}
	if len(bootstraps) == 0 && useDefaults {
		bootstraps = append([]string(nil), DefaultMainlineBootstraps...)
	}
	return len(bootstraps) > 0, bootstraps
}

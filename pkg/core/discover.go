package core

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/config"
	"tunnelchat/pkg/dht"
	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/presence"
)

const (
	maxDiscovered     = 64
	maxSwarmQueries   = 32
	queryCooldown     = 20 * time.Second
	presenceReplyGap  = 50 * time.Millisecond
	discoveryInterval = 20 * time.Second
)

// DiscoveredPeer is a stranger learned from a verified signed presence
// record. It is not a trusted friend until the user explicitly adds it.
type DiscoveredPeer struct {
	Public     identity.Public
	Candidates []candidate.Candidate
	Seen       time.Time
	Source     string
}

func (e *Engine) DHTEnabled() bool { return e.opts.DHTEnabled }

func (e *Engine) DHTBootstraps() []string {
	return append([]string(nil), e.opts.DHTBootstrap...)
}

func (e *Engine) dhtLoop() {
	defer e.wg.Done()
	e.publishDHT()
	e.discoverSwarm()
	t := time.NewTicker(dhtRefreshEvery)
	defer t.Stop()
	d := time.NewTicker(discoveryInterval)
	defer d.Stop()
	// Quick retries so two peers coming online together can meet.
	quick := time.NewTimer(400 * time.Millisecond)
	defer quick.Stop()
	quickLeft := 6
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.publishDHT()
		case <-d.C:
			e.discoverSwarm()
		case <-quick.C:
			e.publishDHT()
			e.discoverSwarm()
			quickLeft--
			if quickLeft > 0 {
				quick.Reset(400 * time.Millisecond)
			}
		}
	}
}

func (e *Engine) publishDHT() {
	if e.dht == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	ih := dht.InfoHashFromSecret(e.rv[:])
	if err := e.dht.Announce(ctx, ih); err != nil {
		e.log("DHT announce: %v", err)
	}
	if err := e.dht.Announce(ctx, dht.PublicSwarmHash()); err != nil {
		e.log("DHT swarm announce: %v", err)
	}
	// Encrypted public hints only (no private addresses), for friends
	// who already know our rendezvous secret.
	e.mu.Lock()
	cands := append([]candidate.Candidate(nil), e.cands...)
	seq := e.seqHint["self"] + 1
	e.seqHint["self"] = seq
	e.mu.Unlock()
	blob, err := dht.SealHints(e.rv[:], cands)
	if err != nil {
		return
	}
	_ = e.dht.PutMutable(ctx, e.id.Ed25519Private(), []byte("tc2-cands"), seq, blob)
}

func (e *Engine) discoverSwarm() {
	if e.dht == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	peers, err := e.dht.GetPeers(ctx, dht.PublicSwarmHash())
	if err != nil {
		e.log("DHT swarm lookup: %v", err)
		return
	}
	self := e.conn.LocalAddr()
	n := 0
	for _, p := range peers {
		if n >= maxSwarmQueries {
			break
		}
		addr := p.UDPAddr()
		if addr == nil {
			continue
		}
		if self != nil && addr.String() == self.String() {
			continue
		}
		if e.recentlyQueried(addr.String()) {
			continue
		}
		e.markQueried(addr.String())
		_, _ = e.mux.WriteTo(presence.EncodeQuery(), addr)
		if pkt, err := e.ownPresence(); err == nil {
			_, _ = e.mux.WriteTo(pkt, addr)
		}
		n++
	}
}

func (e *Engine) ownPresence() ([]byte, error) {
	e.mu.Lock()
	cands := append([]candidate.Candidate(nil), e.cands...)
	e.mu.Unlock()
	return presence.Encode(e.id, cands)
}

func (e *Engine) recentlyQueried(addr string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.querySeen[addr]
	return ok && time.Since(t) < queryCooldown
}

func (e *Engine) markQueried(addr string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.querySeen[addr] = time.Now()
	cut := time.Now().Add(-2 * time.Minute)
	for k, ts := range e.querySeen {
		if ts.Before(cut) {
			delete(e.querySeen, k)
		}
	}
}

func (e *Engine) handlePresenceQuery(from net.Addr) {
	e.mu.Lock()
	if time.Since(e.presenceRL) < presenceReplyGap {
		e.mu.Unlock()
		return
	}
	e.presenceRL = time.Now()
	e.mu.Unlock()
	pkt, err := e.ownPresence()
	if err != nil {
		return
	}
	_, _ = e.mux.WriteTo(pkt, from)
}

func (e *Engine) handlePresenceRecord(pkt []byte, from net.Addr) {
	rec, err := presence.Verify(pkt)
	if err != nil {
		return
	}
	if rec.Public.Equal(e.id.Public()) {
		return
	}
	nonceKey := string(rec.Public.Ed25519[:]) + string(rec.Nonce[:])
	e.mu.Lock()
	if _, ok := e.seenNonce[nonceKey]; ok {
		e.mu.Unlock()
		return
	}
	e.seenNonce[nonceKey] = rec.Timestamp
	if len(e.seenNonce) > 512 {
		for k := range e.seenNonce {
			delete(e.seenNonce, k)
			if len(e.seenNonce) <= 256 {
				break
			}
		}
	}
	e.mu.Unlock()

	// The path we received on is a useful candidate; keep private IPs
	// only as a local connect hint, never re-published in presence.
	cands := append([]candidate.Candidate(nil), rec.Candidates...)
	if ua, ok := from.(*net.UDPAddr); ok && ua.IP != nil && ua.Port > 0 {
		c := candidate.New(candidate.TypeSrflx, ua.IP.String(), ua.Port)
		cands = candidate.Merge(cands, []candidate.Candidate{c})
	}
	e.rememberDiscovered(rec.Public, cands, "signed-presence")
}

func (e *Engine) rememberDiscovered(pub identity.Public, cands []candidate.Candidate, source string) {
	key := friendKey(pub)
	e.mu.Lock()
	_, existed := e.discovered[key]
	if existed {
		existing := e.discovered[key]
		existing.Public = pub
		existing.Candidates = candidate.Merge(existing.Candidates, cands)
		existing.Seen = time.Now()
		existing.Source = source
		e.mu.Unlock()
		return
	}
	if len(e.discovered) >= maxDiscovered {
		var oldest string
		var when time.Time
		for k, d := range e.discovered {
			if oldest == "" || d.Seen.Before(when) {
				oldest, when = k, d.Seen
			}
		}
		delete(e.discovered, oldest)
	}
	e.discovered[key] = &DiscoveredPeer{
		Public:     pub,
		Candidates: cands,
		Seen:       time.Now(),
		Source:     source,
	}
	e.mu.Unlock()
	e.log("discovered peer %s fingerprint %s (not auto-added as a friend)", pub.Name, pub.Fingerprint())
}

func (e *Engine) ListDiscovered() []DiscoveredPeer {
	e.mu.Lock()
	defer e.mu.Unlock()
	cut := time.Now().Add(-presence.MaxAge)
	var out []DiscoveredPeer
	for k, d := range e.discovered {
		if d.Seen.Before(cut) {
			delete(e.discovered, k)
			continue
		}
		cp := *d
		cp.Candidates = append([]candidate.Candidate(nil), d.Candidates...)
		out = append(out, cp)
	}
	return out
}

func (e *Engine) lookupDiscovered(fp string) (DiscoveredPeer, bool) {
	fp = strings.ToLower(strings.TrimSpace(fp))
	if fp == "" {
		return DiscoveredPeer{}, false
	}
	var matches []DiscoveredPeer
	for _, d := range e.ListDiscovered() {
		got := strings.ToLower(d.Public.Fingerprint())
		if got == fp || strings.HasPrefix(got, fp) || strings.EqualFold(d.Public.Name, fp) {
			matches = append(matches, d)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return DiscoveredPeer{}, false
}

// ConnectDiscovered starts an authenticated session with a discovered
// stranger. It does not write them into the trusted friend list.
func (e *Engine) ConnectDiscovered(fingerprint string) error {
	d, ok := e.lookupDiscovered(fingerprint)
	if !ok {
		return fmt.Errorf("no unique discovered peer matching %q; use /peers", fingerprint)
	}
	f := discoveredAsFriend(d)
	go e.connectFriend(f)
	return nil
}

// AddDiscovered pins a discovered stranger as a trusted friend and
// starts a connection. It never runs implicitly.
func (e *Engine) AddDiscovered(fingerprint string) error {
	d, ok := e.lookupDiscovered(fingerprint)
	if !ok {
		return fmt.Errorf("no unique discovered peer matching %q; use /peers", fingerprint)
	}
	f := discoveredAsFriend(d)
	if err := e.cfg.AddFriend(f); err != nil {
		return err
	}
	if err := e.cfg.Save(); err != nil {
		return err
	}
	e.log("pinned friend %s fingerprint %s (from discovery)", f.Name, d.Public.Fingerprint())
	if e.cancel != nil {
		go e.connectFriend(f)
	}
	return nil
}

func discoveredAsFriend(d DiscoveredPeer) config.Friend {
	return config.Friend{
		Name:       d.Public.Name,
		Ed25519:    d.Public.Ed25519[:],
		X25519:     d.Public.X25519[:],
		Candidates: d.Candidates,
	}
}

func (e *Engine) knownIdentities() []identity.Public {
	var out []identity.Public
	for _, f := range e.cfg.ListFriends() {
		if pub, err := f.Public(); err == nil {
			out = append(out, pub)
		}
	}
	for _, d := range e.ListDiscovered() {
		out = append(out, d.Public)
	}
	return out
}

func (e *Engine) friendOrDiscovered(pub identity.Public) (config.Friend, bool) {
	if f, ok := e.cfg.GetFriendByEd25519(pub.Ed25519[:]); ok {
		return f, true
	}
	e.mu.Lock()
	d, ok := e.discovered[friendKey(pub)]
	e.mu.Unlock()
	if !ok {
		return config.Friend{}, false
	}
	return discoveredAsFriend(*d), true
}

func (e *Engine) IsTrustedFriend(nameOrFP string) bool {
	if f, ok := e.cfg.GetFriendByName(nameOrFP); ok {
		return f.Name != ""
	}
	for _, f := range e.cfg.ListFriends() {
		if pub, err := f.Public(); err == nil && strings.EqualFold(pub.Fingerprint(), nameOrFP) {
			return true
		}
	}
	return false
}

package dht

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	tokenTTL   = 10 * time.Minute
	peerTTL    = 30 * time.Minute
	mutableTTL = 2 * time.Hour
	rpcTimeout = 1500 * time.Millisecond
	maxPacket  = 1500
)

type NodeID [20]byte
type InfoHash [20]byte

type Peer struct {
	IP   net.IP
	Port int
}

func (p Peer) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: p.IP, Port: p.Port}
}

type contact struct {
	ID   NodeID
	Addr *net.UDPAddr
}

type peerEntry struct {
	Peer
	Expiry time.Time
}

type tokenEntry struct {
	Token  string
	IP     string
	Expiry time.Time
}

type pending struct {
	ch chan map[string]any
}

// Node is a compact Mainline-compatible DHT participant (BEP 5). It is
// intended to be bound to the shared UDP port via mux.DHT(). It never
// contacts a public bootstrap unless the caller supplies one.
type Node struct {
	id   NodeID
	conn net.PacketConn

	mu      sync.Mutex
	table   []contact
	peers   map[InfoHash][]peerEntry
	tokens  map[string]tokenEntry // remoteIP -> last token we issued
	issued  map[string]tokenEntry // token -> bound IP
	mutable map[InfoHash]mutableItem
	pending map[string]*pending
	secret  [16]byte

	closed chan struct{}
	wg     sync.WaitGroup
}

func NewNode(conn net.PacketConn, id NodeID) *Node {
	if id == ([20]byte{}) {
		if _, err := rand.Read(id[:]); err != nil {
			panic(err)
		}
	}
	n := &Node{
		id:      id,
		conn:    conn,
		peers:   make(map[InfoHash][]peerEntry),
		tokens:  make(map[string]tokenEntry),
		issued:  make(map[string]tokenEntry),
		mutable: make(map[InfoHash]mutableItem),
		pending: make(map[string]*pending),
		closed:  make(chan struct{}),
	}
	if _, err := rand.Read(n.secret[:]); err != nil {
		panic(err)
	}
	n.wg.Add(1)
	go n.readLoop()
	return n
}

func (n *Node) Close() {
	select {
	case <-n.closed:
		return
	default:
		close(n.closed)
	}
	_ = n.conn.Close()
	n.wg.Wait()
}

func (n *Node) ID() NodeID { return n.id }

func (n *Node) LocalAddr() net.Addr { return n.conn.LocalAddr() }

func (n *Node) AddBootstrap(addr *net.UDPAddr) {
	if addr == nil {
		return
	}
	n.mu.Lock()
	n.table = append(n.table, contact{Addr: addr})
	n.mu.Unlock()
}

func (n *Node) readLoop() {
	defer n.wg.Done()
	buf := make([]byte, maxPacket)
	for {
		_ = n.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		num, addr, err := n.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-n.closed:
				return
			default:
				continue
			}
		}
		udp, ok := addr.(*net.UDPAddr)
		if !ok {
			if resolved, err := net.ResolveUDPAddr("udp", addr.String()); err == nil {
				udp = resolved
			} else {
				continue
			}
		}
		n.handle(buf[:num], udp)
	}
}

func (n *Node) handle(data []byte, from *net.UDPAddr) {
	v, err := decode(data)
	if err != nil {
		return
	}
	msg, ok := asDict(v)
	if !ok {
		return
	}
	y, _ := asString(msg["y"])
	t, _ := asString(msg["t"])
	switch y {
	case "q":
		n.handleQuery(msg, t, from)
	case "r":
		n.handleResponse(t, msg)
	}
}

func (n *Node) handleResponse(tid string, msg map[string]any) {
	n.mu.Lock()
	p := n.pending[tid]
	n.mu.Unlock()
	if p == nil {
		return
	}
	r, _ := asDict(msg["r"])
	select {
	case p.ch <- r:
	default:
	}
}

func (n *Node) handleQuery(msg map[string]any, tid string, from *net.UDPAddr) {
	q, _ := asString(msg["q"])
	args, _ := asDict(msg["a"])
	if args == nil {
		return
	}
	if id, ok := asString(args["id"]); ok && len(id) == 20 {
		var nid NodeID
		copy(nid[:], id)
		n.learn(nid, from)
	}
	var resp map[string]any
	switch q {
	case "ping":
		resp = map[string]any{"id": string(n.id[:])}
	case "find_node":
		resp = map[string]any{"id": string(n.id[:]), "nodes": string(n.closestCompact(targetOf(args["target"])))}
	case "get_peers":
		ih := targetOf(args["info_hash"])
		tok := n.issueToken(from)
		r := map[string]any{"id": string(n.id[:]), "token": tok, "nodes": string(n.closestCompact(ih))}
		if values := n.peerValues(ih); len(values) > 0 {
			var list []any
			for _, v := range values {
				list = append(list, v)
			}
			r["values"] = list
		}
		resp = r
	case "announce_peer":
		if err := n.storeAnnounce(args, from); err != nil {
			n.sendError(tid, from, 203, err.Error())
			return
		}
		resp = map[string]any{"id": string(n.id[:])}
	case "get":
		resp = n.handleGet(args, from)
	case "put":
		if err := n.handlePut(args, from); err != nil {
			n.sendError(tid, from, 206, err.Error())
			return
		}
		resp = map[string]any{"id": string(n.id[:])}
	default:
		n.sendError(tid, from, 204, "Method Unknown")
		return
	}
	n.send(map[string]any{"t": tid, "y": "r", "r": resp}, from)
}

func targetOf(v any) InfoHash {
	var h InfoHash
	s, ok := asString(v)
	if ok && len(s) >= 20 {
		copy(h[:], s[:20])
	}
	return h
}

func (n *Node) learn(id NodeID, addr *net.UDPAddr) {
	if addr == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, c := range n.table {
		if c.ID == id || (c.Addr != nil && c.Addr.String() == addr.String()) {
			n.table[i] = contact{ID: id, Addr: cloneUDP(addr)}
			return
		}
	}
	n.table = append(n.table, contact{ID: id, Addr: cloneUDP(addr)})
	if len(n.table) > 64 {
		n.table = n.table[len(n.table)-64:]
	}
}

func cloneUDP(a *net.UDPAddr) *net.UDPAddr {
	ip := make(net.IP, len(a.IP))
	copy(ip, a.IP)
	return &net.UDPAddr{IP: ip, Port: a.Port}
}

func (n *Node) closestCompact(target InfoHash) []byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []byte
	count := 0
	for _, c := range n.table {
		if c.Addr == nil || c.ID == ([20]byte{}) {
			continue
		}
		ip := c.Addr.IP.To4()
		if ip == nil {
			continue
		}
		var rec [26]byte
		copy(rec[0:20], c.ID[:])
		copy(rec[20:24], ip)
		binary.BigEndian.PutUint16(rec[24:26], uint16(c.Addr.Port))
		out = append(out, rec[:]...)
		count++
		if count >= 8 {
			break
		}
	}
	return out
}

func (n *Node) peerValues(ih InfoHash) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	var out []string
	list := n.peers[ih]
	kept := list[:0]
	for _, p := range list {
		if p.Expiry.Before(now) {
			continue
		}
		kept = append(kept, p)
		ip := p.IP.To4()
		if ip == nil {
			continue
		}
		var rec [6]byte
		copy(rec[0:4], ip)
		binary.BigEndian.PutUint16(rec[4:6], uint16(p.Port))
		out = append(out, string(rec[:]))
	}
	if len(kept) == 0 {
		delete(n.peers, ih)
	} else {
		n.peers[ih] = kept
	}
	return out
}

func (n *Node) issueToken(from *net.UDPAddr) string {
	raw := sha1.Sum(append(append(n.secret[:], []byte(from.IP.String())...), byte(from.Port>>8), byte(from.Port)))
	tok := string(raw[:8])
	n.mu.Lock()
	n.issued[tok] = tokenEntry{Token: tok, IP: from.IP.String(), Expiry: time.Now().Add(tokenTTL)}
	n.tokens[from.IP.String()] = n.issued[tok]
	n.mu.Unlock()
	return tok
}

func (n *Node) storeAnnounce(args map[string]any, from *net.UDPAddr) error {
	tok, _ := asString(args["token"])
	n.mu.Lock()
	ent, ok := n.issued[tok]
	n.mu.Unlock()
	if !ok || time.Now().After(ent.Expiry) {
		return errors.New("bad token")
	}
	if ent.IP != from.IP.String() {
		return errors.New("token does not match source IP")
	}
	// Source IP is stored as the peer contact; claimed IPs are ignored.
	ih := targetOf(args["info_hash"])
	port := 0
	if implied, ok := asInt(args["implied_port"]); ok && implied != 0 {
		port = from.Port
	} else if p, ok := asInt(args["port"]); ok {
		port = int(p)
	} else {
		port = from.Port
	}
	if port <= 0 || port > 65535 {
		return errors.New("bad port")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	list := n.peers[ih]
	now := time.Now()
	replaced := false
	for i := range list {
		if list[i].IP.Equal(from.IP) && list[i].Port == port {
			list[i].Expiry = now.Add(peerTTL)
			replaced = true
		}
	}
	if !replaced {
		list = append(list, peerEntry{Peer: Peer{IP: append(net.IP(nil), from.IP...), Port: port}, Expiry: now.Add(peerTTL)})
	}
	n.peers[ih] = list
	return nil
}

func (n *Node) sendError(tid string, to *net.UDPAddr, code int, msg string) {
	n.send(map[string]any{"t": tid, "y": "e", "e": []any{int64(code), msg}}, to)
}

func (n *Node) send(msg map[string]any, to *net.UDPAddr) {
	data, err := encode(msg)
	if err != nil {
		return
	}
	_, _ = n.conn.WriteTo(data, to)
}

func (n *Node) query(ctx context.Context, to *net.UDPAddr, q string, args map[string]any) (map[string]any, error) {
	if to == nil {
		return nil, errors.New("nil address")
	}
	var tid [2]byte
	_, _ = rand.Read(tid[:])
	id := string(tid[:])
	args["id"] = string(n.id[:])
	msg := map[string]any{"t": id, "y": "q", "q": q, "a": args}
	p := &pending{ch: make(chan map[string]any, 1)}
	n.mu.Lock()
	n.pending[id] = p
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, id)
		n.mu.Unlock()
	}()
	n.send(msg, to)
	timeout := rpcTimeout
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < timeout {
			timeout = d
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-p.ch:
		if r != nil {
			if id, ok := asString(r["id"]); ok && len(id) == 20 {
				var nid NodeID
				copy(nid[:], id)
				n.learn(nid, to)
			}
		}
		return r, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("dht rpc %s timeout", q)
	}
}

// GetPeers walks known nodes (including bootstraps) for BEP 5 get_peers.
func (n *Node) GetPeers(ctx context.Context, ih InfoHash) ([]Peer, error) {
	nodes := n.snapshotTable()
	seen := map[string]struct{}{}
	var found []Peer
	for _, c := range nodes {
		if ctx.Err() != nil {
			break
		}
		if c.Addr == nil {
			continue
		}
		key := c.Addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		r, err := n.query(ctx, c.Addr, "get_peers", map[string]any{"info_hash": string(ih[:])})
		if err != nil {
			continue
		}
		if vals, ok := asList(r["values"]); ok {
			for _, v := range vals {
				s, ok := asString(v)
				if !ok {
					continue
				}
				if p, ok := parseCompactPeer(s); ok {
					found = append(found, p)
				}
			}
		}
		n.ingestNodes(r["nodes"])
	}
	// Second pass for newly learned nodes.
	for _, c := range n.snapshotTable() {
		if c.Addr == nil {
			continue
		}
		key := c.Addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		r, err := n.query(ctx, c.Addr, "get_peers", map[string]any{"info_hash": string(ih[:])})
		if err != nil {
			continue
		}
		if vals, ok := asList(r["values"]); ok {
			for _, v := range vals {
				s, ok := asString(v)
				if !ok {
					continue
				}
				if p, ok := parseCompactPeer(s); ok {
					found = append(found, p)
				}
			}
		}
	}
	return uniquePeers(found), nil
}

// Announce publishes this node's source address (implied_port) under ih.
func (n *Node) Announce(ctx context.Context, ih InfoHash) error {
	nodes := n.snapshotTable()
	var last error
	announced := 0
	for _, c := range nodes {
		if c.Addr == nil {
			continue
		}
		r, err := n.query(ctx, c.Addr, "get_peers", map[string]any{"info_hash": string(ih[:])})
		if err != nil {
			last = err
			continue
		}
		tok, ok := asString(r["token"])
		if !ok {
			last = errors.New("no write token")
			continue
		}
		_, err = n.query(ctx, c.Addr, "announce_peer", map[string]any{
			"info_hash":    string(ih[:]),
			"port":         1,
			"implied_port": int64(1),
			"token":        tok,
		})
		if err != nil {
			last = err
			continue
		}
		announced++
	}
	if announced == 0 {
		if last == nil {
			last = errors.New("no DHT nodes to announce to")
		}
		return last
	}
	return nil
}

func (n *Node) snapshotTable() []contact {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]contact, len(n.table))
	copy(out, n.table)
	return out
}

func (n *Node) ingestNodes(v any) {
	s, ok := asString(v)
	if !ok || len(s)%26 != 0 {
		return
	}
	b := []byte(s)
	for i := 0; i+26 <= len(b); i += 26 {
		var id NodeID
		copy(id[:], b[i:i+20])
		ip := net.IPv4(b[i+20], b[i+21], b[i+22], b[i+23])
		port := int(binary.BigEndian.Uint16(b[i+24 : i+26]))
		n.learn(id, &net.UDPAddr{IP: ip, Port: port})
	}
}

func parseCompactPeer(s string) (Peer, bool) {
	b := []byte(s)
	if len(b) != 6 {
		return Peer{}, false
	}
	return Peer{IP: net.IPv4(b[0], b[1], b[2], b[3]), Port: int(binary.BigEndian.Uint16(b[4:6]))}, true
}

func uniquePeers(in []Peer) []Peer {
	seen := map[string]struct{}{}
	var out []Peer
	for _, p := range in {
		k := p.UDPAddr().String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, p)
	}
	return out
}

// InfoHashFromSecret returns a 20-byte BEP 5 infohash derived from a
// rendezvous secret. The secret itself is never placed in the DHT.
func InfoHashFromSecret(secret []byte) InfoHash {
	sum := sha1.Sum(append([]byte("tunnelchat-rv-v2"), secret...))
	return sum
}

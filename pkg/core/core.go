package core

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/config"
	"tunnelchat/pkg/dht"
	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/invite"
	"tunnelchat/pkg/mux"
	"tunnelchat/pkg/presence"
	"tunnelchat/pkg/probe"
	"tunnelchat/pkg/relay"
	"tunnelchat/pkg/session"
	"tunnelchat/pkg/stun"
)

const (
	MsgChat byte = 1

	stunRefreshEvery = 30 * time.Second
	dhtRefreshEvery  = 45 * time.Second
	probeTimeout     = 2 * time.Second
	connectWait      = 4 * time.Second
)

type ChatCallback func(friend config.Friend, text string)
type EventCallback func(event string)

type Options struct {
	Conn        net.PacketConn
	DHTEnabled  bool
	DHTDisabled bool
	// UseDefaultBootstraps expands an empty bootstrap list to the
	// documented public Mainline routers. The CLI sets this for
	// `online` only. Tests must leave it false.
	UseDefaultBootstraps bool
	DHTBootstrap         []string
	STUNServers          []string
	DisableSTUN          bool
	RelayServer          string
	RelayToken           string
	ProbeTimeout         time.Duration
	ConnectWait          time.Duration
}

type peerState struct {
	friend   config.Friend
	pub      identity.Public
	sess     *session.Session
	remote   net.Addr
	viaRelay bool
}

type Engine struct {
	cfg  *config.Config
	opts Options
	id   *identity.Identity
	rv   [32]byte

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mux  *mux.Mux
	conn net.PacketConn
	stun *stun.Client
	dht  *dht.Node
	ver  *probe.Verifier

	mu         sync.Mutex
	cands      []candidate.Candidate
	mapped     *stun.Address
	peers      map[string]*peerState // ed25519 hex / raw key string
	handsh     map[string]*session.Handshaker
	seqHint    map[string]int64
	connecting map[string]bool
	discovered map[string]*DiscoveredPeer
	seenNonce  map[string]int64
	querySeen  map[string]time.Time
	presenceRL time.Time

	chatCb  ChatCallback
	eventCb EventCallback

	relaySrv *relay.Server // not started by engine
}

func New(cfg *config.Config, opts Options) (*Engine, error) {
	if cfg == nil {
		return nil, errors.New("config required")
	}
	id, err := cfg.Identity()
	if err != nil {
		return nil, err
	}
	rv, err := cfg.Rendezvous()
	if err != nil {
		return nil, err
	}
	if opts.ProbeTimeout == 0 {
		opts.ProbeTimeout = probeTimeout
	}
	if opts.ConnectWait == 0 {
		opts.ConnectWait = connectWait
	}
	if len(opts.STUNServers) == 0 {
		opts.STUNServers = append([]string(nil), cfg.STUNServers...)
	}
	if opts.RelayServer == "" && cfg.Relay.Enabled {
		opts.RelayServer = cfg.Relay.Server
		opts.RelayToken = cfg.Relay.Token
	}
	enabled, boots := dht.ResolveSettings(opts.DHTDisabled || cfg.DHT.Disabled, opts.UseDefaultBootstraps, opts.DHTBootstrap, cfg.DHT.Bootstrap)
	opts.DHTEnabled = enabled
	opts.DHTBootstrap = boots
	return &Engine{
		cfg:        cfg,
		opts:       opts,
		id:         id,
		rv:         rv,
		stun:       stun.NewClient(),
		ver:        probe.NewVerifier(),
		peers:      make(map[string]*peerState),
		handsh:     make(map[string]*session.Handshaker),
		seqHint:    make(map[string]int64),
		connecting: make(map[string]bool),
		discovered: make(map[string]*DiscoveredPeer),
		seenNonce:  make(map[string]int64),
		querySeen:  make(map[string]time.Time),
	}, nil
}

func (e *Engine) SetChatCallback(cb ChatCallback) {
	e.mu.Lock()
	e.chatCb = cb
	e.mu.Unlock()
}

func (e *Engine) SetEventCallback(cb EventCallback) {
	e.mu.Lock()
	e.eventCb = cb
	e.mu.Unlock()
}

func (e *Engine) log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	e.mu.Lock()
	cb := e.eventCb
	e.mu.Unlock()
	if cb != nil {
		cb(msg)
	}
}

func (e *Engine) Identity() *identity.Identity { return e.id }

func (e *Engine) Start() error {
	if e.cancel != nil {
		return errors.New("already started")
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())

	var conn net.PacketConn
	var err error
	if e.opts.Conn != nil {
		conn = e.opts.Conn
	} else {
		port := e.cfg.ListenPort
		laddr := &net.UDPAddr{Port: port}
		conn, err = net.ListenUDP("udp4", laddr)
		if err != nil {
			e.cancel()
			return fmt.Errorf("listen udp: %w", err)
		}
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			e.cfg.SetListenPort(ua.Port)
		}
	}
	e.conn = conn
	e.mux = mux.New(conn)

	e.wg.Add(2)
	go e.stunLoop()
	go e.appLoop()

	if err := e.refreshSTUN(); err != nil {
		e.log("STUN refresh failed: %v (continuing with host candidates)", err)
	}
	e.refreshHostCandidates()

	if e.opts.DHTEnabled && len(e.opts.DHTBootstrap) > 0 {
		e.dht = dht.NewNode(e.mux.DHT(), dht.NodeID{})
		for _, b := range e.opts.DHTBootstrap {
			addr, err := net.ResolveUDPAddr("udp", b)
			if err != nil {
				e.log("bad DHT bootstrap %s: %v", b, err)
				continue
			}
			e.dht.AddBootstrap(addr)
		}
		e.wg.Add(1)
		go e.dhtLoop()
	} else if e.opts.DHTDisabled || e.cfg.DHT.Disabled {
		e.log("DHT disabled")
	}

	e.wg.Add(1)
	go e.maintainLoop()

	e.log("engine started on %s", conn.LocalAddr())
	return nil
}

func (e *Engine) Stop() {
	if e.cancel == nil {
		return
	}
	e.cancel()
	if e.dht != nil {
		e.dht.Close()
	}
	if e.mux != nil {
		_ = e.mux.Close()
	} else if e.conn != nil {
		_ = e.conn.Close()
	}
	e.wg.Wait()
	e.cancel = nil
}

func (e *Engine) Candidates() []candidate.Candidate {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]candidate.Candidate(nil), e.cands...)
}

func (e *Engine) Mapped() *stun.Address {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mapped
}

func (e *Engine) refreshHostCandidates() {
	port := 0
	if e.conn != nil {
		if ua, ok := e.conn.LocalAddr().(*net.UDPAddr); ok {
			port = ua.Port
		}
	}
	host := candidate.GatherHost(port)
	e.mu.Lock()
	var srflx []candidate.Candidate
	if e.mapped != nil {
		srflx = append(srflx, candidate.New(candidate.TypeSrflx, e.mapped.IP.String(), e.mapped.Port))
	}
	e.cands = candidate.Merge(host, srflx)
	e.mu.Unlock()
}

func (e *Engine) refreshSTUN() error {
	if e.opts.DisableSTUN || len(e.opts.STUNServers) == 0 {
		return errors.New("STUN disabled")
	}
	ctx, cancel := context.WithTimeout(e.ctx, 4*time.Second)
	defer cancel()
	addr, err := e.stun.Discover(ctx, e.mux.WriteTo, e.opts.STUNServers)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.mapped = addr
	e.mu.Unlock()
	e.refreshHostCandidates()
	e.log("STUN mapped address %s (using mapped port, shared socket)", addr)
	return nil
}

func (e *Engine) stunLoop() {
	defer e.wg.Done()
	buf := make([]byte, 2048)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		_ = e.mux.STUN().SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := e.mux.STUN().ReadFrom(buf)
		if err != nil {
			continue
		}
		e.stun.HandlePacket(addr, buf[:n])
	}
}

func (e *Engine) appLoop() {
	defer e.wg.Done()
	buf := make([]byte, 2048)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		_ = e.mux.App().SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := e.mux.App().ReadFrom(buf)
		if err != nil {
			continue
		}
		e.handleApp(append([]byte(nil), buf[:n]...), addr)
	}
}

func (e *Engine) maintainLoop() {
	defer e.wg.Done()
	stunT := time.NewTicker(stunRefreshEvery)
	defer stunT.Stop()
	connectTick := time.NewTicker(1 * time.Second)
	defer connectTick.Stop()
	// immediate connect attempt
	e.tryConnectAll()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-stunT.C:
			if err := e.refreshSTUN(); err != nil {
				e.log("STUN refresh failed: %v", err)
			}
		case <-connectTick.C:
			e.tryConnectAll()
		}
	}
}

func friendKey(pub identity.Public) string {
	return string(pub.Ed25519[:])
}

func (e *Engine) Invite() (string, error) {
	e.refreshHostCandidates()
	e.mu.Lock()
	cands := append([]candidate.Candidate(nil), e.cands...)
	e.mu.Unlock()
	s, err := invite.Encode(e.id, e.rv, cands)
	if err != nil {
		return "", err
	}
	return s, nil
}

func (e *Engine) AddInvite(s string) error {
	inv, err := invite.Decode(s)
	if err != nil {
		return err
	}
	if inv.Public.Equal(e.id.Public()) {
		return errors.New("cannot add yourself")
	}
	f := config.Friend{
		Name:       inv.Name,
		Ed25519:    inv.Public.Ed25519[:],
		X25519:     inv.Public.X25519[:],
		Rendezvous: inv.Rendezvous[:],
		Candidates: inv.Candidates,
	}
	if err := e.cfg.AddFriend(f); err != nil {
		return err
	}
	if err := e.cfg.Save(); err != nil {
		return err
	}
	e.log("pinned friend %s fingerprint %s", inv.Name, inv.Public.Fingerprint())
	if e.cancel != nil {
		go e.connectFriend(f)
	}
	return nil
}

func (e *Engine) tryConnectAll() {
	for _, f := range e.cfg.ListFriends() {
		e.connectFriend(f)
	}
}

func (e *Engine) hasSession(pub identity.Public) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.peers[friendKey(pub)]
	return st != nil && st.sess != nil
}

func (e *Engine) connectFriend(f config.Friend) {
	pub, err := f.Public()
	if err != nil {
		return
	}
	key := friendKey(pub)
	e.mu.Lock()
	if st := e.peers[key]; st != nil && st.sess != nil {
		e.mu.Unlock()
		return
	}
	if e.connecting[key] {
		e.mu.Unlock()
		return
	}
	e.connecting[key] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.connecting, key)
		e.mu.Unlock()
	}()
	cands := append([]candidate.Candidate(nil), f.Candidates...)
	if e.dht != nil {
		cands = append(cands, e.lookupFriend(f)...)
	}
	cands = candidate.Merge(cands)
	if len(cands) == 0 && e.opts.RelayServer == "" {
		return
	}
	working := e.probeCandidates(pub, cands)
	if len(working) == 0 && e.opts.RelayServer != "" {
		if addr, err := e.setupRelay(pub); err == nil {
			working = []net.Addr{addr}
		}
	}
	if len(working) == 0 {
		return
	}
	e.maybeHandshake(pub, working[0])
}

func (e *Engine) lookupFriend(f config.Friend) []candidate.Candidate {
	rv, err := f.RendezvousKey()
	if err != nil || e.dht == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(e.ctx, 4*time.Second)
	defer cancel()
	ih := dht.InfoHashFromSecret(rv[:])
	peers, _ := e.dht.GetPeers(ctx, ih)
	var cands []candidate.Candidate
	for _, p := range peers {
		cands = append(cands, candidate.New(candidate.TypeSrflx, p.IP.String(), p.Port))
	}
	pub, err := f.Public()
	if err == nil {
		if _, blob, err := e.dht.GetMutable(ctx, ed25519.PublicKey(pub.Ed25519[:]), []byte("tc2-cands")); err == nil {
			if hints, err := dht.OpenHints(rv[:], blob); err == nil {
				cands = append(cands, hints...)
			}
		}
	}
	return cands
}

func (e *Engine) probeCandidates(pub identity.Public, cands []candidate.Candidate) []net.Addr {
	var working []net.Addr
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(e.ctx, e.opts.ProbeTimeout)
	defer cancel()
	seen := map[string]struct{}{}
	for _, c := range cands {
		addr, err := c.UDPAddr()
		if err != nil {
			continue
		}
		if _, ok := seen[addr.String()]; ok {
			continue
		}
		seen[addr.String()] = struct{}{}
		wg.Add(1)
		go func(addr *net.UDPAddr) {
			defer wg.Done()
			if e.sendProbeWait(ctx, pub, addr) {
				mu.Lock()
				working = append(working, addr)
				mu.Unlock()
			}
		}(addr)
	}
	wg.Wait()
	return working
}

func (e *Engine) sendProbeWait(ctx context.Context, pub identity.Public, addr net.Addr) bool {
	pkt, _, err := probe.Encode(e.id, pub, false, [16]byte{})
	if err != nil {
		return false
	}
	// We treat a later incoming probe/ack from this addr as success via
	// handleApp; also send and hope for ack before timeout. A simple
	// success path is: we receive an ack (or a probe) from this peer.
	marker := make(chan struct{}, 1)
	e.mu.Lock()
	// stash a waiter? Keep it simple: send a few times and also rely on
	// incoming probes recorded in peerState.remote.
	e.mu.Unlock()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	_, _ = e.mux.WriteTo(pkt, addr)
	for {
		select {
		case <-ctx.Done():
			return e.remoteKnown(pub, addr)
		case <-ticker.C:
			_, _ = e.mux.WriteTo(pkt, addr)
			if e.remoteKnown(pub, addr) {
				_ = marker
				return true
			}
		}
	}
}

func (e *Engine) remoteKnown(pub identity.Public, addr net.Addr) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.peers[friendKey(pub)]
	if st == nil || st.remote == nil {
		return false
	}
	return st.remote.String() == addr.String()
}

func (e *Engine) rememberRemote(pub identity.Public, f config.Friend, addr net.Addr) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.peers[friendKey(pub)]
	if st == nil {
		st = &peerState{friend: f, pub: pub}
		e.peers[friendKey(pub)] = st
	}
	st.remote = addr
}

func (e *Engine) maybeHandshake(pub identity.Public, addr net.Addr) {
	if e.hasSession(pub) {
		return
	}
	// Lower fingerprint initiates to avoid a dual-IK deadlock; the
	// other side accepts as responder in handleApp.
	if e.id.Public().Fingerprint() < pub.Fingerprint() {
		hs, pkt, err := session.Initiator(e.id, pub)
		if err != nil {
			return
		}
		e.mu.Lock()
		e.handsh[friendKey(pub)] = hs
		e.mu.Unlock()
		e.writePeer(addr, pub, pkt)
	}
}

func (e *Engine) setupRelay(pub identity.Public) (net.Addr, error) {
	raddr, err := net.ResolveUDPAddr("udp", e.opts.RelayServer)
	if err != nil {
		return nil, err
	}
	// Reuse an existing allocation. A second Allocate() would race the
	// relay read loop for AllocOK on the shared mux port.
	e.mu.Lock()
	if st := e.peers[friendKey(pub)]; st != nil && st.viaRelay && st.remote != nil {
		existing := st.remote
		e.mu.Unlock()
		return existing, nil
	}
	e.mu.Unlock()
	localEd := e.id.Public().Ed25519
	peerEd := pub.Ed25519
	id := relay.AllocID(e.opts.RelayToken, localEd[:], peerEd[:])
	cli := relay.NewClient(e.mux.Relay(), raddr, e.opts.RelayToken, id)
	if err := cli.Allocate(); err != nil {
		return nil, err
	}
	// Subsequent writes go to the relay address; handleRelay will unwrap.
	e.wg.Add(1)
	go e.relayReadLoop(cli, pub, raddr)
	e.mu.Lock()
	st := e.peers[friendKey(pub)]
	if st == nil {
		st = &peerState{pub: pub}
		e.peers[friendKey(pub)] = st
	}
	st.viaRelay = true
	st.remote = raddr
	e.mu.Unlock()
	return raddr, nil
}

func (e *Engine) relayReadLoop(cli *relay.Client, pub identity.Public, raddr net.Addr) {
	defer e.wg.Done()
	buf := make([]byte, 2048)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		_ = e.mux.Relay().SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := e.mux.Relay().ReadFrom(buf)
		if err != nil {
			continue
		}
		opaque, ok := cli.Unwrap(buf[:n])
		if !ok {
			continue
		}
		e.handleApp(opaque, raddr)
	}
}

func (e *Engine) writePeer(addr net.Addr, pub identity.Public, pkt []byte) {
	if e.opts.RelayServer != "" {
		e.mu.Lock()
		st := e.peers[friendKey(pub)]
		via := st != nil && st.viaRelay
		e.mu.Unlock()
		if via {
			raddr, err := net.ResolveUDPAddr("udp", e.opts.RelayServer)
			if err == nil {
				localEd := e.id.Public().Ed25519
				peerEd := pub.Ed25519
				id := relay.AllocID(e.opts.RelayToken, localEd[:], peerEd[:])
				cli := relay.NewClient(e.mux.Relay(), raddr, e.opts.RelayToken, id)
				_ = cli.WriteOpaque(pkt)
				return
			}
		}
	}
	_, _ = e.mux.WriteTo(pkt, addr)
}

func (e *Engine) handleApp(pkt []byte, from net.Addr) {
	switch {
	case presence.IsQuery(pkt):
		e.handlePresenceQuery(from)
	case presence.IsRecord(pkt):
		e.handlePresenceRecord(pkt, from)
	case probe.IsProbe(pkt):
		e.handleProbe(pkt, from)
	case session.IsHandshake(pkt):
		e.handleHandshake(pkt, from)
	case session.IsTransport(pkt):
		e.handleTransport(pkt, from)
	}
}

func (e *Engine) handleProbe(pkt []byte, from net.Addr) {
	// Accept pinned friends or currently discovered (signed) identities.
	// Raw DHT IPs without a matching signature are ignored.
	for _, pub := range e.knownIdentities() {
		f, ok := e.friendOrDiscovered(pub)
		if !ok {
			continue
		}
		m, err := e.ver.CheckFor(pkt, pub, e.id.Public())
		if err != nil {
			continue
		}
		e.rememberRemote(pub, f, from)
		if !m.Ack {
			ack, _, err := probe.Encode(e.id, pub, true, m.Nonce)
			if err == nil {
				e.writePeer(from, pub, ack)
			}
		}
		return
	}
}

func (e *Engine) handleHandshake(pkt []byte, from net.Addr) {
	for _, pub := range e.knownIdentities() {
		f, ok := e.friendOrDiscovered(pub)
		if !ok {
			continue
		}
		e.mu.Lock()
		hs := e.handsh[friendKey(pub)]
		e.mu.Unlock()
		if hs != nil {
			reply, sess, err := hs.Continue(pkt)
			if err != nil {
				continue
			}
			if reply != nil {
				e.writePeer(from, pub, reply)
			}
			if sess != nil {
				e.installSession(f, pub, sess, from)
			}
			return
		}
		// Incoming initiator: start responder with the pinned key.
		resp, err := session.StartResponder(e.id, pub)
		if err != nil {
			return
		}
		reply, sess, err := resp.Continue(pkt)
		if err != nil {
			continue
		}
		if reply != nil {
			e.writePeer(from, pub, reply)
		}
		if sess != nil {
			e.installSession(f, pub, sess, from)
		}
		return
	}
}

func (e *Engine) installSession(f config.Friend, pub identity.Public, sess *session.Session, from net.Addr) {
	if !sess.Peer().Equal(pub) {
		return
	}
	e.mu.Lock()
	st := e.peers[friendKey(pub)]
	if st == nil {
		st = &peerState{friend: f, pub: pub}
		e.peers[friendKey(pub)] = st
	}
	if st.sess != nil {
		e.mu.Unlock()
		return
	}
	st.sess = sess
	st.remote = from
	if e.opts.RelayServer != "" && from != nil && from.String() == e.opts.RelayServer {
		st.viaRelay = true
	}
	delete(e.handsh, friendKey(pub))
	e.mu.Unlock()
	e.log("secure session established with %s via %s", f.Name, from)
}

func (e *Engine) handleTransport(pkt []byte, from net.Addr) {
	e.mu.Lock()
	var (
		cb   ChatCallback
		f    config.Friend
		text string
		ok   bool
	)
	cb = e.chatCb
	for _, st := range e.peers {
		if st.sess == nil {
			continue
		}
		pt, err := st.sess.Open(pkt)
		if err != nil {
			continue
		}
		if from != nil {
			st.remote = from
		}
		if len(pt) >= 1 && pt[0] == MsgChat && cb != nil {
			f = st.friend
			text = string(pt[1:])
			ok = true
		}
		e.mu.Unlock()
		if ok {
			cb(f, text)
		}
		return
	}
	e.mu.Unlock()
}

func (e *Engine) SendChat(name, text string) error {
	f, ok := e.cfg.GetFriendByName(name)
	if !ok {
		if d, ok2 := e.lookupDiscovered(name); ok2 {
			f = discoveredAsFriend(d)
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("no friend or discovered peer %q (strangers must be listed by /peers first)", name)
	}
	pub, err := f.Public()
	if err != nil {
		return err
	}
	e.mu.Lock()
	st := e.peers[friendKey(pub)]
	if st == nil || st.sess == nil || st.remote == nil {
		e.mu.Unlock()
		return fmt.Errorf("no secure session with %q yet", name)
	}
	pkt, err := st.sess.Seal(append([]byte{MsgChat}, text...))
	remote := st.remote
	via := st.viaRelay
	e.mu.Unlock()
	if err != nil {
		return err
	}
	if via {
		e.writePeer(remote, pub, pkt)
		return nil
	}
	_, err = e.mux.WriteTo(pkt, remote)
	return err
}

func (e *Engine) HasSession(name string) bool {
	if f, ok := e.cfg.GetFriendByName(name); ok {
		if pub, err := f.Public(); err == nil {
			return e.hasSession(pub)
		}
	}
	if d, ok := e.lookupDiscovered(name); ok {
		return e.hasSession(d.Public)
	}
	for _, f := range e.cfg.ListFriends() {
		if pub, err := f.Public(); err == nil && strings.EqualFold(pub.Fingerprint(), name) {
			return e.hasSession(pub)
		}
	}
	return false
}

func (e *Engine) ListFriends() []config.Friend {
	return e.cfg.ListFriends()
}

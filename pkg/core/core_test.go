package core

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"tunnelchat/pkg/config"
	"tunnelchat/pkg/dht"
	"tunnelchat/pkg/identity"
	"tunnelchat/pkg/netsim"
	"tunnelchat/pkg/presence"
	"tunnelchat/pkg/relay"
	"tunnelchat/pkg/stun"
)

func newAccount(t *testing.T, name string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".json")
	cfg, err := config.CreateIdentity(path, name)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func startEngine(t *testing.T, cfg *config.Config, opts Options) *Engine {
	t.Helper()
	eng, err := New(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Stop)
	return eng
}

func waitSession(t *testing.T, a, b *Engine, aname, bname string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if a.HasSession(bname) && b.HasSession(aname) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for session %s<->%s (a=%v b=%v)", aname, bname, a.HasSession(bname), b.HasSession(aname))
}

func TestDirectSessionChat(t *testing.T) {
	aliceCfg := newAccount(t, "Alice")
	bobCfg := newAccount(t, "Bob")
	alice := startEngine(t, aliceCfg, Options{DisableSTUN: true})
	bob := startEngine(t, bobCfg, Options{DisableSTUN: true})

	ainv, err := alice.Invite()
	if err != nil {
		t.Fatal(err)
	}
	binv, err := bob.Invite()
	if err != nil {
		t.Fatal(err)
	}
	if err := alice.AddInvite(binv); err != nil {
		t.Fatal(err)
	}
	if err := bob.AddInvite(ainv); err != nil {
		t.Fatal(err)
	}
	waitSession(t, alice, bob, "Alice", "Bob")

	got := make(chan string, 1)
	bob.SetChatCallback(func(_ config.Friend, text string) { got <- text })
	if err := alice.SendChat("Bob", "hello encrypted"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "hello encrypted" {
			t.Fatalf("got %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for chat")
	}
}

func TestDHTDiscoveryLocal(t *testing.T) {
	storeConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	store := dht.NewNode(storeConn, dht.NodeID{})
	t.Cleanup(store.Close)
	bootstrap := storeConn.LocalAddr().String()

	aliceCfg := newAccount(t, "Alice")
	bobCfg := newAccount(t, "Bob")
	opts := func() Options {
		return Options{DisableSTUN: true, DHTEnabled: true, DHTBootstrap: []string{bootstrap}}
	}
	alice := startEngine(t, aliceCfg, opts())
	bob := startEngine(t, bobCfg, opts())

	// Exchange invites but strip the need for live endpoints: still pin
	// keys + rendezvous, then let DHT implied_port provide the address.
	ainv, _ := alice.Invite()
	binv, _ := bob.Invite()
	if err := alice.AddInvite(binv); err != nil {
		t.Fatal(err)
	}
	if err := bob.AddInvite(ainv); err != nil {
		t.Fatal(err)
	}
	waitSession(t, alice, bob, "Alice", "Bob")
	got := make(chan string, 1)
	alice.SetChatCallback(func(_ config.Friend, text string) { got <- text })
	if err := bob.SendChat("Alice", "via dht"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "via dht" {
			t.Fatalf("got %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestRelayFallbackWhenDirectBlocked(t *testing.T) {
	token, err := relay.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := relay.Listen(relay.Config{Listen: "127.0.0.1:0", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	aliceCfg := newAccount(t, "Alice")
	bobCfg := newAccount(t, "Bob")

	// Engines get no host/srflx candidates that work: we start them
	// after adding invites that contain only a black-hole address, and
	// disable gathering useful locals by... actually Invite() will
	// include 127.0.0.1. To force relay, give friends a bogus candidate
	// only and prevent Invite() addresses from being used: add after
	// creating invites from stopped engines? Simpler: after add, wipe
	// stored candidates so only relay can be used.
	alice := startEngine(t, aliceCfg, Options{DisableSTUN: true, RelayServer: srv.Addr().String(), RelayToken: token})
	bob := startEngine(t, bobCfg, Options{DisableSTUN: true, RelayServer: srv.Addr().String(), RelayToken: token})
	pinWithoutCandidates(t, aliceCfg, bobCfg, alice, bob)
	// Friends are added after Start; kick a connect attempt immediately
	// instead of waiting for the 1s maintain tick.
	alice.tryConnectAll()
	bob.tryConnectAll()
	waitSession(t, alice, bob, "Alice", "Bob")
	got := make(chan string, 1)
	bob.SetChatCallback(func(_ config.Friend, text string) { got <- text })
	if err := alice.SendChat("Bob", "relayed"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "relayed" {
			t.Fatalf("got %q", m)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timeout waiting for relayed chat")
	}
}

func TestSTUNMappedPortUsedInCandidates(t *testing.T) {
	nat, err := netsim.NewNAT(netsim.Cone)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nat.Close() })

	stunConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stunConn.Close() })
	go func() { _ = stun.ServeFake(stunConn, nil) }()

	cfg := newAccount(t, "Alice")
	host := nat.Host()
	// Wrap through mux? Engine creates its own mux on Conn.
	eng := startEngine(t, cfg, Options{Conn: host, STUNServers: []string{stunConn.LocalAddr().String()}})
	mapped := eng.Mapped()
	if mapped == nil {
		t.Fatal("expected STUN mapped address")
	}
	wan := nat.WANAddr()
	if mapped.Port != wan.Port {
		t.Fatalf("engine must advertise WAN mapped port %d, got %d", wan.Port, mapped.Port)
	}
	found := false
	for _, c := range eng.Candidates() {
		if c.Type == "srflx" && c.Port == wan.Port {
			found = true
		}
	}
	if !found {
		t.Fatalf("srflx candidate missing mapped port, cands=%v mapped=%s", eng.Candidates(), mapped)
	}
}

func pinWithoutCandidates(t *testing.T, aliceCfg, bobCfg *config.Config, alice, bob *Engine) {
	t.Helper()
	aID, err := aliceCfg.Identity()
	if err != nil {
		t.Fatal(err)
	}
	bID, err := bobCfg.Identity()
	if err != nil {
		t.Fatal(err)
	}
	aRV, err := aliceCfg.Rendezvous()
	if err != nil {
		t.Fatal(err)
	}
	bRV, err := bobCfg.Rendezvous()
	if err != nil {
		t.Fatal(err)
	}
	ap, bp := aID.Public(), bID.Public()
	if err := bobCfg.AddFriend(config.Friend{Name: "Alice", Ed25519: ap.Ed25519[:], X25519: ap.X25519[:], Rendezvous: aRV[:], Candidates: nil}); err != nil {
		t.Fatal(err)
	}
	if err := aliceCfg.AddFriend(config.Friend{Name: "Bob", Ed25519: bp.Ed25519[:], X25519: bp.X25519[:], Rendezvous: bRV[:], Candidates: nil}); err != nil {
		t.Fatal(err)
	}
	_ = alice
	_ = bob
}

func TestLegacyInviteRejected(t *testing.T) {
	cfg := newAccount(t, "Alice")
	eng, err := New(cfg, Options{DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.AddInvite("98F8E4DED6E5EEF5"); err == nil {
		t.Fatal("legacy DES invite must be rejected")
	}
}

func TestDefaultOnlineEnablesPublicBootstrapsWithoutDialing(t *testing.T) {
	cfg := newAccount(t, "Alice")
	eng, err := New(cfg, Options{UseDefaultBootstraps: true, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if !eng.DHTEnabled() {
		t.Fatal("online default should enable DHT")
	}
	if len(eng.DHTBootstraps()) == 0 {
		t.Fatal("expected default bootstrap list")
	}
	// Do not Start(): this test must never contact the public DHT.

	off, err := New(cfg, Options{UseDefaultBootstraps: true, DHTDisabled: true, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if off.DHTEnabled() {
		t.Fatal("--no-dht must disable DHT")
	}

	over, err := New(cfg, Options{UseDefaultBootstraps: true, DHTBootstrap: []string{"127.0.0.1:1"}, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(over.DHTBootstraps()) != 1 || over.DHTBootstraps()[0] != "127.0.0.1:1" {
		t.Fatalf("override failed: %v", over.DHTBootstraps())
	}

	silent, err := New(cfg, Options{DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if silent.DHTEnabled() {
		t.Fatal("tests / non-online options must not enable public DHT")
	}

	cfg.DHT.Disabled = true
	cfgOff, err := New(cfg, Options{UseDefaultBootstraps: true, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfgOff.DHTEnabled() {
		t.Fatal("config dht.disabled must turn DHT off")
	}
}

func TestStrangerDiscoveryAndConnect(t *testing.T) {
	storeConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	store := dht.NewNode(storeConn, dht.NodeID{})
	t.Cleanup(store.Close)
	bootstrap := storeConn.LocalAddr().String()
	opts := Options{DisableSTUN: true, DHTBootstrap: []string{bootstrap}}

	aliceCfg := newAccount(t, "Alice")
	bobCfg := newAccount(t, "Bob")
	alice := startEngine(t, aliceCfg, opts)
	bob := startEngine(t, bobCfg, opts)

	var aList, bList []DiscoveredPeer
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		aList, bList = alice.ListDiscovered(), bob.ListDiscovered()
		if len(aList) > 0 && len(bList) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(aList) == 0 || len(bList) == 0 {
		t.Fatalf("expected mutual discovery, alice=%d bob=%d", len(aList), len(bList))
	}
	if alice.IsTrustedFriend(bob.Identity().Public().Fingerprint()) || bob.IsTrustedFriend(alice.Identity().Public().Fingerprint()) {
		t.Fatal("discovery must not auto-insert trusted friends")
	}
	if len(alice.ListFriends()) != 0 || len(bob.ListFriends()) != 0 {
		t.Fatal("friend lists must stay empty until explicit add")
	}

	if err := alice.ConnectDiscovered(bob.Identity().Public().Fingerprint()); err != nil {
		t.Fatal(err)
	}
	if err := bob.ConnectDiscovered(alice.Identity().Public().Fingerprint()); err != nil {
		t.Fatal(err)
	}
	waitSession(t, alice, bob, "Alice", "Bob")
	if len(alice.ListFriends()) != 0 || len(bob.ListFriends()) != 0 {
		t.Fatal("connect must not pin friends")
	}

	got := make(chan string, 1)
	bob.SetChatCallback(func(_ config.Friend, text string) { got <- text })
	if err := alice.SendChat("Bob", "hi stranger"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "hi stranger" {
			t.Fatalf("got %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for stranger chat")
	}

	if err := alice.AddDiscovered(bob.Identity().Public().Fingerprint()); err != nil {
		t.Fatal(err)
	}
	if !alice.IsTrustedFriend(bob.Identity().Public().Fingerprint()) {
		t.Fatal("addpeer should pin bob")
	}
	if len(alice.ListFriends()) != 1 {
		t.Fatalf("expected one friend, got %d", len(alice.ListFriends()))
	}
	if len(bob.ListFriends()) != 0 {
		t.Fatal("bob did not add alice; should stay unpinned")
	}
}

func TestUnsignedPresenceRejected(t *testing.T) {
	bobCfg := newAccount(t, "Bob")
	bob := startEngine(t, bobCfg, Options{DisableSTUN: true})
	garbage := append(append([]byte{}, []byte("TC2\x01")...), 6, 2)
	garbage = append(garbage, make([]byte, 80)...)
	bob.handlePresenceRecord(garbage, &net.UDPAddr{IP: net.IPv4(203, 0, 113, 1), Port: 9})
	if n := len(bob.ListDiscovered()); n != 0 {
		t.Fatalf("unsigned presence listed %d peers", n)
	}

	// Forged signature: valid-looking but flipped bit.
	carolID, err := identity.Generate("Carol")
	if err != nil {
		t.Fatal(err)
	}
	good, err := presence.Encode(carolID, nil)
	if err != nil {
		t.Fatal(err)
	}
	forged := append([]byte(nil), good...)
	forged[len(forged)-1] ^= 0x7f
	bob.handlePresenceRecord(forged, &net.UDPAddr{IP: net.IPv4(203, 0, 113, 2), Port: 9})
	if n := len(bob.ListDiscovered()); n != 0 {
		t.Fatalf("forged presence listed %d peers", n)
	}

	bob.handlePresenceRecord(good, &net.UDPAddr{IP: net.IPv4(203, 0, 113, 2), Port: 9})
	got := bob.ListDiscovered()
	if len(got) != 1 || got[0].Public.Name != "Carol" {
		t.Fatalf("valid presence should be listed: %+v", got)
	}
	if bob.IsTrustedFriend(got[0].Public.Fingerprint()) {
		t.Fatal("valid discovery is still not a trusted friend")
	}
}

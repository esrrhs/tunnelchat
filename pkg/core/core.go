package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"tunnelchat/pkg/config"
	"tunnelchat/pkg/crypto"
	"tunnelchat/pkg/stun"
)

const (
	HeartbeatInterval = 1 * time.Second
	SyncInterval      = 60 * time.Second
	RpcTimeout        = 10 * time.Second
	RpcResendInterval = 1 * time.Second
	MaxMsgLen         = 2048
	HeartbeatPayload  = "hb"
)

type ChatCallback func(friend config.Friend, text string)
type EventCallback func(event string)

type pendingRPC struct {
	msgID    string
	respChan chan string
}

type cachedResp struct {
	msg     string
	created time.Time
}

type Engine struct {
	cfg        *config.Config
	configPath string

	conn     *net.UDPConn
	localIP  string
	localPort int

	pendingMu sync.Mutex
	pending   map[string]*pendingRPC

	cacheMu   sync.Mutex
	respCache map[string]*cachedResp // key: ip:port:msgid

	chatCb  ChatCallback
	eventCb EventCallback

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEngine initializes the core P2P engine.
func NewEngine(cfg *config.Config, configPath string) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		cfg:        cfg,
		configPath: configPath,
		pending:    make(map[string]*pendingRPC),
		respCache:  make(map[string]*cachedResp),
		ctx:        ctx,
		cancel:     cancel,
	}
	return e, nil
}

// SetChatCallback sets the listener for incoming decrypted chat messages.
func (e *Engine) SetChatCallback(cb ChatCallback) {
	e.chatCb = cb
}

// SetEventCallback sets the listener for general notifications.
func (e *Engine) SetEventCallback(cb EventCallback) {
	e.eventCb = cb
}

func (e *Engine) logEvent(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if e.eventCb != nil {
		e.eventCb(msg)
	}
}

// Start begins network listening, STUN resolution, and background worker routines.
func (e *Engine) Start() error {
	user := e.cfg.GetUser()
	port := user.Port
	if port <= 0 {
		port = config.RandPort()
		user.Port = port
	}

	// Bind UDP socket
	laddr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return fmt.Errorf("listen UDP on port %d: %w", port, err)
	}
	e.conn = conn
	e.localPort = port

	if user.IP == "" {
		// STUN / Public IP Discovery
		var stunList []string
		for _, s := range e.cfg.STUNServer.STUNs {
			stunList = append(stunList, s.IP)
		}

		e.logEvent("Discovering network address via STUN...")
		mappedAddr, err := stun.DiscoverPublicAddress(stunList, user.TryPort)
		if err == nil && mappedAddr != nil {
			user.IP = mappedAddr.IP.String()
			e.logEvent("STUN discovered public address: %s (local port: %d)", user.IP, user.Port)
		} else {
			// Fallback to local outbound IP
			user.IP = stun.GetLocalOutboundIP()
			e.logEvent("STUN unavailable; using local address: %s (local port: %d)", user.IP, user.Port)
		}
	} else {
		e.logEvent("Using configured IP address: %s (local port: %d)", user.IP, user.Port)
	}

	e.localIP = user.IP
	e.cfg.SetUser(user)
	_ = e.cfg.SaveConfig(e.configPath)

	// Start reader goroutine
	e.wg.Add(1)
	go e.readLoop()

	// Start periodic worker (heartbeats, sync, cache cleanup)
	e.wg.Add(1)
	go e.periodicLoop()

	return nil
}

// Stop gracefully terminates network operations and persists configuration.
func (e *Engine) Stop() {
	e.cancel()
	if e.conn != nil {
		_ = e.conn.Close()
	}
	e.wg.Wait()
	_ = e.cfg.SaveConfig(e.configPath)
}

// GetUser returns the current user profile.
func (e *Engine) GetUser() config.User {
	return e.cfg.GetUser()
}

// CreateUser generates a new user account with unique GUIDs.
func (e *Engine) CreateUser(name, pwd string) {
	u := e.cfg.GetUser()
	u.Name = name
	u.Acc = crypto.NewGUID(name)
	u.Pwd = crypto.NewGUID(pwd)
	if u.Port <= 0 {
		u.Port = config.RandPort()
	}
	if u.TryPort <= 0 {
		u.TryPort = config.RandTryPort()
	}
	e.cfg.SetUser(u)
	_ = e.cfg.SaveConfig(e.configPath)
}

// GetInfoString encodes user acc, ip, and port using DES("tunnelchat", ...).
func (e *Engine) GetInfoString() string {
	u := e.cfg.GetUser()
	raw := fmt.Sprintf("%s %s %d", u.Acc, u.IP, u.Port)
	return crypto.DESEncrypt("tunnelchat", raw)
}

// AddFriendByInfo decodes an info string and initiates the mutual handshake RPC.
func (e *Engine) AddFriendByInfo(infoStr string) error {
	raw := crypto.DESDecrypt("tunnelchat", infoStr)
	tokens := strings.Fields(raw)
	if len(tokens) < 3 {
		return errors.New("invalid friend info string")
	}

	remoteAcc := tokens[0]
	remoteIP := tokens[1]
	remotePort, err := strconv.Atoi(tokens[2])
	if err != nil {
		return fmt.Errorf("invalid port in friend info: %w", err)
	}

	user := e.cfg.GetUser()
	if user.Acc == "" {
		return errors.New("no user created yet, please create a user first")
	}
	if remoteAcc == user.Acc {
		return errors.New("cannot add yourself as friend")
	}

	// Calculate SKey: MD5(my_pwd + remote_acc)
	friendKey := crypto.MD5(user.Pwd + remoteAcc)

	// Call RPC "add"
	// Payload: my_acc my_name friend_key remote_acc
	payload := fmt.Sprintf("%s %s %s %s", user.Acc, user.Name, friendKey, remoteAcc)
	encryptedPayload := crypto.DESEncrypt("add", payload)

	resp, err := e.CallRPC(remoteIP, remotePort, "add", encryptedPayload)
	if err != nil {
		return fmt.Errorf("rpc add failed: %w", err)
	}
	if resp != "ok" {
		return fmt.Errorf("friend rejected add: %s", resp)
	}

	// Update friend SKey and save
	friend, exists := e.cfg.GetFriend(remoteAcc)
	if !exists {
		friend = config.Friend{
			Acc:  remoteAcc,
			IP:   remoteIP,
			Port: remotePort,
		}
	}
	friend.SKey = friendKey
	e.cfg.SetFriend(friend)
	_ = e.cfg.SaveConfig(e.configPath)

	// Send immediate sync to establish bilateral connection info
	e.sendSyncToFriend(friend)

	return nil
}

// SendChatMessage encrypts and transmits a chat message to a friend by name.
func (e *Engine) SendChatMessage(friendName, words string) error {
	friend, ok := e.cfg.GetFriendByName(friendName)
	if !ok || friend.Acc == "" {
		return fmt.Errorf("no friend named %q found", friendName)
	}

	if friend.SKey == "" {
		return fmt.Errorf("no send key for friend %q (did friend add you back?)", friendName)
	}

	user := e.cfg.GetUser()
	// 1. Encrypt text with friend.SKey
	encryptedWords := crypto.DESEncrypt(friend.SKey, words)
	// 2. Encrypt "<my_acc> <encryptedWords>" with key "chat"
	innerMsg := fmt.Sprintf("%s %s", user.Acc, encryptedWords)
	encryptedPayload := crypto.DESEncrypt("chat", innerMsg)

	// 3. Send RPC
	resp, err := e.CallRPC(friend.IP, friend.Port, "chat", encryptedPayload)
	if err != nil {
		return fmt.Errorf("send message to %s failed: %w", friendName, err)
	}
	if resp != "ok" {
		return fmt.Errorf("remote returned error: %s", resp)
	}

	return nil
}

// CallRPC sends a message with retransmissions and awaits a reply.
func (e *Engine) CallRPC(ip string, port int, cmd, data string) (string, error) {
	msgID := crypto.NewGUID(cmd + " " + data)
	fullMsg := fmt.Sprintf("%s %s %s", msgID, cmd, data)

	respChan := make(chan string, 1)
	e.pendingMu.Lock()
	e.pending[msgID] = &pendingRPC{msgID: msgID, respChan: respChan}
	e.pendingMu.Unlock()

	defer func() {
		e.pendingMu.Lock()
		delete(e.pending, msgID)
		e.pendingMu.Unlock()
	}()

	raddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}

	ticker := time.NewTicker(RpcResendInterval)
	defer ticker.Stop()

	timeout := time.After(RpcTimeout)

	// Initial send
	_, _ = e.conn.WriteToUDP([]byte(fullMsg), raddr)

	for {
		select {
		case <-e.ctx.Done():
			return "", errors.New("engine stopped")
		case resp := <-respChan:
			return resp, nil
		case <-ticker.C:
			// Resend packet
			_, _ = e.conn.WriteToUDP([]byte(fullMsg), raddr)
		case <-timeout:
			return "", errors.New("rpc timeout")
		}
	}
}

// sendUDP transmits a raw packet to destination.
func (e *Engine) sendUDP(ip string, port int, payload string) {
	if e.conn == nil {
		return
	}
	raddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return
	}
	_, _ = e.conn.WriteToUDP([]byte(payload), raddr)
}

func (e *Engine) readLoop() {
	defer e.wg.Done()
	buf := make([]byte, MaxMsgLen)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		n, raddr, err := e.conn.ReadFromUDP(buf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			continue
		}

		msg := string(buf[:n])
		e.handleIncomingPacket(raddr, msg)
	}
}

func (e *Engine) handleIncomingPacket(raddr *net.UDPAddr, msg string) {
	if msg == HeartbeatPayload {
		return // NAT keep-alive packet
	}

	tokens := strings.SplitN(msg, " ", 3)
	if len(tokens) < 3 {
		return
	}

	msgID := tokens[0]
	cmd := tokens[1]
	data := tokens[2]

	remoteIP := raddr.IP.String()
	remotePort := raddr.Port

	if cmd == "res" {
		e.pendingMu.Lock()
		p, found := e.pending[msgID]
		e.pendingMu.Unlock()
		if found {
			select {
			case p.respChan <- data:
			default:
			}
		}
		return
	}

	// Check response cache
	cacheKey := fmt.Sprintf("%s:%d:%s", remoteIP, remotePort, msgID)
	e.cacheMu.Lock()
	cached, ok := e.respCache[cacheKey]
	e.cacheMu.Unlock()
	if ok && cached != nil {
		e.sendUDP(remoteIP, remotePort, cached.msg)
		return
	}

	switch cmd {
	case "add":
		e.onRecvAdd(remoteIP, remotePort, msgID, data)
	case "chat":
		e.onRecvChat(remoteIP, remotePort, msgID, data)
	case "sync":
		e.onRecvSync(remoteIP, remotePort, data)
	default:
		e.replyRPC(remoteIP, remotePort, msgID, "ok")
	}
}

func (e *Engine) replyRPC(ip string, port int, msgID, respData string) {
	realMsg := fmt.Sprintf("%s res %s", msgID, respData)
	e.sendUDP(ip, port, realMsg)

	// Cache response
	cacheKey := fmt.Sprintf("%s:%d:%s", ip, port, msgID)
	e.cacheMu.Lock()
	e.respCache[cacheKey] = &cachedResp{
		msg:     realMsg,
		created: time.Now(),
	}
	e.cacheMu.Unlock()
}

func (e *Engine) onRecvAdd(ip string, port int, msgID, emsg string) {
	smsg := crypto.DESDecrypt("add", emsg)
	tokens := strings.Fields(smsg)
	if len(tokens) < 4 {
		return
	}

	acc := tokens[0]
	name := tokens[1]
	key := tokens[2]
	wantAcc := tokens[3]

	user := e.cfg.GetUser()
	if wantAcc != user.Acc {
		return
	}

	f, exists := e.cfg.GetFriend(acc)
	if exists && f.RKey != "" && f.RKey != key {
		// Existing friend with mismatched key
		log.Printf("[Core] Warning: friend %s key mismatch", acc)
		return
	}

	// Reply OK
	e.replyRPC(ip, port, msgID, "ok")

	f.Acc = acc
	f.Name = name
	f.IP = ip
	f.Port = port
	f.RKey = key
	e.cfg.SetFriend(f)
	_ = e.cfg.SaveConfig(e.configPath)

	e.logEvent("Friend added: %s (%s:%d)", name, ip, port)
}

func (e *Engine) onRecvChat(ip string, port int, msgID, emsg string) {
	smsg := crypto.DESDecrypt("chat", emsg)
	tokens := strings.SplitN(smsg, " ", 2)
	if len(tokens) < 2 {
		return
	}

	acc := tokens[0]
	eWords := tokens[1]

	friend, ok := e.cfg.GetFriend(acc)
	if !ok || friend.RKey == "" {
		return
	}

	plainWords := crypto.DESDecrypt(friend.RKey, eWords)

	// Reply OK
	e.replyRPC(ip, port, msgID, "ok")

	if e.chatCb != nil {
		e.chatCb(friend, plainWords)
	}
}

func (e *Engine) onRecvSync(ip string, port int, emsg string) {
	smsg := crypto.DESDecrypt("sync", emsg)
	tokens := strings.SplitN(smsg, " ", 2)
	if len(tokens) < 2 {
		return
	}

	acc := tokens[0]
	eInfo := tokens[1]

	friend, ok := e.cfg.GetFriend(acc)
	if !ok || friend.RKey == "" {
		return
	}

	sInfo := crypto.DESDecrypt(friend.RKey, eInfo)
	infoTokens := strings.Fields(sInfo)
	if len(infoTokens) < 3 {
		return
	}

	sIP := infoTokens[0]
	sPort, err := strconv.Atoi(infoTokens[1])
	if err != nil {
		return
	}
	sName := infoTokens[2]

	friend.IP = sIP
	friend.Port = sPort
	friend.Name = sName
	e.cfg.SetFriend(friend)

	e.logEvent("Synced friend status: %s -> %s:%d", sName, sIP, sPort)
}

func (e *Engine) sendSyncToFriend(f config.Friend) {
	if f.SKey == "" {
		return
	}
	u := e.cfg.GetUser()
	if u.Acc == "" {
		return
	}

	info := fmt.Sprintf("%s %d %s", u.IP, u.Port, u.Name)
	eInfo := crypto.DESEncrypt(f.SKey, info)
	msg := fmt.Sprintf("%s %s", u.Acc, eInfo)
	emsg := crypto.DESEncrypt("sync", msg)

	packet := fmt.Sprintf("0 sync %s", emsg)
	e.sendUDP(f.IP, f.Port, packet)
}

func (e *Engine) periodicLoop() {
	defer e.wg.Done()

	hbTicker := time.NewTicker(HeartbeatInterval)
	defer hbTicker.Stop()

	syncTicker := time.NewTicker(SyncInterval)
	defer syncTicker.Stop()

	cleanupTicker := time.NewTicker(10 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return

		case <-hbTicker.C:
			// Send heartbeat "hb" to all friends
			for _, f := range e.cfg.ListFriends() {
				if f.IP != "" && f.Port > 0 {
					e.sendUDP(f.IP, f.Port, HeartbeatPayload)
				}
			}

		case <-syncTicker.C:
			// Send sync info to all friends
			for _, f := range e.cfg.ListFriends() {
				if f.IP != "" && f.Port > 0 {
					e.sendSyncToFriend(f)
				}
			}

		case <-cleanupTicker.C:
			// Cleanup response cache older than 15s
			e.cacheMu.Lock()
			now := time.Now()
			for k, v := range e.respCache {
				if now.Sub(v.created) > 15*time.Second {
					delete(e.respCache, k)
				}
			}
			e.cacheMu.Unlock()
		}
	}
}

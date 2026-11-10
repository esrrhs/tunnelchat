package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tunnelchat/pkg/config"
	"tunnelchat/pkg/core"
	"tunnelchat/pkg/relay"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

type App struct {
	configPath string
	opts       core.Options
}

func NewApp(configPath string, opts core.Options) *App {
	return &App{configPath: configPath, opts: opts}
}

func PrintUsage() {
	fmt.Printf(`%sTunnelchat v2%s — authenticated P2P chat (UDP)

%sUsage:%s
    tunnelchat [flags] <command>

%sFlags:%s
    -c <path>                 Config file (JSON, default tunnelchat.json)
    --no-dht                  Disable DHT (no public announce or stranger lookup)
    --dht-bootstrap host:port Override default Mainline bootstrap (repeatable)
    --relay-server host:port  Optional encrypted-payload relay
    --relay-token <token>     Relay access token
    --no-stun                 Skip STUN (LAN / tests)

%sCommands:%s
    new <name>                Create a new identity (refuses to overwrite)
    info                      Print a signed invite (no public DHT announce)
    add <invite>              Pin a friend's identity from their invite
    online                    STUN, DHT discovery (default on), probes, and chat
    relay --listen addr --token t
                              Run an opt-in local relay (default listen 127.0.0.1)

v2 is incompatible with the old DES/MD5 protocol. Existing .xml accounts
are not modified or migrated. DHT is discovery only; it does not punch NAT
and is not a chat relay. 'online' announces your public IP on the public
DHT unless --no-dht is set. Chat payloads stay end-to-end encrypted.
`, ColorBold, ColorReset, ColorYellow, ColorReset, ColorYellow, ColorReset, ColorYellow, ColorReset)
}

func (a *App) CmdNew(name string, ignoredPassword string) int {
	if ignoredPassword != "" {
		fmt.Printf("%sNote:%s the password argument is ignored; v2 keys are random and are not derived from a password.\n", ColorYellow, ColorReset)
	}
	if sib := config.HasLegacySibling(a.configPath); sib != "" {
		fmt.Printf("%sNote:%s found leftover v1 config %s (left untouched).\n", ColorYellow, ColorReset, sib)
	}
	cfg, err := config.CreateIdentity(a.configPath, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ColorRed, ColorReset, err)
		return 1
	}
	id, err := cfg.Identity()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ColorRed, ColorReset, err)
		return 1
	}
	fmt.Printf("%sUser successfully created!%s\n", ColorGreen, ColorReset)
	fmt.Printf("  Name: %s\n  Fingerprint: %s\n  Config: %s\n", id.Name, id.Public().Fingerprint(), a.configPath)
	fmt.Println("This identity is incompatible with tunnelchat v1 clients.")
	return 0
}

func (a *App) withEngine(start bool, fn func(*core.Engine) int) int {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ColorRed, ColorReset, err)
		return 1
	}
	eng, err := core.New(cfg, a.opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ColorRed, ColorReset, err)
		return 1
	}
	if start {
		if err := eng.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize network: %v\n", err)
			return 1
		}
		defer eng.Stop()
	}
	return fn(eng)
}

func (a *App) CmdInfo() int {
	return a.withEngine(true, func(eng *core.Engine) int {
		// Gather STUN/host candidates only. Do not announce to a DHT.
		time.Sleep(200 * time.Millisecond)
		s, err := eng.Invite()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ColorRed, ColorReset, err)
			return 1
		}
		u := eng.Identity().Public()
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Printf("%sMy Information:%s\n", ColorBold, ColorReset)
		fmt.Printf("  Name: %s\n  Fingerprint: %s\n", u.Name, u.Fingerprint())
		if m := eng.Mapped(); m != nil {
			fmt.Printf("  STUN mapped: %s (shared-socket mapped port)\n", m)
		}
		fmt.Printf("  Candidates: %s\n", formatCands(eng))
		fmt.Printf("\n%sShare this invite with a friend (do not post it on a public DHT or forum):%s\n\n", ColorYellow, ColorReset)
		fmt.Printf("%s%s%s\n\n", ColorGreen, s, ColorReset)
		fmt.Println("Both sides must be running 'online' at the same time. Offline delivery is not supported.")
		fmt.Println("--------------------------------------------------------------------------------")
		return 0
	})
}

func formatCands(eng *core.Engine) string {
	cs := eng.Candidates()
	if len(cs) == 0 {
		return "(none yet)"
	}
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(c.String())
	}
	return b.String()
}

func (a *App) CmdAdd(info string) int {
	return a.withEngine(false, func(eng *core.Engine) int {
		if err := eng.AddInvite(info); err != nil {
			fmt.Printf("%sFailed to add friend: %v%s\n", ColorRed, err, ColorReset)
			return 1
		}
		fmt.Printf("%sFriend added successfully!%s Their identity is now pinned. Run 'online' on both sides to connect.\n", ColorGreen, ColorReset)
		return 0
	})
}

func (a *App) CmdOnline() int {
	return a.withEngine(true, func(eng *core.Engine) int {
		eng.SetEventCallback(func(event string) {
			fmt.Printf("\r\033[K%s[Status]%s %s\ntunnelchat> ", ColorYellow, ColorReset, event)
		})
		eng.SetChatCallback(func(friend config.Friend, text string) {
			name := friend.Name
			fmt.Printf("\r\033[K%s[%s]:%s %s\ntunnelchat> ", ColorGreen, name, ColorReset, text)
		})
		u := eng.Identity().Public()
		fmt.Println("================================================================================")
		fmt.Printf("%sTunnelchat v2 online%s | User: %s%s%s | listen %s\n",
			ColorBold, ColorReset, ColorCyan, u.Name, ColorReset, "")
		fmt.Printf("Fingerprint: %s\n", u.Fingerprint())
		fmt.Println("================================================================================")
		if eng.DHTEnabled() {
			fmt.Printf("DHT discovery: on (%d bootstrap node(s)); your public IP is advertised.\n", len(eng.DHTBootstraps()))
		} else {
			fmt.Println("DHT discovery: off")
		}
		fmt.Println("Commands:  <name> <message>  /friends  /peers  /connect <fp>  /addpeer <fp>")
		fmt.Println("           /info  /add <invite>  /help  q")
		fmt.Println("Limitations: no offline inbox; DHT does not punch NAT; going online exposes")
		fmt.Println("             your IP to STUN/DHT nodes. Strangers are listed, not auto-friended.")
		fmt.Println("             Display names are signed with a key; compare fingerprints.")
		fmt.Println("--------------------------------------------------------------------------------")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		inputChan := make(chan string)
		scanner := bufio.NewScanner(os.Stdin)
		go func() {
			for {
				fmt.Print("tunnelchat> ")
				if !scanner.Scan() {
					close(inputChan)
					return
				}
				inputChan <- strings.TrimSpace(scanner.Text())
			}
		}()
		for {
			select {
			case <-sigChan:
				fmt.Printf("\n%sShutting down cleanly...%s\n", ColorYellow, ColorReset)
				return 0
			case line, ok := <-inputChan:
				if !ok {
					return 0
				}
				if line == "" {
					continue
				}
				if line == "q" || line == "quit" || line == "/quit" || line == "exit" || line == "/exit" {
					fmt.Printf("%sGoodbye!%s\n", ColorCyan, ColorReset)
					return 0
				}
				if line == "/help" {
					fmt.Println("  <name> <words>      send a message to a session")
					fmt.Println("  /friends            list pinned (trusted) friends")
					fmt.Println("  /peers or /who      list discovered online strangers")
					fmt.Println("  /connect <fp>       handshake a stranger (does not auto-friend)")
					fmt.Println("  /addpeer <fp>       pin a discovered stranger as a trusted friend")
					fmt.Println("  /info               print invite")
					fmt.Println("  /add <invite>       pin a friend from an invite")
					fmt.Println("  q                   quit")
					continue
				}
				if line == "/info" {
					s, err := eng.Invite()
					if err != nil {
						fmt.Println(err)
						continue
					}
					fmt.Printf("\n%sYour invite:%s\n%s%s%s\n\n", ColorYellow, ColorReset, ColorGreen, s, ColorReset)
					continue
				}
				if line == "/friends" || line == "/list" {
					printFriends(eng)
					continue
				}
				if line == "/peers" || line == "/who" {
					printPeers(eng)
					continue
				}
				if strings.HasPrefix(line, "/connect ") {
					fp := strings.TrimSpace(line[len("/connect "):])
					if err := eng.ConnectDiscovered(fp); err != nil {
						fmt.Printf("%s%s%s\n", ColorRed, err, ColorReset)
					} else {
						fmt.Printf("connecting to %s (not added as a trusted friend; use /addpeer to pin)\n", fp)
					}
					continue
				}
				if strings.HasPrefix(line, "/addpeer ") {
					fp := strings.TrimSpace(line[len("/addpeer "):])
					if err := eng.AddDiscovered(fp); err != nil {
						fmt.Printf("%s%s%s\n", ColorRed, err, ColorReset)
					} else {
						fmt.Printf("%sPinned as a trusted friend.%s\n", ColorGreen, ColorReset)
					}
					continue
				}
				if strings.HasPrefix(line, "/add ") {
					if err := eng.AddInvite(strings.TrimSpace(line[5:])); err != nil {
						fmt.Printf("%sFailed to add friend: %v%s\n", ColorRed, err, ColorReset)
					} else {
						fmt.Printf("%sFriend added successfully!%s\n", ColorGreen, ColorReset)
					}
					continue
				}
				parts := strings.SplitN(line, " ", 2)
				if len(parts) < 2 {
					fmt.Println("Input format: [name] [words]  or /help")
					continue
				}
				if err := eng.SendChat(parts[0], parts[1]); err != nil {
					fmt.Printf("%s[Failed]: %v%s\n", ColorRed, err, ColorReset)
				} else {
					fmt.Printf("%s[Sent to %s]:%s %s\n", ColorCyan, parts[0], ColorReset, parts[1])
				}
			}
		}
	})
}

func printFriends(eng *core.Engine) {
	friends := eng.ListFriends()
	if len(friends) == 0 {
		fmt.Println("No friends pinned yet. Use '/add <invite>'.")
		return
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("%-15s %-34s %s\n", "NAME", "FINGERPRINT", "SESSION")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, f := range friends {
		pub, err := f.Public()
		fp := "?"
		if err == nil {
			fp = pub.Fingerprint()
		}
		status := "offline/pending"
		if eng.HasSession(f.Name) {
			status = "secure"
		}
		fmt.Printf("%-15s %-34s %s\n", f.Name, fp, status)
	}
	fmt.Println("--------------------------------------------------------------------------------")
}

func printPeers(eng *core.Engine) {
	peers := eng.ListDiscovered()
	if len(peers) == 0 {
		fmt.Println("No discovered strangers yet. DHT must be on, and both sides online.")
		fmt.Println("They are not trusted friends until you /addpeer <fingerprint>.")
		return
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("%-15s %-34s %-8s %s\n", "NAME", "FINGERPRINT", "TRUSTED", "SEEN")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, d := range peers {
		fp := d.Public.Fingerprint()
		trusted := "no"
		if eng.IsTrustedFriend(fp) {
			trusted = "yes"
		}
		sess := ""
		if eng.HasSession(fp) {
			sess = " session"
		}
		fmt.Printf("%-15s %-34s %-8s %s%s\n", d.Public.Name, fp, trusted, d.Seen.Format("15:04:05"), sess)
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("Names are signed by that key, but anyone can pick any display name. Compare fingerprints.")
	fmt.Println("Use /connect <fingerprint> to handshake, /addpeer <fingerprint> to pin as a friend.")
}

func CmdRelay(listen, token string, maxSessions int) int {
	if token == "" {
		fmt.Fprintln(os.Stderr, "relay requires --token")
		return 1
	}
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	srv, err := relay.Listen(relay.Config{Listen: listen, Token: token, MaxSessions: maxSessions})
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay listen: %v\n", err)
		return 1
	}
	defer srv.Close()
	fmt.Printf("Relay listening on %s (opaque ciphertext only; Ctrl-C to stop)\n", srv.Addr())
	fmt.Println("Do not expose this without understanding that IPs of users will be visible to the operator.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("relay stopped")
	return 0
}

package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"tunnelchat/pkg/config"
	"tunnelchat/pkg/core"
)

// ANSI color codes for terminal styling
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

type App struct {
	engine     *core.Engine
	configPath string
}

func NewApp(cfg *config.Config, configPath string) (*App, error) {
	engine, err := core.NewEngine(cfg, configPath)
	if err != nil {
		return nil, err
	}
	return &App{
		engine:     engine,
		configPath: configPath,
	}, nil
}

// PrintUsage prints command line usage.
func PrintUsage() {
	fmt.Printf(`%sTunnelchat (Go Edition)%s - Serverless P2P Encrypted Chat

%sUsage:%s
    tunnelchat <command> [arguments]

%sCommands:%s
    %snew <name> <password>%s
        Create or overwrite user account with given name and password
    %sinfo%s
        Display connection string for friends to add you
    %sadd <info>%s
        Add a friend using their encrypted connection info string
    %sonline%s
        Enter interactive Linux terminal chat mode
    %shelp%s
        Show this help message

`, ColorBold, ColorReset, ColorYellow, ColorReset, ColorYellow, ColorReset,
		ColorGreen, ColorReset,
		ColorGreen, ColorReset,
		ColorGreen, ColorReset,
		ColorGreen, ColorReset,
		ColorGreen, ColorReset)
}

func (a *App) CmdNew(name, pwd string) {
	user := a.engine.GetUser()
	if user.Acc != "" {
		fmt.Printf("%sWarning:%s User %q already exists (%s). Overwrite? (y/n): ", ColorYellow, ColorReset, user.Name, user.Acc)
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans != "y" && ans != "yes" {
			fmt.Println("Cancelled.")
			return
		}
	}

	a.engine.CreateUser(name, pwd)
	u := a.engine.GetUser()
	fmt.Printf("%sUser successfully created!%s\n", ColorGreen, ColorReset)
	fmt.Printf("  Name: %s\n  Account: %s\n  Port: %d\n", u.Name, u.Acc, u.Port)
	fmt.Println("Run 'tunnelchat online' or 'tunnelchat info' to start chatting.")
}

func (a *App) CmdInfo() {
	user := a.engine.GetUser()
	if user.Acc == "" {
		fmt.Println("No user found. Please create one first using 'tunnelchat new <name> <pwd>'.")
		return
	}

	// We start the engine briefly to ensure local address and STUN mapped address are resolved
	if err := a.engine.Start(); err != nil {
		fmt.Printf("Failed to initialize network: %v\n", err)
		return
	}
	defer a.engine.Stop()

	info := a.engine.GetInfoString()
	u := a.engine.GetUser()
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("%sMy Information:%s\n", ColorBold, ColorReset)
	fmt.Printf("  Name: %s\n  Account: %s\n  Address: %s:%d\n", u.Name, u.Acc, u.IP, u.Port)
	fmt.Printf("\n%sShare this info string with your friends to add you:%s\n\n", ColorYellow, ColorReset)
	fmt.Printf("%s%s%s\n\n", ColorGreen, info, ColorReset)
	fmt.Println("--------------------------------------------------------------------------------")
}

func (a *App) CmdAdd(infoStr string) {
	user := a.engine.GetUser()
	if user.Acc == "" {
		fmt.Println("No user found. Please create one first using 'tunnelchat new <name> <pwd>'.")
		return
	}

	if err := a.engine.Start(); err != nil {
		fmt.Printf("Failed to initialize network: %v\n", err)
		return
	}
	defer a.engine.Stop()

	fmt.Println("Attempting handshake with peer...")
	if err := a.engine.AddFriendByInfo(infoStr); err != nil {
		fmt.Printf("%sFailed to add friend: %v%s\n", ColorRed, err, ColorReset)
		return
	}

	fmt.Printf("%sFriend added successfully!%s\n", ColorGreen, ColorReset)
}

func (a *App) CmdOnline() {
	user := a.engine.GetUser()
	if user.Acc == "" {
		fmt.Println("No user profile found. Please create one first:")
		fmt.Println("  tunnelchat new <name> <password>")
		return
	}

	// Handle status / log callbacks
	a.engine.SetEventCallback(func(event string) {
		fmt.Printf("\r\033[K%s[Status]%s %s\ntunnelchat> ", ColorYellow, ColorReset, event)
	})

	// Handle incoming chat messages
	a.engine.SetChatCallback(func(friend config.Friend, text string) {
		name := friend.Name
		if name == "" {
			name = friend.Acc
		}
		fmt.Printf("\r\033[K%s[%s]:%s %s\ntunnelchat> ", ColorGreen, name, ColorReset, text)
	})

	fmt.Printf("%sConnecting to P2P network...%s\n", ColorCyan, ColorReset)
	if err := a.engine.Start(); err != nil {
		fmt.Printf("%sNetwork startup error: %v%s\n", ColorRed, err, ColorReset)
		return
	}
	defer a.engine.Stop()

	u := a.engine.GetUser()
	fmt.Println("================================================================================")
	fmt.Printf("%sTunnelchat P2P Online%s | User: %s%s%s | Address: %s:%d\n",
		ColorBold, ColorReset, ColorCyan, u.Name, ColorReset, u.IP, u.Port)
	fmt.Println("================================================================================")
	fmt.Println("Commands:")
	fmt.Println("  <name> <message>       Send message to friend (e.g. 'bob hello!')")
	fmt.Println("  /friends or /list      Show friends list and status")
	fmt.Println("  /info                  Show your connection info string")
	fmt.Println("  /add <info>            Add a friend using their info string")
	fmt.Println("  /help                  Show help")
	fmt.Println("  q or /quit             Exit tunnelchat")
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
			return

		case line, ok := <-inputChan:
			if !ok {
				return
			}
			if line == "" {
				continue
			}

			if line == "q" || line == "quit" || line == "/quit" || line == "exit" || line == "/exit" {
				fmt.Printf("%sGoodbye!%s\n", ColorCyan, ColorReset)
				return
			}

			if line == "/help" {
				fmt.Println("Commands:")
				fmt.Println("  <name> <words>         Send chat message to friend")
				fmt.Println("  /friends or /list      List friends")
				fmt.Println("  /info                  Show own info string")
				fmt.Println("  /add <info>            Add friend")
				fmt.Println("  q                      Quit")
				continue
			}

			if line == "/info" {
				info := a.engine.GetInfoString()
				fmt.Printf("\n%sYour info string:%s\n%s%s%s\n\n", ColorYellow, ColorReset, ColorGreen, info, ColorReset)
				continue
			}

			if line == "/friends" || line == "/list" {
				friends := a.engine.GetUser()
				_ = friends
				// Access via engine's config
				cfgList := a.engine.GetInfoString()
				_ = cfgList
				a.printFriendsList()
				continue
			}

			if strings.HasPrefix(line, "/add ") {
				infoStr := strings.TrimSpace(line[5:])
				if infoStr == "" {
					fmt.Println("Usage: /add <info_string>")
					continue
				}
				fmt.Println("Adding friend...")
				if err := a.engine.AddFriendByInfo(infoStr); err != nil {
					fmt.Printf("%sFailed to add friend: %v%s\n", ColorRed, err, ColorReset)
				} else {
					fmt.Printf("%sFriend added successfully!%s\n", ColorGreen, ColorReset)
				}
				continue
			}

			// Format: <friend_name> <words>
			parts := strings.SplitN(line, " ", 2)
			if len(parts) < 2 {
				fmt.Println("Input format: [name] [words] to chat (e.g. 'alice hello') or /help")
				continue
			}

			targetName := parts[0]
			message := parts[1]

			if err := a.engine.SendChatMessage(targetName, message); err != nil {
				fmt.Printf("%s[Failed]: %v%s\n", ColorRed, err, ColorReset)
			} else {
				fmt.Printf("%s[Sent to %s]:%s %s\n", ColorCyan, targetName, ColorReset, message)
			}
		}
	}
}

func (a *App) printFriendsList() {
	// Re-read friends list from engine config
	u := a.engine.GetUser()
	_ = u
	// Let's get friends from config
	cfg, err := config.LoadConfig(a.configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	friends := cfg.ListFriends()
	if len(friends) == 0 {
		fmt.Println("No friends added yet. Use '/add <info>' to add one.")
		return
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("%-15s %-34s %-22s %-10s\n", "NAME", "ACCOUNT", "ADDRESS", "STATUS")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, f := range friends {
		status := "Ready"
		if f.SKey == "" {
			status = "Needs Key"
		}
		addr := fmt.Sprintf("%s:%d", f.IP, f.Port)
		fmt.Printf("%-15s %-34s %-22s %-10s\n", f.Name, f.Acc, addr, status)
	}
	fmt.Println("--------------------------------------------------------------------------------")
}

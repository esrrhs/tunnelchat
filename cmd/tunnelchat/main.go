package main

import (
	"flag"
	"fmt"
	"os"

	"tunnelchat/pkg/cli"
	"tunnelchat/pkg/core"
)

type stringList []string

func (s *stringList) String() string { return fmt.Sprint(*s) }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("tunnelchat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		configPath  string
		dhtEnable   bool
		noDHT       bool
		noSTUN      bool
		relayServer string
		relayToken  string
		listen      string
		maxSess     int
		boot        stringList
	)
	fs.StringVar(&configPath, "c", "tunnelchat.json", "Path to configuration file")
	fs.BoolVar(&dhtEnable, "dht", false, "Legacy flag; DHT is already on by default for 'online'")
	fs.BoolVar(&noDHT, "no-dht", false, "Disable DHT discovery and public swarm announce")
	fs.Var(&boot, "dht-bootstrap", "Override DHT bootstrap host:port (repeatable)")
	fs.BoolVar(&noSTUN, "no-stun", false, "Do not query STUN servers")
	fs.StringVar(&relayServer, "relay-server", "", "Optional relay host:port")
	fs.StringVar(&relayToken, "relay-token", "", "Relay access token")
	fs.StringVar(&listen, "listen", "127.0.0.1:0", "Listen address for 'relay' command")
	fs.IntVar(&maxSess, "max-sessions", 32, "Max relay allocations")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		cli.PrintUsage()
		return 0
	}
	opts := core.Options{
		DHTDisabled:          noDHT,
		DHTEnabled:           dhtEnable && !noDHT,
		UseDefaultBootstraps: false, // set per-command; only online uses defaults
		DHTBootstrap:         append([]string(nil), boot...),
		DisableSTUN:          noSTUN,
		RelayServer:          relayServer,
		RelayToken:           relayToken,
	}
	_ = dhtEnable
	app := cli.NewApp(configPath, opts)
	cmd := rest[0]
	switch cmd {
	case "new":
		if len(rest) < 2 {
			fmt.Println("Usage: tunnelchat new <name> [ignored-password]")
			return 1
		}
		pwd := ""
		if len(rest) >= 3 {
			pwd = rest[2]
		}
		return app.CmdNew(rest[1], pwd)
	case "info":
		return app.CmdInfo()
	case "add":
		if len(rest) < 2 {
			fmt.Println("Usage: tunnelchat add <invite>")
			return 1
		}
		return app.CmdAdd(rest[1])
	case "online":
		if !noDHT {
			opts.UseDefaultBootstraps = len(boot) == 0
			app = cli.NewApp(configPath, opts)
		}
		return app.CmdOnline()
	case "relay":
		return cli.CmdRelay(listen, relayToken, maxSess)
	case "help", "-h", "--help":
		cli.PrintUsage()
		return 0
	default:
		fmt.Printf("Unknown command: %q\n\n", cmd)
		cli.PrintUsage()
		return 1
	}
}

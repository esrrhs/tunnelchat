package main

import (
	"flag"
	"fmt"
	"os"

	"tunnelchat/pkg/cli"
	"tunnelchat/pkg/config"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "c", "tunnelchat.xml", "Path to configuration file")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		cli.PrintUsage()
		os.Exit(0)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration (%s): %v\n", configPath, err)
		os.Exit(1)
	}

	app, err := cli.NewApp(cfg, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing application: %v\n", err)
		os.Exit(1)
	}

	cmd := args[0]
	switch cmd {
	case "new":
		if len(args) < 3 {
			fmt.Println("Usage: tunnelchat new <name> <password>")
			os.Exit(1)
		}
		app.CmdNew(args[1], args[2])

	case "info":
		app.CmdInfo()

	case "add":
		if len(args) < 2 {
			fmt.Println("Usage: tunnelchat add <info>")
			os.Exit(1)
		}
		app.CmdAdd(args[1])

	case "online":
		app.CmdOnline()

	case "help", "-h", "--help":
		cli.PrintUsage()

	default:
		fmt.Printf("Unknown command: %q\n\n", cmd)
		cli.PrintUsage()
		os.Exit(1)
	}
}

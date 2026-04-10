package main

import (
	"fmt"
	"os"
)

const defaultServer = "http://localhost:4000"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "export":
		runExport(args)
	case "apply":
		runApply(args)
	case "get":
		runGet(args)
	case "diff":
		runDiff(args)
	case "validate":
		runValidate(args)
	case "help", "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Println("tentactl v0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`tentactl — configuration management CLI for tentacle

Usage:
  tentactl <command> [flags]

Commands:
  export     Export current configuration as YAML
  apply      Apply a YAML manifest to the system
  get        Get resources by kind
  diff       Compare a manifest against the current state
  validate   Validate a manifest without applying

Flags:
  -s <url>   Server URL (default: http://localhost:4000 or $TENTACLE_SERVER)
  -f <file>  Manifest file (- for stdin)
  -k <kinds> Comma-separated resource kinds to filter

Examples:
  tentactl export > site.yaml
  tentactl apply -f site.yaml
  tentactl diff -f site.yaml
  tentactl get services
  tentactl validate -f site.yaml
`)
}

func getServer(args []string) (string, []string) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-s" {
			server := args[i+1]
			remaining := append(args[:i], args[i+2:]...)
			return server, remaining
		}
	}
	if s := os.Getenv("TENTACLE_SERVER"); s != "" {
		return s, args
	}
	return defaultServer, args
}

func getFlag(args []string, flag string) (string, []string) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			val := args[i+1]
			remaining := append(args[:i], args[i+2:]...)
			return val, remaining
		}
	}
	return "", args
}

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/joyautomation/tentacle/internal/manifest"
)

func runDiff(args []string) {
	server, args := getServer(args)
	file, _ := getFlag(args, "-f")

	if file == "" {
		fmt.Fprintln(os.Stderr, "error: -f <file> is required (use - for stdin)")
		os.Exit(1)
	}

	data, err := readFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	c := newClient(server)
	resp, err := c.postYAML("/diff", data, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var result manifest.DiffResult
	if err := json.Unmarshal(resp, &result); err != nil {
		os.Stdout.Write(resp)
		fmt.Println()
		return
	}

	if len(result.Changes) == 0 {
		fmt.Println("no changes")
		return
	}

	for _, ch := range result.Changes {
		switch ch.Action {
		case "create":
			fmt.Printf("+ %s/%s (new)\n", ch.Kind, ch.Name)
		case "update":
			fmt.Printf("~ %s/%s (%s)\n", ch.Kind, ch.Name, ch.Detail)
		case "unchanged":
			fmt.Printf("  %s/%s (unchanged)\n", ch.Kind, ch.Name)
		}
	}
}

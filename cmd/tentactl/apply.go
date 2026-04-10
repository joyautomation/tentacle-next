package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/joyautomation/tentacle/internal/manifest"
)

func runApply(args []string) {
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
	resp, err := c.postYAML("/apply", data, "cli")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var result manifest.ApplyResult
	if err := json.Unmarshal(resp, &result); err != nil {
		// Print raw response if can't parse.
		os.Stdout.Write(resp)
		fmt.Println()
		return
	}

	for _, r := range result.Applied {
		fmt.Printf("%s/%s applied\n", r.Kind, r.Name)
	}
	for _, s := range result.Skipped {
		fmt.Printf("%s/%s skipped (%s)\n", s.Kind, s.Name, s.Reason)
	}
}

func readFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func runValidate(args []string) {
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
	resp, err := c.postYAML("/validate", data, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var result struct {
		Valid     bool     `json:"valid"`
		Resources int      `json:"resources"`
		Errors    []string `json:"errors"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		os.Stdout.Write(resp)
		fmt.Println()
		return
	}

	if result.Valid {
		fmt.Printf("valid (%d resources)\n", result.Resources)
	} else {
		fmt.Println("invalid:")
		for _, e := range result.Errors {
			fmt.Printf("  - %s\n", e)
		}
		os.Exit(1)
	}
}

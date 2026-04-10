package main

import (
	"fmt"
	"os"
	"strings"
)

var kindAliases = map[string]string{
	"gateways":      "Gateway",
	"gateway":       "Gateway",
	"gw":            "Gateway",
	"services":      "Service",
	"service":       "Service",
	"svc":           "Service",
	"config":        "ModuleConfig",
	"moduleconfig":  "ModuleConfig",
	"configs":       "ModuleConfig",
	"nftables":      "Nftables",
	"nft":           "Nftables",
	"network":       "Network",
	"net":           "Network",
}

func runGet(args []string) {
	server, args := getServer(args)

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: specify a resource kind (gateways, services, config, nftables, network)")
		os.Exit(1)
	}

	input := strings.ToLower(args[0])
	kind, ok := kindAliases[input]
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown resource kind %q\n", args[0])
		fmt.Fprintln(os.Stderr, "available: gateways, services, config, nftables, network")
		os.Exit(1)
	}

	c := newClient(server)
	data, err := c.get("/export?kind=" + kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.Write(data)
}

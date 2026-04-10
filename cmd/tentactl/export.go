package main

import (
	"fmt"
	"os"
)

func runExport(args []string) {
	server, args := getServer(args)
	kinds, _ := getFlag(args, "-k")

	c := newClient(server)
	path := "/export"
	if kinds != "" {
		path += "?kind=" + kinds
	}

	data, err := c.get(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.Write(data)
}

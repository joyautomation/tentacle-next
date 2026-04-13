//go:build all || stable

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/joyautomation/tentacle/internal/service"
)

func runServiceCommand(args []string) {
	if len(args) == 0 {
		printServiceUsage()
		os.Exit(1)
	}

	log := slog.Default()

	switch args[0] {
	case "status":
		s := service.GetStatus("cli")
		data, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(data))

	case "install":
		if err := service.Install(log); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			os.Exit(1)
		}
		fmt.Println("Service installed and enabled.")
		fmt.Println("Start it with: sudo systemctl start tentacle")

	case "uninstall":
		removeBin := false
		for _, a := range args[1:] {
			if a == "--remove-binary" {
				removeBin = true
			}
		}
		if err := service.Uninstall(removeBin, log); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			os.Exit(1)
		}
		fmt.Println("Service uninstalled.")

	default:
		fmt.Fprintf(os.Stderr, "unknown service command: %s\n", args[0])
		printServiceUsage()
		os.Exit(1)
	}
}

func printServiceUsage() {
	fmt.Fprintln(os.Stderr, "Usage: tentacle service <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  status      Show service installation status")
	fmt.Fprintln(os.Stderr, "  install     Install tentacle as a systemd service")
	fmt.Fprintln(os.Stderr, "  uninstall   Remove the systemd service")
}

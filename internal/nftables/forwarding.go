//go:build nftables || all

package nftables

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	sysctlConfDir  = "/etc/sysctl.d"
	sysctlConfFile = "60-tentacle.conf"
	sysctlContent  = "net.ipv4.ip_forward = 1\n"
)

// enableForwarding enables IPv4 forwarding immediately via sysctl and persists
// the setting to /etc/sysctl.d/60-tentacle.conf.
func enableForwarding(log *slog.Logger) error {
	// Enable immediately.
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Error("nftables: failed to enable ip forwarding", "error", err, "output", string(out))
		return err
	}
	log.Info("nftables: IPv4 forwarding enabled")

	// Persist to disk.
	confPath := filepath.Join(sysctlConfDir, sysctlConfFile)
	if err := os.MkdirAll(sysctlConfDir, 0755); err != nil {
		log.Warn("nftables: failed to create sysctl.d directory", "error", err)
		return err
	}
	if err := os.WriteFile(confPath, []byte(sysctlContent), 0644); err != nil {
		log.Warn("nftables: failed to persist sysctl config", "path", confPath, "error", err)
		return err
	}
	log.Info("nftables: sysctl config persisted", "path", confPath)
	return nil
}

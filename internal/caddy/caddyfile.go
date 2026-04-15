//go:build caddy || all

package caddy

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

const caddyfilePath = "/etc/caddy/Caddyfile"

// generateCaddyfile creates a simple reverse-proxy Caddyfile from structured config.
func generateCaddyfile(domain, upstreamPort string) string {
	return fmt.Sprintf("%s {\n\treverse_proxy localhost:%s\n}\n", domain, upstreamPort)
}

// resolveCaddyfile returns the Caddyfile content based on current config.
// In advanced mode it uses the raw Caddyfile; in simple mode it generates one.
func resolveCaddyfile(cfg caddyConfig) string {
	if cfg.AdvancedMode && cfg.Caddyfile != "" {
		return cfg.Caddyfile
	}
	return generateCaddyfile(cfg.Domain, cfg.UpstreamPort)
}

// writeCaddyfile writes the given content to /etc/caddy/Caddyfile.
func writeCaddyfile(content string, log *slog.Logger) error {
	if err := os.MkdirAll("/etc/caddy", 0755); err != nil {
		return fmt.Errorf("create /etc/caddy: %w", err)
	}
	if err := os.WriteFile(caddyfilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	log.Info("caddy: wrote Caddyfile", "path", caddyfilePath)
	return nil
}

// reloadCaddy asks systemd to reload the caddy service so it picks up the new Caddyfile.
func reloadCaddy(log *slog.Logger) error {
	out, err := exec.Command("systemctl", "reload", "caddy").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl reload caddy: %s (%w)", string(out), err)
	}
	log.Info("caddy: reloaded")
	return nil
}

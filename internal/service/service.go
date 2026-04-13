package service

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/systemd"
)

const (
	UnitName   = "tentacle.service"
	BinaryPath = "/usr/local/bin/tentacle"
	SystemdDir = "/etc/systemd/system"
)

// Status describes the current service installation state.
type Status struct {
	Mode             string `json:"mode"`
	SystemdAvailable bool   `json:"systemdAvailable"`
	UnitExists       bool   `json:"unitExists"`
	UnitEnabled      bool   `json:"unitEnabled"`
	UnitActive       bool   `json:"unitActive"`
	BinaryInstalled  bool   `json:"binaryInstalled"`
	CanInstall       bool   `json:"canInstall"`
	Reason           string `json:"reason,omitempty"`
}

// GetStatus probes the system and returns current service state.
func GetStatus(mode string) Status {
	s := Status{Mode: mode}

	if !systemd.Available() {
		s.Reason = "systemd is not available on this system"
		return s
	}
	s.SystemdAvailable = true

	s.UnitExists = systemd.UnitExists("tentacle")
	s.UnitEnabled = systemd.IsEnabled(UnitName)
	s.UnitActive = systemd.GetState(UnitName) == "active"

	if _, err := os.Stat(BinaryPath); err == nil {
		s.BinaryInstalled = true
	}

	if mode == "systemd" {
		s.Reason = "already running as a systemd service"
		return s
	}

	if unix.Access(SystemdDir, unix.W_OK) != nil {
		s.Reason = "insufficient permissions — run with sudo or as root"
		return s
	}

	s.CanInstall = true
	return s
}

// Install copies the binary, writes the unit file, and enables the service.
func Install(log *slog.Logger) error {
	if !systemd.Available() {
		return fmt.Errorf("systemd is not available")
	}
	if unix.Access(SystemdDir, unix.W_OK) != nil {
		return fmt.Errorf("insufficient permissions to write to %s", SystemdDir)
	}

	// Resolve current binary path.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve executable symlinks: %w", err)
	}

	// Copy binary to /usr/local/bin if not already there.
	if exe != BinaryPath {
		log.Info("copying binary", "from", exe, "to", BinaryPath)
		if err := copyFile(exe, BinaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	} else {
		log.Info("binary already at install path", "path", BinaryPath)
	}

	// Write environment file.
	envPath := filepath.Join(paths.DataDir(), "tentacle.env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	envContent := buildEnvFile()
	if err := os.WriteFile(envPath, envContent, 0o644); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}
	log.Info("wrote environment file", "path", envPath)

	// Write unit file.
	unitPath := filepath.Join(SystemdDir, UnitName)
	unitContent := generateUnit(envPath)
	if err := os.WriteFile(unitPath, []byte(unitContent), 0o644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}
	log.Info("wrote unit file", "path", unitPath)

	// Reload and enable.
	if !systemd.DaemonReload() {
		return fmt.Errorf("systemctl daemon-reload failed")
	}
	if !systemd.Enable(UnitName) {
		return fmt.Errorf("systemctl enable failed")
	}
	log.Info("service installed and enabled")
	return nil
}

// Activate writes a fire-and-forget script that starts the service after
// the current process exits. The caller should exit shortly after calling this.
func Activate(log *slog.Logger) error {
	if !systemd.UnitExists("tentacle") {
		return fmt.Errorf("unit file not found — run install first")
	}

	script := `#!/bin/bash
set -e
sleep 2
systemctl start tentacle.service
rm -f "$0"
`
	scriptPath := "/tmp/tentacle-activate.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("failed to write activation script: %w", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start activation script: %w", err)
	}

	log.Info("activation script launched — service will start in ~2s")
	return nil
}

// Uninstall stops, disables, and removes the service.
func Uninstall(removeBinary bool, log *slog.Logger) error {
	if systemd.GetState(UnitName) == "active" {
		systemd.Stop(UnitName)
	}
	systemd.Disable(UnitName)

	unitPath := filepath.Join(SystemdDir, UnitName)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}
	systemd.DaemonReload()

	if removeBinary {
		if err := os.Remove(BinaryPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove binary: %w", err)
		}
	}

	log.Info("service uninstalled")
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func buildEnvFile() []byte {
	var lines []string
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(key, "TENTACLE_") || key == "API_PORT" {
			lines = append(lines, env)
		}
	}
	if len(lines) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func generateUnit(envFilePath string) string {
	return fmt.Sprintf(`[Unit]
Description=Tentacle IoT Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-%s
ExecStart=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tentacle

[Install]
WantedBy=multi-user.target
`, envFilePath, BinaryPath)
}

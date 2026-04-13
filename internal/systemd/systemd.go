package systemd

import (
	"os"
	"os/exec"
	"strings"
)

// Available returns true if systemctl is on PATH.
func Available() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// RunCmd executes a command with a full PATH and returns stdout, stderr, and success.
func RunCmd(name string, args ...string) (stdout, stderr string, ok bool) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), err == nil
}

// UnitExists checks whether a systemd unit is loaded.
func UnitExists(unit string) bool {
	stdout, _, _ := RunCmd("systemctl", "show", "--property=LoadState", unit)
	return strings.Contains(stdout, "LoadState=loaded")
}

// GetState returns the is-active state for a unit.
func GetState(unit string) string {
	stdout, _, _ := RunCmd("systemctl", "is-active", unit)
	switch stdout {
	case "active", "inactive", "failed", "activating", "deactivating":
		return stdout
	default:
		return "not-found"
	}
}

// IsEnabled returns true if the unit is enabled.
func IsEnabled(unit string) bool {
	stdout, _, _ := RunCmd("systemctl", "is-enabled", unit)
	return stdout == "enabled"
}

// DaemonReload runs systemctl daemon-reload.
func DaemonReload() bool {
	_, _, ok := RunCmd("systemctl", "daemon-reload")
	return ok
}

// Enable enables a unit.
func Enable(unit string) bool {
	_, _, ok := RunCmd("systemctl", "enable", unit)
	return ok
}

// Disable disables a unit.
func Disable(unit string) bool {
	_, _, ok := RunCmd("systemctl", "disable", unit)
	return ok
}

// Start starts a unit.
func Start(unit string) bool {
	_, _, ok := RunCmd("systemctl", "start", unit)
	return ok
}

// Stop stops a unit.
func Stop(unit string) bool {
	_, _, ok := RunCmd("systemctl", "stop", unit)
	return ok
}

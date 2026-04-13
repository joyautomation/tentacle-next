package paths

import (
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	once    sync.Once
	dataDir string
)

// DataDir returns the resolved data directory for tentacle.
// Priority: TENTACLE_DATA_DIR env > /var/lib/tentacle (if writable) > ~/.local/share/tentacle.
func DataDir() string {
	once.Do(func() {
		if v := os.Getenv("TENTACLE_DATA_DIR"); v != "" {
			dataDir = v
			return
		}

		const systemDir = "/var/lib/tentacle"
		// Check if we can write to the system directory (or create it).
		if unix.Access(systemDir, unix.W_OK) == nil {
			dataDir = systemDir
			return
		}
		// Try creating it — works if parent is writable (e.g. running as root first time).
		if err := os.MkdirAll(systemDir, 0o755); err == nil {
			dataDir = systemDir
			return
		}

		// Fall back to user-local directory.
		home, err := os.UserHomeDir()
		if err != nil {
			// Last resort.
			dataDir = systemDir
			return
		}
		dataDir = filepath.Join(home, ".local", "share", "tentacle")
	})
	return dataDir
}

// SSHKeyPath returns the default SSH key path within the data directory.
func SSHKeyPath() string {
	return filepath.Join(DataDir(), ".ssh", "id_ed25519")
}

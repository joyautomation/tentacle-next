//go:build caddy || all

package caddy

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

// ensureInstalled checks whether caddy is on the PATH and installs it via
// the official Caddy apt repository if it is missing.
func ensureInstalled(log *slog.Logger) error {
	if _, err := exec.LookPath("caddy"); err == nil {
		log.Info("caddy: already installed")
		return nil
	}

	log.Info("caddy: not found, installing via apt")

	steps := []struct {
		label string
		fn    func() error
	}{
		{"install prerequisites", installPrereqs},
		{"add Caddy GPG key", addGPGKey},
		{"add Caddy apt source", addAptSource},
		{"apt-get update", aptUpdate},
		{"apt-get install caddy", aptInstallCaddy},
	}

	for _, s := range steps {
		log.Info("caddy: "+s.label, "step", s.label)
		if err := s.fn(); err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
	}

	log.Info("caddy: installation complete")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func installPrereqs() error {
	return run("apt-get", "install", "-y",
		"debian-keyring", "debian-archive-keyring", "apt-transport-https", "curl")
}

func addGPGKey() error {
	// Download GPG key and dearmor into keyring
	cmd := exec.Command("bash", "-c",
		`curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | `+
			`gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg`)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func addAptSource() error {
	cmd := exec.Command("bash", "-c",
		`curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | `+
			`tee /etc/apt/sources.list.d/caddy-stable.list`)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func aptUpdate() error {
	return run("apt-get", "update")
}

func aptInstallCaddy() error {
	return run("apt-get", "install", "-y", "caddy")
}

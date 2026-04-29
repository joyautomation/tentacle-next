//go:build api || all

package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/topics"
	"golang.org/x/crypto/ssh"
)

// handleGitCheck checks whether git is installed on the system.
// GET /api/v1/gitops/git-check
func (m *Module) handleGitCheck(w http.ResponseWriter, _ *http.Request) {
	_, err := exec.LookPath("git")
	writeJSON(w, http.StatusOK, map[string]bool{"installed": err == nil})
}

// handleGitInstall attempts to install git via the system package manager.
// POST /api/v1/gitops/git-install
func (m *Module) handleGitInstall(w http.ResponseWriter, _ *http.Request) {
	// Already installed?
	if _, err := exec.LookPath("git"); err == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "git is already installed"})
		return
	}

	// Try common package managers in order.
	type pm struct {
		check string
		args  []string
	}
	managers := []pm{
		{"apt-get", []string{"apt-get", "install", "-y", "git"}},
		{"dnf", []string{"dnf", "install", "-y", "git"}},
		{"yum", []string{"yum", "install", "-y", "git"}},
		{"apk", []string{"apk", "add", "git"}},
		{"pacman", []string{"pacman", "-S", "--noconfirm", "git"}},
	}

	for _, p := range managers {
		if _, err := exec.LookPath(p.check); err != nil {
			continue
		}
		cmd := exec.Command(p.args[0], p.args[1:]...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("%s failed: %s", p.check, strings.TrimSpace(stderr.String())),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": false,
		"error":   "no supported package manager found (tried apt-get, dnf, yum, apk, pacman)",
	})
}

// handleGetSSHKey returns the public SSH key at the configured path.
// GET /api/v1/gitops/ssh-key
func (m *Module) handleGetSSHKey(w http.ResponseWriter, r *http.Request) {
	keyPath := m.getGitopsSSHKeyPath()

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"exists":    false,
				"publicKey": "",
				"path":      keyPath,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read public key: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exists":    true,
		"publicKey": strings.TrimSpace(string(pubBytes)),
		"path":      keyPath,
	})
}

// handleGenerateSSHKey generates a new ed25519 SSH key pair.
// POST /api/v1/gitops/ssh-key/generate
func (m *Module) handleGenerateSSHKey(w http.ResponseWriter, r *http.Request) {
	keyPath := m.getGitopsSSHKeyPath()

	// Create directory if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create key directory: "+err.Error())
		return
	}

	// Generate ed25519 key pair.
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate key: "+err.Error())
		return
	}

	// Marshal private key to OpenSSH PEM format.
	privPEM, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal private key: "+err.Error())
		return
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(privPEM), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write private key: "+err.Error())
		return
	}

	// Marshal public key to authorized_keys format.
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal public key: "+err.Error())
		return
	}

	hostname, _ := os.Hostname()
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " tentacle@" + hostname

	if err := os.WriteFile(keyPath+".pub", []byte(pubLine+"\n"), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write public key: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exists":    true,
		"publicKey": pubLine,
		"path":      keyPath,
	})
}

// handleTestGitConnection tests SSH connectivity to a git remote.
// POST /api/v1/gitops/test-connection
func (m *Module) handleTestGitConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoURL string `json:"repoUrl"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if body.RepoURL == "" {
		writeError(w, http.StatusBadRequest, "repoUrl is required")
		return
	}

	keyPath := m.getGitopsSSHKeyPath()

	cmd := exec.Command("git", "ls-remote", body.RepoURL)
	if keyPath != "" {
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10", keyPath),
		)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   errMsg,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// handleGetHostname returns the device hostname.
// GET /api/v1/system/hostname
func (m *Module) handleGetHostname(w http.ResponseWriter, _ *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get hostname: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"hostname": hostname})
}

// handleStreamGitopsApplied forwards GitOpsApplied bus events as SSE so the
// edge web UI can call invalidateAll() after a remote-driven config change.
// GET /api/v1/gitops/applied/stream
func (m *Module) handleStreamGitopsApplied(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	sub, err := m.bus.Subscribe(topics.GitOpsApplied, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("applied", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// getGitopsSSHKeyPath resolves the SSH key path from KV config or default.
func (m *Module) getGitopsSSHKeyPath() string {
	if data, _, err := m.bus.KVGet("tentacle_config", "gitops.GITOPS_SSH_KEY_PATH"); err == nil && len(data) > 0 {
		return string(data)
	}
	return paths.SSHKeyPath()
}

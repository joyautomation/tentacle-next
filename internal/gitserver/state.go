//go:build gitserver || gitopsserver || mantle || all

package gitserver

import (
	"sync"

	"github.com/joyautomation/tentacle/internal/gitops"
)

// instance holds the running gitserver module so the API package's HTTP
// routes can look up the server without a hard import dependency on the
// orchestrator. Set by the module's Start, cleared by Stop.
var (
	mu       sync.RWMutex
	instance *Module
)

func register(m *Module) {
	mu.Lock()
	defer mu.Unlock()
	instance = m
}

func unregister(m *Module) {
	mu.Lock()
	defer mu.Unlock()
	if instance == m {
		instance = nil
	}
}

// Get returns the running gitserver module, or nil if not enabled. Callers
// (typically the API package's git routes) must handle nil by returning a
// 503 / "remote configuration unavailable" error.
func Get() *Module {
	mu.RLock()
	defer mu.RUnlock()
	return instance
}

// Server returns the running gitops.Server, or nil if the module isn't enabled.
func Server() *gitops.Server {
	m := Get()
	if m == nil {
		return nil
	}
	return m.Server()
}

// Store returns the running RepoStore, or nil if the module isn't enabled.
func Store() *gitops.RepoStore {
	m := Get()
	if m == nil {
		return nil
	}
	return m.Store()
}

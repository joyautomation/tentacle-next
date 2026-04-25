//go:build gitserver || gitopsserver || mantle || all

// Package gitserver wraps the gitops smart-HTTP server as an orchestrated
// tentacle module. It exists so that mantle deployments can run the git
// server (and only mantle deployments do) gated by orchestrator desired
// state rather than build tags — `all` builds compile the code in but
// the module only starts when explicitly enabled.
//
// The module owns the singleton *gitops.Server; the API module's HTTP
// routes look it up via Get() and refuse to serve if the module isn't
// running.
package gitserver

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/gitops"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/paths"
)

const ServiceType = "gitserver"

type Module struct {
	moduleID string
	log      *slog.Logger

	mu     sync.Mutex
	srv    *gitops.Server
	store  *gitops.RepoStore
	stopHB func()
}

func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "gitserver"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return ServiceType }

func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.log = slog.Default().With("serviceType", ServiceType, "moduleID", m.moduleID)

	rootDir := filepath.Join(paths.DataDir(), "gitops", "server")
	srv := gitops.NewServer(rootDir)
	store := gitops.NewRepoStore(srv)

	m.mu.Lock()
	m.srv = srv
	m.store = store
	m.mu.Unlock()

	register(m)
	m.log.Info("gitserver: started", "rootDir", rootDir)

	m.stopHB = heartbeat.Start(b, m.moduleID, ServiceType, func() map[string]interface{} {
		repos, _ := srv.ListRepos(context.Background())
		return map[string]interface{}{
			"rootDir":   rootDir,
			"repoCount": len(repos),
		}
	})

	<-ctx.Done()
	return nil
}

func (m *Module) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopHB != nil {
		m.stopHB()
		m.stopHB = nil
	}
	unregister(m)
	m.srv = nil
	m.store = nil
	return nil
}

// Server returns the underlying gitops.Server for HTTP route mounting.
func (m *Module) Server() *gitops.Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.srv
}

// Store returns the underlying RepoStore for target-aware reads/writes.
func (m *Module) Store() *gitops.RepoStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store
}

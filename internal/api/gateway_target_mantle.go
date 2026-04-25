//go:build (api || all) && (gitopsserver || mantle || all)

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// gatewayConfigPath is the file inside each target's repo that holds the
// full GatewayConfigKV blob. Mantle reads/writes this through gitops.RepoStore
// for fleet-mode configurator endpoints.
const gatewayConfigPath = "gateway.json"

// loadGatewayConfigForTarget reads the gateway config for a remote tentacle
// out of its git repo on mantle. If the file doesn't exist yet, returns an
// empty config seeded with the requested gatewayID so handlers can build
// up the first revision idempotently.
func (m *Module) loadGatewayConfigForTarget(t Target, gatewayID string) (*itypes.GatewayConfigKV, error) {
	if !t.IsRemote() {
		return nil, errors.New("loadGatewayConfigForTarget called without target")
	}
	rs := ensureRepoStore()
	data, err := rs.ReadFile(t.Group, t.Node, gatewayConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &itypes.GatewayConfigKV{GatewayID: gatewayID}, nil
		}
		return nil, fmt.Errorf("read %s: %w", gatewayConfigPath, err)
	}
	var cfg itypes.GatewayConfigKV
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", gatewayConfigPath, err)
	}
	if cfg.GatewayID == "" {
		cfg.GatewayID = gatewayID
	}
	return &cfg, nil
}

// saveGatewayConfigForTarget writes the gateway config back to the target's
// git repo and pushes the commit. The commit message identifies the operator
// action so audit consumers can group related revisions.
func (m *Module) saveGatewayConfigForTarget(t Target, cfg *itypes.GatewayConfigKV, msg string) error {
	if !t.IsRemote() {
		return errors.New("saveGatewayConfigForTarget called without target")
	}
	cfg.UpdatedAt = time.Now().UnixMilli()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	rs := ensureRepoStore()
	if msg == "" {
		msg = "update gateway config"
	}
	return rs.WriteFile(t.Group, t.Node, gatewayConfigPath, data, msg)
}

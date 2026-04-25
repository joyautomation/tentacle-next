//go:build (api || all) && (gitopsserver || mantle || all)

package api

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/joyautomation/tentacle/internal/manifest"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// gatewayConfigDir is the directory inside each target's repo that holds
// gateway manifest YAML files, one per gateway. This matches the layout
// the edge gitops module produces and consumes (config/gateways/*.yaml),
// so the same repo round-trips through both writers.
const gatewayConfigDir = "config/gateways"

func gatewayManifestPath(gatewayID string) string {
	return path.Join(gatewayConfigDir, gatewayID+".yaml")
}

// loadGatewayConfigForTarget reads the gateway config for a remote tentacle
// out of its git repo on mantle. If the file doesn't exist yet, returns an
// empty config seeded with the requested gatewayID so handlers can build
// up the first revision idempotently.
func (m *Module) loadGatewayConfigForTarget(t Target, gatewayID string) (*itypes.GatewayConfigKV, error) {
	if !t.IsRemote() {
		return nil, errors.New("loadGatewayConfigForTarget called without target")
	}
	rs := ensureRepoStore()
	data, err := rs.ReadFile(t.Group, t.Node, gatewayManifestPath(gatewayID))
	if err != nil {
		if os.IsNotExist(err) {
			return &itypes.GatewayConfigKV{GatewayID: gatewayID}, nil
		}
		return nil, fmt.Errorf("read %s: %w", gatewayManifestPath(gatewayID), err)
	}
	resources, err := manifest.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", gatewayManifestPath(gatewayID), err)
	}
	for _, res := range resources {
		gw, ok := res.(*manifest.GatewayResource)
		if !ok {
			continue
		}
		cfg := &itypes.GatewayConfigKV{
			GatewayID:    gw.Metadata.Name,
			Devices:      gw.Spec.Devices,
			Variables:    gw.Spec.Variables,
			UdtTemplates: gw.Spec.UdtTemplates,
			UdtVariables: gw.Spec.UdtVariables,
		}
		if cfg.GatewayID == "" {
			cfg.GatewayID = gatewayID
		}
		return cfg, nil
	}
	return &itypes.GatewayConfigKV{GatewayID: gatewayID}, nil
}

// saveGatewayConfigForTarget writes the gateway config back to the target's
// git repo and pushes the commit. The commit message identifies the operator
// action so audit consumers can group related revisions.
func (m *Module) saveGatewayConfigForTarget(t Target, cfg *itypes.GatewayConfigKV, msg string) error {
	if !t.IsRemote() {
		return errors.New("saveGatewayConfigForTarget called without target")
	}
	cfg.UpdatedAt = time.Now().UnixMilli()

	res := &manifest.GatewayResource{
		ResourceHeader: manifest.NewHeader(manifest.KindGateway, cfg.GatewayID),
		Spec: manifest.GatewaySpec{
			Devices:      cfg.Devices,
			Variables:    cfg.Variables,
			UdtTemplates: cfg.UdtTemplates,
			UdtVariables: cfg.UdtVariables,
		},
	}
	if res.Spec.Devices == nil {
		res.Spec.Devices = map[string]itypes.GatewayDeviceConfig{}
	}
	if res.Spec.Variables == nil {
		res.Spec.Variables = map[string]itypes.GatewayVariableConfig{}
	}

	data, err := manifest.Serialize([]any{res})
	if err != nil {
		return fmt.Errorf("serialize gateway manifest: %w", err)
	}

	rs := ensureRepoStore()
	if msg == "" {
		msg = "update gateway config"
	}
	return rs.WriteFile(t.Group, t.Node, gatewayManifestPath(cfg.GatewayID), data, msg)
}

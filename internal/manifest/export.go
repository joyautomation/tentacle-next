package manifest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// ExportOptions controls which resource kinds are included in the export.
type ExportOptions struct {
	// Kinds limits the export to specific kinds. Empty means all.
	Kinds []string
}

// Export reads all configuration from the bus and returns typed manifest resources.
// Resources are returned with secrets redacted.
func Export(b bus.Bus, opts ExportOptions) ([]any, error) {
	kindSet := make(map[string]bool)
	for _, k := range opts.Kinds {
		kindSet[k] = true
	}
	includeAll := len(kindSet) == 0

	var resources []any

	// Gateway configs.
	if includeAll || kindSet[KindGateway] {
		gateways, err := exportGateways(b)
		if err != nil {
			slog.Warn("export: skipping gateways", "error", err)
		} else {
			resources = append(resources, gateways...)
		}
	}

	// Desired services → Service resources.
	if includeAll || kindSet[KindService] {
		services, err := exportServices(b)
		if err != nil {
			slog.Warn("export: skipping services", "error", err)
		} else {
			resources = append(resources, services...)
		}
	}

	// Module config → ModuleConfig resources.
	if includeAll || kindSet[KindModuleConfig] {
		configs, err := exportModuleConfigs(b)
		if err != nil {
			slog.Warn("export: skipping module configs", "error", err)
		} else {
			resources = append(resources, configs...)
		}
	}

	// Nftables config — skip if module isn't running.
	if includeAll || kindSet[KindNftables] {
		if isServiceRunning(b, "nftables") {
			nft, err := exportNftables(b)
			if err != nil {
				slog.Warn("export: skipping nftables", "error", err)
			} else if nft != nil {
				resources = append(resources, nft)
			}
		}
	}

	// PLC configs.
	if includeAll || kindSet[KindPlc] {
		plcs, err := exportPlcs(b)
		if err != nil {
			slog.Warn("export: skipping plcs", "error", err)
		} else {
			resources = append(resources, plcs...)
		}
	}

	// Device configs from the shared devices bucket.
	if includeAll || kindSet[KindDevice] {
		devices, err := exportDevices(b)
		if err != nil {
			slog.Warn("export: skipping devices", "error", err)
		} else {
			resources = append(resources, devices...)
		}
	}

	// Network config — skip if module isn't running.
	if includeAll || kindSet[KindNetwork] {
		if isServiceRunning(b, "network") {
			net, err := exportNetwork(b)
			if err != nil {
				slog.Warn("export: skipping network", "error", err)
			} else if net != nil {
				resources = append(resources, net)
			}
		}
	}

	RedactSecrets(resources)
	return resources, nil
}

func exportGateways(b bus.Bus) ([]any, error) {
	keys, err := b.KVKeys(topics.BucketGatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("list gateway keys: %w", err)
	}

	var resources []any
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketGatewayConfig, key)
		if err != nil {
			slog.Warn("export: skipping gateway", "key", key, "error", err)
			continue
		}
		var kv itypes.GatewayConfigKV
		if err := json.Unmarshal(data, &kv); err != nil {
			slog.Warn("export: skipping gateway", "key", key, "error", err)
			continue
		}
		res := &GatewayResource{
			ResourceHeader: NewHeader(KindGateway, key),
			Spec: GatewaySpec{
				Variables:    kv.Variables,
				UdtTemplates: kv.UdtTemplates,
				UdtVariables: kv.UdtVariables,
			},
		}
		if res.Spec.Variables == nil {
			res.Spec.Variables = make(map[string]itypes.GatewayVariableConfig)
		}
		resources = append(resources, res)
	}
	return resources, nil
}

func exportServices(b bus.Bus) ([]any, error) {
	keys, err := b.KVKeys(topics.BucketDesiredServices)
	if err != nil {
		return nil, fmt.Errorf("list desired service keys: %w", err)
	}

	var resources []any
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketDesiredServices, key)
		if err != nil {
			continue
		}
		var kv itypes.DesiredServiceKV
		if err := json.Unmarshal(data, &kv); err != nil {
			continue
		}

		spec := ServiceSpec{
			Version: kv.Version,
			Running: kv.Running,
		}

		// Include enabled state if available.
		if enabledData, _, err := b.KVGet(topics.BucketServiceEnabled, key); err == nil && len(enabledData) > 0 {
			var enabled ttypes.ServiceEnabledKV
			if err := json.Unmarshal(enabledData, &enabled); err == nil {
				spec.Enabled = &enabled.Enabled
			}
		}

		resources = append(resources, &ServiceResource{
			ResourceHeader: NewHeader(KindService, key),
			Spec:           spec,
		})
	}
	return resources, nil
}

func exportModuleConfigs(b bus.Bus) ([]any, error) {
	keys, err := b.KVKeys(topics.BucketTentacleConfig)
	if err != nil {
		return nil, fmt.Errorf("list tentacle config keys: %w", err)
	}

	// Group entries by moduleId.
	modules := make(map[string]map[string]string) // moduleId → envVar → value
	for _, key := range keys {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			continue
		}
		moduleID, envVar := parts[0], parts[1]
		data, _, err := b.KVGet(topics.BucketTentacleConfig, key)
		if err != nil {
			continue
		}
		if modules[moduleID] == nil {
			modules[moduleID] = make(map[string]string)
		}
		modules[moduleID][envVar] = string(data)
	}

	// Sort module IDs for deterministic output.
	moduleIDs := sortedMapKeys(modules)
	var resources []any
	for _, moduleID := range moduleIDs {
		resources = append(resources, &ModuleConfigResource{
			ResourceHeader: NewHeader(KindModuleConfig, moduleID),
			Spec:           ModuleConfigSpec{Values: modules[moduleID]},
		})
	}
	return resources, nil
}

func exportNftables(b bus.Bus) (any, error) {
	req := itypes.NftablesCommandRequest{
		Action:    "get-config",
		Timestamp: time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := b.Request(topics.NftablesCommand, payload, 5*time.Second)
	if err != nil {
		return nil, err
	}

	var cmdResp itypes.NftablesCommandResponse
	if err := json.Unmarshal(resp, &cmdResp); err != nil {
		return nil, err
	}
	if !cmdResp.Success {
		return nil, fmt.Errorf("nftables: %s", cmdResp.Error)
	}
	if cmdResp.Config == nil || len(cmdResp.Config.NatRules) == 0 {
		return nil, nil
	}

	return &NftablesResource{
		ResourceHeader: NewHeader(KindNftables, "default"),
		Spec:           NftablesSpec{NatRules: cmdResp.Config.NatRules},
	}, nil
}

func exportNetwork(b bus.Bus) (any, error) {
	req := itypes.NetworkCommandRequest{
		Action:    "get-config",
		Timestamp: time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := b.Request(topics.NetworkCommand, payload, 5*time.Second)
	if err != nil {
		return nil, err
	}

	var cmdResp itypes.NetworkCommandResponse
	if err := json.Unmarshal(resp, &cmdResp); err != nil {
		return nil, err
	}
	if !cmdResp.Success {
		return nil, fmt.Errorf("network: %s", cmdResp.Error)
	}
	if len(cmdResp.Config) == 0 {
		return nil, nil
	}

	return &NetworkResource{
		ResourceHeader: NewHeader(KindNetwork, "default"),
		Spec:           NetworkSpec{Interfaces: cmdResp.Config},
	}, nil
}

func exportPlcs(b bus.Bus) ([]any, error) {
	keys, err := b.KVKeys(topics.BucketPlcConfig)
	if err != nil {
		return nil, fmt.Errorf("list plc config keys: %w", err)
	}

	var resources []any
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketPlcConfig, key)
		if err != nil {
			slog.Warn("export: skipping plc", "key", key, "error", err)
			continue
		}
		var kv itypes.PlcConfigKV
		if err := json.Unmarshal(data, &kv); err != nil {
			slog.Warn("export: skipping plc", "key", key, "error", err)
			continue
		}

		spec := PlcSpec{
			Variables:    kv.Variables,
			UdtTemplates: kv.UdtTemplates,
			Tasks:        kv.Tasks,
			Programs:     make(map[string]PlcProgramSpec),
		}
		if spec.Variables == nil {
			spec.Variables = make(map[string]itypes.PlcVariableConfigKV)
		}
		if spec.Tasks == nil {
			spec.Tasks = make(map[string]itypes.PlcTaskConfigKV)
		}

		// Load programs referenced by tasks.
		progRefs := make(map[string]bool)
		for _, task := range kv.Tasks {
			if task.ProgramRef != "" {
				progRefs[task.ProgramRef] = true
			}
		}
		for progRef := range progRefs {
			progData, _, err := b.KVGet(topics.BucketPlcPrograms, progRef)
			if err != nil {
				continue
			}
			var prog itypes.PlcProgramKV
			if err := json.Unmarshal(progData, &prog); err != nil {
				continue
			}
			spec.Programs[progRef] = PlcProgramSpec{
				Language: prog.Language,
				Source:   prog.Source,
				StSource: prog.StSource,
			}
		}

		resources = append(resources, &PlcResource{
			ResourceHeader: NewHeader(KindPlc, key),
			Spec:           spec,
		})
	}
	return resources, nil
}

func exportDevices(b bus.Bus) ([]any, error) {
	keys, err := b.KVKeys(topics.BucketDevices)
	if err != nil {
		return nil, fmt.Errorf("list device keys: %w", err)
	}

	var resources []any
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketDevices, key)
		if err != nil {
			slog.Warn("export: skipping device", "key", key, "error", err)
			continue
		}
		var cfg itypes.DeviceConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			slog.Warn("export: skipping device", "key", key, "error", err)
			continue
		}
		resources = append(resources, &DeviceResource{
			ResourceHeader: NewHeader(KindDevice, key),
			Spec:           cfg,
		})
	}
	return resources, nil
}

// isServiceRunning checks if a module with the given serviceType has a heartbeat.
func isServiceRunning(b bus.Bus, serviceType string) bool {
	keys, err := b.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		return false
	}
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketHeartbeats, key)
		if err != nil {
			continue
		}
		var hb ttypes.ServiceHeartbeat
		if json.Unmarshal(data, &hb) != nil {
			continue
		}
		if hb.ServiceType == serviceType {
			return true
		}
	}
	return false
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort — maps are small.
	for i := 1; i < len(keys); i++ {
		key := keys[i]
		j := i - 1
		for j >= 0 && keys[j] > key {
			keys[j+1] = keys[j]
			j--
		}
		keys[j+1] = key
	}
	return keys
}

package manifest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// ApplyResult summarizes what was applied.
type ApplyResult struct {
	Applied []AppliedResource `json:"applied"`
	Skipped []SkippedResource `json:"skipped,omitempty"`
}

// AppliedResource describes a single resource that was applied.
type AppliedResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SkippedResource describes a resource that was skipped.
type SkippedResource struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Apply writes resources to the appropriate KV buckets and bus targets.
// The source parameter is used for change tracking (e.g., "gui", "cli", "gitops").
func Apply(b bus.Bus, resources []any, source string) (*ApplyResult, error) {
	if err := Validate(resources); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	result := &ApplyResult{}

	for _, res := range resources {
		switch r := res.(type) {
		case *GatewayResource:
			if err := applyGateway(b, r, source); err != nil {
				return result, fmt.Errorf("apply Gateway %q: %w", r.Metadata.Name, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindGateway, Name: r.Metadata.Name})

		case *ServiceResource:
			if err := applyService(b, r, source); err != nil {
				return result, fmt.Errorf("apply Service %q: %w", r.Metadata.Name, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindService, Name: r.Metadata.Name})

		case *ModuleConfigResource:
			skipped := applyModuleConfig(b, r, source)
			result.Applied = append(result.Applied, AppliedResource{Kind: KindModuleConfig, Name: r.Metadata.Name})
			result.Skipped = append(result.Skipped, skipped...)

		case *NftablesResource:
			if err := applyNftables(b, r, source); err != nil {
				return result, fmt.Errorf("apply Nftables %q: %w", r.Metadata.Name, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindNftables, Name: r.Metadata.Name})

		case *NetworkResource:
			if err := applyNetwork(b, r, source); err != nil {
				return result, fmt.Errorf("apply Network %q: %w", r.Metadata.Name, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindNetwork, Name: r.Metadata.Name})

		case *PlcResource:
			if err := applyPlc(b, r, source); err != nil {
				return result, fmt.Errorf("apply Plc %q: %w", r.Metadata.Name, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindPlc, Name: r.Metadata.Name})
		}
	}

	return result, nil
}

func applyGateway(b bus.Bus, r *GatewayResource, source string) error {
	kv := itypes.GatewayConfigKV{
		GatewayID:    r.Metadata.Name,
		Devices:      r.Spec.Devices,
		Variables:    r.Spec.Variables,
		UdtTemplates: r.Spec.UdtTemplates,
		UdtVariables: r.Spec.UdtVariables,
		UpdatedAt:    time.Now().UnixMilli(),
	}
	if kv.Devices == nil {
		kv.Devices = make(map[string]itypes.GatewayDeviceConfig)
	}
	if kv.Variables == nil {
		kv.Variables = make(map[string]itypes.GatewayVariableConfig)
	}

	data, err := json.Marshal(kv)
	if err != nil {
		return err
	}
	if _, err := b.KVPut(topics.BucketGatewayConfig, kv.GatewayID, data); err != nil {
		return err
	}
	writeSourceMetadata(b, topics.BucketGatewayConfig, kv.GatewayID, source)
	return nil
}

func applyService(b bus.Bus, r *ServiceResource, source string) error {
	kv := itypes.DesiredServiceKV{
		ModuleID:  r.Metadata.Name,
		Version:   r.Spec.Version,
		Running:   r.Spec.Running,
		UpdatedAt: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(kv)
	if err != nil {
		return err
	}
	if _, err := b.KVPut(topics.BucketDesiredServices, kv.ModuleID, data); err != nil {
		return err
	}
	writeSourceMetadata(b, topics.BucketDesiredServices, kv.ModuleID, source)

	// Also apply enabled state if specified.
	if r.Spec.Enabled != nil {
		enabled := ttypes.ServiceEnabledKV{
			ModuleID:  r.Metadata.Name,
			Enabled:   *r.Spec.Enabled,
			UpdatedAt: time.Now().UnixMilli(),
		}
		enabledData, err := json.Marshal(enabled)
		if err != nil {
			return err
		}
		if _, err := b.KVPut(topics.BucketServiceEnabled, kv.ModuleID, enabledData); err != nil {
			return err
		}
		writeSourceMetadata(b, topics.BucketServiceEnabled, kv.ModuleID, source)
	}

	return nil
}

func applyModuleConfig(b bus.Bus, r *ModuleConfigResource, source string) []SkippedResource {
	var skipped []SkippedResource
	moduleID := r.Metadata.Name

	for envVar, value := range r.Spec.Values {
		// Skip secret placeholders — preserve existing value.
		if IsSecretPlaceholder(value) {
			skipped = append(skipped, SkippedResource{
				Kind:   KindModuleConfig,
				Name:   moduleID + "." + envVar,
				Reason: "secret placeholder preserved",
			})
			continue
		}

		key := moduleID + "." + envVar
		if _, err := b.KVPut(topics.BucketTentacleConfig, key, []byte(value)); err != nil {
			slog.Warn("apply: failed to write config", "key", key, "error", err)
			continue
		}
		writeSourceMetadata(b, topics.BucketTentacleConfig, key, source)
	}

	return skipped
}

func applyNftables(b bus.Bus, r *NftablesResource, source string) error {
	req := itypes.NftablesCommandRequest{
		Action:    "apply-config",
		NatRules:  r.Spec.NatRules,
		Timestamp: time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := b.Request(topics.NftablesCommand, payload, 10*time.Second)
	if err != nil {
		return err
	}

	var cmdResp itypes.NftablesCommandResponse
	if err := json.Unmarshal(resp, &cmdResp); err != nil {
		return err
	}
	if !cmdResp.Success {
		return fmt.Errorf("nftables: %s", cmdResp.Error)
	}
	return nil
}

func applyNetwork(b bus.Bus, r *NetworkResource, source string) error {
	req := itypes.NetworkCommandRequest{
		Action:     "apply-config",
		Interfaces: r.Spec.Interfaces,
		Timestamp:  time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := b.Request(topics.NetworkCommand, payload, 10*time.Second)
	if err != nil {
		return err
	}

	var cmdResp itypes.NetworkCommandResponse
	if err := json.Unmarshal(resp, &cmdResp); err != nil {
		return err
	}
	if !cmdResp.Success {
		return fmt.Errorf("network: %s", cmdResp.Error)
	}
	return nil
}

func applyPlc(b bus.Bus, r *PlcResource, source string) error {
	kv := itypes.PlcConfigKV{
		PlcID:        r.Metadata.Name,
		Devices:      r.Spec.Devices,
		Variables:    r.Spec.Variables,
		UdtTemplates: r.Spec.UdtTemplates,
		Tasks:        r.Spec.Tasks,
		UpdatedAt:    time.Now().UnixMilli(),
	}
	if kv.Devices == nil {
		kv.Devices = make(map[string]itypes.PlcDeviceConfigKV)
	}
	if kv.Variables == nil {
		kv.Variables = make(map[string]itypes.PlcVariableConfigKV)
	}
	if kv.Tasks == nil {
		kv.Tasks = make(map[string]itypes.PlcTaskConfigKV)
	}

	data, err := json.Marshal(kv)
	if err != nil {
		return err
	}
	if _, err := b.KVPut(topics.BucketPlcConfig, kv.PlcID, data); err != nil {
		return err
	}
	writeSourceMetadata(b, topics.BucketPlcConfig, kv.PlcID, source)

	// Write programs to plc_programs bucket.
	for progName, progSpec := range r.Spec.Programs {
		prog := itypes.PlcProgramKV{
			Name:      progName,
			Language:  progSpec.Language,
			Source:    progSpec.Source,
			StSource:  progSpec.StSource,
			UpdatedAt: time.Now().UnixMilli(),
			UpdatedBy: source,
		}
		progData, err := json.Marshal(prog)
		if err != nil {
			slog.Warn("apply: failed to marshal plc program", "program", progName, "error", err)
			continue
		}
		if _, err := b.KVPut(topics.BucketPlcPrograms, progName, progData); err != nil {
			slog.Warn("apply: failed to write plc program", "program", progName, "error", err)
			continue
		}
		writeSourceMetadata(b, topics.BucketPlcPrograms, progName, source)
	}

	return nil
}

// writeSourceMetadata records the source of a config write for GitOps feedback loop prevention.
func writeSourceMetadata(b bus.Bus, bucket, key, source string) {
	meta := struct {
		Source    string `json:"source"`
		Timestamp int64  `json:"timestamp"`
	}{
		Source:    source,
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	metaKey := bucket + "." + key
	if _, err := b.KVPut(topics.BucketConfigMetadata, metaKey, data); err != nil {
		slog.Warn("apply: failed to write source metadata", "key", metaKey, "error", err)
	}
}

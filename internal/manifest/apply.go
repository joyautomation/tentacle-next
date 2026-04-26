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
//
// Per-resource resilience: if validation or apply fails for one resource, that
// resource is recorded in result.Skipped and the loop continues with the rest.
// This prevents one bad manifest in a gitops repo from blocking every other
// resource on the node. The returned error is non-nil only if the whole batch
// could not be processed (currently never — kept in the signature for future
// fatal cases).
func Apply(b bus.Bus, resources []any, source string) (*ApplyResult, error) {
	result := &ApplyResult{}

	for _, res := range resources {
		kind := resourceKindOf(res)
		name := resourceNameOf(res)

		if err := ValidateResource(res); err != nil {
			result.Skipped = append(result.Skipped, SkippedResource{
				Kind: kind, Name: name,
				Reason: "validation: " + err.Error(),
			})
			slog.Warn("apply: skipping invalid resource", "kind", kind, "name", name, "error", err)
			continue
		}

		switch r := res.(type) {
		case *GatewayResource:
			if err := applyGateway(b, r, source); err != nil {
				result.Skipped = append(result.Skipped, SkippedResource{Kind: KindGateway, Name: r.Metadata.Name, Reason: err.Error()})
				slog.Warn("apply: gateway failed", "name", r.Metadata.Name, "error", err)
				continue
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindGateway, Name: r.Metadata.Name})

		case *ServiceResource:
			if err := applyService(b, r, source); err != nil {
				result.Skipped = append(result.Skipped, SkippedResource{Kind: KindService, Name: r.Metadata.Name, Reason: err.Error()})
				slog.Warn("apply: service failed", "name", r.Metadata.Name, "error", err)
				continue
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindService, Name: r.Metadata.Name})

		case *ModuleConfigResource:
			skipped := applyModuleConfig(b, r, source)
			result.Applied = append(result.Applied, AppliedResource{Kind: KindModuleConfig, Name: r.Metadata.Name})
			result.Skipped = append(result.Skipped, skipped...)

		case *NftablesResource:
			if err := applyNftables(b, r, source); err != nil {
				result.Skipped = append(result.Skipped, SkippedResource{Kind: KindNftables, Name: r.Metadata.Name, Reason: err.Error()})
				slog.Warn("apply: nftables failed", "name", r.Metadata.Name, "error", err)
				continue
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindNftables, Name: r.Metadata.Name})

		case *NetworkResource:
			if err := applyNetwork(b, r, source); err != nil {
				result.Skipped = append(result.Skipped, SkippedResource{Kind: KindNetwork, Name: r.Metadata.Name, Reason: err.Error()})
				slog.Warn("apply: network failed", "name", r.Metadata.Name, "error", err)
				continue
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindNetwork, Name: r.Metadata.Name})

		case *PlcResource:
			if err := applyPlc(b, r, source); err != nil {
				result.Skipped = append(result.Skipped, SkippedResource{Kind: KindPlc, Name: r.Metadata.Name, Reason: err.Error()})
				slog.Warn("apply: plc failed", "name", r.Metadata.Name, "error", err)
				continue
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: KindPlc, Name: r.Metadata.Name})
		}
	}

	return result, nil
}

func resourceKindOf(res any) string {
	switch r := res.(type) {
	case *GatewayResource:
		return r.Kind
	case *ServiceResource:
		return r.Kind
	case *ModuleConfigResource:
		return r.Kind
	case *NftablesResource:
		return r.Kind
	case *NetworkResource:
		return r.Kind
	case *PlcResource:
		return r.Kind
	}
	return "Unknown"
}

func resourceNameOf(res any) string {
	switch r := res.(type) {
	case *GatewayResource:
		return r.Metadata.Name
	case *ServiceResource:
		return r.Metadata.Name
	case *ModuleConfigResource:
		return r.Metadata.Name
	case *NftablesResource:
		return r.Metadata.Name
	case *NetworkResource:
		return r.Metadata.Name
	case *PlcResource:
		return r.Metadata.Name
	}
	return ""
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

// Prune removes KV entries in gitops-owned buckets that were not in the
// applied resource set. This is what makes "delete a manifest file" actually
// take effect — without it, removed files leave orphan KV entries that the
// edge would re-export and re-commit on the next sync, undoing the deletion.
//
// Only buckets that are 1:1 with manifest kinds get pruned (services,
// gateways, plc). Other buckets (config env vars, metadata) are not pruned
// here because they don't have a clean per-resource mapping.
func Prune(b bus.Bus, result *ApplyResult, source string) {
	pruneBucket(b, topics.BucketDesiredServices, KindService, result, source)
	pruneBucket(b, topics.BucketGatewayConfig, KindGateway, result, source)
	pruneBucket(b, topics.BucketPlcConfig, KindPlc, result, source)
}

func pruneBucket(b bus.Bus, bucket, kind string, result *ApplyResult, source string) {
	desired := make(map[string]bool)
	for _, ar := range result.Applied {
		if ar.Kind == kind {
			desired[ar.Name] = true
		}
	}

	keys, err := b.KVKeys(bucket)
	if err != nil {
		return
	}
	for _, key := range keys {
		if desired[key] {
			continue
		}
		if err := b.KVDelete(bucket, key); err != nil {
			slog.Warn("prune: delete failed", "bucket", bucket, "key", key, "error", err)
			continue
		}
		writeSourceMetadata(b, bucket, key, source)
		slog.Info("prune: removed orphan", "bucket", bucket, "key", key)
	}
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

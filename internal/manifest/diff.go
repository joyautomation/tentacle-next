package manifest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joyautomation/tentacle/internal/bus"
)

// DiffResult describes the differences between desired and current state.
type DiffResult struct {
	Changes []DiffChange `json:"changes"`
}

// DiffChange describes a single difference.
type DiffChange struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"` // "create", "update", "unchanged"
	Detail string `json:"detail,omitempty"`
}

// Diff compares a set of manifest resources against the current system state.
func Diff(b bus.Bus, resources []any) (*DiffResult, error) {
	// Export current state for comparison.
	current, err := Export(b, ExportOptions{})
	if err != nil {
		return nil, fmt.Errorf("export current state: %w", err)
	}

	// Build a map of current resources by kind+name.
	currentMap := make(map[string]any) // "Kind/Name" → resource
	for _, res := range current {
		key := resourceKey(res)
		if key != "" {
			currentMap[key] = res
		}
	}

	result := &DiffResult{}
	for _, desired := range resources {
		key := resourceKey(desired)
		if key == "" {
			continue
		}

		existing, exists := currentMap[key]
		if !exists {
			result.Changes = append(result.Changes, DiffChange{
				Kind:   resourceKind(desired),
				Name:   resourceName(desired),
				Action: "create",
			})
			continue
		}

		// Compare by serializing both to JSON and diffing.
		detail := compareResources(existing, desired)
		action := "unchanged"
		if detail != "" {
			action = "update"
		}
		result.Changes = append(result.Changes, DiffChange{
			Kind:   resourceKind(desired),
			Name:   resourceName(desired),
			Action: action,
			Detail: detail,
		})
	}

	return result, nil
}

func resourceKey(res any) string {
	kind := resourceKind(res)
	name := resourceName(res)
	if kind == "" || name == "" {
		return ""
	}
	return kind + "/" + name
}

func resourceKind(res any) string {
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
	case *SourceResource:
		return r.Kind
	default:
		return ""
	}
}

func resourceName(res any) string {
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
	case *SourceResource:
		return r.Metadata.Name
	default:
		return ""
	}
}

// compareResources does a JSON-level comparison, ignoring secret placeholders.
func compareResources(current, desired any) string {
	currJSON, _ := json.Marshal(specOf(current))
	desJSON, _ := json.Marshal(specOf(desired))

	if string(currJSON) == string(desJSON) {
		return ""
	}

	// Build a human-readable summary of top-level field differences.
	var currMap, desMap map[string]json.RawMessage
	json.Unmarshal(currJSON, &currMap)
	json.Unmarshal(desJSON, &desMap)

	var diffs []string
	allKeys := make(map[string]bool)
	for k := range currMap {
		allKeys[k] = true
	}
	for k := range desMap {
		allKeys[k] = true
	}

	for k := range allKeys {
		c, cOK := currMap[k]
		d, dOK := desMap[k]
		if !cOK {
			diffs = append(diffs, fmt.Sprintf("+%s", k))
		} else if !dOK {
			diffs = append(diffs, fmt.Sprintf("-%s", k))
		} else if string(c) != string(d) {
			diffs = append(diffs, fmt.Sprintf("~%s", k))
		}
	}

	if len(diffs) == 0 {
		return "spec changed"
	}
	return strings.Join(diffs, ", ")
}

func specOf(res any) any {
	switch r := res.(type) {
	case *GatewayResource:
		return r.Spec
	case *ServiceResource:
		return r.Spec
	case *ModuleConfigResource:
		return r.Spec
	case *NftablesResource:
		return r.Spec
	case *NetworkResource:
		return r.Spec
	case *PlcResource:
		return r.Spec
	case *SourceResource:
		return r.Spec
	default:
		return nil
	}
}

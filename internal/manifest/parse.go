package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Parse reads multi-document YAML and returns typed resources.
func Parse(r io.Reader) ([]any, error) {
	dec := yaml.NewDecoder(r)
	var resources []any

	for {
		// First decode into a raw map to inspect kind.
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode YAML document: %w", err)
		}
		if raw == nil {
			continue
		}

		res, err := decodeResource(raw)
		if err != nil {
			return nil, err
		}
		resources = append(resources, res)
	}
	return resources, nil
}

// ParseBytes is a convenience wrapper around Parse.
func ParseBytes(data []byte) ([]any, error) {
	return Parse(bytes.NewReader(data))
}

// decodeResource converts a raw YAML map into a typed resource struct.
// We round-trip through JSON so that json struct tags are respected.
func decodeResource(raw map[string]any) (any, error) {
	apiVersion, _ := raw["apiVersion"].(string)
	if apiVersion != APIVersion {
		return nil, fmt.Errorf("unsupported apiVersion: %q (expected %q)", apiVersion, APIVersion)
	}

	kind, _ := raw["kind"].(string)
	if !KnownKind(kind) {
		return nil, fmt.Errorf("unknown resource kind: %q", kind)
	}

	// Round-trip through JSON to leverage json struct tags.
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-encode %s: %w", kind, err)
	}

	switch kind {
	case KindGateway:
		var res GatewayResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Gateway: %w", err)
		}
		return &res, nil
	case KindService:
		var res ServiceResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Service: %w", err)
		}
		return &res, nil
	case KindModuleConfig:
		var res ModuleConfigResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode ModuleConfig: %w", err)
		}
		return &res, nil
	case KindNftables:
		var res NftablesResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Nftables: %w", err)
		}
		return &res, nil
	case KindNetwork:
		var res NetworkResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Network: %w", err)
		}
		return &res, nil
	case KindPlc:
		var res PlcResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Plc: %w", err)
		}
		return &res, nil
	case KindSource:
		var res SourceResource
		if err := json.Unmarshal(jsonBytes, &res); err != nil {
			return nil, fmt.Errorf("decode Source: %w", err)
		}
		return &res, nil
	default:
		return nil, fmt.Errorf("unhandled kind: %q", kind)
	}
}

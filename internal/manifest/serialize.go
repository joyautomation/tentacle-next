package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Serialize converts a list of resources to multi-document YAML.
// Existing internal types only have json tags, so we round-trip through JSON
// to preserve camelCase field names in the YAML output.
func Serialize(resources []any) ([]byte, error) {
	var buf bytes.Buffer
	for i, res := range resources {
		if i > 0 {
			buf.WriteString("---\n")
		}
		yamlBytes, err := toYAML(res)
		if err != nil {
			return nil, fmt.Errorf("serialize resource %d: %w", i, err)
		}
		buf.Write(yamlBytes)
	}
	return buf.Bytes(), nil
}

// toYAML converts a value to YAML by round-tripping through JSON so that
// json struct tags (camelCase) are used as the YAML keys.
func toYAML(v any) ([]byte, error) {
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Decode into an ordered structure that yaml.v3 can marshal.
	var node yaml.Node
	if err := yamlUnmarshalJSON(jsonBytes, &node); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlUnmarshalJSON converts JSON bytes into a yaml.Node tree.
// This preserves map key ordering from the JSON.
func yamlUnmarshalJSON(data []byte, node *yaml.Node) error {
	var raw any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	doc := buildYAMLNode(raw)
	*node = yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{doc},
	}
	return nil
}

// buildYAMLNode recursively converts a JSON-decoded value to a yaml.Node.
func buildYAMLNode(v any) *yaml.Node {
	switch val := v.(type) {
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range sortedKeys(val) {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: k, Tag: "!!str"}
			valNode := buildYAMLNode(val[k])
			node.Content = append(node.Content, keyNode, valNode)
		}
		return node
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range val {
			node.Content = append(node.Content, buildYAMLNode(item))
		}
		return node
	case json.Number:
		s := val.String()
		// Detect integer vs float.
		tag := "!!float"
		if _, err := val.Int64(); err == nil {
			tag = "!!int"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: tag}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: "!!str"}
	case bool:
		s := "false"
		if val {
			s = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!bool"}
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", v), Tag: "!!str"}
	}
}

// sortedKeys returns map keys in sorted order for deterministic output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort with a priority for well-known fields so the YAML reads naturally.
	sortManifestKeys(keys)
	return keys
}

// sortManifestKeys orders keys so that apiVersion, kind, metadata, spec come
// first (in that order), followed by remaining keys alphabetically.
func sortManifestKeys(keys []string) {
	priority := map[string]int{
		"apiVersion": 0,
		"kind":       1,
		"metadata":   2,
		"name":       3,
		"spec":       4,
	}
	maxPriority := len(priority)

	for i := 1; i < len(keys); i++ {
		key := keys[i]
		j := i - 1
		for j >= 0 && compareKeys(keys[j], key, priority, maxPriority) > 0 {
			keys[j+1] = keys[j]
			j--
		}
		keys[j+1] = key
	}
}

func compareKeys(a, b string, priority map[string]int, maxPriority int) int {
	pa, aOK := priority[a]
	pb, bOK := priority[b]
	if !aOK {
		pa = maxPriority
	}
	if !bOK {
		pb = maxPriority
	}
	if pa != pb {
		return pa - pb
	}
	// Both have the same priority bucket — sort alphabetically.
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

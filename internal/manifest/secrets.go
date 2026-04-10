package manifest

import "strings"

// SecretPlaceholder is the prefix used for redacted secret values.
const SecretPlaceholder = "<secret>"

// secretEnvVarPatterns matches env var names that contain secrets.
// These are redacted in ModuleConfig resources on export.
var secretEnvVarPatterns = []string{
	"PASSWORD",
	"_SECRET",
	"_TOKEN",
}

// RedactSecrets replaces secret values with placeholders in resources.
// Call this before serializing for export.
func RedactSecrets(resources []any) {
	for _, res := range resources {
		switch r := res.(type) {
		case *ModuleConfigResource:
			redactModuleConfig(r)
		case *GatewayResource:
			redactGateway(r)
		}
	}
}

// IsSecretPlaceholder returns true if the value is a redacted placeholder.
func IsSecretPlaceholder(v string) bool {
	return v == SecretPlaceholder
}

func redactModuleConfig(r *ModuleConfigResource) {
	for key, val := range r.Spec.Values {
		if val == "" {
			continue
		}
		upper := strings.ToUpper(key)
		for _, pattern := range secretEnvVarPatterns {
			if strings.Contains(upper, pattern) {
				r.Spec.Values[key] = SecretPlaceholder
				break
			}
		}
	}
}

func redactGateway(r *GatewayResource) {
	for id, dev := range r.Spec.Devices {
		if dev.V3Auth != nil {
			dev.V3Auth.AuthPassword = ""
			dev.V3Auth.PrivPassword = ""
			r.Spec.Devices[id] = dev
		}
	}
}

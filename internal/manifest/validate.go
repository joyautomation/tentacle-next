package manifest

import (
	"fmt"
	"strings"
)

// ValidationError collects multiple validation issues.
type ValidationError struct {
	Errors []string
}

func (ve *ValidationError) Error() string {
	return strings.Join(ve.Errors, "; ")
}

func (ve *ValidationError) add(format string, args ...any) {
	ve.Errors = append(ve.Errors, fmt.Sprintf(format, args...))
}

func (ve *ValidationError) hasErrors() bool {
	return len(ve.Errors) > 0
}

// Validate checks a list of parsed resources for errors.
// Returns nil if all resources are valid.
func Validate(resources []any) error {
	ve := &ValidationError{}
	for i, res := range resources {
		switch r := res.(type) {
		case *GatewayResource:
			validateGateway(r, i, ve)
		case *ServiceResource:
			validateService(r, i, ve)
		case *ModuleConfigResource:
			validateModuleConfig(r, i, ve)
		case *NftablesResource:
			validateNftables(r, i, ve)
		case *NetworkResource:
			validateNetwork(r, i, ve)
		default:
			ve.add("resource %d: unknown type %T", i, res)
		}
	}
	if ve.hasErrors() {
		return ve
	}
	return nil
}

func validateGateway(r *GatewayResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("Gateway %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}

	// Check that variable deviceIds reference defined devices.
	for varID, v := range r.Spec.Variables {
		if v.DeviceID == "" {
			ve.add("%s: variable %q has no deviceId", prefix, varID)
		} else if _, ok := r.Spec.Devices[v.DeviceID]; !ok {
			ve.add("%s: variable %q references unknown device %q", prefix, varID, v.DeviceID)
		}
	}

	// Check UDT variables reference defined templates and devices.
	for udtID, u := range r.Spec.UdtVariables {
		if u.DeviceID == "" {
			ve.add("%s: udtVariable %q has no deviceId", prefix, udtID)
		} else if _, ok := r.Spec.Devices[u.DeviceID]; !ok {
			ve.add("%s: udtVariable %q references unknown device %q", prefix, udtID, u.DeviceID)
		}
		if u.TemplateName == "" {
			ve.add("%s: udtVariable %q has no templateName", prefix, udtID)
		} else if _, ok := r.Spec.UdtTemplates[u.TemplateName]; !ok {
			ve.add("%s: udtVariable %q references unknown template %q", prefix, udtID, u.TemplateName)
		}
	}

	// Check device protocols.
	validProtocols := map[string]bool{
		"ethernetip": true, "opcua": true, "snmp": true, "modbus": true,
	}
	for devID, d := range r.Spec.Devices {
		if d.Protocol == "" {
			ve.add("%s: device %q has no protocol", prefix, devID)
		} else if !validProtocols[d.Protocol] {
			ve.add("%s: device %q has unknown protocol %q", prefix, devID, d.Protocol)
		}
	}
}

func validateService(r *ServiceResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("Service %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}
	if r.Spec.Version == "" {
		ve.add("%s: spec.version is required", prefix)
	}
}

func validateModuleConfig(r *ModuleConfigResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("ModuleConfig %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}
	if r.Spec.Values == nil {
		ve.add("%s: spec.values is required", prefix)
	}
}

func validateNftables(r *NftablesResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("Nftables %q (resource %d)", r.Metadata.Name, idx)

	ids := make(map[string]bool)
	for i, rule := range r.Spec.NatRules {
		if rule.ID == "" {
			ve.add("%s: natRules[%d] has no id", prefix, i)
		} else if ids[rule.ID] {
			ve.add("%s: duplicate rule id %q", prefix, rule.ID)
		}
		ids[rule.ID] = true
	}
}

func validateNetwork(r *NetworkResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("Network %q (resource %d)", r.Metadata.Name, idx)

	for i, iface := range r.Spec.Interfaces {
		if iface.InterfaceName == "" {
			ve.add("%s: interfaces[%d] has no interfaceName", prefix, i)
		}
	}
}

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

	// Collect DeviceResource ids in the batch so we can cross-validate
	// variable/udtVariable deviceId references when devices are provided
	// alongside gateway/plc resources. If no DeviceResources are in the
	// batch, we skip the cross-check entirely (the device may already be
	// in the bucket from a prior apply).
	deviceIDs := make(map[string]bool)
	hasDevices := false
	for _, res := range resources {
		if r, ok := res.(*DeviceResource); ok {
			hasDevices = true
			deviceIDs[r.Metadata.Name] = true
		}
	}

	for i, res := range resources {
		switch r := res.(type) {
		case *GatewayResource:
			validateGateway(r, i, ve, hasDevices, deviceIDs)
		case *ServiceResource:
			validateService(r, i, ve)
		case *ModuleConfigResource:
			validateModuleConfig(r, i, ve)
		case *NftablesResource:
			validateNftables(r, i, ve)
		case *NetworkResource:
			validateNetwork(r, i, ve)
		case *PlcResource:
			validatePlc(r, i, ve, hasDevices, deviceIDs)
		case *DeviceResource:
			validateDevice(r, i, ve)
		default:
			ve.add("resource %d: unknown type %T", i, res)
		}
	}
	if ve.hasErrors() {
		return ve
	}
	return nil
}

func validateGateway(r *GatewayResource, idx int, ve *ValidationError, hasDevices bool, deviceIDs map[string]bool) {
	prefix := fmt.Sprintf("Gateway %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}

	// Check that variable deviceIds are set (and resolve to a known device
	// when devices are supplied in the same batch).
	for varID, v := range r.Spec.Variables {
		if v.DeviceID == "" {
			ve.add("%s: variable %q has no deviceId", prefix, varID)
		} else if hasDevices && !deviceIDs[v.DeviceID] {
			ve.add("%s: variable %q references unknown device %q", prefix, varID, v.DeviceID)
		}
	}

	// Check UDT variables reference defined templates and devices.
	for udtID, u := range r.Spec.UdtVariables {
		if u.DeviceID == "" {
			ve.add("%s: udtVariable %q has no deviceId", prefix, udtID)
		} else if hasDevices && !deviceIDs[u.DeviceID] {
			ve.add("%s: udtVariable %q references unknown device %q", prefix, udtID, u.DeviceID)
		}
		if u.TemplateName == "" {
			ve.add("%s: udtVariable %q has no templateName", prefix, udtID)
		} else if _, ok := r.Spec.UdtTemplates[u.TemplateName]; !ok {
			ve.add("%s: udtVariable %q references unknown template %q", prefix, udtID, u.TemplateName)
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

func validatePlc(r *PlcResource, idx int, ve *ValidationError, hasDevices bool, deviceIDs map[string]bool) {
	prefix := fmt.Sprintf("Plc %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}

	// Check that input variables with sources reference a known device
	// when devices are supplied in the same batch.
	for varID, v := range r.Spec.Variables {
		if v.Source != nil && v.Source.DeviceID != "" && hasDevices {
			if !deviceIDs[v.Source.DeviceID] {
				ve.add("%s: variable %q source references unknown device %q", prefix, varID, v.Source.DeviceID)
			}
		}
	}

	// Check that tasks reference programs that exist in the manifest.
	for taskID, t := range r.Spec.Tasks {
		if t.ProgramRef == "" {
			ve.add("%s: task %q has no programRef", prefix, taskID)
		} else if r.Spec.Programs != nil {
			if _, ok := r.Spec.Programs[t.ProgramRef]; !ok {
				ve.add("%s: task %q references unknown program %q", prefix, taskID, t.ProgramRef)
			}
		}
		if t.ScanRateMs <= 0 {
			ve.add("%s: task %q has invalid scanRateMs %d", prefix, taskID, t.ScanRateMs)
		}
	}
}

func validateDevice(r *DeviceResource, idx int, ve *ValidationError) {
	prefix := fmt.Sprintf("Device %q (resource %d)", r.Metadata.Name, idx)

	if r.Metadata.Name == "" {
		ve.add("%s: metadata.name is required", prefix)
	}

	validProtocols := map[string]bool{
		"ethernetip": true, "opcua": true, "snmp": true, "modbus": true,
	}
	if r.Spec.Protocol == "" {
		ve.add("%s: spec.protocol is required", prefix)
	} else if !validProtocols[r.Spec.Protocol] {
		ve.add("%s: unknown protocol %q", prefix, r.Spec.Protocol)
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

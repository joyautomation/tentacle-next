package scanner

import (
	"encoding/json"
	"fmt"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// WriteSubscription publishes a subscribe request to the protocol's scanner
// KV bucket. The scanner watches its bucket and picks up changes whenever
// it's ready, so this is fire-and-forget beyond the KV put itself.
//
// structTypes maps tag → UDT template name; only meaningful for ethernetip.
// Pass nil for other protocols.
//
// Returns handled=false when the device is auto-managed (self-publishing) or
// the protocol has no scanner bucket — callers can treat this as a no-op.
func WriteSubscription(
	b bus.Bus,
	subscriberID, deviceID string,
	device itypes.GatewayDeviceConfig,
	vars map[string]itypes.GatewayVariableConfig,
	structTypes map[string]string,
) (handled bool, err error) {
	if device.AutoManaged {
		return false, nil
	}
	bucket := topics.ScannerBucket(device.Protocol)
	if bucket == "" {
		return false, nil
	}

	scanRate := func(def int) int {
		if device.ScanRate != nil {
			return *device.ScanRate
		}
		return def
	}

	var payload []byte

	switch device.Protocol {
	case "ethernetip":
		tags := make([]string, 0, len(vars))
		cipTypes := make(map[string]string)
		deadbands := make(map[string]types.DeadBandConfig)
		disableRBE := make(map[string]bool)
		for _, v := range vars {
			tags = append(tags, v.Tag)
			if v.CipType != "" {
				cipTypes[v.Tag] = v.CipType
			}
			if v.Deadband != nil {
				deadbands[v.Tag] = *v.Deadband
			}
			if v.DisableRBE {
				disableRBE[v.Tag] = true
			}
		}
		port := 44818
		if device.Port != nil {
			port = *device.Port
		}
		slot := 0
		if device.Slot != nil {
			slot = *device.Slot
		}
		req := itypes.EthernetIPSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port, Slot: slot,
			Tags: tags, ScanRate: scanRate(1000), CipTypes: cipTypes, StructTypes: structTypes,
			Deadbands: deadbands, DisableRBE: disableRBE,
		}
		payload, err = json.Marshal(req)

	case "opcua":
		nodeIDs := make([]string, 0, len(vars))
		for _, v := range vars {
			nodeIDs = append(nodeIDs, v.Tag)
		}
		req := itypes.OpcUASubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, EndpointURL: device.EndpointURL,
			NodeIDs: nodeIDs, ScanRate: scanRate(1000),
		}
		payload, err = json.Marshal(req)

	case "snmp":
		oids := make([]string, 0, len(vars))
		for _, v := range vars {
			oids = append(oids, v.Tag)
		}
		port := 161
		if device.Port != nil {
			port = *device.Port
		}
		req := itypes.SNMPSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port,
			Version: device.Version, Community: device.Community, V3Auth: device.V3Auth,
			OIDs: oids, ScanRate: scanRate(5000),
		}
		payload, err = json.Marshal(req)

	case "modbus":
		tags := make([]itypes.ModbusTagConfig, 0, len(vars))
		for _, v := range vars {
			fc := "holding"
			if v.FunctionCode != nil {
				fc = itypes.FunctionCodeToString(*v.FunctionCode)
			}
			addr := 0
			if v.Address != nil {
				addr = *v.Address
			}
			dt := "uint16"
			if v.ModbusDatatype != "" {
				dt = v.ModbusDatatype
			}
			bo := device.ByteOrder
			if v.ByteOrder != "" {
				bo = v.ByteOrder
			}
			tags = append(tags, itypes.ModbusTagConfig{
				ID: v.Tag, Address: addr, FunctionCode: fc, Datatype: dt, ByteOrder: bo,
			})
		}
		port := 502
		if device.Port != nil {
			port = *device.Port
		}
		unitID := 1
		if device.UnitID != nil {
			unitID = *device.UnitID
		}
		req := itypes.ModbusScannerSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port,
			UnitID: unitID, ByteOrder: device.ByteOrder, Tags: tags, ScanRate: scanRate(1000),
		}
		payload, err = json.Marshal(req)
	}

	if err != nil {
		return true, fmt.Errorf("marshal subscribe config: %w", err)
	}

	key := fmt.Sprintf("%s.%s", subscriberID, deviceID)
	if _, err := b.KVPut(bucket, key, payload); err != nil {
		return true, fmt.Errorf("write scanner config to %s: %w", bucket, err)
	}
	return true, nil
}

// DeleteSubscription removes the subscribe request from the protocol's
// scanner KV bucket. No-op for protocols without a scanner bucket.
func DeleteSubscription(b bus.Bus, subscriberID, deviceID, protocol string) {
	bucket := topics.ScannerBucket(protocol)
	if bucket == "" {
		return
	}
	key := fmt.Sprintf("%s.%s", subscriberID, deviceID)
	_ = b.KVDelete(bucket, key)
}

// SubscriberID is the conventional subscriber ID for a consumer module.
// Scanner KV keys are "{subscriberId}.{deviceId}".
func SubscriberID(serviceType, moduleID string) string {
	return fmt.Sprintf("%s-%s", serviceType, moduleID)
}

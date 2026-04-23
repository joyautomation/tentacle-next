package scanner

import (
	"encoding/json"
	"fmt"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// TagSpec is a protocol-neutral bundle of per-tag metadata for building a
// scanner subscribe request. Callers (gateway, PLC) translate their own
// variable configs into a []TagSpec before calling WriteSubscription.
type TagSpec struct {
	Tag            string
	CipType        string                 // ethernetip
	FunctionCode   *int                   // modbus
	ModbusDatatype string                 // modbus
	ByteOrder      string                 // modbus
	Address        *int                   // modbus
	Deadband       *types.DeadBandConfig  // ethernetip
	DisableRBE     bool                   // ethernetip
}

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
	device itypes.SourceConfig,
	tags []TagSpec,
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
		tagNames := make([]string, 0, len(tags))
		cipTypes := make(map[string]string)
		deadbands := make(map[string]types.DeadBandConfig)
		disableRBE := make(map[string]bool)
		for _, t := range tags {
			tagNames = append(tagNames, t.Tag)
			if t.CipType != "" {
				cipTypes[t.Tag] = t.CipType
			}
			if t.Deadband != nil {
				deadbands[t.Tag] = *t.Deadband
			}
			if t.DisableRBE {
				disableRBE[t.Tag] = true
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
			Tags: tagNames, ScanRate: scanRate(1000), CipTypes: cipTypes, StructTypes: structTypes,
			Deadbands: deadbands, DisableRBE: disableRBE,
		}
		payload, err = json.Marshal(req)

	case "opcua":
		nodeIDs := make([]string, 0, len(tags))
		for _, t := range tags {
			nodeIDs = append(nodeIDs, t.Tag)
		}
		req := itypes.OpcUASubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, EndpointURL: device.EndpointURL,
			NodeIDs: nodeIDs, ScanRate: scanRate(1000),
		}
		payload, err = json.Marshal(req)

	case "snmp":
		oids := make([]string, 0, len(tags))
		for _, t := range tags {
			oids = append(oids, t.Tag)
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
		mtags := make([]itypes.ModbusTagConfig, 0, len(tags))
		for _, t := range tags {
			fc := "holding"
			if t.FunctionCode != nil {
				fc = itypes.FunctionCodeToString(*t.FunctionCode)
			}
			addr := 0
			if t.Address != nil {
				addr = *t.Address
			}
			dt := "uint16"
			if t.ModbusDatatype != "" {
				dt = t.ModbusDatatype
			}
			bo := device.ByteOrder
			if t.ByteOrder != "" {
				bo = t.ByteOrder
			}
			mtags = append(mtags, itypes.ModbusTagConfig{
				ID: t.Tag, Address: addr, FunctionCode: fc, Datatype: dt, ByteOrder: bo,
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
			UnitID: unitID, ByteOrder: device.ByteOrder, Tags: mtags, ScanRate: scanRate(1000),
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

// TagSpecFromGatewayVar adapts a gateway variable config to a TagSpec.
func TagSpecFromGatewayVar(v itypes.GatewayVariableConfig) TagSpec {
	return TagSpec{
		Tag:            v.Tag,
		CipType:        v.CipType,
		FunctionCode:   v.FunctionCode,
		ModbusDatatype: v.ModbusDatatype,
		ByteOrder:      v.ByteOrder,
		Address:        v.Address,
		Deadband:       v.Deadband,
		DisableRBE:     v.DisableRBE,
	}
}

// TagSpecFromPlcSource adapts a PLC variable source ref to a TagSpec.
func TagSpecFromPlcSource(src itypes.PlcVariableSourceKV) TagSpec {
	return TagSpec{
		Tag:            src.Tag,
		CipType:        src.CipType,
		FunctionCode:   src.FunctionCode,
		ModbusDatatype: src.ModbusDatatype,
		ByteOrder:      src.ByteOrder,
		Address:        src.Address,
	}
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

//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// scannerBridge subscribes to protocol scanner topics and feeds values into the VariableStore.
type scannerBridge struct {
	b     bus.Bus
	plcID string
	vars  *VariableStore
	log   *slog.Logger

	// Maps scanner subject key "{protocol}.{deviceId}.{tag}" to variable IDs.
	tagIndex map[string][]string

	subs []bus.Subscription
}

// newScannerBridge creates a scanner bridge.
func newScannerBridge(b bus.Bus, plcID string, vars *VariableStore, log *slog.Logger) *scannerBridge {
	return &scannerBridge{
		b:        b,
		plcID:    plcID,
		vars:     vars,
		log:      log,
		tagIndex: make(map[string][]string),
	}
}

// subscribe sets up subscriptions based on the PLC config's input variables.
func (sb *scannerBridge) subscribe(config *itypes.PlcConfigKV) {
	sb.unsubscribe()
	sb.tagIndex = make(map[string][]string)

	if config == nil {
		return
	}

	// Build tag index from input variables.
	// Group subscribe requests by protocol+device.
	type deviceKey struct {
		protocol string
		deviceID string
	}
	eipRequests := make(map[string]*itypes.EthernetIPSubscribeRequest)
	modbusRequests := make(map[string]*itypes.ModbusScannerSubscribeRequest)
	opcuaRequests := make(map[string]*itypes.OpcUASubscribeRequest)
	snmpRequests := make(map[string]*itypes.SNMPSubscribeRequest)

	for varID, vcfg := range config.Variables {
		if vcfg.Direction != "input" || vcfg.Source == nil {
			continue
		}
		src := vcfg.Source
		device, ok := config.Devices[src.DeviceID]
		if !ok {
			sb.log.Warn("scanner_bridge: device not found for variable",
				"variable", varID, "deviceId", src.DeviceID)
			continue
		}

		// Build tag index key.
		sanitizedTag := types.SanitizeForSubject(src.Tag)
		key := src.Protocol + "." + src.DeviceID + "." + sanitizedTag
		sb.tagIndex[key] = append(sb.tagIndex[key], varID)

		dk := src.Protocol + ":" + src.DeviceID
		scanRate := 1000
		if device.ScanRate != nil {
			scanRate = *device.ScanRate
		}

		switch src.Protocol {
		case "ethernetip":
			req, exists := eipRequests[dk]
			if !exists {
				port := 44818
				if device.Port != nil {
					port = *device.Port
				}
				slot := 0
				if device.Slot != nil {
					slot = *device.Slot
				}
				req = &itypes.EthernetIPSubscribeRequest{
					SubscriberID: "plc-" + sb.plcID,
					DeviceID:     src.DeviceID,
					Host:         device.Host,
					Port:         port,
					Slot:         slot,
					ScanRate:     scanRate,
					CipTypes:     make(map[string]string),
				}
				eipRequests[dk] = req
			}
			req.Tags = append(req.Tags, src.Tag)
			if src.CipType != "" {
				req.CipTypes[src.Tag] = src.CipType
			}

		case "modbus":
			req, exists := modbusRequests[dk]
			if !exists {
				port := 502
				if device.Port != nil {
					port = *device.Port
				}
				unitID := 1
				if device.UnitID != nil {
					unitID = *device.UnitID
				}
				req = &itypes.ModbusScannerSubscribeRequest{
					SubscriberID: "plc-" + sb.plcID,
					DeviceID:     src.DeviceID,
					Host:         device.Host,
					Port:         port,
					UnitID:       unitID,
					ScanRate:     scanRate,
				}
				modbusRequests[dk] = req
			}
			fc := "holding"
			if src.FunctionCode != nil {
				fc = itypes.FunctionCodeToString(*src.FunctionCode)
			}
			dt := src.ModbusDatatype
			if dt == "" {
				dt = "uint16"
			}
			tag := itypes.ModbusTagConfig{
				ID:           src.Tag,
				FunctionCode: fc,
				Datatype:     dt,
				ByteOrder:    src.ByteOrder,
			}
			if src.Address != nil {
				tag.Address = *src.Address
			}
			req.Tags = append(req.Tags, tag)

		case "opcua":
			req, exists := opcuaRequests[dk]
			if !exists {
				req = &itypes.OpcUASubscribeRequest{
					SubscriberID: "plc-" + sb.plcID,
					DeviceID:     src.DeviceID,
					EndpointURL:  device.EndpointURL,
					ScanRate:     scanRate,
				}
				opcuaRequests[dk] = req
			}
			req.NodeIDs = append(req.NodeIDs, src.Tag)

		case "snmp":
			req, exists := snmpRequests[dk]
			if !exists {
				port := 161
				if device.Port != nil {
					port = *device.Port
				}
				req = &itypes.SNMPSubscribeRequest{
					SubscriberID: "plc-" + sb.plcID,
					DeviceID:     src.DeviceID,
					Host:         device.Host,
					Port:         port,
					Version:      device.Version,
					Community:    device.Community,
					ScanRate:     scanRate,
				}
				snmpRequests[dk] = req
			}
			req.OIDs = append(req.OIDs, src.Tag)
		}
	}

	// Write subscribe requests to scanner config KV buckets.
	subscriberKey := "plc-" + sb.plcID
	for _, req := range eipRequests {
		sb.writeSubscription(topics.BucketScannerEthernetIP, subscriberKey+":"+req.DeviceID, req)
	}
	for _, req := range modbusRequests {
		sb.writeSubscription(topics.BucketScannerModbus, subscriberKey+":"+req.DeviceID, req)
	}
	for _, req := range opcuaRequests {
		sb.writeSubscription(topics.BucketScannerOpcUA, subscriberKey+":"+req.DeviceID, req)
	}
	for _, req := range snmpRequests {
		sb.writeSubscription(topics.BucketScannerSNMP, subscriberKey+":"+req.DeviceID, req)
	}

	// Subscribe to scanner data topics for all protocols that have input variables.
	protocols := make(map[string]bool)
	for _, vcfg := range config.Variables {
		if vcfg.Direction == "input" && vcfg.Source != nil {
			protocols[vcfg.Source.Protocol] = true
		}
	}

	for protocol := range protocols {
		sub, err := sb.b.Subscribe(topics.DataWildcard(protocol), func(subject string, data []byte, reply bus.ReplyFunc) {
			sb.handleScannerData(subject, data)
		})
		if err != nil {
			sb.log.Error("scanner_bridge: failed to subscribe", "protocol", protocol, "error", err)
			continue
		}
		sb.subs = append(sb.subs, sub)
		sb.log.Info("scanner_bridge: subscribed to scanner data", "protocol", protocol)
	}

	sb.log.Info("scanner_bridge: subscriptions configured",
		"tagIndexEntries", len(sb.tagIndex),
		"protocols", len(protocols))
}

func (sb *scannerBridge) writeSubscription(bucket, key string, req interface{}) {
	data, err := json.Marshal(req)
	if err != nil {
		sb.log.Error("scanner_bridge: failed to marshal subscribe request", "error", err)
		return
	}
	if _, err := sb.b.KVPut(bucket, key, data); err != nil {
		sb.log.Error("scanner_bridge: failed to write scanner config", "bucket", bucket, "key", key, "error", err)
	}
}

func (sb *scannerBridge) handleScannerData(subject string, data []byte) {
	// Subject format: {protocol}.data.{deviceId}.{tag}
	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 4 {
		return
	}
	protocol := parts[0]
	deviceID := parts[2]
	tag := parts[3]

	key := protocol + "." + deviceID + "." + tag
	varIDs, ok := sb.tagIndex[key]
	if !ok {
		return
	}

	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		sb.log.Error("scanner_bridge: failed to parse scanner data", "subject", subject, "error", err)
		return
	}

	now := time.Now().UnixMilli()
	for _, varID := range varIDs {
		sb.vars.Set(varID, msg.Value, now)
	}
}

func (sb *scannerBridge) unsubscribe() {
	for _, sub := range sb.subs {
		if sub != nil {
			sub.Unsubscribe()
		}
	}
	sb.subs = nil
}

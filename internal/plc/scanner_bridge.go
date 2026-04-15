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
	pnRequests := make(map[string]*itypes.ProfinetControllerSubscribeRequest)

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

		case "profinetcontroller":
			req, exists := pnRequests[dk]
			if !exists {
				cycleMs := 32
				if device.CycleTimeMs != nil {
					cycleMs = *device.CycleTimeMs
				}
				var vendorID, deviceIDPN uint16
				if device.VendorID != nil {
					vendorID = uint16(*device.VendorID)
				}
				if device.DeviceIDPN != nil {
					deviceIDPN = uint16(*device.DeviceIDPN)
				}
				req = &itypes.ProfinetControllerSubscribeRequest{
					SubscriberID:  "plc-" + sb.plcID,
					DeviceID:      src.DeviceID,
					StationName:   device.StationName,
					IP:            device.Host,
					InterfaceName: device.InterfaceName,
					VendorID:      vendorID,
					DeviceIDPN:    deviceIDPN,
					CycleTimeMs:   cycleMs,
				}
				pnRequests[dk] = req
			}
			// Build tag from variable source fields.
			pnDir := src.PnDirection
			if pnDir == "" {
				pnDir = vcfg.Direction // fall back to PLC variable direction
			}
			tag := itypes.ProfinetControllerTag{
				TagID:     src.Tag,
				Datatype:  src.PnDatatype,
				Direction: pnDir,
			}
			if src.PnByteOffset != nil {
				tag.ByteOffset = uint16(*src.PnByteOffset)
			}
			if src.PnBitOffset != nil {
				tag.BitOffset = uint8(*src.PnBitOffset)
			}
			// Find or create the matching slot/subslot.
			slotNum := uint16(1)
			if src.PnSlotNumber != nil {
				slotNum = uint16(*src.PnSlotNumber)
			}
			subslotNum := uint16(1)
			if src.PnSubslotNumber != nil {
				subslotNum = uint16(*src.PnSubslotNumber)
			}
			var moduleIdent uint32
			if src.PnModuleIdentNo != nil {
				moduleIdent = uint32(*src.PnModuleIdentNo)
			}
			var submoduleIdent uint32
			if src.PnSubmoduleIdentNo != nil {
				submoduleIdent = uint32(*src.PnSubmoduleIdentNo)
			}
			var inputSize, outputSize uint16
			if src.PnInputSize != nil {
				inputSize = uint16(*src.PnInputSize)
			}
			if src.PnOutputSize != nil {
				outputSize = uint16(*src.PnOutputSize)
			}
			// Find existing slot or create new one.
			slotIdx := -1
			for i, s := range req.Slots {
				if s.SlotNumber == slotNum {
					slotIdx = i
					break
				}
			}
			if slotIdx < 0 {
				req.Slots = append(req.Slots, itypes.ProfinetControllerSlot{
					SlotNumber:    slotNum,
					ModuleIdentNo: moduleIdent,
				})
				slotIdx = len(req.Slots) - 1
			}
			// Find existing subslot or create new one.
			subslotIdx := -1
			for i, ss := range req.Slots[slotIdx].Subslots {
				if ss.SubslotNumber == subslotNum {
					subslotIdx = i
					break
				}
			}
			if subslotIdx < 0 {
				req.Slots[slotIdx].Subslots = append(req.Slots[slotIdx].Subslots, itypes.ProfinetControllerSubslot{
					SubslotNumber:    subslotNum,
					SubmoduleIdentNo: submoduleIdent,
					InputSize:        inputSize,
					OutputSize:       outputSize,
				})
				subslotIdx = len(req.Slots[slotIdx].Subslots) - 1
			}
			req.Slots[slotIdx].Subslots[subslotIdx].Tags = append(
				req.Slots[slotIdx].Subslots[subslotIdx].Tags, tag,
			)
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
	for _, req := range pnRequests {
		sb.writeSubscription(topics.BucketScannerProfinetController, subscriberKey+":"+req.DeviceID, req)
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

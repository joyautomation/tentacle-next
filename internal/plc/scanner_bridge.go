//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/scanner"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// scannerBridge subscribes to protocol scanner topics and feeds values into the VariableStore.
type scannerBridge struct {
	b       bus.Bus
	plcID   string
	vars    *VariableStore
	devices *scanner.Registry
	log     *slog.Logger

	// Maps scanner subject key "{protocol}.{deviceId}.{tag}" to variable IDs.
	tagIndex map[string][]string

	// Known (subscriberId, deviceId, protocol) tuples so we can clean up on reconfig.
	active []activeSub

	subs []bus.Subscription
}

type activeSub struct {
	subscriberID string
	deviceID     string
	protocol     string
}

// newScannerBridge creates a scanner bridge.
func newScannerBridge(b bus.Bus, plcID string, vars *VariableStore, devices *scanner.Registry, log *slog.Logger) *scannerBridge {
	return &scannerBridge{
		b:        b,
		plcID:    plcID,
		vars:     vars,
		devices:  devices,
		log:      log,
		tagIndex: make(map[string][]string),
	}
}

// subscribe sets up subscriptions based on the PLC config's input variables
// plus any read_tag("dev", "tag") calls extracted from program source. The
// two paths are unioned so users can either declare a variable explicitly
// (when they need deadband/RBE config) or just reference the tag in code
// and have the runtime subscribe it automatically.
func (sb *scannerBridge) subscribe(config *itypes.PlcConfigKV, programSources map[string]string) {
	sb.unsubscribe()
	sb.tagIndex = make(map[string][]string)

	if sb.devices == nil {
		return
	}

	// Group input-variable tag specs by deviceID. The device's protocol is
	// the source of truth for the scanner; the variable's embedded
	// src.Protocol is preserved only for legacy compatibility.
	type deviceGroup struct {
		deviceID string
		tags     []scanner.TagSpec
		// seenTag dedupes tag names within a device when the same tag is
		// both declared as a variable and referenced by read_tag().
		seenTag map[string]struct{}
	}
	groups := make(map[string]*deviceGroup)
	getGroup := func(deviceID string) *deviceGroup {
		grp, ok := groups[deviceID]
		if !ok {
			grp = &deviceGroup{deviceID: deviceID, seenTag: make(map[string]struct{})}
			groups[deviceID] = grp
		}
		return grp
	}

	if config != nil {
		for _, vcfg := range config.Variables {
			if vcfg.Direction != "input" || vcfg.Source == nil {
				continue
			}
			src := vcfg.Source
			device, ok := sb.devices.Get(src.DeviceID)
			if !ok {
				sb.log.Warn("scanner_bridge: device not found for variable",
					"variable", vcfg.ID, "deviceId", src.DeviceID)
				continue
			}

			sanitizedTag := types.SanitizeForSubject(src.Tag)
			key := device.Protocol + "." + src.DeviceID + "." + sanitizedTag
			sb.tagIndex[key] = append(sb.tagIndex[key], vcfg.ID)

			grp := getGroup(src.DeviceID)
			if _, dup := grp.seenTag[src.Tag]; dup {
				continue
			}
			grp.seenTag[src.Tag] = struct{}{}
			grp.tags = append(grp.tags, scanner.TagSpecFromPlcSource(*src))
		}
	}

	// Fold in read_tag() refs. We only add a TagSpec if no declared variable
	// already covers the (device, tag) pair — declared variables carry
	// deadband/RBE config we want to preserve.
	for _, ref := range collectReadTagRefs(programSources) {
		if _, ok := sb.devices.Get(ref.DeviceID); !ok {
			sb.log.Warn("scanner_bridge: read_tag references unknown device",
				"deviceId", ref.DeviceID, "tag", ref.Tag)
			continue
		}
		grp := getGroup(ref.DeviceID)
		if _, dup := grp.seenTag[ref.Tag]; dup {
			continue
		}
		grp.seenTag[ref.Tag] = struct{}{}
		grp.tags = append(grp.tags, scanner.TagSpec{Tag: ref.Tag})
	}

	subscriberID := scanner.SubscriberID(serviceType, sb.plcID)
	protocols := make(map[string]bool)
	for _, grp := range groups {
		device, ok := sb.devices.Get(grp.deviceID)
		if !ok {
			continue
		}
		handled, err := scanner.WriteSubscription(sb.b, subscriberID, grp.deviceID, device, grp.tags, nil)
		if err != nil {
			sb.log.Error("scanner_bridge: failed to write scanner config",
				"device", grp.deviceID, "protocol", device.Protocol, "error", err)
			continue
		}
		if handled {
			sb.active = append(sb.active, activeSub{subscriberID: subscriberID, deviceID: grp.deviceID, protocol: device.Protocol})
		}
		protocols[device.Protocol] = true
	}

	// Subscribe to scanner data topics for all protocols in use.
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
	for _, a := range sb.active {
		scanner.DeleteSubscription(sb.b, a.subscriberID, a.deviceID, a.protocol)
	}
	sb.active = nil
}

//go:build gateway || all

package gateway

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/rbe"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

const udtDebounceDuration = 100 * time.Millisecond

// UdtAssembler collects member values for a single UDT variable instance
// and publishes an assembled object after a short debounce.
type UdtAssembler struct {
	mu              sync.Mutex
	config          itypes.GatewayUdtVariableConfig
	template        itypes.GatewayUdtTemplateConfig
	values          map[string]interface{} // member name → latest value
	dirty           bool
	timer           *time.Timer
	b               bus.Bus
	gatewayID       string
	variableID      string
	lastJSON        string                          // last published JSON (for RBE)
	lastValues      map[string]interface{}           // last published member values (for per-member RBE)
	lastPubTime     int64
	deadband        *types.DeadBandConfig            // effective UDT-level deadband (variable → device fallback)
	memberDeadbands map[string]types.DeadBandConfig  // per-member resolved deadbands
	disableRBE      bool
}

// NewUdtAssembler creates an assembler for a UDT variable instance.
func NewUdtAssembler(b bus.Bus, gatewayID, variableID string, config itypes.GatewayUdtVariableConfig, template itypes.GatewayUdtTemplateConfig, deadband *types.DeadBandConfig, disableRBE bool, memberDeadbands map[string]types.DeadBandConfig) *UdtAssembler {
	return &UdtAssembler{
		config:          config,
		template:        template,
		values:          make(map[string]interface{}),
		b:               b,
		gatewayID:       gatewayID,
		variableID:      variableID,
		deadband:        deadband,
		memberDeadbands: memberDeadbands,
		disableRBE:      disableRBE,
	}
}

// SetMember updates a member value and schedules a debounced publish.
func (a *UdtAssembler) SetMember(memberName string, value interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.values[memberName] = value
	a.dirty = true

	if a.timer != nil {
		a.timer.Stop()
	}
	a.timer = time.AfterFunc(udtDebounceDuration, func() {
		a.publish()
	})
}

// publish sends the assembled UDT object via the Bus.
func (a *UdtAssembler) publish() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.dirty {
		return
	}
	a.dirty = false

	// Build the assembled value object
	assembled := make(map[string]interface{})
	for _, member := range a.template.Members {
		if val, ok := a.values[member.Name]; ok {
			assembled[member.Name] = val
		}
	}

	nowMs := time.Now().UnixMilli()

	// RBE check
	if !a.disableRBE && a.lastPubTime > 0 {
		elapsed := nowMs - a.lastPubTime

		// UDT-level timing: maxTime forces publish, minTime suppresses
		if a.deadband != nil && a.deadband.MaxTime > 0 && elapsed >= a.deadband.MaxTime {
			// MaxTime exceeded — force publish regardless
		} else if a.deadband != nil && a.deadband.MinTime > 0 && elapsed < a.deadband.MinTime {
			// MinTime not elapsed — suppress and schedule deferred publish
			remaining := a.deadband.MinTime - elapsed
			a.dirty = true
			a.timer = time.AfterFunc(time.Duration(remaining)*time.Millisecond, func() {
				a.publish()
			})
			return
		} else if len(a.memberDeadbands) > 0 && a.lastValues != nil {
			// Per-member RBE: check if any member's change exceeds its deadband
			if !a.anyMemberExceedsDeadband(assembled) {
				return
			}
		} else {
			// Fallback: whole-JSON comparison
			jsonBytes, err := json.Marshal(assembled)
			if err != nil {
				return
			}
			if string(jsonBytes) == a.lastJSON {
				return
			}
		}
	}

	// Update tracking state
	jsonBytes, _ := json.Marshal(assembled)
	a.lastJSON = string(jsonBytes)
	a.lastValues = make(map[string]interface{}, len(assembled))
	for k, v := range assembled {
		a.lastValues[k] = v
	}
	a.lastPubTime = nowMs

	// Build UdtTemplateDefinition for inline sending.
	members := make([]types.UdtMemberDefinition, len(a.template.Members))
	for i, m := range a.template.Members {
		datatype := m.Datatype
		if a.config.MemberCipTypes != nil {
			if cipType, ok := a.config.MemberCipTypes[m.Name]; ok {
				datatype = itypes.CipToNatsDatatype(cipType)
			}
		}
		members[i] = types.UdtMemberDefinition{
			Name:        m.Name,
			Datatype:    datatype,
			TemplateRef: m.TemplateRef,
		}
	}

	msg := types.PlcDataMessage{
		ModuleID:   a.gatewayID,
		DeviceID:   a.config.DeviceID,
		VariableID: a.variableID,
		Value:      assembled,
		Timestamp:  nowMs,
		Datatype:   "udt",
		UdtTemplate: &types.UdtTemplateDefinition{
			Name:    a.template.Name,
			Version: a.template.Version,
			Members: members,
		},
	}
	if a.deadband != nil {
		msg.Deadband = a.deadband
	}
	if len(a.memberDeadbands) > 0 {
		msg.MemberDeadbands = a.memberDeadbands
	}
	if a.config.HistoryEnabled {
		msg.HistoryEnabled = true
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	subject := topics.Data(a.gatewayID, types.SanitizeForSubject(a.config.DeviceID), types.SanitizeForSubject(a.variableID))
	_ = a.b.Publish(subject, data)
}

// anyMemberExceedsDeadband checks if any member's change exceeds its configured deadband.
func (a *UdtAssembler) anyMemberExceedsDeadband(current map[string]interface{}) bool {
	for memberName, newVal := range current {
		prevVal, hasPrev := a.lastValues[memberName]
		if !hasPrev {
			return true // new member — publish
		}
		db, hasDb := a.memberDeadbands[memberName]
		if hasDb {
			// Numeric member with deadband: check threshold
			newNum, newOk := rbe.ToFloat64(newVal)
			prevNum, prevOk := rbe.ToFloat64(prevVal)
			if newOk && prevOk {
				if math.Abs(newNum-prevNum) > db.Value {
					return true
				}
				continue
			}
		}
		// Non-numeric or no deadband: publish on any change
		if newVal != prevVal {
			return true
		}
	}
	// Check for removed members
	for memberName := range a.lastValues {
		if _, ok := current[memberName]; !ok {
			return true
		}
	}
	return false
}

// Stop cancels any pending debounce timer.
func (a *UdtAssembler) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
}

// Value returns the current assembled value (for variables response).
func (a *UdtAssembler) Value() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	assembled := make(map[string]interface{})
	for _, member := range a.template.Members {
		if val, ok := a.values[member.Name]; ok {
			assembled[member.Name] = val
		}
	}
	return assembled
}

// Package module provides shared utilities for module status publishing.
package module

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/types"
)

// StatusVar describes a single status variable that a module exposes.
type StatusVar struct {
	Name     string `json:"name"`
	Datatype string `json:"datatype"` // "number", "boolean", "string"
}

// StatusValue holds a value and its datatype for publishing.
type StatusValue struct {
	Value    interface{}
	Datatype string
}

// PublishStatus publishes PlcDataMessages for each status variable.
// Subject pattern: {moduleType}.data.{moduleType}.{varName}
func PublishStatus(b bus.Bus, moduleType string, values map[string]StatusValue) {
	nowMs := time.Now().UnixMilli()
	for name, sv := range values {
		msg := types.PlcDataMessage{
			ModuleID:   moduleType,
			DeviceID:   moduleType,
			VariableID: name,
			Value:      sv.Value,
			Timestamp:  nowMs,
			Datatype:   sv.Datatype,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		subject := fmt.Sprintf("%s.data.%s.%s", moduleType, moduleType, name)
		_ = b.Publish(subject, data)
	}
}

// HandleStatusBrowse replies to a status.browse request with variable definitions.
func HandleStatusBrowse(variables []StatusVar, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}
	resp := struct {
		Variables []StatusVar `json:"variables"`
	}{Variables: variables}
	data, _ := json.Marshal(resp)
	_ = reply(data)
}

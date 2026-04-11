//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// publisher publishes changed PLC output variables to the bus.
type publisher struct {
	b     bus.Bus
	plcID string
	vars  *VariableStore
	log   *slog.Logger

	stopCh chan struct{}
	doneCh chan struct{}
}

// newPublisher creates a publisher that periodically drains changed variables.
func newPublisher(b bus.Bus, plcID string, vars *VariableStore, log *slog.Logger) *publisher {
	return &publisher{
		b:      b,
		plcID:  plcID,
		vars:   vars,
		log:    log,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// start begins the publish loop.
func (p *publisher) start() {
	go p.run()
}

func (p *publisher) run() {
	defer close(p.doneCh)
	// Drain changed variables every 10ms to batch publishes within a scan cycle.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.publishChanged()
		}
	}
}

func (p *publisher) publishChanged() {
	changed := p.vars.DrainChanged()
	if len(changed) == 0 {
		return
	}

	now := time.Now().UnixMilli()
	for _, v := range changed {
		// Only publish output and internal variables.
		if v.Direction == "input" {
			continue
		}

		msg := types.PlcDataMessage{
			ModuleID:   p.plcID,
			DeviceID:   p.plcID,
			VariableID: v.ID,
			Value:      v.Value,
			Timestamp:  now,
			Datatype:   v.Datatype,
		}

		data, err := json.Marshal(msg)
		if err != nil {
			p.log.Error("publish: marshal failed", "variable", v.ID, "error", err)
			continue
		}

		subject := topics.Data(p.plcID, p.plcID, types.SanitizeForSubject(v.ID))
		if err := p.b.Publish(subject, data); err != nil {
			p.log.Error("publish: failed", "variable", v.ID, "subject", subject, "error", err)
		}
	}
}

// stop signals the publisher to stop and waits for it to finish.
func (p *publisher) stop() {
	close(p.stopCh)
	<-p.doneCh
}

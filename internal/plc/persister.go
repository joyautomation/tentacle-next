//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
)

// persistedValue is the on-disk snapshot of a single runtime variable.
// Kept deliberately minimal — enough to restore the value and metadata
// on restart; scan-rate metadata (quality, lastUpdated) is advisory.
type persistedValue struct {
	VariableID  string      `json:"variableId"`
	Datatype    string      `json:"datatype"`
	Value       interface{} `json:"value"`
	Quality     string      `json:"quality,omitempty"`
	LastUpdated int64       `json:"lastUpdated,omitempty"`
}

// persister periodically snapshots changed variables to the plc_values
// KV bucket so values survive restart and redeploy.
//
// Design choice: persist on a debounce (default 1s) rather than on
// every change. On a 1000-tag PLC cycling at 100Hz we'd otherwise hammer
// the KV with ~100k writes/sec; a 1s debounce coalesces those to at
// most one write per variable per second — bounded and cheap.
type persister struct {
	b      bus.Bus
	vars   *VariableStore
	log    *slog.Logger
	period time.Duration

	stopCh chan struct{}
	doneCh chan struct{}
}

func newPersister(b bus.Bus, vars *VariableStore, log *slog.Logger) *persister {
	return &persister{
		b:      b,
		vars:   vars,
		log:    log,
		period: time.Second,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (p *persister) start() {
	go p.run()
}

func (p *persister) run() {
	defer close(p.doneCh)
	ticker := time.NewTicker(p.period)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			// Flush anything still dirty before exiting so a clean
			// shutdown captures the final state.
			p.flush()
			return
		case <-ticker.C:
			p.flush()
		}
	}
}

func (p *persister) flush() {
	changed := p.vars.DrainPersistDirty()
	if len(changed) == 0 {
		return
	}
	for _, v := range changed {
		pv := persistedValue{
			VariableID:  v.ID,
			Datatype:    v.Datatype,
			Value:       v.Value,
			Quality:     v.Quality,
			LastUpdated: v.LastUpdated,
		}
		data, err := json.Marshal(pv)
		if err != nil {
			p.log.Error("persist: marshal failed", "variable", v.ID, "error", err)
			continue
		}
		if _, err := p.b.KVPut(topics.BucketPlcValues, v.ID, data); err != nil {
			p.log.Error("persist: kv put failed", "variable", v.ID, "error", err)
		}
	}
}

func (p *persister) stop() {
	close(p.stopCh)
	<-p.doneCh
}

// loadPersistedValues returns the last-known value for every variable
// in the plc_values bucket. Called during module startup so restored
// values can overlay the config defaults before tasks begin running.
// Silently skips entries that fail to parse — a malformed snapshot
// shouldn't block the PLC from starting.
func loadPersistedValues(b bus.Bus, log *slog.Logger) map[string]persistedValue {
	out := map[string]persistedValue{}
	keys, err := b.KVKeys(topics.BucketPlcValues)
	if err != nil {
		return out
	}
	for _, k := range keys {
		data, _, err := b.KVGet(topics.BucketPlcValues, k)
		if err != nil {
			continue
		}
		var pv persistedValue
		if err := json.Unmarshal(data, &pv); err != nil {
			log.Warn("persist: skipping malformed snapshot", "key", k, "error", err)
			continue
		}
		out[k] = pv
	}
	return out
}

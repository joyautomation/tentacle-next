package plc

import (
	"fmt"
	"log/slog"
	"time"
)

// Config is the top-level PLC configuration.
type Config struct {
	ProjectID string
	Variables map[string]VariableConfig
	Tasks     map[string]TaskConfig
	NatsURL   string
	NatsUser  string
	NatsPass  string
	NatsToken string
	// SkipHeartbeat disables the 10s heartbeat publication.
	SkipHeartbeat bool
}

// Plc is a running PLC instance.
type Plc struct {
	config    Config
	vars      *Variables
	tasks     map[string]*taskRunner
	nats      *natsManager
	log       *slog.Logger
	startedAt int64
	done      chan struct{}
}

// Create initializes variables, connects to NATS, registers with protocol
// scanners, and starts all task scan loops.
func Create(cfg Config) (*Plc, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("plc: ProjectID is required")
	}
	if cfg.NatsURL == "" {
		cfg.NatsURL = "nats://localhost:4222"
	}

	log := slog.Default().With("plc", cfg.ProjectID)

	// Build runtime variables.
	vars := &Variables{vars: make(map[string]*Variable, len(cfg.Variables))}
	for id, vcfg := range cfg.Variables {
		vars.vars[id] = &Variable{
			value:    vcfg.Default,
			datatype: vcfg.Datatype,
		}
	}

	p := &Plc{
		config:    cfg,
		vars:      vars,
		tasks:     make(map[string]*taskRunner),
		log:       log,
		startedAt: time.Now().UnixMilli(),
		done:      make(chan struct{}),
	}

	// The update function sets the variable and publishes to NATS.
	update := func(variableID string, value interface{}) {
		v := p.vars.vars[variableID]
		if v == nil {
			return
		}
		v.set(value)
		if p.nats != nil {
			p.nats.publishVariable(variableID, value)
		}
	}

	// Connect to NATS.
	nm := newNatsManager(cfg.ProjectID, vars, cfg.Variables, update, log)
	if err := nm.connect(cfg); err != nil {
		return nil, err
	}
	p.nats = nm

	// Register with protocol scanners (tell them what to poll).
	nm.registerScannerSubscriptions()

	// Set up NATS subscriptions (commands, variables request, source data).
	if err := nm.setupSubscriptions(); err != nil {
		nm.close()
		return nil, fmt.Errorf("plc: subscriptions: %w", err)
	}

	// Start heartbeat.
	if !cfg.SkipHeartbeat {
		nm.startHeartbeat(p.startedAt)
	}

	// Publish initial variable values.
	for id, v := range vars.vars {
		nm.publishVariable(id, v.Value())
	}

	// Start task scan loops.
	for taskID, taskCfg := range cfg.Tasks {
		runner := newTaskRunner(taskCfg, vars, update, log)
		p.tasks[taskID] = runner
		runner.start()
	}

	log.Info("PLC started", "variables", len(cfg.Variables), "tasks", len(cfg.Tasks))
	return p, nil
}

// Stop gracefully shuts down all tasks and the NATS connection.
func (p *Plc) Stop() error {
	for _, runner := range p.tasks {
		runner.stop()
	}
	if p.nats != nil {
		p.nats.close()
	}
	p.log.Info("PLC stopped")
	close(p.done)
	return nil
}

// Wait blocks until Stop is called.
func (p *Plc) Wait() { <-p.done }

// Variables returns the runtime variable accessor.
func (p *Plc) Variables() *Variables { return p.vars }

//go:build history || all

// Package history stores PLC/gateway data into PostgreSQL (optionally with
// TimescaleDB hypertable, compression, and retention policies).
// It subscribes to all data topics on the bus, applies RBE deadband filtering,
// and batch-inserts records periodically.
package history

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
)

const serviceType = "history"

// History implements module.Module for the TimescaleDB historian.
type History struct {
	moduleID string

	log *slog.Logger

	mu  sync.Mutex
	b   bus.Bus
	db  *sql.DB
	sub bus.Subscription

	// Lifecycle subscriptions (shutdown, etc.).
	subs []bus.Subscription

	stopHeartbeat   func()
	stopFlusher     chan struct{}
	stopCompressor  chan struct{}
	hyperEnabled    bool

	batch     *batchBuffer
	rbeStates *variableRBEState
	stats     *historyStats
}

// New creates a new History module instance.
func New(moduleID string) *History {
	if moduleID == "" {
		moduleID = "history"
	}
	return &History{
		moduleID:  moduleID,
		batch:     newBatchBuffer(),
		rbeStates: newVariableRBEState(),
		stats:     &historyStats{},
	}
}

func (h *History) ModuleID() string    { return h.moduleID }
func (h *History) ServiceType() string { return serviceType }

// Start initializes the history module: loads config from env, connects to
// PostgreSQL, ensures the schema, optionally enables TimescaleDB features,
// starts the heartbeat, subscribes to data topics, and blocks until shutdown.
func (h *History) Start(ctx context.Context, b bus.Bus) error {
	h.b = b
	h.log = slog.Default().With("serviceType", h.ServiceType(), "moduleID", h.ModuleID())

	// Load configuration from environment variables.
	cfg := loadConfigFromEnv()
	h.log.Info("history: loaded config",
		"host", cfg.DBHost,
		"port", cfg.DBPort,
		"db", cfg.DBName,
		"enableHyper", cfg.EnableHyper,
		"retentionDays", cfg.RetentionDays,
	)

	// Connect to PostgreSQL.
	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	h.db = db
	h.log.Info("history: connected to PostgreSQL")

	// Ensure schema exists.
	if err := ensureSchema(db, h.log); err != nil {
		h.db.Close()
		return err
	}

	// Optionally enable TimescaleDB features.
	if cfg.EnableHyper {
		if enableHypertable(db, h.log) {
			h.hyperEnabled = true

			if err := enableCompressionPolicy(db, h.log); err != nil {
				h.log.Warn("history: failed to enable compression policy", "error", err)
			}
			if err := enableRetentionPolicy(db, cfg.RetentionDays, h.log); err != nil {
				h.log.Warn("history: failed to enable retention policy", "error", err)
			}

			// Start hourly compression goroutine.
			h.stopCompressor = make(chan struct{})
			go h.compressionLoop()
		}
	}

	// Start heartbeat.
	h.stopHeartbeat = heartbeat.Start(b, h.moduleID, serviceType, func() map[string]interface{} {
		meta := h.stats.snapshot()
		meta["hyperEnabled"] = h.hyperEnabled
		meta["bufferSize"] = h.batch.len()
		return meta
	})

	// Subscribe to all data topics.
	dataSub, err := b.Subscribe(topics.AllData(), func(subject string, data []byte, reply bus.ReplyFunc) {
		h.handleDataMessage(subject, data)
	})
	if err != nil {
		h.log.Error("history: failed to subscribe to data topics", "error", err)
		h.cleanup()
		return err
	}
	h.sub = dataSub

	// Listen for shutdown via bus.
	shutdownSub, _ := b.Subscribe(topics.Shutdown(h.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		h.log.Info("history: received shutdown command via Bus")
		h.Stop()
		os.Exit(0)
	})
	h.mu.Lock()
	h.subs = append(h.subs, shutdownSub)
	h.mu.Unlock()

	// Start the periodic batch flusher.
	h.stopFlusher = make(chan struct{})
	go h.flushLoop()

	h.log.Info("history: module started", "moduleID", h.moduleID)

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop gracefully shuts down the history module.
func (h *History) Stop() error {
	h.log.Info("history: stopping", "moduleID", h.moduleID)

	// Stop the periodic flusher.
	if h.stopFlusher != nil {
		close(h.stopFlusher)
	}

	// Stop the compression loop.
	if h.stopCompressor != nil {
		close(h.stopCompressor)
	}

	// Final flush of any remaining records.
	h.flushBatch()

	h.cleanup()
	h.log.Info("history: stopped", "moduleID", h.moduleID)
	return nil
}

// cleanup releases subscriptions, heartbeat, and database connection.
func (h *History) cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.sub != nil {
		_ = h.sub.Unsubscribe()
		h.sub = nil
	}
	for _, sub := range h.subs {
		_ = sub.Unsubscribe()
	}
	h.subs = nil

	if h.stopHeartbeat != nil {
		h.stopHeartbeat()
		h.stopHeartbeat = nil
	}
	if h.db != nil {
		h.db.Close()
		h.db = nil
	}
}

// flushLoop runs a ticker that flushes the batch buffer every batchFlushPeriod.
func (h *History) flushLoop() {
	ticker := time.NewTicker(batchFlushPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopFlusher:
			return
		case <-ticker.C:
			h.flushBatch()
		}
	}
}

// compressionLoop runs hourly manual compression when TimescaleDB is enabled.
func (h *History) compressionLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCompressor:
			return
		case <-ticker.C:
			if h.db != nil {
				if err := compressEligibleChunks(h.db, h.log); err != nil {
					h.log.Warn("history: hourly compression failed", "error", err)
				}
			}
		}
	}
}

// flushBatch drains the batch buffer and inserts all records into the database.
func (h *History) flushBatch() {
	records := h.batch.drain()
	if len(records) == 0 {
		return
	}

	h.mu.Lock()
	db := h.db
	h.mu.Unlock()

	if db == nil {
		h.log.Warn("history: database not available, dropping batch", "count", len(records))
		return
	}

	if err := insertBatch(db, records); err != nil {
		h.log.Error("history: batch insert failed", "count", len(records), "error", err)
		return
	}

	now := time.Now().UnixMilli()
	h.stats.addBatch(int64(len(records)))
	h.stats.setLastFlush(now)
	h.log.Debug("history: flushed batch", "count", len(records))
}

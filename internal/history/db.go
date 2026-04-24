//go:build history || all || mantle

package history

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/lib/pq"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// openDB opens and pings a PostgreSQL connection.
func openDB(cfg itypes.HistoryConfig) (*sql.DB, error) {
	db, err := sql.Open("postgres", connString(cfg))
	if err != nil {
		return nil, fmt.Errorf("history: sql.Open: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("history: db.Ping: %w", err)
	}
	return db, nil
}

// ensureSchema creates the history, history_properties, metric_properties, and
// hidden_items tables with constraints and indexes matching the mantle schema.
func ensureSchema(db *sql.DB, log *slog.Logger) error {
	// ── history table ──
	const createHistory = `
CREATE TABLE IF NOT EXISTS history (
    group_id     TEXT            NOT NULL,
    node_id      TEXT            NOT NULL,
    device_id    TEXT            DEFAULT '',
    metric_id    TEXT            NOT NULL,
    int_value    BIGINT,
    float_value  REAL,
    string_value TEXT,
    bool_value   BOOLEAN,
    timestamp    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    CONSTRAINT history_group_id_node_id_device_id_metric_id_timestamp_unique
        UNIQUE (group_id, node_id, device_id, metric_id, timestamp)
);`

	const createHistoryIndex = `
CREATE INDEX IF NOT EXISTS idx_history_metric_time
    ON history (group_id, node_id, device_id, metric_id, timestamp DESC);`

	// ── history_properties table ──
	const createHistoryProperties = `
CREATE TABLE IF NOT EXISTS history_properties (
    group_id     TEXT            NOT NULL,
    node_id      TEXT            NOT NULL,
    device_id    TEXT            DEFAULT '',
    metric_id    TEXT            NOT NULL,
    property_id  TEXT            NOT NULL,
    int_value    BIGINT,
    float_value  REAL,
    string_value TEXT,
    bool_value   BOOLEAN,
    timestamp    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    CONSTRAINT history_properties_group_id_node_id_device_id_metric_id_property_id_timestamp_unique
        UNIQUE (group_id, node_id, device_id, metric_id, property_id, timestamp)
);`

	const createHistoryPropertiesIndex = `
CREATE INDEX IF NOT EXISTS idx_history_properties_metric_time
    ON history_properties (group_id, node_id, device_id, metric_id, property_id, timestamp DESC);`

	// ── metric_properties table ──
	const createMetricProperties = `
CREATE TABLE IF NOT EXISTS metric_properties (
    group_id   TEXT  NOT NULL,
    node_id    TEXT  NOT NULL,
    device_id  TEXT  DEFAULT '' NOT NULL,
    metric_id  TEXT  NOT NULL,
    properties JSONB DEFAULT '{}' NOT NULL,
    CONSTRAINT metric_properties_group_id_node_id_device_id_metric_id_pk
        PRIMARY KEY (group_id, node_id, device_id, metric_id)
);`

	// ── hidden_items table ──
	const createHiddenItems = `
CREATE TABLE IF NOT EXISTS hidden_items (
    group_id  TEXT            NOT NULL,
    node_id   TEXT            NOT NULL,
    device_id TEXT            DEFAULT '' NOT NULL,
    metric_id TEXT            DEFAULT '' NOT NULL,
    hidden_at TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    CONSTRAINT hidden_items_group_id_node_id_device_id_metric_id_pk
        PRIMARY KEY (group_id, node_id, device_id, metric_id)
);`

	stmts := []string{
		createHistory, createHistoryIndex,
		createHistoryProperties, createHistoryPropertiesIndex,
		createMetricProperties,
		createHiddenItems,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("history: ensureSchema: %w", err)
		}
	}
	log.Info("history: schema ensured")
	return nil
}

// insertBatch inserts multiple HistoryRecords in a single multi-row INSERT.
func insertBatch(db *sql.DB, records []itypes.HistoryRecord) error {
	if len(records) == 0 {
		return nil
	}

	const cols = 9 // group_id, node_id, device_id, metric_id, int_value, float_value, string_value, bool_value, timestamp
	valueStrings := make([]string, 0, len(records))
	args := make([]interface{}, 0, len(records)*cols)

	for i, rec := range records {
		base := i * cols
		valueStrings = append(valueStrings,
			fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9))
		ts := time.UnixMilli(rec.Timestamp)
		args = append(args, rec.GroupID, rec.NodeID, rec.DeviceID, rec.MetricID,
			rec.IntValue, rec.FloatValue, rec.StringValue, rec.BoolValue, ts)
	}

	query := fmt.Sprintf(
		`INSERT INTO history (group_id, node_id, device_id, metric_id, int_value, float_value, string_value, bool_value, timestamp)
		 VALUES %s
		 ON CONFLICT ON CONSTRAINT history_group_id_node_id_device_id_metric_id_timestamp_unique DO NOTHING`,
		strings.Join(valueStrings, ", "),
	)

	_, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("history: insertBatch (%d records): %w", len(records), err)
	}
	return nil
}

//go:build history || all || mantle

package history

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// enableHypertables converts the history and history_properties tables to
// TimescaleDB hypertables. Returns false if TimescaleDB is not installed.
func enableHypertables(db *sql.DB, log *slog.Logger) bool {
	// Try to create the extension if it's available but not yet enabled on the db.
	// Harmless if already present; fails only if the package isn't installed at the OS level.
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb`); err != nil {
		log.Warn("history: could not create timescaledb extension (package may not be installed)", "error", err)
		return false
	}

	_, err := db.Exec(`SELECT create_hypertable('history', 'timestamp', if_not_exists => TRUE)`)
	if err != nil {
		log.Warn("history: could not create hypertable (TimescaleDB may not be installed)", "error", err)
		return false
	}
	log.Info("history: hypertable enabled on history table")

	_, err = db.Exec(`SELECT create_hypertable('history_properties', 'timestamp', if_not_exists => TRUE)`)
	if err != nil {
		log.Warn("history: could not create hypertable on history_properties", "error", err)
	} else {
		log.Info("history: hypertable enabled on history_properties table")
	}

	// Set daily chunk intervals to match mantle.
	db.Exec(`SELECT set_chunk_time_interval('history', INTERVAL '1 day')`)
	db.Exec(`SELECT set_chunk_time_interval('history_properties', INTERVAL '1 day')`)

	return true
}

// enableCompressionPolicy sets compression policies on the hypertables.
// Segments by (group_id, node_id, device_id, metric_id), orders by timestamp DESC.
func enableCompressionPolicy(db *sql.DB, log *slog.Logger) error {
	const alterHistory = `
ALTER TABLE history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'group_id, node_id, device_id, metric_id',
    timescaledb.compress_orderby = 'timestamp DESC NULLS FIRST'
);`
	if _, err := db.Exec(alterHistory); err != nil {
		return fmt.Errorf("history: enable compression settings: %w", err)
	}

	const addHistoryPolicy = `SELECT add_compression_policy('history', INTERVAL '1 hour', if_not_exists => TRUE)`
	if _, err := db.Exec(addHistoryPolicy); err != nil {
		return fmt.Errorf("history: add compression policy: %w", err)
	}

	log.Info("history: compression policy enabled (compress chunks > 1 hour)")
	return nil
}

// enableRetentionPolicy creates a data retention policy that drops chunks
// older than the configured number of days.
func enableRetentionPolicy(db *sql.DB, retentionDays int, log *slog.Logger) error {
	if retentionDays <= 0 {
		log.Info("history: retention policy disabled (retentionDays <= 0)")
		return nil
	}

	query := fmt.Sprintf(
		`SELECT add_retention_policy('history', INTERVAL '%d days', if_not_exists => TRUE)`,
		retentionDays,
	)
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("history: add retention policy: %w", err)
	}
	log.Info("history: retention policy enabled", "days", retentionDays)
	return nil
}

// compressEligibleChunks manually triggers compression on any chunks that
// are eligible. Intended to be called periodically (e.g., every hour).
func compressEligibleChunks(db *sql.DB, log *slog.Logger) error {
	const query = `
SELECT compress_chunk(c.chunk_name, if_not_compressed => TRUE)
FROM timescaledb_information.chunks c
WHERE c.hypertable_name = 'history'
  AND c.is_compressed = false
  AND c.range_end < NOW() - INTERVAL '1 hour'`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("history: compressEligibleChunks: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("history: compressEligibleChunks iteration: %w", err)
	}
	if count > 0 {
		log.Info("history: compressed eligible chunks", "count", count)
	}
	return nil
}

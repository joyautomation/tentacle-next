//go:build history || all

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

// ensureSchema creates the history table, unique constraint, and index if
// they do not already exist.
func ensureSchema(db *sql.DB, log *slog.Logger) error {
	const createTable = `
CREATE TABLE IF NOT EXISTS history (
    module_id    TEXT            NOT NULL,
    variable_id  TEXT            NOT NULL,
    int_value    BIGINT,
    float_value  REAL,
    string_value TEXT,
    bool_value   BOOLEAN,
    timestamp    TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);`

	const createUnique = `
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'history_module_variable_ts_unique'
    ) THEN
        ALTER TABLE history ADD CONSTRAINT history_module_variable_ts_unique
            UNIQUE (module_id, variable_id, timestamp);
    END IF;
END
$$;`

	const createIndex = `
CREATE INDEX IF NOT EXISTS idx_history_module_variable_ts
    ON history (module_id, variable_id, timestamp);`

	for _, stmt := range []string{createTable, createUnique, createIndex} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("history: ensureSchema: %w", err)
		}
	}
	log.Info("history: schema ensured")
	return nil
}

// insertRecord inserts a single HistoryRecord into the database.
func insertRecord(db *sql.DB, rec itypes.HistoryRecord) error {
	ts := time.UnixMilli(rec.Timestamp)
	_, err := db.Exec(
		`INSERT INTO history (module_id, variable_id, int_value, float_value, string_value, bool_value, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT ON CONSTRAINT history_module_variable_ts_unique DO NOTHING`,
		rec.ModuleID, rec.VariableID, rec.IntValue, rec.FloatValue, rec.StringValue, rec.BoolValue, ts,
	)
	return err
}

// insertBatch inserts multiple HistoryRecords in a single multi-row INSERT
// for performance.
func insertBatch(db *sql.DB, records []itypes.HistoryRecord) error {
	if len(records) == 0 {
		return nil
	}

	const cols = 7 // module_id, variable_id, int_value, float_value, string_value, bool_value, timestamp
	valueStrings := make([]string, 0, len(records))
	args := make([]interface{}, 0, len(records)*cols)

	for i, rec := range records {
		base := i * cols
		valueStrings = append(valueStrings,
			fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7))
		ts := time.UnixMilli(rec.Timestamp)
		args = append(args, rec.ModuleID, rec.VariableID, rec.IntValue, rec.FloatValue, rec.StringValue, rec.BoolValue, ts)
	}

	query := fmt.Sprintf(
		`INSERT INTO history (module_id, variable_id, int_value, float_value, string_value, bool_value, timestamp)
		 VALUES %s
		 ON CONFLICT ON CONSTRAINT history_module_variable_ts_unique DO NOTHING`,
		strings.Join(valueStrings, ", "),
	)

	_, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("history: insertBatch (%d records): %w", len(records), err)
	}
	return nil
}

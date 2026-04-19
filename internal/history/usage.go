//go:build history || all

package history

import (
	"context"
	"database/sql"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// queryUsage returns total row count, approximate storage size, and oldest
// record timestamp. Size comes from pg_total_relation_size('history').
func queryUsage(ctx context.Context, db *sql.DB) (*itypes.HistoryUsageStats, error) {
	stats := &itypes.HistoryUsageStats{}

	row := db.QueryRowContext(ctx, `SELECT COALESCE(pg_total_relation_size('history'), 0)`)
	if err := row.Scan(&stats.TotalSize); err != nil {
		return nil, err
	}

	row = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history`)
	if err := row.Scan(&stats.RowCount); err != nil {
		return nil, err
	}

	var oldest sql.NullTime
	row = db.QueryRowContext(ctx, `SELECT MIN(timestamp) FROM history`)
	if err := row.Scan(&oldest); err != nil {
		return nil, err
	}
	if oldest.Valid {
		stats.OldestRecord = oldest.Time.UnixMilli()
	}

	monthRows, err := db.QueryContext(ctx, `
SELECT to_char(date_trunc('month', timestamp), 'YYYY-MM') AS month, COUNT(*)
FROM history
GROUP BY month
ORDER BY month DESC
LIMIT 24`)
	if err == nil {
		defer monthRows.Close()
		for monthRows.Next() {
			var mu itypes.HistoryMonthUsage
			if err := monthRows.Scan(&mu.Month, &mu.RowCount); err != nil {
				return nil, err
			}
			stats.ByMonth = append(stats.ByMonth, mu)
		}
	}

	return stats, nil
}

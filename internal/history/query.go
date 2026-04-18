//go:build history || all

package history

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// queryHistory runs a bounded history query and returns per-metric point arrays.
// When raw=true, returns every stored point inside [start,end] for each metric.
// When raw=false and samples>0, uses TimescaleDB time_bucket to aggregate into
// `samples` evenly-sized buckets with min/avg/max for numeric metrics; falls
// back to last-value bucketing for non-numeric (bool/string) metrics.
func queryHistory(ctx context.Context, db *sql.DB, req itypes.HistoryQueryRequest) ([]itypes.HistoryMetricData, error) {
	if len(req.Metrics) == 0 {
		return []itypes.HistoryMetricData{}, nil
	}
	startTs := time.UnixMilli(req.Start)
	endTs := time.UnixMilli(req.End)

	out := make([]itypes.HistoryMetricData, 0, len(req.Metrics))
	for _, m := range req.Metrics {
		var (
			points []itypes.HistoryPoint
			err    error
		)
		if req.Raw || req.Samples <= 0 {
			points, err = queryRawPoints(ctx, db, m, startTs, endTs)
		} else {
			points, err = queryBucketedPoints(ctx, db, m, startTs, endTs, req.Samples)
		}
		if err != nil {
			return nil, fmt.Errorf("query metric %s/%s/%s/%s: %w", m.GroupID, m.NodeID, m.DeviceID, m.MetricID, err)
		}
		out = append(out, itypes.HistoryMetricData{
			GroupID:  m.GroupID,
			NodeID:   m.NodeID,
			DeviceID: m.DeviceID,
			MetricID: m.MetricID,
			Points:   points,
		})
	}
	return out, nil
}

func queryRawPoints(ctx context.Context, db *sql.DB, m itypes.HistoryMetricRef, startTs, endTs time.Time) ([]itypes.HistoryPoint, error) {
	const sqlStmt = `
SELECT timestamp, int_value, float_value, string_value, bool_value
FROM history
WHERE group_id = $1 AND node_id = $2 AND device_id = $3 AND metric_id = $4
  AND timestamp BETWEEN $5 AND $6
ORDER BY timestamp ASC`
	rows, err := db.QueryContext(ctx, sqlStmt, m.GroupID, m.NodeID, m.DeviceID, m.MetricID, startTs, endTs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []itypes.HistoryPoint{}
	for rows.Next() {
		var (
			ts    time.Time
			iv    sql.NullInt64
			fv    sql.NullFloat64
			sv    sql.NullString
			bv    sql.NullBool
		)
		if err := rows.Scan(&ts, &iv, &fv, &sv, &bv); err != nil {
			return nil, err
		}
		p := itypes.HistoryPoint{Timestamp: ts.UnixMilli()}
		if iv.Valid {
			v := iv.Int64
			p.IntValue = &v
		}
		if fv.Valid {
			v := fv.Float64
			p.FloatValue = &v
		}
		if sv.Valid {
			v := sv.String
			p.StringValue = &v
		}
		if bv.Valid {
			v := bv.Bool
			p.BoolValue = &v
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// queryBucketedPoints uses time_bucket to aggregate numeric metrics into
// `samples` buckets. Returns avg/min/max per bucket plus the first non-null
// string/bool encountered (so downsampled boolean/string metrics still render).
func queryBucketedPoints(ctx context.Context, db *sql.DB, m itypes.HistoryMetricRef, startTs, endTs time.Time, samples int) ([]itypes.HistoryPoint, error) {
	durMs := endTs.Sub(startTs).Milliseconds()
	if durMs <= 0 || samples <= 0 {
		return queryRawPoints(ctx, db, m, startTs, endTs)
	}
	bucketMs := durMs / int64(samples)
	if bucketMs < 1 {
		bucketMs = 1
	}
	bucketInterval := fmt.Sprintf("%d milliseconds", bucketMs)

	const sqlStmt = `
SELECT
  time_bucket($7::interval, timestamp) AS bucket,
  AVG(COALESCE(float_value::double precision, int_value::double precision)) AS avg_v,
  MIN(COALESCE(float_value::double precision, int_value::double precision)) AS min_v,
  MAX(COALESCE(float_value::double precision, int_value::double precision)) AS max_v,
  bool_or(bool_value) FILTER (WHERE bool_value IS NOT NULL) AS bool_v,
  (ARRAY_AGG(string_value) FILTER (WHERE string_value IS NOT NULL))[1] AS string_v
FROM history
WHERE group_id = $1 AND node_id = $2 AND device_id = $3 AND metric_id = $4
  AND timestamp BETWEEN $5 AND $6
GROUP BY bucket
ORDER BY bucket ASC`

	rows, err := db.QueryContext(ctx, sqlStmt, m.GroupID, m.NodeID, m.DeviceID, m.MetricID, startTs, endTs, bucketInterval)
	if err != nil {
		// If time_bucket isn't available (no TimescaleDB), fall back to raw.
		if strings.Contains(strings.ToLower(err.Error()), "time_bucket") {
			return queryRawPoints(ctx, db, m, startTs, endTs)
		}
		return nil, err
	}
	defer rows.Close()
	points := []itypes.HistoryPoint{}
	for rows.Next() {
		var (
			bucket time.Time
			avg    sql.NullFloat64
			mn     sql.NullFloat64
			mx     sql.NullFloat64
			bv     sql.NullBool
			sv     sql.NullString
		)
		if err := rows.Scan(&bucket, &avg, &mn, &mx, &bv, &sv); err != nil {
			return nil, err
		}
		p := itypes.HistoryPoint{Timestamp: bucket.UnixMilli()}
		if avg.Valid {
			v := avg.Float64
			p.Avg = &v
		}
		if mn.Valid {
			v := mn.Float64
			p.Min = &v
		}
		if mx.Valid {
			v := mx.Float64
			p.Max = &v
		}
		if bv.Valid {
			v := bv.Bool
			p.BoolValue = &v
		}
		if sv.Valid {
			v := sv.String
			p.StringValue = &v
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// queryMetricsList returns every distinct (group, node, device, metric) key
// currently in the history table. Ordered for stable tree rendering.
func queryMetricsList(ctx context.Context, db *sql.DB) ([]itypes.HistoryMetricRef, error) {
	const sqlStmt = `
SELECT DISTINCT group_id, node_id, device_id, metric_id
FROM history
ORDER BY group_id, node_id, device_id, metric_id`
	rows, err := db.QueryContext(ctx, sqlStmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []itypes.HistoryMetricRef{}
	for rows.Next() {
		var ref itypes.HistoryMetricRef
		if err := rows.Scan(&ref.GroupID, &ref.NodeID, &ref.DeviceID, &ref.MetricID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

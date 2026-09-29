package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
)

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms) }

func nullTime(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64)
}

func (s *Store) ListSeries(ctx context.Context) ([]history.SeriesInfo, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT key, name, unit, labels_json, first_ts, last_ts
		FROM metric_series WHERE last_ts IS NOT NULL ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []history.SeriesInfo
	for rows.Next() {
		var si history.SeriesInfo
		var labels string
		var first, last sql.NullInt64
		if err := rows.Scan(&si.Key, &si.Name, &si.Unit, &labels, &first, &last); err != nil {
			return nil, err
		}
		if labels != "" && labels != "{}" {
			_ = json.Unmarshal([]byte(labels), &si.Labels)
		}
		si.First, si.Last = nullTime(first), nullTime(last)
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *Store) QueryRange(ctx context.Context, key string, from, to time.Time, buckets int) ([]history.Bucket, error) {
	if buckets < 1 {
		buckets = 1
	}
	if !to.After(from) {
		return nil, fmt.Errorf("invalid range %s..%s", from, to)
	}
	width := to.Sub(from).Milliseconds() / int64(buckets)
	if width < 1000 {
		width = 1000
	}
	rows, err := s.read.QueryContext(ctx, `
		SELECT ?3 + ((m.ts - ?3) / ?1) * ?1 AS b, AVG(m.value), MIN(m.min), MAX(m.max), SUM(m.count)
		FROM metric_samples m
		WHERE m.series_id = (SELECT id FROM metric_series WHERE key = ?2)
		  AND m.ts >= ?3 AND m.ts < ?4
		GROUP BY b ORDER BY b`,
		width, key, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []history.Bucket
	for rows.Next() {
		var b history.Bucket
		var ts int64
		if err := rows.Scan(&ts, &b.Avg, &b.Min, &b.Max, &b.Count); err != nil {
			return nil, err
		}
		b.At = fromMillis(ts)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) RecentChecks(ctx context.Context, serviceID string, limit int) ([]history.ServiceCheck, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT ts, status, ok, latency_ms, status_code, error FROM service_checks
		WHERE service_id = ? ORDER BY ts DESC LIMIT ?`, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []history.ServiceCheck
	for rows.Next() {
		c := history.ServiceCheck{ServiceID: serviceID}
		var ts int64
		var ok int
		var latency sql.NullFloat64
		var code sql.NullInt64
		var errText sql.NullString
		if err := rows.Scan(&ts, &c.Status, &ok, &latency, &code, &errText); err != nil {
			return nil, err
		}
		c.At, c.OK = fromMillis(ts), ok == 1
		c.LatencyMs, c.StatusCode, c.Err = latency.Float64, int(code.Int64), errText.String
		out = append(out, c)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

const incidentCols = `id, rule_id, rule_name, metric_name, severity, message, threshold, unit,
	started_at, ended_at, acknowledged_at, peak_value, last_value`

func scanIncidents(rows *sql.Rows) ([]alert.Incident, error) {
	defer rows.Close()
	var out []alert.Incident
	for rows.Next() {
		var inc alert.Incident
		var sev string
		var started int64
		var ended, acked sql.NullInt64
		var peak, last sql.NullFloat64
		if err := rows.Scan(&inc.ID, &inc.RuleID, &inc.RuleName, &inc.Metric, &sev, &inc.Message, &inc.Threshold, &inc.Unit,
			&started, &ended, &acked, &peak, &last); err != nil {
			return nil, err
		}
		inc.Severity, _ = alert.ParseSeverity(sev)
		inc.StartedAt, inc.EndedAt, inc.AckedAt = fromMillis(started), nullTime(ended), nullTime(acked)
		inc.PeakValue, inc.LastValue = peak.Float64, last.Float64
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *Store) RecentIncidents(ctx context.Context, limit int) ([]alert.Incident, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+incidentCols+" FROM alert_events ORDER BY started_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	return scanIncidents(rows)
}

func (s *Store) OpenIncidents(ctx context.Context) ([]alert.Incident, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+incidentCols+" FROM alert_events WHERE ended_at IS NULL ORDER BY started_at")
	if err != nil {
		return nil, err
	}
	return scanIncidents(rows)
}

func (s *Store) Stats(ctx context.Context) (history.DBStats, error) {
	st := history.DBStats{Path: s.path, At: time.Now()}
	if fi, err := os.Stat(s.path); err == nil {
		st.SizeBytes = fi.Size()
	}
	if fi, err := os.Stat(s.path + "-wal"); err == nil {
		st.WALBytes = fi.Size()
	}
	err := s.read.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT MAX(id) - MIN(id) + 1 FROM metric_samples), 0),
			(SELECT COUNT(*) FROM service_checks),
			(SELECT COUNT(*) FROM alert_events),
			(SELECT COUNT(*) FROM metric_series),
			(SELECT COALESCE(MAX(version), 0) FROM schema_migrations)`).
		Scan(&st.MetricRows, &st.CheckRows, &st.IncidentRows, &st.Series, &st.SchemaVersion)
	return st, err
}

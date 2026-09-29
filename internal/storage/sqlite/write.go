package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
)

func (s *Store) WriteBatch(ctx context.Context, b history.Batch) error {
	if b.Len() == 0 {
		return nil
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	newSeries, err := s.writeMetrics(ctx, tx, b.Metrics)
	if err != nil {
		return err
	}
	if err := writeChecks(ctx, tx, b.Checks); err != nil {
		return err
	}
	if err := writeIncidents(ctx, tx, b.Incidents); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if len(newSeries) > 0 {
		s.mu.Lock()
		for k, id := range newSeries {
			s.series[k] = id
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *Store) writeMetrics(ctx context.Context, tx *sql.Tx, pts []history.MetricPoint) (map[string]int64, error) {
	if len(pts) == 0 {
		return nil, nil
	}
	upsertSeries, err := tx.PrepareContext(ctx, `
		INSERT INTO metric_series(key, name, unit, source, labels_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET unit = excluded.unit
		RETURNING id`)
	if err != nil {
		return nil, err
	}
	defer upsertSeries.Close()
	insert, err := tx.PrepareContext(ctx, `
		INSERT INTO metric_samples(series_id, ts, value, min, max, count) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(series_id, ts) DO UPDATE SET
			value = (value * count + excluded.value * excluded.count) / (count + excluded.count),
			min   = MIN(min, excluded.min),
			max   = MAX(max, excluded.max),
			count = count + excluded.count`)
	if err != nil {
		return nil, err
	}
	defer insert.Close()
	bounds, err := tx.PrepareContext(ctx, `
		UPDATE metric_series SET
			first_ts = MIN(COALESCE(first_ts, ?1), ?1),
			last_ts  = MAX(COALESCE(last_ts, ?2), ?2)
		WHERE id = ?3`)
	if err != nil {
		return nil, err
	}
	defer bounds.Close()

	type span struct{ first, last int64 }
	spans := map[int64]*span{}
	fresh := map[string]int64{}
	now := time.Now().UnixMilli()
	for _, p := range pts {
		s.mu.Lock()
		id, ok := s.series[p.Key]
		s.mu.Unlock()
		if !ok {
			id, ok = fresh[p.Key]
		}
		if !ok {
			labels := "{}"
			if len(p.Labels) > 0 {
				raw, err := json.Marshal(p.Labels)
				if err != nil {
					return nil, err
				}
				labels = string(raw)
			}
			if err := upsertSeries.QueryRowContext(ctx, p.Key, p.Name, p.Unit, p.Source, labels, now).Scan(&id); err != nil {
				return nil, fmt.Errorf("upsert series %s: %w", p.Key, err)
			}
			fresh[p.Key] = id
		}
		ts := p.At.UnixMilli()
		count := p.Count
		if count < 1 {
			count = 1
		}
		if _, err := insert.ExecContext(ctx, id, ts, p.Value, p.Min, p.Max, count); err != nil {
			return nil, fmt.Errorf("insert sample %s: %w", p.Key, err)
		}
		sp := spans[id]
		if sp == nil {
			spans[id] = &span{first: ts, last: ts}
		} else {
			sp.first, sp.last = min(sp.first, ts), max(sp.last, ts)
		}
	}
	for id, sp := range spans {
		if _, err := bounds.ExecContext(ctx, sp.first, sp.last, id); err != nil {
			return nil, err
		}
	}
	return fresh, nil
}

func writeChecks(ctx context.Context, tx *sql.Tx, checks []history.ServiceCheck) error {
	if len(checks) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO service_checks(service_id, ts, status, ok, latency_ms, status_code, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, c := range checks {
		var latency, code, errText any
		if c.OK {
			latency = c.LatencyMs
		}
		if c.StatusCode != 0 {
			code = c.StatusCode
		}
		if c.Err != "" {
			errText = c.Err
		}
		if _, err := stmt.ExecContext(ctx, c.ServiceID, c.At.UnixMilli(), c.Status, boolInt(c.OK), latency, code, errText); err != nil {
			return fmt.Errorf("insert service check %s: %w", c.ServiceID, err)
		}
	}
	return nil
}

func writeIncidents(ctx context.Context, tx *sql.Tx, events []history.IncidentEvent) error {
	for _, ev := range events {
		inc := ev.Incident
		var err error
		switch ev.Kind {
		case history.IncidentOpened:
			_, err = tx.ExecContext(ctx, `
				INSERT INTO alert_events(id, rule_id, rule_name, metric_name, severity, message, threshold, unit,
					started_at, ended_at, acknowledged_at, peak_value, last_value)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)
				ON CONFLICT(id) DO UPDATE SET peak_value = excluded.peak_value, last_value = excluded.last_value`,
				inc.ID, inc.RuleID, inc.RuleName, inc.Metric, inc.Severity.String(), inc.Message, inc.Threshold, inc.Unit,
				inc.StartedAt.UnixMilli(), inc.PeakValue, inc.LastValue)
		case history.IncidentResolved:
			_, err = tx.ExecContext(ctx, `
				UPDATE alert_events SET ended_at = ?, peak_value = ?, last_value = ? WHERE id = ?`,
				inc.EndedAt.UnixMilli(), inc.PeakValue, inc.LastValue, inc.ID)
		case history.IncidentAcked:
			_, err = tx.ExecContext(ctx, `
				UPDATE alert_events SET acknowledged_at = COALESCE(acknowledged_at, ?) WHERE id = ?`,
				inc.AckedAt.UnixMilli(), inc.ID)
		}
		if err != nil {
			return fmt.Errorf("persist incident %s: %w", inc.ID, err)
		}
	}
	return nil
}

func (s *Store) SyncRules(ctx context.Context, rules []alert.Rule) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, "UPDATE alert_rules SET enabled = 0, updated_at = ?", now); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO alert_rules(id, name, metric_name, operator, threshold, duration_seconds, clear_seconds,
			hysteresis, severity, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, metric_name = excluded.metric_name, operator = excluded.operator,
			threshold = excluded.threshold, duration_seconds = excluded.duration_seconds,
			clear_seconds = excluded.clear_seconds, hysteresis = excluded.hysteresis,
			severity = excluded.severity, enabled = excluded.enabled, updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rules {
		if _, err := stmt.ExecContext(ctx, r.ID, r.Name, r.Metric, string(r.Op), r.Threshold,
			int64(r.For.Seconds()), int64(r.ClearFor.Seconds()), r.Hysteresis, r.Severity.String(), boolInt(r.Enabled), now); err != nil {
			return fmt.Errorf("sync rule %s: %w", r.ID, err)
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

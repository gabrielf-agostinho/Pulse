package sqlite

import (
	"context"

	"github.com/gabrielagostinho/pulse/internal/application/history"
)

const pruneChunk = 5000

func (s *Store) Prune(ctx context.Context, r history.Retention) (history.PruneResult, error) {
	var res history.PruneResult
	var err error
	if !r.MetricsBefore.IsZero() {
		if res.Metrics, err = s.chunkedDelete(ctx,
			"DELETE FROM metric_samples WHERE id IN (SELECT id FROM metric_samples WHERE ts < ? LIMIT ?)",
			r.MetricsBefore.UnixMilli()); err != nil {
			return res, err
		}
	}
	if !r.ChecksBefore.IsZero() {
		if res.Checks, err = s.chunkedDelete(ctx,
			"DELETE FROM service_checks WHERE id IN (SELECT id FROM service_checks WHERE ts < ? LIMIT ?)",
			r.ChecksBefore.UnixMilli()); err != nil {
			return res, err
		}
	}
	if !r.IncidentBefore.IsZero() {
		if res.Incidents, err = s.chunkedDelete(ctx,
			"DELETE FROM alert_events WHERE id IN (SELECT id FROM alert_events WHERE ended_at IS NOT NULL AND ended_at < ? LIMIT ?)",
			r.IncidentBefore.UnixMilli()); err != nil {
			return res, err
		}
	}
	if res.Metrics > 0 {
		if _, err := s.write.ExecContext(ctx, `
			UPDATE metric_series SET
				first_ts = (SELECT MIN(ts) FROM metric_samples WHERE series_id = metric_series.id),
				last_ts  = (SELECT MAX(ts) FROM metric_samples WHERE series_id = metric_series.id)`); err != nil {
			return res, err
		}
	}
	if res.Metrics+res.Checks+res.Incidents > 0 {
		if _, err := s.write.ExecContext(ctx, "PRAGMA incremental_vacuum(2000)"); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Store) chunkedDelete(ctx context.Context, query string, cutoff int64) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		r, err := s.write.ExecContext(ctx, query, cutoff, pruneChunk)
		if err != nil {
			return total, err
		}
		n, _ := r.RowsAffected()
		total += n
		if n < pruneChunk {
			return total, nil
		}
	}
}

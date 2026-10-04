package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	"github.com/gabrielagostinho/pulse/internal/scheduler"
)

const Group = "services"

type Checker interface {
	Check(ctx context.Context, def service.Definition) service.Probe
}

type Watcher struct {
	Hub     *monitoring.Hub
	Checker Checker
	Store   interface {
		Enqueue(ctx context.Context, recs ...history.Record) error
	}
	Sink monitoring.SampleSink
	Log  *slog.Logger
}

func (w *Watcher) Jobs(defs []service.Definition) []scheduler.Job {
	jobs := make([]scheduler.Job, 0, len(defs))
	for i, d := range defs {
		def := d
		jobs = append(jobs, scheduler.Job{
			Name:     "service:" + def.ID,
			Group:    Group,
			Interval: def.Interval,
			Timeout:  def.Timeout + 3*time.Second,
			Delay:    time.Duration(i) * 150 * time.Millisecond,
			Run:      func(ctx context.Context) error { return w.check(ctx, def) },
		})
	}
	return jobs
}

func (w *Watcher) check(ctx context.Context, def service.Definition) error {
	probe := w.Checker.Check(ctx, def)
	if ctx.Err() == context.Canceled {
		return ctx.Err()
	}
	var res service.CheckResult
	w.Hub.Update(func(s *monitoring.State) {
		for i := range s.Services {
			if s.Services[i].Def.ID == def.ID {
				res = s.Services[i].Apply(probe)
				return
			}
		}
	})

	if w.Store != nil {
		rec := history.Record{Check: &history.ServiceCheck{
			ServiceID: def.ID, At: res.At, Status: res.Status.String(), OK: res.OK,
			LatencyMs: ms(res.Latency), StatusCode: res.StatusCode, Err: res.Err,
		}}
		if err := w.Store.Enqueue(ctx, rec); err != nil && w.Log != nil {
			w.Log.Warn("service check not persisted", "service", def.ID, "err", err)
		}
	}
	if w.Sink != nil {
		w.Sink(ctx, Samples(def.ID, res))
	}
	if !res.OK && w.Log != nil {
		w.Log.Debug("service check failed", "service", def.ID, "status", res.Status.String(), "err", res.Err)
	}
	return nil
}

func Samples(id string, res service.CheckResult) []metrics.Sample {
	labels := map[string]string{"service": id}
	up := 1.0
	if res.Status == service.StatusDown {
		up = 0
	}
	out := []metrics.Sample{{Name: metrics.ServiceUp, Labels: labels, Value: up, Source: "service", At: res.At}}
	if res.OK {
		out = append(out, metrics.Sample{Name: metrics.ServiceLatency, Labels: labels, Value: ms(res.Latency), Unit: metrics.UnitMillis, Source: "service", At: res.At})
	}
	return out
}

func Replay(hub *monitoring.Hub, id string, checks []history.ServiceCheck) {
	if len(checks) == 0 {
		return
	}
	hub.Update(func(s *monitoring.State) {
		for i := range s.Services {
			if s.Services[i].Def.ID != id {
				continue
			}
			for _, c := range checks {
				s.Services[i].Apply(service.Probe{
					At: c.At, OK: c.OK, StatusCode: c.StatusCode, Err: c.Err,
					Latency: time.Duration(c.LatencyMs * float64(time.Millisecond)),
				})
			}
		}
	})
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

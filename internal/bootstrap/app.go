package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/alerts"
	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/application/services"
	"github.com/gabrielagostinho/pulse/internal/collectors/cpu"
	"github.com/gabrielagostinho/pulse/internal/collectors/disk"
	"github.com/gabrielagostinho/pulse/internal/collectors/host"
	"github.com/gabrielagostinho/pulse/internal/collectors/memory"
	"github.com/gabrielagostinho/pulse/internal/collectors/network"
	proccol "github.com/gabrielagostinho/pulse/internal/collectors/process"
	"github.com/gabrielagostinho/pulse/internal/collectors/servicecheck"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
	"github.com/gabrielagostinho/pulse/internal/scheduler"
	"github.com/gabrielagostinho/pulse/internal/storage/sqlite"
)

const (
	groupSystem      = "system"
	groupMaintenance = "maintenance"
)

type App struct {
	cfg   config.Config
	log   *slog.Logger
	logC  io.Closer
	hub   *monitoring.Hub
	sched *scheduler.Scheduler

	store  *sqlite.Store
	writer *history.Writer
	agg    *history.Aggregator
	alerts *alerts.Engine
	procs  proccol.Inspector

	started bool
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	log, logC, err := openLog(cfg.Logging)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, log: log, logC: logC}
	log.Info("pulse starting", "config", cfg.Source, "db", cfg.Storage.Path)

	ringCap := int(cfg.General.LiveWindow / minInterval(cfg))
	a.hub = monitoring.NewHub(max(ringCap, 30), cfg.ServiceDefinitions())
	a.sched = scheduler.New(log, map[string]int{
		groupSystem:      cfg.Scheduler.MaxConcurrency,
		services.Group:   cfg.Services.MaxConcurrency,
		groupMaintenance: 1,
	})

	storage := monitoring.StorageView{Enabled: cfg.Storage.Enabled}
	if cfg.Storage.Enabled {
		openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		a.store, err = sqlite.Open(openCtx, cfg.Storage.Path)
		cancel()
		if err != nil {
			storage.InitErr = err.Error()
			log.Error("storage unavailable; running without history", "err", err)
		} else {
			a.writer = history.NewWriter(a.store, log, history.WriterOptions{
				QueueSize:      cfg.Storage.QueueSize,
				BatchSize:      cfg.Storage.BatchSize,
				FlushInterval:  cfg.Storage.FlushInterval,
				EnqueueTimeout: cfg.Storage.EnqueueTimeout,
				Retries:        3,
			})
			a.agg = history.NewAggregator(a.writer, cfg.Storage.Resolution)
		}
	}
	a.hub.Update(func(s *monitoring.State) { s.Storage = storage })

	rules, err := cfg.AlertRules()
	if err != nil {
		return nil, err
	}
	if cfg.Alerts.Enabled {
		var enq alerts.Enqueuer
		if a.writer != nil {
			enq = a.writer
		}
		a.alerts = alerts.New(rules, enq, func(v alerts.View) {
			a.hub.Update(func(s *monitoring.State) { s.Alerts = v })
		}, log)
	}

	if a.store != nil {
		a.restore(ctx, rules)
	}
	if err := a.registerJobs(); err != nil {
		return nil, err
	}
	return a, nil
}

func minInterval(cfg config.Config) time.Duration {
	d := cfg.Collectors.CPU.Interval
	for _, c := range []time.Duration{cfg.Collectors.Memory.Interval, cfg.Collectors.Network.Interval} {
		if c > 0 && c < d {
			d = c
		}
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}

func (a *App) restore(ctx context.Context, rules []alert.Rule) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := a.store.SyncRules(ctx, rules); err != nil {
		a.log.Warn("could not sync alert rules", "err", err)
	}
	if a.alerts != nil {
		open, err1 := a.store.OpenIncidents(ctx)
		recent, err2 := a.store.RecentIncidents(ctx, 200)
		if err := errors.Join(err1, err2); err != nil {
			a.log.Warn("could not restore incidents", "err", err)
		}
		a.alerts.Restore(ctx, open, recent)
	}
	for _, d := range a.cfg.ServiceDefinitions() {
		checks, err := a.store.RecentChecks(ctx, d.ID, 120)
		if err != nil {
			a.log.Warn("could not restore service history", "service", d.ID, "err", err)
			continue
		}
		services.Replay(a.hub, d.ID, checks)
	}
}

func (a *App) sink(ctx context.Context, samples []metrics.Sample) {
	if a.alerts != nil {
		a.alerts.Observe(ctx, samples)
	}
	if a.agg != nil {
		if err := a.agg.Add(ctx, samples); err != nil {
			a.log.Warn("samples not persisted", "count", len(samples), "err", err)
		}
	}
}

func (a *App) registerJobs() error {
	m := &monitoring.Monitor{Hub: a.hub, Sink: a.sink, Log: a.log}
	cc := a.cfg.Collectors
	var jobs []scheduler.Job
	add := func(enabled bool, name string, c config.Collector, run func(context.Context) error) {
		if enabled {
			jobs = append(jobs, scheduler.Job{Name: name, Group: groupSystem, Interval: c.Interval, Timeout: c.Timeout, Run: run})
		}
	}
	memCol := memory.New()
	memTotal := a.hub.MemTotal

	add(cc.Host.Enabled, "host", cc.Host, monitoring.Run(m, host.New(), monitoring.ApplyHost, nil))
	add(cc.CPU.Enabled, "cpu", cc.CPU, monitoring.Run(m, cpu.New(), monitoring.ApplyCPU, monitoring.CPUSamples))
	add(cc.Memory.Enabled, "memory", cc.Memory, monitoring.Run(m, memCol, monitoring.ApplyMemory, monitoring.MemorySamples))
	add(cc.Disk.Enabled, "disk", cc.Disk.Collector, monitoring.Run(m,
		disk.New(disk.Options{ExcludeFSTypes: cc.Disk.ExcludeFSTypes, ExcludeMounts: cc.Disk.ExcludeMounts}),
		monitoring.ApplyDisk, monitoring.DiskSamples))
	add(cc.Network.Enabled, "network", cc.Network.Collector, monitoring.Run(m, network.New(cc.Network.ExcludeInterfaces),
		monitoring.ApplyNetwork, monitoring.NetworkSamples))
	add(cc.Process.Enabled, "processes", cc.Process.Collector, monitoring.Run(m, proccol.New(memTotal),
		monitoring.ApplyProcesses, monitoring.ProcessSamples))

	w := &services.Watcher{Hub: a.hub, Checker: servicecheck.New(), Sink: a.sink, Log: a.log}
	if a.writer != nil {
		w.Store = a.writer
	}
	jobs = append(jobs, w.Jobs(a.cfg.ServiceDefinitions())...)

	jobs = append(jobs, scheduler.Job{Name: "pulse:status", Group: groupMaintenance, Interval: time.Second, Timeout: time.Second, Run: a.publishStatus})
	if a.alerts != nil {
		jobs = append(jobs, scheduler.Job{Name: "alerts:sweep", Group: groupMaintenance, Interval: 30 * time.Second, Timeout: 5 * time.Second,
			Run: func(ctx context.Context) error { a.alerts.Sweep(ctx); return nil }})
	}
	if a.store != nil {
		jobs = append(jobs,
			scheduler.Job{Name: "storage:stats", Group: groupMaintenance, Interval: 15 * time.Second, Timeout: 10 * time.Second, Run: a.dbStats},
			scheduler.Job{Name: "storage:retention", Group: groupMaintenance, Interval: a.cfg.Storage.Retention.Interval,
				Timeout: 5 * time.Minute, Delay: 20 * time.Second, Run: a.prune},
		)
	}
	for _, j := range jobs {
		if err := a.sched.Add(j); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) publishStatus(context.Context) error {
	stats := a.sched.Stats()
	var ws history.WriterStats
	if a.writer != nil {
		ws = a.writer.Stats()
	}
	a.hub.Update(func(s *monitoring.State) {
		s.Jobs = stats
		s.Storage.Writer = ws
	})
	return nil
}

func (a *App) dbStats(ctx context.Context) error {
	st, err := a.store.Stats(ctx)
	a.hub.Update(func(s *monitoring.State) {
		if err != nil {
			s.Storage.DBErr = err.Error()
			return
		}
		s.Storage.DB, s.Storage.DBErr = st, ""
	})
	return err
}

func (a *App) prune(ctx context.Context) error {
	now := time.Now()
	r := a.cfg.Storage.Retention
	res, err := a.store.Prune(ctx, history.Retention{
		MetricsBefore:  now.Add(-r.Metrics),
		ChecksBefore:   now.Add(-r.ServiceChecks),
		IncidentBefore: now.Add(-r.Alerts),
	})
	a.hub.Update(func(s *monitoring.State) {
		s.Storage.LastPrune, s.Storage.Pruned = now, res
		s.Storage.PruneErr = ""
		if err != nil {
			s.Storage.PruneErr = err.Error()
		}
	})
	if err == nil && res.Metrics+res.Checks+res.Incidents > 0 {
		a.log.Info("retention applied", "metrics", res.Metrics, "checks", res.Checks, "incidents", res.Incidents)
	}
	return err
}

func (a *App) Start(ctx context.Context) {
	a.sched.Start(ctx)
	a.started = true
}

func (a *App) Shutdown(ctx context.Context) error {
	var errs []error
	if a.started {
		if err := a.sched.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if a.agg != nil {
		if err := a.agg.Flush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush aggregated samples: %w", err))
		}
	}
	if a.writer != nil {
		if err := a.writer.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		st := a.writer.Stats()
		a.log.Info("history writer closed", "written", st.Written, "rejected", st.Rejected, "failed", st.Failed)
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}
	err := errors.Join(errs...)
	if err != nil {
		a.log.Error("shutdown completed with errors", "err", err)
	} else {
		a.log.Info("pulse stopped cleanly")
	}
	if a.logC != nil {
		_ = a.logC.Close()
	}
	return err
}

func (a *App) Snapshot() monitoring.State { return a.hub.Snapshot() }

func (a *App) Changed() <-chan struct{} { return a.hub.Changed() }

func (a *App) Refresh() { a.sched.Trigger() }

func (a *App) Config() config.Config { return a.cfg }

func (a *App) HistoryAvailable() bool { return a.store != nil }

func (a *App) ListSeries(ctx context.Context) ([]history.SeriesInfo, error) {
	if a.store == nil {
		return nil, errNoStorage
	}
	return a.store.ListSeries(ctx)
}

func (a *App) QueryRange(ctx context.Context, key string, from, to time.Time, buckets int) ([]history.Bucket, error) {
	if a.store == nil {
		return nil, errNoStorage
	}
	return a.store.QueryRange(ctx, key, from, to, buckets)
}

func (a *App) Inspect(ctx context.Context, pid int32) (process.Details, error) {
	return a.procs.Inspect(ctx, pid)
}

func (a *App) Terminate(ctx context.Context, pid int32) error {
	if !a.cfg.Collectors.Process.AllowKill {
		return errors.New("terminating processes is disabled (collectors.processes.allow_kill)")
	}
	err := a.procs.Terminate(ctx, pid)
	a.log.Info("terminate process requested", "pid", pid, "err", err)
	if err == nil {
		a.sched.Trigger()
	}
	return err
}

func (a *App) Acknowledge(ctx context.Context, id string) error {
	if a.alerts == nil {
		return errors.New("alerting is disabled")
	}
	return a.alerts.Acknowledge(ctx, id)
}

var errNoStorage = errors.New("history storage is not available")

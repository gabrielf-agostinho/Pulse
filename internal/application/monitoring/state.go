package monitoring

import (
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/alerts"
	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	"github.com/gabrielagostinho/pulse/internal/scheduler"
)

type StorageView struct {
	Enabled   bool
	InitErr   string
	Writer    history.WriterStats
	DB        history.DBStats
	DBErr     string
	LastPrune time.Time
	Pruned    history.PruneResult
	PruneErr  string
}

func (s StorageView) Degraded() bool {
	return s.InitErr != "" || s.DBErr != "" || s.PruneErr != "" || s.Writer.Failed > 0 ||
		s.Writer.Rejected > 0 && time.Since(s.Writer.LastErrAt) < 5*time.Minute
}

type State struct {
	Version uint64
	Started time.Time

	CPU     metrics.CPU
	Memory  metrics.Memory
	Disk    metrics.Disk
	Network metrics.Network
	Host    metrics.Host

	Processes process.Snapshot
	Services  []service.State
	Alerts    alerts.View
	Storage   StorageView
	Jobs      []scheduler.Stats

	Series map[string]*metrics.Ring
}

func (s State) Ring(key string) *metrics.Ring { return s.Series[key] }

func (s State) Service(id string) (service.State, bool) {
	for _, sv := range s.Services {
		if sv.Def.ID == id {
			return sv, true
		}
	}
	return service.State{}, false
}

func (s State) Clone() State {
	c := s
	c.CPU.PerCore = append([]float64(nil), s.CPU.PerCore...)
	c.Disk.Volumes = append([]metrics.Volume(nil), s.Disk.Volumes...)
	c.Network.Interfaces = append([]metrics.Interface(nil), s.Network.Interfaces...)
	c.Processes.Processes = append([]process.Info(nil), s.Processes.Processes...)
	c.Services = make([]service.State, len(s.Services))
	for i, sv := range s.Services {
		c.Services[i] = sv.Clone()
	}
	c.Alerts = s.Alerts.Clone()
	c.Jobs = append([]scheduler.Stats(nil), s.Jobs...)
	c.Series = make(map[string]*metrics.Ring, len(s.Series))
	for k, r := range s.Series {
		c.Series[k] = r.Clone()
	}
	return c
}

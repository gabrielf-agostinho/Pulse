package tui

import (
	"context"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
)

type Backend interface {
	Snapshot() monitoring.State
	Changed() <-chan struct{}
	Refresh()
	Config() config.Config

	HistoryAvailable() bool
	ListSeries(ctx context.Context) ([]history.SeriesInfo, error)
	QueryRange(ctx context.Context, key string, from, to time.Time, buckets int) ([]history.Bucket, error)

	Inspect(ctx context.Context, pid int32) (process.Details, error)
	Terminate(ctx context.Context, pid int32) error
	Acknowledge(ctx context.Context, id string) error
}

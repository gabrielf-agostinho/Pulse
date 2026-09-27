package history

import (
	"context"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/alert"
)

type MetricPoint struct {
	Key    string
	Name   string
	Labels map[string]string
	Unit   string
	Source string
	At     time.Time
	Value  float64
	Min    float64
	Max    float64
	Count  int
}

type ServiceCheck struct {
	ServiceID  string
	At         time.Time
	Status     string
	OK         bool
	LatencyMs  float64
	StatusCode int
	Err        string
}

type IncidentEventKind uint8

const (
	IncidentOpened IncidentEventKind = iota + 1
	IncidentResolved
	IncidentAcked
)

type IncidentEvent struct {
	Kind     IncidentEventKind
	Incident alert.Incident
}

type Batch struct {
	Metrics   []MetricPoint
	Checks    []ServiceCheck
	Incidents []IncidentEvent
}

func (b Batch) Len() int { return len(b.Metrics) + len(b.Checks) + len(b.Incidents) }

type BatchWriter interface {
	WriteBatch(ctx context.Context, b Batch) error
}

type SeriesInfo struct {
	Key     string
	Name    string
	Unit    string
	Labels  map[string]string
	First   time.Time
	Last    time.Time
	Samples int64
}

type Bucket struct {
	At    time.Time
	Avg   float64
	Min   float64
	Max   float64
	Count int64
}

type Retention struct {
	MetricsBefore  time.Time
	ChecksBefore   time.Time
	IncidentBefore time.Time
}

type PruneResult struct {
	Metrics   int64
	Checks    int64
	Incidents int64
}

type DBStats struct {
	Path          string
	SizeBytes     int64
	WALBytes      int64
	MetricRows    int64
	CheckRows     int64
	IncidentRows  int64
	Series        int64
	SchemaVersion int
	At            time.Time
}

type Reader interface {
	ListSeries(ctx context.Context) ([]SeriesInfo, error)
	QueryRange(ctx context.Context, key string, from, to time.Time, buckets int) ([]Bucket, error)
	RecentChecks(ctx context.Context, serviceID string, limit int) ([]ServiceCheck, error)
	RecentIncidents(ctx context.Context, limit int) ([]alert.Incident, error)
	OpenIncidents(ctx context.Context) ([]alert.Incident, error)
	Stats(ctx context.Context) (DBStats, error)
}

type Maintainer interface {
	Prune(ctx context.Context, r Retention) (PruneResult, error)
}

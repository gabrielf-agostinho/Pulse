package history

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

type Aggregator struct {
	window time.Duration
	w      *Writer

	mu  sync.Mutex
	acc map[string]*accumulator
}

type accumulator struct {
	point  MetricPoint
	sum    float64
	bucket time.Time
}

func NewAggregator(w *Writer, window time.Duration) *Aggregator {
	if window <= 0 {
		window = 10 * time.Second
	}
	return &Aggregator{window: window, w: w, acc: map[string]*accumulator{}}
}

func (a *Aggregator) Add(ctx context.Context, samples []metrics.Sample) error {
	var ready []Record
	a.mu.Lock()
	for _, s := range samples {
		if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			continue
		}
		key := s.Key()
		bucket := s.At.Truncate(a.window)
		acc := a.acc[key]
		if acc != nil && !acc.bucket.Equal(bucket) {
			ready = append(ready, acc.record())
			acc = nil
		}
		if acc == nil {
			acc = &accumulator{
				bucket: bucket,
				point: MetricPoint{
					Key: key, Name: s.Name, Labels: s.Labels, Unit: s.Unit, Source: s.Source,
					At: bucket, Min: s.Value, Max: s.Value,
				},
			}
			a.acc[key] = acc
		}
		acc.sum += s.Value
		acc.point.Count++
		acc.point.Min = math.Min(acc.point.Min, s.Value)
		acc.point.Max = math.Max(acc.point.Max, s.Value)
	}
	a.mu.Unlock()
	if len(ready) == 0 {
		return nil
	}
	return a.w.Enqueue(ctx, ready...)
}

func (a *Aggregator) Flush(ctx context.Context) error {
	a.mu.Lock()
	ready := make([]Record, 0, len(a.acc))
	for k, acc := range a.acc {
		ready = append(ready, acc.record())
		delete(a.acc, k)
	}
	a.mu.Unlock()
	if len(ready) == 0 {
		return nil
	}
	return a.w.Enqueue(ctx, ready...)
}

func (acc *accumulator) record() Record {
	p := acc.point
	p.Value = acc.sum / float64(p.Count)
	return Record{Metric: &p}
}

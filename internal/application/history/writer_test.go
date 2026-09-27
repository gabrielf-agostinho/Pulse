package history

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

type fakeStore struct {
	mu      sync.Mutex
	batches []Batch
	fail    int
	block   chan struct{}
}

func (f *fakeStore) WriteBatch(ctx context.Context, b Batch) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errors.New("disk I/O error")
	}
	f.batches = append(f.batches, b)
	return nil
}

func (f *fakeStore) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.batches {
		n += b.Len()
	}
	return n
}

func metric(v float64) Record {
	return Record{Metric: &MetricPoint{Key: "cpu.usage", Value: v}}
}

func TestWriterBatchesAndFlushesOnClose(t *testing.T) {
	st := &fakeStore{}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 100, BatchSize: 10, FlushInterval: time.Hour})
	for i := 0; i < 25; i++ {
		if err := w.Enqueue(context.Background(), metric(float64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.total() != 25 {
		t.Fatalf("written = %d, want all 25 including the partial batch", st.total())
	}
	if len(st.batches) != 3 {
		t.Fatalf("batches = %d, want 10+10+5", len(st.batches))
	}
	s := w.Stats()
	if s.Written != 25 || s.Failed != 0 || !s.Closed {
		t.Fatalf("stats = %+v", s)
	}
	if err := w.Enqueue(context.Background(), metric(1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("enqueue after close = %v", err)
	}
}

func TestWriterFlushesOnInterval(t *testing.T) {
	st := &fakeStore{}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 100, BatchSize: 1000, FlushInterval: 10 * time.Millisecond})
	defer func() { _ = w.Close(context.Background()) }()
	_ = w.Enqueue(context.Background(), metric(1), Record{Check: &ServiceCheck{ServiceID: "api"}})
	deadline := time.Now().Add(2 * time.Second)
	for st.total() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st.total() != 2 {
		t.Fatal("interval flush did not happen")
	}
}

func TestWriterBackpressureRejectsExplicitly(t *testing.T) {
	st := &fakeStore{block: make(chan struct{})}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 16, BatchSize: 1, FlushInterval: time.Hour, EnqueueTimeout: 30 * time.Millisecond})
	var err error
	start := time.Now()
	for i := 0; i < 40 && err == nil; i++ {
		err = w.Enqueue(context.Background(), metric(float64(i)))
	}
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("producer must be held back (backpressure) before rejecting")
	}
	s := w.Stats()
	if s.Rejected == 0 || s.LastErr == "" {
		t.Fatalf("rejection must be counted and reported: %+v", s)
	}
	close(st.block)
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := uint64(st.total()); got != s.Enqueued {
		t.Fatalf("every accepted record must be written: wrote %d accepted %d", got, s.Enqueued)
	}
}

func TestWriterRetriesThenReportsLoss(t *testing.T) {
	st := &fakeStore{fail: 2}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 16, BatchSize: 4, FlushInterval: time.Hour, Retries: 2, Backoff: time.Millisecond})
	_ = w.Enqueue(context.Background(), metric(1), metric(2), metric(3), metric(4))
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("batch should succeed on third attempt: %v", err)
	}
	if st.total() != 4 || w.Stats().Retries != 2 {
		t.Fatalf("total=%d stats=%+v", st.total(), w.Stats())
	}

	st2 := &fakeStore{fail: 100}
	w2 := NewWriter(st2, nil, WriterOptions{QueueSize: 16, BatchSize: 2, FlushInterval: time.Hour, Retries: 1, Backoff: time.Millisecond})
	_ = w2.Enqueue(context.Background(), metric(1), metric(2))
	err := w2.Close(context.Background())
	if err == nil || w2.Stats().Failed != 2 {
		t.Fatalf("loss must be reported on close: err=%v stats=%+v", err, w2.Stats())
	}
}

func TestWriterCloseDeadlineReportsPending(t *testing.T) {
	st := &fakeStore{block: make(chan struct{})}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 64, BatchSize: 1, FlushInterval: time.Hour})
	for i := 0; i < 5; i++ {
		_ = w.Enqueue(context.Background(), metric(1))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := w.Close(ctx)
	if err == nil {
		t.Fatal("expected pending report")
	}
	close(st.block)
}

func TestConcurrentProducers(t *testing.T) {
	st := &fakeStore{}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 64, BatchSize: 16, FlushInterval: 5 * time.Millisecond, EnqueueTimeout: time.Second})
	var wg sync.WaitGroup
	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = w.Enqueue(context.Background(), metric(float64(i)))
			}
		}()
	}
	wg.Wait()
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.total() != 1600 {
		t.Fatalf("written %d of 1600", st.total())
	}
}

func TestAggregatorDownsamples(t *testing.T) {
	st := &fakeStore{}
	w := NewWriter(st, nil, WriterOptions{QueueSize: 64, BatchSize: 64, FlushInterval: time.Hour})
	agg := NewAggregator(w, 10*time.Second)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	for i, v := range []float64{10, 20, 30} {
		_ = agg.Add(ctx, []metrics.Sample{{Name: "cpu.usage", Value: v, At: base.Add(time.Duration(i) * time.Second)}})
	}
	_ = agg.Add(ctx, []metrics.Sample{{Name: "cpu.usage", Value: 50, At: base.Add(11 * time.Second)}})
	_ = agg.Flush(ctx)
	_ = w.Close(ctx)

	var pts []MetricPoint
	for _, b := range st.batches {
		pts = append(pts, b.Metrics...)
	}
	if len(pts) != 2 {
		t.Fatalf("points = %d", len(pts))
	}
	first := pts[0]
	if !first.At.Equal(base) {
		first = pts[1]
	}
	if first.Value != 20 || first.Min != 10 || first.Max != 30 || first.Count != 3 {
		t.Fatalf("first window = %+v", first)
	}
}

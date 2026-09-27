package history

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueFull = errors.New("history: write queue full")

var ErrClosed = errors.New("history: writer closed")

type Record struct {
	Metric   *MetricPoint
	Check    *ServiceCheck
	Incident *IncidentEvent
}

type WriterOptions struct {
	QueueSize      int
	BatchSize      int
	FlushInterval  time.Duration
	EnqueueTimeout time.Duration
	WriteTimeout   time.Duration
	Retries        int
	Backoff        time.Duration
}

type WriterStats struct {
	QueueDepth   int
	QueueCap     int
	Enqueued     uint64
	Written      uint64
	Rejected     uint64
	Failed       uint64
	Batches      uint64
	Retries      uint64
	LastFlush    time.Time
	LastFlushDur time.Duration
	LastErr      string
	LastErrAt    time.Time
	Closed       bool
}

type Writer struct {
	store BatchWriter
	log   *slog.Logger
	opts  WriterOptions

	in     chan Record
	done   chan struct{}
	mu     sync.RWMutex
	closed bool

	enqueued, written, rejected, failed, batches, retries atomic.Uint64

	statMu       sync.Mutex
	lastFlush    time.Time
	lastFlushDur time.Duration
	lastErr      string
	lastErrAt    time.Time
}

func NewWriter(store BatchWriter, log *slog.Logger, opts WriterOptions) *Writer {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4096
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 512
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	if opts.EnqueueTimeout <= 0 {
		opts.EnqueueTimeout = 2 * time.Second
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = 15 * time.Second
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	if opts.Backoff <= 0 {
		opts.Backoff = 250 * time.Millisecond
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	w := &Writer{
		store: store,
		log:   log,
		opts:  opts,
		in:    make(chan Record, opts.QueueSize),
		done:  make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *Writer) Enqueue(ctx context.Context, recs ...Record) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.rejected.Add(uint64(len(recs)))
		return ErrClosed
	}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for i, r := range recs {
		select {
		case w.in <- r:
			w.enqueued.Add(1)
			continue
		default:
		}
		if timer == nil {
			timer = time.NewTimer(w.opts.EnqueueTimeout)
		}
		select {
		case w.in <- r:
			w.enqueued.Add(1)
		case <-timer.C:
			lost := uint64(len(recs) - i)
			w.rejected.Add(lost)
			w.setErr(fmt.Errorf("%w: %d records rejected after waiting %s", ErrQueueFull, lost, w.opts.EnqueueTimeout))
			w.log.Error("history write queue full; records rejected", "rejected", lost, "queue", cap(w.in))
			return ErrQueueFull
		case <-ctx.Done():
			lost := uint64(len(recs) - i)
			w.rejected.Add(lost)
			return ctx.Err()
		}
	}
	return nil
}

func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		<-w.done
		return nil
	}
	w.closed = true
	close(w.in)
	w.mu.Unlock()

	select {
	case <-w.done:
		if f := w.failed.Load(); f > 0 {
			return fmt.Errorf("history: %d records could not be written (last error: %s)", f, w.Stats().LastErr)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("history: shutdown deadline reached with %d records pending", len(w.in))
	}
}

func (w *Writer) Stats() WriterStats {
	w.statMu.Lock()
	defer w.statMu.Unlock()
	w.mu.RLock()
	closed := w.closed
	w.mu.RUnlock()
	return WriterStats{
		QueueDepth:   len(w.in),
		QueueCap:     cap(w.in),
		Enqueued:     w.enqueued.Load(),
		Written:      w.written.Load(),
		Rejected:     w.rejected.Load(),
		Failed:       w.failed.Load(),
		Batches:      w.batches.Load(),
		Retries:      w.retries.Load(),
		LastFlush:    w.lastFlush,
		LastFlushDur: w.lastFlushDur,
		LastErr:      w.lastErr,
		LastErrAt:    w.lastErrAt,
		Closed:       closed,
	}
}

func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.opts.FlushInterval)
	defer ticker.Stop()
	var b Batch
	for {
		select {
		case r, ok := <-w.in:
			if !ok {
				w.flush(&b)
				return
			}
			add(&b, r)
			if b.Len() >= w.opts.BatchSize {
				w.flush(&b)
			}
		case <-ticker.C:
			w.flush(&b)
		}
	}
}

func add(b *Batch, r Record) {
	switch {
	case r.Metric != nil:
		b.Metrics = append(b.Metrics, *r.Metric)
	case r.Check != nil:
		b.Checks = append(b.Checks, *r.Check)
	case r.Incident != nil:
		b.Incidents = append(b.Incidents, *r.Incident)
	}
}

func (w *Writer) flush(b *Batch) {
	n := b.Len()
	if n == 0 {
		return
	}
	var err error
	start := time.Now()
	for attempt := 0; attempt <= w.opts.Retries; attempt++ {
		if attempt > 0 {
			w.retries.Add(1)
			time.Sleep(w.opts.Backoff * time.Duration(attempt))
		}
		ctx, cancel := context.WithTimeout(context.Background(), w.opts.WriteTimeout)
		err = w.store.WriteBatch(ctx, *b)
		cancel()
		if err == nil {
			break
		}
		w.log.Warn("history batch write failed", "attempt", attempt+1, "records", n, "err", err)
	}
	w.batches.Add(1)
	w.statMu.Lock()
	w.lastFlush = time.Now()
	w.lastFlushDur = time.Since(start)
	w.statMu.Unlock()
	if err != nil {
		w.failed.Add(uint64(n))
		w.setErr(fmt.Errorf("batch of %d records lost: %w", n, err))
		w.log.Error("history batch dropped after retries", "records", n, "err", err)
	} else {
		w.written.Add(uint64(n))
	}
	*b = Batch{}
}

func (w *Writer) setErr(err error) {
	w.statMu.Lock()
	w.lastErr = err.Error()
	w.lastErrAt = time.Now()
	w.statMu.Unlock()
}

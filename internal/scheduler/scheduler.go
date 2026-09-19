package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

type Job struct {
	Name     string
	Group    string
	Interval time.Duration
	Timeout  time.Duration
	Run      func(ctx context.Context) error
	Delay    time.Duration
}

type Stats struct {
	Name         string
	Group        string
	Interval     time.Duration
	Runs         uint64
	Failures     uint64
	Timeouts     uint64
	Overruns     uint64
	Running      bool
	LastStart    time.Time
	LastDuration time.Duration
	LastErr      string
	LastErrAt    time.Time
}

func (s Stats) Healthy() bool { return s.LastErr == "" }

type entry struct {
	job     Job
	trigger chan struct{}
	stats   Stats
}

type Scheduler struct {
	log    *slog.Logger
	mu     sync.Mutex
	jobs   map[string]*entry
	groups map[string]chan struct{}
	wg     sync.WaitGroup
	cancel context.CancelFunc
	start  bool
}

func New(log *slog.Logger, limits map[string]int) *Scheduler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Scheduler{log: log, jobs: map[string]*entry{}, groups: map[string]chan struct{}{}}
	for g, n := range limits {
		if n > 0 {
			s.groups[g] = make(chan struct{}, n)
		}
	}
	return s
}

func (s *Scheduler) Add(j Job) error {
	if j.Name == "" || j.Run == nil || j.Interval <= 0 {
		return fmt.Errorf("scheduler: invalid job %q", j.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start {
		return errors.New("scheduler: cannot add jobs after start")
	}
	if _, dup := s.jobs[j.Name]; dup {
		return fmt.Errorf("scheduler: duplicate job %q", j.Name)
	}
	s.jobs[j.Name] = &entry{
		job:     j,
		trigger: make(chan struct{}, 1),
		stats:   Stats{Name: j.Name, Group: j.Group, Interval: j.Interval},
	}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start {
		return
	}
	s.start = true
	ctx, s.cancel = context.WithCancel(ctx)
	for _, e := range s.jobs {
		s.wg.Add(1)
		go s.loop(ctx, e)
	}
}

func (s *Scheduler) Trigger() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.jobs {
		select {
		case e.trigger <- struct{}{}:
		default:
		}
	}
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		var stuck []string
		for _, st := range s.Stats() {
			if st.Running {
				stuck = append(stuck, st.Name)
			}
		}
		return fmt.Errorf("scheduler: jobs still running at shutdown: %v", stuck)
	}
}

func (s *Scheduler) Stats() []Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Stats, 0, len(s.jobs))
	for _, e := range s.jobs {
		out = append(out, e.stats)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Scheduler) loop(ctx context.Context, e *entry) {
	defer s.wg.Done()
	if e.job.Delay > 0 {
		select {
		case <-time.After(e.job.Delay):
		case <-ctx.Done():
			return
		}
	}
	ticker := time.NewTicker(e.job.Interval)
	defer ticker.Stop()
	for {
		s.runOnce(ctx, e)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-e.trigger:
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context, e *entry) {
	if sem := s.groups[e.job.Group]; sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return
		}
	}
	if ctx.Err() != nil {
		return
	}

	start := time.Now()
	s.mu.Lock()
	e.stats.Running = true
	e.stats.LastStart = start
	s.mu.Unlock()

	runCtx := ctx
	cancel := context.CancelFunc(func() {})
	if e.job.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, e.job.Timeout)
	}
	err := safeRun(runCtx, e.job.Run)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancel()
	elapsed := time.Since(start)

	s.mu.Lock()
	defer s.mu.Unlock()
	e.stats.Running = false
	e.stats.Runs++
	e.stats.LastDuration = elapsed
	if elapsed > e.job.Interval {
		e.stats.Overruns++
	}
	switch {
	case err == nil:
		e.stats.LastErr = ""
	case ctx.Err() != nil:
	default:
		e.stats.Failures++
		if timedOut {
			e.stats.Timeouts++
			err = fmt.Errorf("timed out after %s: %w", e.job.Timeout, err)
		}
		e.stats.LastErr = err.Error()
		e.stats.LastErrAt = time.Now()
		s.log.Warn("job failed", "job", e.job.Name, "err", err, "duration", elapsed)
	}
}

func safeRun(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return fn(ctx)
}

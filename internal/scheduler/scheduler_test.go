package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func statsOf(s *Scheduler, name string) Stats {
	for _, st := range s.Stats() {
		if st.Name == name {
			return st
		}
	}
	return Stats{}
}

func TestJobNeverOverlapsItself(t *testing.T) {
	s := New(nil, nil)
	var active, maxActive, runs atomic.Int32
	_ = s.Add(Job{Name: "slow", Interval: 5 * time.Millisecond, Run: func(ctx context.Context) error {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		runs.Add(1)
		return nil
	}})
	s.Start(context.Background())
	waitFor(t, func() bool { return runs.Load() >= 3 })
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if maxActive.Load() != 1 {
		t.Fatalf("job overlapped itself: %d concurrent runs", maxActive.Load())
	}
	if statsOf(s, "slow").Overruns == 0 {
		t.Fatal("runs longer than the interval must be counted as overruns")
	}
}

func TestGroupConcurrencyLimit(t *testing.T) {
	s := New(nil, map[string]int{"svc": 2})
	var active, maxActive, runs atomic.Int32
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		_ = s.Add(Job{Name: name, Group: "svc", Interval: time.Millisecond, Run: func(ctx context.Context) error {
			n := active.Add(1)
			for {
				m := maxActive.Load()
				if n <= m || maxActive.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			runs.Add(1)
			return nil
		}})
	}
	s.Start(context.Background())
	waitFor(t, func() bool { return runs.Load() >= 10 })
	_ = s.Stop(context.Background())
	if maxActive.Load() > 2 {
		t.Fatalf("group limit exceeded: %d", maxActive.Load())
	}
}

func TestTimeoutAndFailureIsolation(t *testing.T) {
	s := New(nil, nil)
	var healthy atomic.Int32
	_ = s.Add(Job{Name: "hang", Interval: 10 * time.Millisecond, Timeout: 20 * time.Millisecond, Run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	_ = s.Add(Job{Name: "panics", Interval: 10 * time.Millisecond, Run: func(ctx context.Context) error {
		panic("boom")
	}})
	_ = s.Add(Job{Name: "healthy", Interval: 5 * time.Millisecond, Run: func(ctx context.Context) error {
		healthy.Add(1)
		return nil
	}})
	s.Start(context.Background())
	waitFor(t, func() bool { return statsOf(s, "hang").Timeouts >= 1 && statsOf(s, "panics").Failures >= 2 })
	waitFor(t, func() bool { return healthy.Load() >= 5 })
	_ = s.Stop(context.Background())

	if st := statsOf(s, "hang"); !strings.Contains(st.LastErr, "timed out") {
		t.Fatalf("timeout error = %q", st.LastErr)
	}
	if st := statsOf(s, "panics"); !strings.Contains(st.LastErr, "panic: boom") {
		t.Fatalf("panic error = %q", st.LastErr)
	}
	if st := statsOf(s, "healthy"); !st.Healthy() || st.Failures != 0 {
		t.Fatalf("healthy job affected: %+v", st)
	}
}

func TestStopCancelsInFlightJobs(t *testing.T) {
	s := New(nil, nil)
	started := make(chan struct{})
	var cancelled atomic.Bool
	_ = s.Add(Job{Name: "long", Interval: time.Hour, Run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return ctx.Err()
	}})
	s.Start(context.Background())
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !cancelled.Load() {
		t.Fatal("in-flight job was not cancelled")
	}
	if st := statsOf(s, "long"); st.Failures != 0 {
		t.Fatal("cancellation at shutdown is not a failure")
	}
}

func TestStopReportsStuckJobs(t *testing.T) {
	s := New(nil, nil)
	release := make(chan struct{})
	started := make(chan struct{})
	_ = s.Add(Job{Name: "stubborn", Interval: time.Hour, Run: func(ctx context.Context) error {
		close(started)
		<-release
		return nil
	}})
	s.Start(context.Background())
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Stop(ctx)
	close(release)
	if err == nil || !strings.Contains(err.Error(), "stubborn") {
		t.Fatalf("expected stuck job report, got %v", err)
	}
}

func TestTriggerRunsImmediately(t *testing.T) {
	s := New(nil, nil)
	var runs atomic.Int32
	_ = s.Add(Job{Name: "j", Interval: time.Hour, Run: func(ctx context.Context) error { runs.Add(1); return nil }})
	s.Start(context.Background())
	waitFor(t, func() bool { return runs.Load() == 1 })
	s.Trigger()
	waitFor(t, func() bool { return runs.Load() == 2 })
	_ = s.Stop(context.Background())
}

func TestAddValidation(t *testing.T) {
	s := New(nil, nil)
	if err := s.Add(Job{Name: "x"}); err == nil {
		t.Fatal("job without Run must be rejected")
	}
	ok := Job{Name: "x", Interval: time.Second, Run: func(context.Context) error { return errors.New("x") }}
	if err := s.Add(ok); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ok); err == nil {
		t.Fatal("duplicate must be rejected")
	}
	s.Start(context.Background())
	defer func() { _ = s.Stop(context.Background()) }()
	if err := s.Add(Job{Name: "late", Interval: time.Second, Run: ok.Run}); err == nil {
		t.Fatal("adding after start must fail")
	}
}

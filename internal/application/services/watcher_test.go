package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/collectors/servicecheck"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	"github.com/gabrielagostinho/pulse/internal/scheduler"
)

type memStore struct {
	mu   sync.Mutex
	recs []history.Record
}

func (m *memStore) Enqueue(_ context.Context, recs ...history.Record) error {
	m.mu.Lock()
	m.recs = append(m.recs, recs...)
	m.mu.Unlock()
	return nil
}

func TestSlowServiceDoesNotBlockOthers(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fast.Close()

	defs := []service.Definition{
		{ID: "slow", Kind: service.KindHTTP, Target: slow.URL, Interval: time.Hour, Timeout: 300 * time.Millisecond, FailureThreshold: 1},
		{ID: "fast", Kind: service.KindHTTP, Target: fast.URL, Interval: time.Hour, Timeout: time.Second, FailureThreshold: 1},
	}
	hub := monitoring.NewHub(10, defs)
	store := &memStore{}
	var mu sync.Mutex
	var samples []metrics.Sample
	w := &Watcher{Hub: hub, Checker: servicecheck.New(), Store: store, Sink: func(_ context.Context, s []metrics.Sample) {
		mu.Lock()
		samples = append(samples, s...)
		mu.Unlock()
	}}
	sch := scheduler.New(nil, map[string]int{Group: 2})
	for _, j := range w.Jobs(defs) {
		if err := sch.Add(j); err != nil {
			t.Fatal(err)
		}
	}
	sch.Start(context.Background())
	defer func() { _ = sch.Stop(context.Background()) }()

	deadline := time.Now().Add(3 * time.Second)
	var fastState, slowState service.State
	var fastSeen, slowSeen time.Time
	for time.Now().Before(deadline) {
		st := hub.Snapshot()
		fastState, _ = st.Service("fast")
		slowState, _ = st.Service("slow")
		if fastSeen.IsZero() && fastState.Status != service.StatusUnknown {
			fastSeen = time.Now()
		}
		if slowSeen.IsZero() && slowState.Status != service.StatusUnknown {
			slowSeen = time.Now()
		}
		if !fastSeen.IsZero() && !slowSeen.IsZero() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fastState.Status != service.StatusHealthy {
		t.Fatalf("fast = %v", fastState.Status)
	}
	if slowState.Status != service.StatusDown || slowState.LastError == "" {
		t.Fatalf("slow = %v (%s)", slowState.Status, slowState.LastError)
	}
	if !fastSeen.Before(slowSeen) {
		t.Fatal("fast check should have finished before the slow one timed out")
	}
	store.mu.Lock()
	n := len(store.recs)
	store.mu.Unlock()
	if n != 2 {
		t.Fatalf("persisted checks = %d", n)
	}
	mu.Lock()
	defer mu.Unlock()
	var up, down bool
	for _, s := range samples {
		if s.Name == metrics.ServiceUp && s.Labels["service"] == "fast" && s.Value == 1 {
			up = true
		}
		if s.Name == metrics.ServiceUp && s.Labels["service"] == "slow" && s.Value == 0 {
			down = true
		}
	}
	if !up || !down {
		t.Fatalf("samples = %+v", samples)
	}
}

func TestReplaySeedsHistory(t *testing.T) {
	defs := []service.Definition{{ID: "api", FailureThreshold: 2}}
	hub := monitoring.NewHub(10, defs)
	now := time.Now()
	Replay(hub, "api", []history.ServiceCheck{
		{At: now.Add(-2 * time.Minute), OK: true, LatencyMs: 20, Status: "healthy"},
		{At: now.Add(-time.Minute), Err: "refused", Status: "degraded"},
	})
	st, _ := hub.Snapshot().Service("api")
	if len(st.History) != 2 || st.Status != service.StatusDegraded || st.LastLatency != 20*time.Millisecond {
		t.Fatalf("state = %+v", st)
	}
}

func TestSamplesDebounceUp(t *testing.T) {
	deg := Samples("api", service.CheckResult{Status: service.StatusDegraded, At: time.Now()})
	if deg[0].Value != 1 || len(deg) != 1 {
		t.Fatalf("a single failure (degraded) must not report down: %+v", deg)
	}
	ok := Samples("api", service.CheckResult{Status: service.StatusHealthy, OK: true, Latency: 1500 * time.Microsecond})
	if len(ok) != 2 || ok[1].Value != 1.5 {
		t.Fatalf("latency sample = %+v", ok)
	}
}

package alerts

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

type recStore struct {
	mu   sync.Mutex
	recs []history.Record
}

func (r *recStore) Enqueue(_ context.Context, recs ...history.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, recs...)
	return nil
}

func (r *recStore) kinds() []history.IncidentEventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []history.IncidentEventKind
	for _, rec := range r.recs {
		out = append(out, rec.Incident.Kind)
	}
	return out
}

var base = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

func sample(name string, labels map[string]string, v float64, sec int) metrics.Sample {
	return metrics.Sample{Name: name, Labels: labels, Value: v, At: base.Add(time.Duration(sec) * time.Second)}
}

func rules() []alert.Rule {
	return []alert.Rule{
		{ID: "cpu", Name: "CPU", Metric: "cpu.usage", Op: alert.OpGT, Threshold: 90, For: 5 * time.Second, Severity: alert.SeverityWarning, Enabled: true, Unit: "%"},
		{ID: "down", Name: "Service down", Metric: "service.up", Op: alert.OpLT, Threshold: 1, Severity: alert.SeverityCritical, Enabled: true},
	}
}

func TestSustainedBreachPersistsOneIncident(t *testing.T) {
	st := &recStore{}
	var last View
	e := New(rules(), st, func(v View) { last = v }, nil)
	ctx := context.Background()
	for s := 0; s < 120; s++ {
		e.Observe(ctx, []metrics.Sample{sample("cpu.usage", nil, 97, s)})
	}
	if k := st.kinds(); len(k) != 1 || k[0] != history.IncidentOpened {
		t.Fatalf("expected a single persisted open, got %v", k)
	}
	if len(last.Active) != 1 || last.Rules[0].Phase != alert.PhaseFiring {
		t.Fatalf("view = %+v", last)
	}
	for s := 120; s < 125; s++ {
		e.Observe(ctx, []metrics.Sample{sample("cpu.usage", nil, 10, s)})
	}
	if k := st.kinds(); len(k) != 2 || k[1] != history.IncidentResolved {
		t.Fatalf("expected open+resolve, got %v", k)
	}
	if v := e.View(); len(v.Active) != 0 || len(v.Recent) != 1 {
		t.Fatalf("view after resolve = %+v", v)
	}
}

func TestBareMetricRuleTracksEachSeries(t *testing.T) {
	st := &recStore{}
	e := New(rules(), st, nil, nil)
	ctx := context.Background()
	e.Observe(ctx, []metrics.Sample{
		sample("service.up", map[string]string{"service": "api"}, 0, 0),
		sample("service.up", map[string]string{"service": "db"}, 0, 0),
		sample("service.up", map[string]string{"service": "web"}, 1, 0),
	})
	v := e.View()
	if len(v.Active) != 2 {
		t.Fatalf("expected one incident per failing service, got %d", len(v.Active))
	}
	if v.Active[0].Severity != alert.SeverityCritical || v.Active[0].RuleName == "Service down" {
		t.Fatalf("incident should name the series: %+v", v.Active[0])
	}
	var down RuleStatus
	for _, r := range v.Rules {
		if r.Rule.ID == "down" {
			down = r
		}
	}
	if down.Series != 3 || down.Firing != 2 {
		t.Fatalf("rule status = %+v", down)
	}
}

func TestExactSeriesRule(t *testing.T) {
	r := []alert.Rule{{ID: "api", Name: "API", Metric: "service.up{service=api}", Op: alert.OpLT, Threshold: 1, Enabled: true}}
	e := New(r, nil, nil, nil)
	e.Observe(context.Background(), []metrics.Sample{sample("service.up", map[string]string{"service": "db"}, 0, 0)})
	if len(e.View().Active) != 0 {
		t.Fatal("labelled rule must not match other series")
	}
	e.Observe(context.Background(), []metrics.Sample{sample("service.up", map[string]string{"service": "api"}, 0, 0)})
	if len(e.View().Active) != 1 {
		t.Fatal("labelled rule must match its series")
	}
}

func TestAcknowledgeIsPersistedAndCarriedToResolution(t *testing.T) {
	st := &recStore{}
	e := New(rules(), st, nil, nil)
	ctx := context.Background()
	e.Observe(ctx, []metrics.Sample{sample("service.up", map[string]string{"service": "api"}, 0, 0)})
	id := e.View().Active[0].ID
	if err := e.Acknowledge(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := e.Acknowledge(ctx, id); err != nil {
		t.Fatal("acknowledging twice is a no-op")
	}
	if !e.View().Active[0].Acknowledged() {
		t.Fatal("active incident must show ack")
	}
	if err := e.Acknowledge(ctx, "missing"); err == nil {
		t.Fatal("unknown incident must error")
	}
	e.Observe(ctx, []metrics.Sample{sample("service.up", map[string]string{"service": "api"}, 1, 1)})
	k := st.kinds()
	if len(k) != 3 || k[1] != history.IncidentAcked || k[2] != history.IncidentResolved {
		t.Fatalf("events = %v", k)
	}
	if rec := e.View().Recent[0]; !rec.Acknowledged() {
		t.Fatal("resolved incident must keep its acknowledgement")
	}
}

func TestRestoreReattachesAndClosesOrphans(t *testing.T) {
	st := &recStore{}
	e := New(rules(), st, nil, nil)
	ctx := context.Background()
	open := []alert.Incident{
		{ID: "keep", RuleID: "cpu", RuleName: "CPU", Metric: "cpu.usage", StartedAt: base.Add(-time.Hour)},
		{ID: "orphan", RuleID: "deleted-rule", Metric: "x", StartedAt: base.Add(-time.Hour)},
	}
	e.Restore(ctx, open, nil)
	v := e.View()
	if len(v.Active) != 1 || v.Active[0].ID != "keep" {
		t.Fatalf("active after restore = %+v", v.Active)
	}
	if k := st.kinds(); len(k) != 1 || k[0] != history.IncidentResolved {
		t.Fatalf("orphan must be resolved and persisted, got %v", k)
	}
	for s := 0; s < 30; s++ {
		e.Observe(ctx, []metrics.Sample{sample("cpu.usage", nil, 99, s)})
	}
	if len(st.kinds()) != 1 {
		t.Fatal("restored incident duplicated")
	}
}

func TestSweepResolvesStaleSeries(t *testing.T) {
	st := &recStore{}
	e := New(rules(), st, nil, nil)
	now := base
	e.now = func() time.Time { return now }
	ctx := context.Background()
	e.Observe(ctx, []metrics.Sample{sample("service.up", map[string]string{"service": "gone"}, 0, 0)})
	now = base.Add(time.Minute)
	e.Sweep(ctx)
	if len(e.View().Active) != 1 {
		t.Fatal("recent series must not be swept")
	}
	now = base.Add(staleAfter + time.Minute)
	e.Sweep(ctx)
	if len(e.View().Active) != 0 {
		t.Fatal("stale incident must be resolved")
	}
	if k := st.kinds(); k[len(k)-1] != history.IncidentResolved {
		t.Fatalf("events = %v", k)
	}
}

func TestConcurrentObserve(t *testing.T) {
	e := New(rules(), &recStore{}, func(View) {}, nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for s := 0; s < 200; s++ {
				e.Observe(context.Background(), []metrics.Sample{
					sample("cpu.usage", nil, float64(80+s%20), s),
					sample("service.up", map[string]string{"service": "s"}, float64(s%2), s),
				})
				_ = e.View()
			}
		}(g)
	}
	wg.Wait()
}

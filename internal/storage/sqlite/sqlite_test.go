package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pulse.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func pt(key string, at time.Time, v float64) history.MetricPoint {
	name, rest, _ := strings.Cut(key, "{")
	var labels map[string]string
	if k, v, ok := strings.Cut(strings.TrimSuffix(rest, "}"), "="); ok {
		labels = map[string]string{k: v}
	}
	return history.MetricPoint{Key: key, Name: name, Labels: labels, Unit: "%", Source: "test", At: at, Value: v, Min: v, Max: v, Count: 1}
}

func TestMigrationsAreIdempotentAndVersioned(t *testing.T) {
	s, path := openTemp(t)
	v, err := s.SchemaVersion(context.Background())
	if err != nil || v != 1 {
		t.Fatalf("version = %d err=%v", v, err)
	}
	_ = s.Close()
	s2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	var n int
	if err := s2.read.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil || n != 1 {
		t.Fatalf("migrations applied %d times", n)
	}
	var mode string
	_ = s2.write.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode = %s", mode)
	}
}

func TestNewerSchemaIsRejected(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.write.Exec("INSERT INTO schema_migrations VALUES (999, 'future', 0)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	_, err := Open(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("got %v", err)
	}
}

func TestNotADatabaseIsDiagnosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("this is not sqlite ", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "hint:") {
		t.Fatalf("expected actionable hint, got %v", err)
	}
}

func TestWriteAndQueryRangePersistsAcrossReopen(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Minute)
	var b history.Batch
	for i := 0; i < 60; i++ {
		b.Metrics = append(b.Metrics, pt("cpu.usage", base.Add(time.Duration(i)*time.Minute), float64(i)))
		b.Metrics = append(b.Metrics, pt("disk.used_pct{mount=/}", base.Add(time.Duration(i)*time.Minute), 50))
	}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	series, err := s.ListSeries(ctx)
	if err != nil || len(series) != 2 {
		t.Fatalf("series = %+v err=%v", series, err)
	}
	if series[1].Labels["mount"] != "/" || !series[0].First.Equal(base) {
		t.Fatalf("series metadata = %+v", series)
	}
	buckets, err := s.QueryRange(ctx, "cpu.usage", base, base.Add(time.Hour), 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 6 {
		t.Fatalf("buckets = %d", len(buckets))
	}
	if buckets[0].Count != 10 || buckets[0].Min != 0 || buckets[0].Max != 9 || buckets[0].Avg != 4.5 {
		t.Fatalf("first bucket = %+v", buckets[0])
	}
	if _, err := s.QueryRange(ctx, "cpu.usage", base, base, 6); err == nil {
		t.Fatal("empty range must error")
	}
}

func TestSameWindowMergesAfterRestart(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	at := time.Now().Truncate(10 * time.Second)
	first := pt("cpu.usage", at, 10)
	first.Count = 2
	second := pt("cpu.usage", at, 40)
	second.Min, second.Max = 30, 50
	if err := s.WriteBatch(ctx, history.Batch{Metrics: []history.MetricPoint{first}}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBatch(ctx, history.Batch{Metrics: []history.MetricPoint{second}}); err != nil {
		t.Fatal(err)
	}
	var v, mn, mx float64
	var c int
	_ = s.read.QueryRow("SELECT value, min, max, count FROM metric_samples").Scan(&v, &mn, &mx, &c)
	if v != 20 || mn != 10 || mx != 50 || c != 3 {
		t.Fatalf("merged = %v %v %v %v", v, mn, mx, c)
	}
}

func TestServiceChecksAndIncidentLifecycle(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	inc := alert.Incident{
		ID: "abc", RuleID: "cpu-high", RuleName: "CPU", Metric: "cpu.usage", Severity: alert.SeverityCritical,
		Message: "CPU: 95% > 90%", Threshold: 90, Unit: "%", StartedAt: now.Add(-time.Minute), PeakValue: 95, LastValue: 95,
	}
	err := s.WriteBatch(ctx, history.Batch{
		Checks: []history.ServiceCheck{
			{ServiceID: "api", At: now.Add(-2 * time.Second), Status: "healthy", OK: true, LatencyMs: 12.5, StatusCode: 200},
			{ServiceID: "api", At: now, Status: "down", Err: "connection refused"},
		},
		Incidents: []history.IncidentEvent{{Kind: history.IncidentOpened, Incident: inc}},
	})
	if err != nil {
		t.Fatal(err)
	}
	open, err := s.OpenIncidents(ctx)
	if err != nil || len(open) != 1 || open[0].Severity != alert.SeverityCritical || !open[0].StartedAt.Equal(inc.StartedAt) {
		t.Fatalf("open = %+v err=%v", open, err)
	}

	inc.AckedAt = now
	acked := inc
	inc.EndedAt, inc.PeakValue = now.Add(time.Second), 99
	if err := s.WriteBatch(ctx, history.Batch{Incidents: []history.IncidentEvent{
		{Kind: history.IncidentAcked, Incident: acked},
		{Kind: history.IncidentResolved, Incident: inc},
	}}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, _ = Open(ctx, path)
	defer s.Close()
	if open, _ := s.OpenIncidents(ctx); len(open) != 0 {
		t.Fatal("resolved incident must not be open")
	}
	recent, _ := s.RecentIncidents(ctx, 10)
	if len(recent) != 1 || recent[0].PeakValue != 99 || !recent[0].Acknowledged() || recent[0].Active() {
		t.Fatalf("recent = %+v", recent)
	}
	checks, _ := s.RecentChecks(ctx, "api", 10)
	if len(checks) != 2 || !checks[0].OK || checks[0].LatencyMs != 12.5 || checks[1].Err != "connection refused" {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestPruneRespectsCutoffsAndOpenIncidents(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := time.Now()
	old, fresh := now.Add(-10*24*time.Hour), now.Add(-time.Hour)
	var b history.Batch
	for i := 0; i < 12000; i++ {
		b.Metrics = append(b.Metrics, pt("cpu.usage", old.Add(time.Duration(i)*time.Second), 1))
	}
	b.Metrics = append(b.Metrics, pt("cpu.usage", fresh, 2), pt("mem.used_pct", old, 3))
	b.Checks = []history.ServiceCheck{{ServiceID: "a", At: old, Status: "down"}, {ServiceID: "a", At: fresh, Status: "healthy", OK: true}}
	b.Incidents = []history.IncidentEvent{
		{Kind: history.IncidentOpened, Incident: alert.Incident{ID: "still-open", RuleID: "r", StartedAt: old}},
		{Kind: history.IncidentOpened, Incident: alert.Incident{ID: "closed", RuleID: "r", StartedAt: old}},
		{Kind: history.IncidentResolved, Incident: alert.Incident{ID: "closed", EndedAt: old.Add(time.Minute)}},
	}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	cut := now.Add(-7 * 24 * time.Hour)
	res, err := s.Prune(ctx, history.Retention{MetricsBefore: cut, ChecksBefore: cut, IncidentBefore: cut})
	if err != nil {
		t.Fatal(err)
	}
	if res.Metrics != 12001 || res.Checks != 1 || res.Incidents != 1 {
		t.Fatalf("pruned = %+v", res)
	}
	series, _ := s.ListSeries(ctx)
	if len(series) != 1 || series[0].Key != "cpu.usage" || series[0].First.UnixMilli() != fresh.UnixMilli() {
		t.Fatalf("series after prune = %+v", series)
	}
	open, _ := s.OpenIncidents(ctx)
	if len(open) != 1 || open[0].ID != "still-open" {
		t.Fatal("open incidents must survive retention")
	}
	if err := s.WriteBatch(ctx, history.Batch{Metrics: []history.MetricPoint{pt("mem.used_pct", now, 4)}}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(ctx)
	if err != nil || st.SchemaVersion != 1 || st.SizeBytes == 0 {
		t.Fatalf("stats = %+v err=%v", st, err)
	}
}

func TestSyncRules(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	rules := []alert.Rule{{ID: "a", Name: "A", Metric: "cpu.usage", Op: ">", Threshold: 1, Enabled: true}, {ID: "b", Name: "B", Metric: "x", Op: "<", Enabled: true}}
	if err := s.SyncRules(ctx, rules); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncRules(ctx, rules[:1]); err != nil {
		t.Fatal(err)
	}
	var enabled int
	_ = s.read.QueryRow("SELECT enabled FROM alert_rules WHERE id = 'b'").Scan(&enabled)
	if enabled != 0 {
		t.Fatal("removed rule must be disabled, not deleted")
	}
}

func BenchmarkWriteBatch(b *testing.B) {
	s, err := Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	keys := []string{"cpu.usage", "mem.used_pct", "net.rx_bps", "net.tx_bps", "disk.read_bps", "disk.write_bps"}
	base := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var batch history.Batch
		for j := 0; j < 512; j++ {
			batch.Metrics = append(batch.Metrics, pt(keys[j%len(keys)], base.Add(time.Duration(i*512+j)*time.Second), float64(j)))
		}
		if err := s.WriteBatch(context.Background(), batch); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N*512)/b.Elapsed().Seconds(), "rows/s")
}

func BenchmarkQueryRange(b *testing.B) {
	s, err := Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	base := time.Now().Add(-7 * 24 * time.Hour)
	for chunk := 0; chunk < 60; chunk++ {
		var batch history.Batch
		for j := 0; j < 1008; j++ {
			i := chunk*1008 + j
			batch.Metrics = append(batch.Metrics, pt("cpu.usage", base.Add(time.Duration(i)*10*time.Second), float64(i%100)))
		}
		if err := s.WriteBatch(context.Background(), batch); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.QueryRange(context.Background(), "cpu.usage", base, time.Now(), 200); err != nil {
			b.Fatal(err)
		}
	}
}

package integration

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/bootstrap"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	"github.com/gabrielagostinho/pulse/internal/storage/sqlite"
)

func writeConfig(t *testing.T, dir, healthyURL, deadAddr string) string {
	t.Helper()
	body := `
general:
  live_window: 1m
collectors:
  processes:
    interval: 1s
storage:
  path: ` + filepath.ToSlash(filepath.Join(dir, "pulse.db")) + `
  resolution: 1s
  flush_interval: 200ms
services:
  targets:
    - id: web
      url: ` + healthyURL + `
      interval: 1s
      timeout: 1s
    - id: dead
      type: tcp
      address: ` + deadAddr + `
      interval: 1s
      timeout: 500ms
      failure_threshold: 1
alerts:
  rules:
    - id: dead-down
      service: dead
      condition: down
      severity: critical
    - id: cpu-any
      metric: cpu.usage
      operator: ">="
      threshold: 0
      severity: info
logging:
  file: "-"
`
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func deadAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEndToEndPersistAlertAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	dir := t.TempDir()
	t.Setenv("PULSE_DATA_DIR", dir)
	t.Setenv("PULSE_CONFIG_DIR", dir)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer web.Close()
	cfgPath := writeConfig(t, dir, web.URL, deadAddress(t))
	cfg, err := config.Load(config.Options{Path: cfgPath})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	app.Start(ctx)
	waitFor(t, "live state", 10*time.Second, func() bool {
		st := app.Snapshot()
		webState, _ := st.Service("web")
		deadState, _ := st.Service("dead")
		return st.CPU.OK() && st.Memory.OK() && st.Processes.OK &&
			webState.Status == service.StatusHealthy && deadState.Status == service.StatusDown &&
			len(st.Alerts.Active) >= 2
	})
	time.Sleep(2500 * time.Millisecond)
	st := app.Snapshot()
	var deadIncidents int
	var deadID string
	for _, inc := range st.Alerts.Active {
		if inc.RuleID == "dead-down" {
			deadIncidents++
			deadID = inc.ID
		}
	}
	if deadIncidents != 1 {
		t.Fatalf("a continuously failing service must have exactly one incident, got %d", deadIncidents)
	}
	if err := app.Acknowledge(ctx, deadID); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := app.Shutdown(sctx); err != nil {
		t.Fatalf("clean shutdown expected: %v", err)
	}

	store, err := sqlite.Open(ctx, cfg.Storage.Path)
	if err != nil {
		t.Fatal(err)
	}
	stats, _ := store.Stats(ctx)
	if stats.MetricRows == 0 || stats.CheckRows < 4 || stats.IncidentRows < 2 {
		t.Fatalf("expected persisted data, got %+v", stats)
	}
	buckets, err := store.QueryRange(ctx, "cpu.usage", time.Now().Add(-time.Minute), time.Now().Add(time.Second), 60)
	if err != nil || len(buckets) == 0 {
		t.Fatalf("cpu history not queryable: %v (%d buckets)", err, len(buckets))
	}
	open, _ := store.OpenIncidents(ctx)
	var acked bool
	for _, inc := range open {
		if inc.ID == deadID && inc.Acknowledged() {
			acked = true
		}
	}
	if !acked {
		t.Fatalf("acknowledged open incident must be persisted, open=%+v", open)
	}
	_ = store.Close()

	app2, err := bootstrap.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	app2.Start(ctx)
	waitFor(t, "restored service history", 5*time.Second, func() bool {
		s, _ := app2.Snapshot().Service("dead")
		return len(s.History) > 0
	})
	time.Sleep(2 * time.Second)
	st2 := app2.Snapshot()
	var restored int
	for _, inc := range st2.Alerts.Active {
		if inc.RuleID == "dead-down" {
			restored++
			if inc.ID != deadID || !inc.Acknowledged() {
				t.Fatalf("restart must continue the same acknowledged incident, got %+v", inc)
			}
		}
	}
	if restored != 1 {
		t.Fatalf("expected the restored incident only, got %d", restored)
	}
	sctx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	if err := app2.Shutdown(sctx2); err != nil {
		t.Fatal(err)
	}
}

func TestStorageFailureDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PULSE_DATA_DIR", dir)
	t.Setenv("PULSE_CONFIG_DIR", dir)
	bad := filepath.Join(dir, "not-a-db")
	if err := os.WriteFile(bad, []byte("garbage garbage garbage garbage garbage garbage garbage garbage garbage garbage garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{DBPath: bad, LogFile: "-"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		t.Fatalf("a broken database must not prevent startup: %v", err)
	}
	app.Start(ctx)
	waitFor(t, "live metrics without storage", 5*time.Second, func() bool { return app.Snapshot().CPU.OK() })
	st := app.Snapshot()
	if st.Storage.InitErr == "" || app.HistoryAvailable() {
		t.Fatal("storage failure must be reported in state")
	}
	if _, err := app.ListSeries(ctx); err == nil {
		t.Fatal("history queries must fail explicitly")
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := app.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
}

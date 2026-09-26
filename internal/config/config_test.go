package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/configs"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PULSE_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("PULSE_CONFIG_DIR", filepath.Join(dir, "cfg"))
	t.Setenv("PULSE_CONFIG", "")
	return dir
}

func write(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsAreValid(t *testing.T) {
	dir := isolate(t)
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if cfg.Source != "" {
		t.Fatalf("no file should be used, got %q", cfg.Source)
	}
	if want := filepath.Join(dir, "data", "pulse.db"); cfg.Storage.Path != want {
		t.Fatalf("db path = %s want %s", cfg.Storage.Path, want)
	}
	if cfg.Storage.Retention.Metrics != 7*24*time.Hour {
		t.Fatal("seven days of metric retention by default")
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	dir := isolate(t)
	p := write(t, dir, string(configs.Example))
	cfg, err := Load(Options{Path: p})
	if err != nil {
		t.Fatalf("example config must validate: %v", err)
	}
	if len(cfg.Services.Targets) != 3 || cfg.Services.Targets[1].Type != "tcp" {
		t.Fatalf("targets = %+v", cfg.Services.Targets)
	}
	rules, err := cfg.AlertRules()
	if err != nil {
		t.Fatal(err)
	}
	var slow alert.Rule
	for _, r := range rules {
		if r.ID == "local-api-slow" {
			slow = r
		}
	}
	if slow.Metric != "service.latency_ms{service=local-api}" || slow.Op != alert.OpGT || slow.Unit != "ms" {
		t.Fatalf("service sugar not compiled: %+v", slow)
	}
}

func TestUserListsReplaceDefaults(t *testing.T) {
	dir := isolate(t)
	p := write(t, dir, `
alerts:
  rules:
    - id: only
      metric: cpu.usage
      operator: ">="
      threshold: 50
      severity: info
`)
	cfg, err := Load(Options{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Alerts.Rules) != 1 || cfg.Alerts.Rules[0].ID != "only" || cfg.Alerts.Rules[0].For != 0 {
		t.Fatalf("rules must be replaced, not merged: %+v", cfg.Alerts.Rules)
	}
}

func TestDurationsWithDays(t *testing.T) {
	dir := isolate(t)
	p := write(t, dir, "storage:\n  retention:\n    metrics: 14d\n    alerts: 1d12h\n")
	cfg, err := Load(Options{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Retention.Metrics != 14*24*time.Hour || cfg.Storage.Retention.Alerts != 36*time.Hour {
		t.Fatalf("retention = %+v", cfg.Storage.Retention)
	}
}

func TestValidationReportsAllProblems(t *testing.T) {
	dir := isolate(t)
	p := write(t, dir, `
ui:
  charts: ascii
services:
  targets:
    - id: API
      url: ftp://nope
    - id: db
      type: tcp
      address: localhost
      interval: 5s
      timeout: 10s
alerts:
  rules:
    - id: bad
      metric: cpu.usge
      operator: "!="
      severity: urgent
    - id: ghost
      service: missing
`)
	_, err := Load(Options{Path: p})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	msg := ve.Error()
	for _, want := range []string{
		"ui.charts", "id must be lowercase", "absolute http(s) URL", "address must be host:port",
		"timeout (10s) must not exceed interval", `metric "cpu.usge" is unknown`, "operator must be one of",
		"unknown severity", `service "missing" does not match`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
}

func TestUnknownKeysAreRejected(t *testing.T) {
	dir := isolate(t)
	p := write(t, dir, "storage:\n  retension: 7d\n")
	_, err := Load(Options{Path: p})
	if err == nil || !strings.Contains(err.Error(), "retension") {
		t.Fatalf("typo must be reported, got %v", err)
	}
}

func TestExplicitMissingFileFails(t *testing.T) {
	dir := isolate(t)
	_, err := Load(Options{Path: filepath.Join(dir, "nope.yaml")})
	if err == nil || !strings.Contains(err.Error(), "pulse config init") {
		t.Fatalf("got %v", err)
	}
}

func TestOverridesAndRelativePaths(t *testing.T) {
	dir := isolate(t)
	cfg, err := Load(Options{DBPath: "custom.db", LogLevel: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Path != filepath.Join(dir, "data", "custom.db") || cfg.Logging.Level != "debug" {
		t.Fatalf("overrides not applied: %s %s", cfg.Storage.Path, cfg.Logging.Level)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{"7d": 168 * time.Hour, "90s": 90 * time.Second, "": 0, "0.5d": 12 * time.Hour}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseDuration("soon"); err == nil {
		t.Error("invalid duration must error")
	}
}

package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
)

var KnownMetrics = []string{
	metrics.CPUUsage, metrics.CPUCoreUsage, metrics.CPULoad1,
	metrics.MemUsedPct, metrics.MemUsedBytes, metrics.SwapUsedPct,
	metrics.DiskUsedPct, metrics.DiskReadBps, metrics.DiskWriteBps,
	metrics.NetRxBps, metrics.NetTxBps,
	metrics.ServiceUp, metrics.ServiceLatency, metrics.ProcessCount,
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

type ValidationError struct {
	Source   string
	Problems []string
}

func (e *ValidationError) Error() string {
	src := e.Source
	if src == "" {
		src = "built-in defaults"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "invalid configuration (%s):", src)
	for _, p := range e.Problems {
		b.WriteString("\n  • ")
		b.WriteString(p)
	}
	return b.String()
}

func (c Config) Validate() error {
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }
	positive := func(name string, d time.Duration) {
		if d <= 0 {
			add("%s must be a positive duration (got %s)", name, d)
		}
	}

	positive("general.live_window", c.General.LiveWindow)
	if c.UI.Charts != "braille" && c.UI.Charts != "blocks" {
		add(`ui.charts must be "braille" or "blocks" (got %q)`, c.UI.Charts)
	}
	if c.UI.FPS < 1 || c.UI.FPS > 60 {
		add("ui.fps must be between 1 and 60 (got %d)", c.UI.FPS)
	}
	if !slices.Contains(Screens, c.UI.StartScreen) {
		add("ui.start_screen must be one of %s (got %q)", strings.Join(Screens, ", "), c.UI.StartScreen)
	}

	col := map[string]Collector{
		"cpu": c.Collectors.CPU, "memory": c.Collectors.Memory, "disk": c.Collectors.Disk.Collector,
		"network": c.Collectors.Network.Collector, "processes": c.Collectors.Process.Collector, "host": c.Collectors.Host,
	}
	for _, name := range []string{"cpu", "memory", "disk", "network", "processes", "host"} {
		cc := col[name]
		if !cc.Enabled {
			continue
		}
		if cc.Interval < 200*time.Millisecond {
			add("collectors.%s.interval must be at least 200ms (got %s)", name, cc.Interval)
		}
		positive("collectors."+name+".timeout", cc.Timeout)
	}
	if c.Scheduler.MaxConcurrency < 1 {
		add("scheduler.max_concurrency must be at least 1")
	}

	if c.Storage.Enabled {
		s := c.Storage
		if s.Path == "" {
			add("storage.path must not be empty")
		}
		if s.Resolution < time.Second {
			add("storage.resolution must be at least 1s (got %s)", s.Resolution)
		}
		if s.QueueSize < 16 {
			add("storage.queue_size must be at least 16 (got %d)", s.QueueSize)
		}
		if s.BatchSize < 1 || s.BatchSize > s.QueueSize {
			add("storage.batch_size must be between 1 and storage.queue_size (got %d)", s.BatchSize)
		}
		positive("storage.flush_interval", s.FlushInterval)
		positive("storage.enqueue_timeout", s.EnqueueTimeout)
		positive("storage.retention.metrics", s.Retention.Metrics)
		positive("storage.retention.service_checks", s.Retention.ServiceChecks)
		positive("storage.retention.alerts", s.Retention.Alerts)
		if s.Retention.Interval < time.Minute {
			add("storage.retention.interval must be at least 1m (got %s)", s.Retention.Interval)
		}
	}

	if c.Services.MaxConcurrency < 1 {
		add("services.max_concurrency must be at least 1")
	}
	serviceIDs := map[string]bool{}
	for i, t := range c.Services.Targets {
		at := fmt.Sprintf("services.targets[%d]", i)
		if t.ID != "" {
			at = fmt.Sprintf("services.targets[%d] (%s)", i, t.ID)
		}
		switch {
		case t.ID == "":
			add("%s.id is required", at)
		case !idRe.MatchString(t.ID):
			add("%s.id must be lowercase letters, digits, '.', '_' or '-'", at)
		case serviceIDs[t.ID]:
			add("%s.id is duplicated", at)
		}
		serviceIDs[t.ID] = true
		switch t.Type {
		case "http":
			u, err := url.Parse(t.URL)
			if t.URL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				add("%s.url must be an absolute http(s) URL (got %q)", at, t.URL)
			}
			if !slices.Contains([]string{"GET", "HEAD", "POST", "OPTIONS"}, t.Method) {
				add("%s.method must be GET, HEAD, POST or OPTIONS (got %q)", at, t.Method)
			}
			for _, code := range t.ExpectStatus {
				if code < 100 || code > 599 {
					add("%s.expect_status contains invalid HTTP status %d", at, code)
				}
			}
		case "tcp":
			if _, port, err := net.SplitHostPort(t.Address); err != nil || port == "" {
				add("%s.address must be host:port (got %q)", at, t.Address)
			}
		default:
			add(`%s.type must be "http" or "tcp" (got %q)`, at, t.Type)
		}
		if t.Interval < time.Second {
			add("%s.interval must be at least 1s (got %s)", at, t.Interval)
		}
		if t.Timeout <= 0 {
			add("%s.timeout must be positive", at)
		} else if t.Timeout > t.Interval {
			add("%s.timeout (%s) must not exceed interval (%s)", at, t.Timeout, t.Interval)
		}
		if t.FailureThreshold < 1 {
			add("%s.failure_threshold must be at least 1", at)
		}
	}

	ruleIDs := map[string]bool{}
	for i, r := range c.Alerts.Rules {
		at := fmt.Sprintf("alerts.rules[%d]", i)
		if r.ID != "" {
			at = fmt.Sprintf("alerts.rules[%d] (%s)", i, r.ID)
		}
		switch {
		case r.ID == "":
			add("%s.id is required", at)
		case !idRe.MatchString(r.ID):
			add("%s.id must be lowercase letters, digits, '.', '_' or '-'", at)
		case ruleIDs[r.ID]:
			add("%s.id is duplicated", at)
		}
		ruleIDs[r.ID] = true

		switch {
		case r.Metric != "" && r.Service != "":
			add("%s: set either metric or service, not both", at)
		case r.Metric != "":
			name, _, _ := strings.Cut(r.Metric, "{")
			if !slices.Contains(KnownMetrics, name) {
				add("%s.metric %q is unknown; known metrics: %s", at, name, strings.Join(KnownMetrics, ", "))
			}
			if !alert.Operator(r.Operator).Valid() {
				add(`%s.operator must be one of >, >=, <, <= (got %q)`, at, r.Operator)
			}
		case r.Service != "":
			if !serviceIDs[r.Service] {
				add("%s.service %q does not match any services.targets id", at, r.Service)
			}
			switch r.Condition {
			case "down", "":
			case "latency":
				if r.Threshold <= 0 {
					add("%s.threshold must be a positive latency in milliseconds", at)
				}
			default:
				add(`%s.condition must be "down" or "latency" (got %q)`, at, r.Condition)
			}
		default:
			add("%s: metric or service is required", at)
		}
		if r.For < 0 || r.ClearFor < 0 {
			add("%s: for and clear_for must not be negative", at)
		}
		if r.Hysteresis < 0 {
			add("%s.hysteresis must not be negative", at)
		}
		if _, err := alert.ParseSeverity(r.Severity); err != nil {
			add("%s.severity: %v", at, err)
		}
	}

	if !slices.Contains([]string{"debug", "info", "warn", "error"}, strings.ToLower(c.Logging.Level)) {
		add(`logging.level must be debug, info, warn or error (got %q)`, c.Logging.Level)
	}
	if c.Logging.Format != "text" && c.Logging.Format != "json" {
		add(`logging.format must be "text" or "json" (got %q)`, c.Logging.Format)
	}

	if len(p) > 0 {
		return &ValidationError{Source: c.Source, Problems: p}
	}
	return nil
}

var Screens = []string{"overview", "processes", "services", "alerts", "history", "settings"}

func (c Config) ServiceDefinitions() []service.Definition {
	out := make([]service.Definition, 0, len(c.Services.Targets))
	for _, t := range c.Services.Targets {
		target := t.URL
		if t.Type == "tcp" {
			target = t.Address
		}
		out = append(out, service.Definition{
			ID:               t.ID,
			Name:             t.Name,
			Kind:             service.Kind(t.Type),
			Target:           target,
			Interval:         t.Interval,
			Timeout:          t.Timeout,
			Method:           t.Method,
			Headers:          t.Headers,
			ExpectStatus:     t.ExpectStatus,
			TLSSkipVerify:    t.TLSSkipVerify,
			DegradedLatency:  t.DegradedLatency,
			FailureThreshold: t.FailureThreshold,
		})
	}
	return out
}

func (c Config) AlertRules() ([]alert.Rule, error) {
	out := make([]alert.Rule, 0, len(c.Alerts.Rules))
	for _, r := range c.Alerts.Rules {
		sev, err := alert.ParseSeverity(r.Severity)
		if err != nil {
			return nil, err
		}
		rule := alert.Rule{
			ID: r.ID, Name: r.Name, Metric: r.Metric, Op: alert.Operator(r.Operator),
			Threshold: r.Threshold, For: r.For, ClearFor: r.ClearFor, Hysteresis: r.Hysteresis,
			Severity: sev, Enabled: r.Enabled == nil || *r.Enabled,
		}
		if r.Service != "" {
			labels := map[string]string{"service": r.Service}
			if r.Condition == "latency" {
				rule.Metric = metrics.SeriesKey(metrics.ServiceLatency, labels)
				rule.Op = alert.OpGT
			} else {
				rule.Metric = metrics.SeriesKey(metrics.ServiceUp, labels)
				rule.Op, rule.Threshold = alert.OpLT, 1
			}
		}
		if rule.Name == "" {
			rule.Name = rule.ID
		}
		rule.Unit = unitFor(rule.Metric)
		out = append(out, rule)
	}
	return out, nil
}

func unitFor(metric string) string {
	name, _, _ := strings.Cut(metric, "{")
	switch {
	case strings.HasSuffix(name, "_pct"), name == metrics.CPUUsage, name == metrics.CPUCoreUsage:
		return metrics.UnitPercent
	case strings.HasSuffix(name, "_bps"):
		return metrics.UnitBytesPerSec
	case strings.HasSuffix(name, "_ms"):
		return metrics.UnitMillis
	case strings.HasSuffix(name, "_bytes"):
		return metrics.UnitBytes
	}
	return ""
}

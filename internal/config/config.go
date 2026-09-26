package config

import (
	"time"
)

type Config struct {
	General    General    `koanf:"general"`
	UI         UI         `koanf:"ui"`
	Collectors Collectors `koanf:"collectors"`
	Scheduler  Scheduler  `koanf:"scheduler"`
	Storage    Storage    `koanf:"storage"`
	Services   Services   `koanf:"services"`
	Alerts     Alerts     `koanf:"alerts"`
	Logging    Logging    `koanf:"logging"`

	Source  string `koanf:"-"`
	DataDir string `koanf:"-"`
}

type General struct {
	LiveWindow time.Duration `koanf:"live_window"`
}

type UI struct {
	Charts      string `koanf:"charts"`
	FPS         int    `koanf:"fps"`
	StartScreen string `koanf:"start_screen"`
}

type Collector struct {
	Enabled  bool          `koanf:"enabled"`
	Interval time.Duration `koanf:"interval"`
	Timeout  time.Duration `koanf:"timeout"`
}

type Collectors struct {
	CPU     Collector `koanf:"cpu"`
	Memory  Collector `koanf:"memory"`
	Disk    Disk      `koanf:"disk"`
	Network Network   `koanf:"network"`
	Process Process   `koanf:"processes"`
	Host    Collector `koanf:"host"`
}

type Disk struct {
	Collector      `koanf:",squash"`
	ExcludeFSTypes []string `koanf:"exclude_fstypes"`
	ExcludeMounts  []string `koanf:"exclude_mounts"`
}

type Network struct {
	Collector         `koanf:",squash"`
	ExcludeInterfaces []string `koanf:"exclude_interfaces"`
}

type Process struct {
	Collector `koanf:",squash"`
	AllowKill bool `koanf:"allow_kill"`
}

type Scheduler struct {
	MaxConcurrency int `koanf:"max_concurrency"`
}

type Storage struct {
	Enabled        bool          `koanf:"enabled"`
	Path           string        `koanf:"path"`
	Resolution     time.Duration `koanf:"resolution"`
	QueueSize      int           `koanf:"queue_size"`
	BatchSize      int           `koanf:"batch_size"`
	FlushInterval  time.Duration `koanf:"flush_interval"`
	EnqueueTimeout time.Duration `koanf:"enqueue_timeout"`
	Retention      Retention     `koanf:"retention"`
}

type Retention struct {
	Metrics       time.Duration `koanf:"metrics"`
	ServiceChecks time.Duration `koanf:"service_checks"`
	Alerts        time.Duration `koanf:"alerts"`
	Interval      time.Duration `koanf:"interval"`
}

type Services struct {
	MaxConcurrency int             `koanf:"max_concurrency"`
	Defaults       ServiceDefaults `koanf:"defaults"`
	Targets        []ServiceTarget `koanf:"targets"`
}

type ServiceDefaults struct {
	Interval         time.Duration `koanf:"interval"`
	Timeout          time.Duration `koanf:"timeout"`
	DegradedLatency  time.Duration `koanf:"degraded_latency"`
	FailureThreshold int           `koanf:"failure_threshold"`
}

type ServiceTarget struct {
	ID               string            `koanf:"id"`
	Name             string            `koanf:"name"`
	Type             string            `koanf:"type"`
	URL              string            `koanf:"url"`
	Address          string            `koanf:"address"`
	Method           string            `koanf:"method"`
	ExpectStatus     []int             `koanf:"expect_status"`
	Headers          map[string]string `koanf:"headers"`
	TLSSkipVerify    bool              `koanf:"tls_skip_verify"`
	Interval         time.Duration     `koanf:"interval"`
	Timeout          time.Duration     `koanf:"timeout"`
	DegradedLatency  time.Duration     `koanf:"degraded_latency"`
	FailureThreshold int               `koanf:"failure_threshold"`
}

type Alerts struct {
	Enabled bool        `koanf:"enabled"`
	Rules   []AlertRule `koanf:"rules"`
}

type AlertRule struct {
	ID         string        `koanf:"id"`
	Name       string        `koanf:"name"`
	Metric     string        `koanf:"metric"`
	Service    string        `koanf:"service"`
	Condition  string        `koanf:"condition"`
	Operator   string        `koanf:"operator"`
	Threshold  float64       `koanf:"threshold"`
	For        time.Duration `koanf:"for"`
	ClearFor   time.Duration `koanf:"clear_for"`
	Hysteresis float64       `koanf:"hysteresis"`
	Severity   string        `koanf:"severity"`
	Enabled    *bool         `koanf:"enabled"`
}

type Logging struct {
	Level     string `koanf:"level"`
	Format    string `koanf:"format"`
	File      string `koanf:"file"`
	MaxSizeMB int    `koanf:"max_size_mb"`
}

func Default() Config {
	c := func(interval, timeout time.Duration) Collector {
		return Collector{Enabled: true, Interval: interval, Timeout: timeout}
	}
	return Config{
		General: General{LiveWindow: 5 * time.Minute},
		UI:      UI{Charts: "braille", FPS: 20, StartScreen: "overview"},
		Collectors: Collectors{
			CPU:    c(time.Second, 2*time.Second),
			Memory: c(time.Second, 2*time.Second),
			Disk: Disk{
				Collector:      c(2*time.Second, 5*time.Second),
				ExcludeFSTypes: []string{"tmpfs", "devtmpfs", "squashfs", "overlay", "proc", "sysfs", "cgroup", "cgroup2", "autofs", "devfs", "nullfs", "ramfs", "fuse.snapfuse"},
			},
			Network: Network{Collector: c(time.Second, 2*time.Second)},
			Process: Process{Collector: c(3*time.Second, 8*time.Second), AllowKill: true},
			Host:    c(30*time.Second, 5*time.Second),
		},
		Scheduler: Scheduler{MaxConcurrency: 6},
		Storage: Storage{
			Enabled:        true,
			Resolution:     10 * time.Second,
			QueueSize:      4096,
			BatchSize:      512,
			FlushInterval:  2 * time.Second,
			EnqueueTimeout: 2 * time.Second,
			Retention: Retention{
				Metrics:       7 * 24 * time.Hour,
				ServiceChecks: 7 * 24 * time.Hour,
				Alerts:        30 * 24 * time.Hour,
				Interval:      time.Hour,
			},
		},
		Services: Services{
			MaxConcurrency: 4,
			Defaults: ServiceDefaults{
				Interval:         30 * time.Second,
				Timeout:          5 * time.Second,
				DegradedLatency:  time.Second,
				FailureThreshold: 2,
			},
		},
		Alerts: Alerts{
			Enabled: true,
			Rules: []AlertRule{
				{ID: "cpu-high", Name: "CPU saturated", Metric: "cpu.usage", Operator: ">", Threshold: 90, For: time.Minute, ClearFor: 30 * time.Second, Hysteresis: 10, Severity: "warning"},
				{ID: "memory-high", Name: "Memory pressure", Metric: "mem.used_pct", Operator: ">", Threshold: 92, For: 2 * time.Minute, ClearFor: 30 * time.Second, Hysteresis: 4, Severity: "critical"},
				{ID: "disk-full", Name: "Volume almost full", Metric: "disk.used_pct", Operator: ">", Threshold: 95, For: time.Minute, Hysteresis: 2, Severity: "warning"},
				{ID: "service-down", Name: "Service down", Metric: "service.up", Operator: "<", Threshold: 1, Severity: "critical"},
				{ID: "service-slow", Name: "Service slow", Metric: "service.latency_ms", Operator: ">", Threshold: 2000, For: 2 * time.Minute, ClearFor: time.Minute, Hysteresis: 500, Severity: "warning"},
			},
		},
		Logging: Logging{Level: "info", Format: "text", MaxSizeMB: 10},
	}
}

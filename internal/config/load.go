package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/gabrielagostinho/pulse/internal/platform"
)

type Options struct {
	Path     string
	DBPath   string
	LogFile  string
	LogLevel string
}

var listKeys = []string{
	"alerts.rules",
	"services.targets",
	"collectors.disk.exclude_fstypes",
	"collectors.disk.exclude_mounts",
	"collectors.network.exclude_interfaces",
}

func Load(opts Options) (Config, error) {
	cfg := Default()

	path := opts.Path
	explicit := path != ""
	if !explicit {
		if env := os.Getenv("PULSE_CONFIG"); env != "" {
			path, explicit = env, true
		} else if p, err := platform.DefaultConfigFile(); err == nil {
			path = p
		}
	}

	if path != "" {
		_, err := os.Stat(path)
		switch {
		case err == nil:
			if err := decodeFile(path, &cfg); err != nil {
				return cfg, err
			}
			cfg.Source = path
		case errors.Is(err, fs.ErrNotExist) && !explicit:
		case errors.Is(err, fs.ErrNotExist):
			return cfg, fmt.Errorf("config file %s does not exist (create one with `pulse config init`)", path)
		default:
			return cfg, fmt.Errorf("config file %s: %w", path, err)
		}
	}

	if opts.DBPath != "" {
		cfg.Storage.Path = opts.DBPath
	}
	if opts.LogFile != "" {
		cfg.Logging.File = opts.LogFile
	}
	if opts.LogLevel != "" {
		cfg.Logging.Level = opts.LogLevel
	}

	if err := cfg.resolvePaths(); err != nil {
		return cfg, err
	}
	cfg.applyServiceDefaults()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func decodeFile(path string, cfg *Config) error {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return fmt.Errorf("config file %s is not valid YAML: %w", path, err)
	}
	for _, key := range listKeys {
		if k.Exists(key) {
			clearList(cfg, key)
		}
	}
	err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				durationHook,
				mapstructure.StringToSliceHookFunc(","),
			),
			ErrorUnused:      true,
			WeaklyTypedInput: true,
			Result:           cfg,
			TagName:          "koanf",
		},
	})
	if err != nil {
		return fmt.Errorf("config file %s: %s", path, humanizeDecodeError(err))
	}
	return nil
}

func clearList(cfg *Config, key string) {
	switch key {
	case "alerts.rules":
		cfg.Alerts.Rules = nil
	case "services.targets":
		cfg.Services.Targets = nil
	case "collectors.disk.exclude_fstypes":
		cfg.Collectors.Disk.ExcludeFSTypes = nil
	case "collectors.disk.exclude_mounts":
		cfg.Collectors.Disk.ExcludeMounts = nil
	case "collectors.network.exclude_interfaces":
		cfg.Collectors.Network.ExcludeInterfaces = nil
	}
}

var unusedKeyRe = regexp.MustCompile(`has invalid keys: (.+)`)

func humanizeDecodeError(err error) string {
	msg := err.Error()
	if m := unusedKeyRe.FindStringSubmatch(msg); m != nil {
		return "unknown setting(s) " + m[1] + " (see configs/config.example.yaml)"
	}
	return msg
}

func durationHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeOf(time.Duration(0)) {
		return data, nil
	}
	switch v := data.(type) {
	case string:
		return ParseDuration(v)
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	case float64:
		return time.Duration(v * float64(time.Second)), nil
	}
	return data, nil
}

func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var days time.Duration
	if i := strings.Index(s, "d"); i > 0 {
		n, err := strconv.ParseFloat(s[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		days = time.Duration(n * 24 * float64(time.Hour))
		s = s[i+1:]
		if s == "" {
			return days, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (examples: 500ms, 30s, 5m, 7d)", s)
	}
	return days + d, nil
}

func (c *Config) resolvePaths() error {
	dataDir, err := platform.DataDir()
	if err != nil {
		return err
	}
	c.DataDir = dataDir
	if c.Storage.Path == "" {
		c.Storage.Path = filepath.Join(dataDir, "pulse.db")
	} else {
		c.Storage.Path = expand(c.Storage.Path, dataDir)
	}
	if c.Logging.File == "" {
		c.Logging.File = filepath.Join(dataDir, "logs", "pulse.log")
	} else if c.Logging.File != "-" {
		c.Logging.File = expand(c.Logging.File, dataDir)
	}
	return nil
}

func expand(p, base string) string {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	if p == ":memory:" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func (c *Config) applyServiceDefaults() {
	d := c.Services.Defaults
	for i := range c.Services.Targets {
		t := &c.Services.Targets[i]
		t.Type = strings.ToLower(strings.TrimSpace(t.Type))
		if t.Type == "" {
			if t.Address != "" && t.URL == "" {
				t.Type = "tcp"
			} else {
				t.Type = "http"
			}
		}
		if t.Name == "" {
			t.Name = t.ID
		}
		if t.Method == "" {
			t.Method = "GET"
		}
		t.Method = strings.ToUpper(t.Method)
		if t.Interval == 0 {
			t.Interval = d.Interval
		}
		if t.Timeout == 0 {
			t.Timeout = d.Timeout
		}
		if t.DegradedLatency == 0 {
			t.DegradedLatency = d.DegradedLatency
		}
		if t.FailureThreshold == 0 {
			t.FailureThreshold = d.FailureThreshold
		}
	}
}

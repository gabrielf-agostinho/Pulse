package bootstrap

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabrielagostinho/pulse/internal/config"
)

func openLog(c config.Logging) (*slog.Logger, io.Closer, error) {
	var lvl slog.Level
	switch strings.ToLower(c.Level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	if c.File == "-" {
		return slog.New(slog.DiscardHandler), io.NopCloser(nil), nil
	}
	if err := os.MkdirAll(filepath.Dir(c.File), 0o750); err != nil {
		return nil, nil, fmt.Errorf("log directory: %w", err)
	}
	rotate(c.File, int64(c.MaxSizeMB)<<20)
	f, err := os.OpenFile(c.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(f, opts)
	if c.Format == "json" {
		h = slog.NewJSONHandler(f, opts)
	}
	return slog.New(h), f, nil
}

func rotate(path string, limit int64) {
	if limit <= 0 {
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < limit {
		return
	}
	_ = os.Remove(path + ".1")
	_ = os.Rename(path, path+".1")
}

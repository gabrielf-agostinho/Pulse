package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gabrielagostinho/pulse/configs"
	"github.com/gabrielagostinho/pulse/internal/bootstrap"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/platform"
	"github.com/gabrielagostinho/pulse/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `pulse - system monitor for the terminal

Usage:
  pulse [flags]                 start the dashboard
  pulse config init [path]      write the annotated example configuration
  pulse config validate         validate the configuration and exit
  pulse config path             print where configuration and data live
  pulse version                 print version information

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Fprintf(stdout, "pulse %s (%s, built %s) %s/%s\n", version, commit, date, runtime.GOOS, runtime.GOARCH)
			return 0
		case "config":
			return configCmd(args[1:], stdout, stderr)
		case "help", "-h", "--help":
			fmt.Fprint(stdout, usage)
			flags(io.Discard).PrintDefaults()
			return 0
		}
	}
	fs := flags(stderr)
	opts := bindOptions(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*opts)
	if err != nil {
		fmt.Fprintln(stderr, "pulse:", err)
		return 2
	}
	return dashboard(cfg, stderr)
}

func flags(out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("pulse", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		fmt.Fprint(out, usage)
		fs.PrintDefaults()
	}
	return fs
}

func bindOptions(fs *flag.FlagSet) *config.Options {
	o := &config.Options{}
	fs.StringVar(&o.Path, "config", "", "configuration file (default: per-user config dir, or $PULSE_CONFIG)")
	fs.StringVar(&o.DBPath, "db", "", "history database path (overrides storage.path)")
	fs.StringVar(&o.LogFile, "log-file", "", `log file path, or "-" to disable (overrides logging.file)`)
	fs.StringVar(&o.LogLevel, "log-level", "", "debug | info | warn | error (overrides logging.level)")
	return o
}

func dashboard(cfg config.Config, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "pulse:", err)
		return 1
	}
	app.Start(ctx)

	p := tea.NewProgram(tui.New(app), tea.WithAltScreen())
	go func() {
		<-ctx.Done()
		p.Quit()
	}()
	_, runErr := p.Run()

	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutErr := app.Shutdown(sctx)

	switch {
	case runErr != nil && !errors.Is(runErr, tea.ErrProgramKilled):
		fmt.Fprintln(stderr, "pulse: terminal error:", runErr)
		return 1
	case shutErr != nil:
		fmt.Fprintln(stderr, "pulse: shutdown:", shutErr)
		return 1
	}
	return 0
}

func configCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: pulse config init [path] | validate [--config file] | path")
		return 2
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("config init", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := fs.Bool("force", false, "overwrite an existing file")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		path := fs.Arg(0)
		if path == "" {
			p, err := platform.DefaultConfigFile()
			if err != nil {
				fmt.Fprintln(stderr, "pulse:", err)
				return 1
			}
			path = p
		}
		if _, err := os.Stat(path); err == nil && !*force {
			fmt.Fprintf(stderr, "pulse: %s already exists (use --force to overwrite)\n", path)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			fmt.Fprintln(stderr, "pulse:", err)
			return 1
		}
		if err := os.WriteFile(path, configs.Example, 0o600); err != nil {
			fmt.Fprintln(stderr, "pulse:", err)
			return 1
		}
		fmt.Fprintln(stdout, "wrote", path)
		return 0
	case "validate":
		fs := flags(stderr)
		opts := bindOptions(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		cfg, err := config.Load(*opts)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		src := cfg.Source
		if src == "" {
			src = "built-in defaults (no config file found)"
		}
		fmt.Fprintf(stdout, "✓ configuration is valid: %s\n  %d services · %d alert rules · history %s\n",
			src, len(cfg.Services.Targets), len(cfg.Alerts.Rules), cfg.Storage.Path)
		return 0
	case "path":
		cfgFile, _ := platform.DefaultConfigFile()
		data, _ := platform.DataDir()
		fmt.Fprintf(stdout, "config  %s\ndata    %s\n", cfgFile, data)
		return 0
	}
	fmt.Fprintf(stderr, "pulse: unknown config command %q\n", args[0])
	return 2
}

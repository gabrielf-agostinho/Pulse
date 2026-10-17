package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/gabrielagostinho/pulse/internal/bootstrap"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/tui"
)

func snapshotCmd(args []string, stdout, stderr io.Writer) int {
	fs := flags(stderr)
	opts := bindOptions(fs)
	size := fs.String("size", "160x46", "terminal size COLSxROWS")
	wait := fs.Duration("wait", 12*time.Second, "how long to collect before rendering")
	screens := fs.String("screens", "overview,processes,services,alerts,history,settings", "comma-separated screens")
	keyspec := fs.String("keys", "", `per-screen key presses, e.g. "processes=enter;alerts=tab tab"`)
	out := fs.String("out", "snapshots", "output directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var cols, rows int
	if _, err := fmt.Sscanf(*size, "%dx%d", &cols, &rows); err != nil || cols < 44 || rows < 12 {
		fmt.Fprintln(stderr, "pulse: --size must look like 160x46 (minimum 44x12)")
		return 2
	}
	cfg, err := config.Load(*opts)
	if err != nil {
		fmt.Fprintln(stderr, "pulse:", err)
		return 2
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "pulse:", err)
		return 1
	}
	app.Start(ctx)
	fmt.Fprintf(stdout, "collecting live data for %s…\n", *wait)
	time.Sleep(*wait)

	keys := map[string]string{}
	for _, part := range strings.Split(*keyspec, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			keys[strings.TrimSpace(k)] = v
		}
	}
	if err := os.MkdirAll(*out, 0o750); err != nil {
		fmt.Fprintln(stderr, "pulse:", err)
		return 1
	}
	snap := tui.NewSnapshot(app, cols, rows)
	code := 0
	for _, name := range strings.Split(*screens, ",") {
		name = strings.TrimSpace(name)
		idx := tui.ScreenIndex(name)
		if idx < 0 {
			fmt.Fprintf(stderr, "pulse: unknown screen %q\n", name)
			code = 2
			continue
		}
		frame := snap.Render(idx, tui.ParseKeys(keys[name]))
		base := filepath.Join(*out, strings.ToLower(name))
		title := fmt.Sprintf("pulse %s %d×%d", strings.ToLower(name), cols, rows)
		if err := os.WriteFile(base+".ans", []byte(frame), 0o640); err != nil {
			fmt.Fprintln(stderr, "pulse:", err)
			code = 1
		}
		if err := os.WriteFile(base+".html", []byte(tui.ExportHTML(frame, title, cols, rows)), 0o640); err != nil {
			fmt.Fprintln(stderr, "pulse:", err)
			code = 1
		}
		fmt.Fprintln(stdout, "wrote", base+".{ans,html}")
	}
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	if err := app.Shutdown(sctx); err != nil {
		fmt.Fprintln(stderr, "pulse: shutdown:", err)
		code = 1
	}
	return code
}

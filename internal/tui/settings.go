package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/gabrielagostinho/pulse/internal/platform"
	"github.com/gabrielagostinho/pulse/internal/scheduler"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type settingsView struct {
	vp viewport.Model
}

func newSettings() *settingsView {
	vp := viewport.New(0, 0)
	return &settingsView{vp: vp}
}

func (s *settingsView) bindings() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, keys.PageDown, keys.Top}
}
func (s *settingsView) capturing() bool   { return false }
func (s *settingsView) modal(vctx) string { return "" }

func (s *settingsView) update(m *Model, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(k, keys.Top):
		s.vp.GotoTop()
	case key.Matches(k, keys.Bottom):
		s.vp.GotoBottom()
	default:
		var cmd tea.Cmd
		s.vp, cmd = s.vp.Update(k)
		return cmd
	}
	return nil
}

func section(title string, w int) []string {
	rule := strings.Repeat("─", max(w-c.Width(title)-3, 0))
	return []string{"", theme.B(theme.Accent, title) + " " + theme.C(theme.Faint, rule)}
}

func (s *settingsView) view(v vctx) string {
	p := c.Panel{Title: "Settings & diagnostics", Marker: theme.Accent, Width: v.w, Height: v.h, Focused: true}
	iw, ih := c.InnerWidth(v.w), c.InnerHeight(v.h)
	cfg := v.cfg
	st := v.st
	lw := 20
	val := func(s string) string { return theme.C(theme.Text, s) }
	dim := func(s string) string { return theme.C(theme.Muted, s) }
	path := func(s string) string { return theme.C(theme.Subtle, s) }
	var lines []string

	lines = append(lines, section("Configuration", iw)...)
	src := cfg.Source
	if src == "" {
		def, _ := platform.DefaultConfigFile()
		src = dim("built-in defaults") + theme.C(theme.Faint, " (create "+def+" with `pulse config init`)")
	} else {
		src = path(src)
	}
	lines = append(lines,
		kv("config file", lw, src),
		kv("data directory", lw, path(cfg.DataDir)),
		kv("log file", lw, path(cfg.Logging.File)+dim("  level "+cfg.Logging.Level)),
		kv("platform", lw, val(platform.OSName+" · "+st.Host.Platform+" "+st.Host.PlatformVersion)+dim("  kernel "+st.Host.KernelVersion+" · "+st.Host.Arch)),
		kv("live window", lw, val(cfg.General.LiveWindow.String())+dim("  charts "+cfg.UI.Charts+" · max "+itoa(cfg.UI.FPS)+" fps")),
		kv("uptime", lw, val(c.Duration(st.Host.Uptime(v.now)))+dim("  pulse running "+c.Duration(v.now.Sub(st.Started)))),
	)

	lines = append(lines, section("Collectors", iw)...)
	lines = append(lines, jobTable(st.Jobs, v.now, iw)...)

	lines = append(lines, section("Storage", iw)...)
	sv := st.Storage
	switch {
	case !sv.Enabled:
		lines = append(lines, kv("status", lw, dim("disabled (storage.enabled: false)")))
	case sv.InitErr != "":
		lines = append(lines, kv("status", lw, theme.C(theme.Crit, "✗ unavailable")))
		for _, l := range wrap(sv.InitErr, iw-lw) {
			lines = append(lines, strings.Repeat(" ", lw)+theme.C(theme.Crit, l))
		}
	default:
		w := sv.Writer
		status := theme.C(theme.OK, "✓ healthy")
		if sv.Degraded() {
			status = theme.C(theme.Warn, "▲ degraded")
		}
		lines = append(lines,
			kv("status", lw, status+dim("  schema v"+itoa(sv.DB.SchemaVersion))),
			kv("database", lw, path(cfg.Storage.Path)),
			kv("size", lw, val(c.Bytes(uint64(sv.DB.SizeBytes)))+dim("  + wal "+c.Bytes(uint64(sv.DB.WALBytes)))),
			kv("rows", lw, val(c.Count(sv.DB.MetricRows))+dim(" samples · ")+val(c.Count(sv.DB.CheckRows))+dim(" checks · ")+
				val(c.Count(sv.DB.IncidentRows))+dim(" incidents · ")+val(c.Count(sv.DB.Series))+dim(" series")),
			kv("write queue", lw, queueBar(w.QueueDepth, w.QueueCap, 24)+dim(fmt.Sprintf("  %d / %d", w.QueueDepth, w.QueueCap))),
			kv("written", lw, val(c.Count(int64(w.Written)))+dim(fmt.Sprintf(" records in %d batches · last flush %s (%s)",
				w.Batches, c.Ago(w.LastFlush, v.now), c.Elapsed(w.LastFlushDur)))),
			kv("rejected / lost", lw, countColor(w.Rejected)+dim(" rejected (queue full) · ")+countColor(w.Failed)+dim(" lost after retries")),
			kv("resolution", lw, val(cfg.Storage.Resolution.String())+dim(fmt.Sprintf("  batch %d · flush %s", cfg.Storage.BatchSize, cfg.Storage.FlushInterval))),
			kv("retention", lw, val("metrics "+c.Span(cfg.Storage.Retention.Metrics))+dim(" · ")+val("checks "+c.Span(cfg.Storage.Retention.ServiceChecks))+
				dim(" · ")+val("alerts "+c.Span(cfg.Storage.Retention.Alerts))),
		)
		if !sv.LastPrune.IsZero() {
			lines = append(lines, kv("last prune", lw, val(c.Ago(sv.LastPrune, v.now))+
				dim(fmt.Sprintf("  removed %d samples · %d checks · %d incidents", sv.Pruned.Metrics, sv.Pruned.Checks, sv.Pruned.Incidents))))
		}
		for _, e := range []string{w.LastErr, sv.DBErr, sv.PruneErr} {
			if e != "" {
				lines = append(lines, kv("last error", lw, theme.C(theme.Warn, e)))
			}
		}
	}

	lines = append(lines, section("Service Watch", iw)...)
	lines = append(lines, kv("targets", lw, val(itoa(len(cfg.Services.Targets)))+dim(fmt.Sprintf("  max %d concurrent checks", cfg.Services.MaxConcurrency))))
	for _, t := range cfg.Services.Targets {
		target := t.URL
		if t.Type == "tcp" {
			target = t.Address
		}
		lines = append(lines, "  "+c.Fit(theme.C(theme.Text, t.ID), lw-2)+theme.C(theme.Muted, c.Fit(strings.ToUpper(t.Type), 5))+
			path(target)+dim(fmt.Sprintf("  every %s · timeout %s · degraded > %s", t.Interval, t.Timeout, t.DegradedLatency)))
	}

	lines = append(lines, section("Alert rules", iw)...)
	if !cfg.Alerts.Enabled {
		lines = append(lines, dim("  alerting disabled"))
	}
	for _, r := range st.Alerts.Rules {
		extra := ""
		if r.Rule.Hysteresis > 0 {
			extra += fmt.Sprintf(" · hysteresis %g", r.Rule.Hysteresis)
		}
		if r.Rule.ClearFor > 0 {
			extra += " · clear after " + r.Rule.ClearFor.String()
		}
		lines = append(lines, "  "+c.Fit(theme.C(severityLevel(r.Rule.Severity).Color(), "■ ")+theme.C(theme.Text, r.Rule.ID), lw-2)+
			path(r.Rule.Describe())+dim(extra))
	}

	lines = append(lines, section("Terminal", iw)...)
	profile := map[termenv.Profile]string{termenv.TrueColor: "true colour", termenv.ANSI256: "256 colours", termenv.ANSI: "16 colours", termenv.Ascii: "no colour"}[lipgloss.ColorProfile()]
	term := os.Getenv("TERM_PROGRAM")
	if term == "" {
		term = os.Getenv("TERM")
	}
	if os.Getenv("WT_SESSION") != "" {
		term = "Windows Terminal"
	}
	lines = append(lines,
		kv("colour profile", lw, val(profile)+dim("  (NO_COLOR is honoured)")),
		kv("terminal", lw, val(fmt.Sprintf("%d × %d", v.w, v.h+4))+dim("  "+term)),
		kv("palette", lw, theme.C(theme.OK, "■ ")+theme.C(theme.Warn, "■ ")+theme.C(theme.Crit, "■ ")+theme.C(theme.Info, "■  ")+
			theme.C(theme.CPU, "■ ")+theme.C(theme.Mem, "■ ")+theme.C(theme.Rx, "■ ")+theme.C(theme.Tx, "■ ")+theme.C(theme.Read, "■ ")+theme.C(theme.Write, "■ ")+
			dim("  braille ")+theme.C(theme.CPU, "⣀⣤⣶⣿")+dim("  blocks ")+theme.C(theme.CPU, "▁▃▅█")),
	)

	s.vp.Width, s.vp.Height = iw, ih
	s.vp.SetContent(strings.Join(lines[1:], "\n"))
	p.Body = c.Lines(s.vp.View())
	if s.vp.TotalLineCount() > ih {
		p.Badge = theme.C(theme.Muted, fmt.Sprintf("%d%%", int(s.vp.ScrollPercent()*100)))
	}
	return p.Render()
}

func jobTable(jobs []scheduler.Stats, now time.Time, w int) []string {
	if len(jobs) == 0 {
		return []string{theme.C(theme.Muted, "  starting…")}
	}
	rows := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		state := theme.C(theme.OK, "● ok")
		switch {
		case j.Runs == 0:
			state = theme.C(theme.Muted, "○ waiting")
		case j.LastErr != "":
			state = theme.C(theme.Crit, "● failing")
		case j.Overruns > 0 && float64(j.Overruns)/float64(j.Runs) > 0.2:
			state = theme.C(theme.Warn, "● slow")
		}
		errText := theme.C(theme.Faint, "—")
		if j.LastErr != "" {
			errText = theme.C(theme.Crit, strings.SplitN(j.LastErr, "\n", 2)[0])
		}
		rows = append(rows, []string{
			state, theme.C(theme.Text, j.Name), theme.C(theme.Muted, j.Interval.String()),
			theme.C(theme.Subtle, c.Count(int64(j.Runs))), countColor(j.Failures), countColor(j.Timeouts),
			theme.C(theme.Subtle, c.Elapsed(j.LastDuration)), errText,
		})
	}
	t := c.Table{Columns: []c.Column{
		{Title: "STATE", Width: 9}, {Title: "JOB", Width: 22}, {Title: "EVERY", Width: 6, Right: true},
		{Title: "RUNS", Width: 7, Right: true}, {Title: "FAILS", Width: 5, Right: true}, {Title: "T/O", Width: 4, Right: true, HideBelow: 90},
		{Title: "LAST RUN", Width: 9, Right: true}, {Title: "LAST ERROR", Flex: true},
	}, Rows: rows, Selected: -1, Width: w, Height: len(rows) + 1, SortCol: -1}
	return t.Render()
}

func countColor(n uint64) string {
	if n == 0 {
		return theme.C(theme.Faint, "0")
	}
	return theme.B(theme.Warn, c.Count(int64(n)))
}

func queueBar(depth, capacity, w int) string {
	if capacity == 0 {
		return theme.C(theme.Faint, "—")
	}
	return c.Gauge(float64(depth)/float64(capacity)*100, w)
}

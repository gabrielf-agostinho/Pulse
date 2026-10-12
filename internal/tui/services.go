package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/domain/service"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type servicesView struct {
	sel     int
	offset  int
	focusR  bool
	expand  bool
	visible int
}

func (s *servicesView) bindings() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, keys.Enter}
}
func (s *servicesView) capturing() bool   { return false }
func (s *servicesView) modal(vctx) string { return "" }

func (s *servicesView) update(m *Model, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	n := len(m.st.Services)
	switch {
	case key.Matches(k, keys.Up):
		s.sel = max(s.sel-1, 0)
	case key.Matches(k, keys.Down):
		s.sel = min(s.sel+1, max(n-1, 0))
	case key.Matches(k, keys.Top):
		s.sel = 0
	case key.Matches(k, keys.Bottom):
		s.sel = max(n-1, 0)
	case key.Matches(k, keys.Focus):
		s.focusR = !s.focusR
	case key.Matches(k, keys.Enter):
		s.expand = !s.expand
		s.focusR = s.expand
	case key.Matches(k, keys.Back):
		s.expand, s.focusR = false, false
	}
	return nil
}

func (s *servicesView) view(v vctx) string {
	svcs := v.st.Services
	if len(svcs) == 0 {
		p := c.Panel{Title: "Services", Marker: theme.Latency, Width: v.w, Height: v.h, Focused: true}
		iw, ih := c.InnerWidth(v.w), c.InnerHeight(v.h)
		body := c.Empty(iw, max(ih-8, 3), "◇", "No services are being watched", "Add HTTP or TCP targets to your configuration:")
		snippet := []string{
			theme.C(theme.Muted, "services:"),
			theme.C(theme.Muted, "  targets:"),
			theme.C(theme.Muted, "    - id: ") + theme.C(theme.Text, "api"),
			theme.C(theme.Muted, "      url: ") + theme.C(theme.Text, "https://example.com/healthz"),
			theme.C(theme.Muted, "    - id: ") + theme.C(theme.Text, "db"),
			theme.C(theme.Muted, "      address: ") + theme.C(theme.Text, "localhost:5432"),
		}
		for _, l := range snippet {
			body = append(body, c.Center(c.Fit(l, 40), iw))
		}
		p.Body = body
		return p.Render()
	}
	s.sel = min(s.sel, len(svcs)-1)
	wide := v.w >= 110
	if !wide && s.expand {
		return s.detail(v, svcs[s.sel], v.w, v.h, true)
	}
	listW := v.w
	if wide {
		listW = v.w * 55 / 100
	}
	list := s.list(v, listW, v.h, !s.focusR || !wide)
	if !wide {
		return list
	}
	return c.HJoin(1, []string{list, s.detail(v, svcs[s.sel], v.w-listW-1, v.h, s.focusR)}, []int{listW, v.w - listW - 1})
}

func (s *servicesView) list(v vctx, w, h int, focused bool) string {
	svcs := v.st.Services
	healthy, degraded, down := 0, 0, 0
	for _, sv := range svcs {
		switch sv.Status {
		case service.StatusHealthy:
			healthy++
		case service.StatusDegraded:
			degraded++
		case service.StatusDown:
			down++
		}
	}
	p := c.Panel{Title: "Services", Marker: theme.Latency, Width: w, Height: h, Focused: focused}
	badge := theme.C(theme.OK, fmt.Sprintf("● %d", healthy))
	if degraded > 0 {
		badge += "  " + theme.C(theme.Warn, fmt.Sprintf("● %d", degraded))
	}
	if down > 0 {
		badge += "  " + theme.C(theme.Crit, fmt.Sprintf("● %d", down))
	}
	p.Badge = badge
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	stripW := min(max(iw/4, 8), 30)
	rows := make([][]string, len(svcs))
	for i, sv := range svcs {
		lat := theme.C(theme.Faint, "—")
		if sv.LastSuccess.After(sv.LastFailure) || (!sv.LastSuccess.IsZero() && sv.Status != service.StatusDown) {
			lat = theme.C(latencyColor(sv), c.Millis(sv.LastLatency))
		}
		up := theme.C(theme.Faint, "—")
		if pct, ok := sv.Uptime(); ok {
			up = theme.C(colorOr(pct >= 99, theme.Subtle, theme.Warn), fmt.Sprintf("%.1f%%", pct))
			if pct >= 99.95 {
				up = theme.C(theme.Subtle, "100%")
			}
		}
		rows[i] = []string{
			c.Dot(serviceLevel(sv.Status)) + " " + theme.C(theme.Text, sv.Def.Name),
			theme.C(theme.Muted, strings.ToUpper(string(sv.Def.Kind))),
			theme.C(theme.Muted, statusWord(sv.Status)),
			lat, up, healthStrip(sv.History, stripW),
		}
	}
	tableH := ih
	s.visible = tableH - 1
	s.offset = c.Clamp(s.sel, s.offset, s.visible, len(rows))
	t := c.Table{
		Columns: []c.Column{
			{Title: "SERVICE", Flex: true},
			{Title: "TYPE", Width: 4, HideBelow: 70},
			{Title: "STATE", Width: 8, HideBelow: 60},
			{Title: "LATENCY", Width: 8, Right: true},
			{Title: "UPTIME", Width: 6, Right: true, HideBelow: 80},
			{Title: "RECENT CHECKS", Width: stripW},
		},
		Rows: rows, Selected: s.sel, Offset: s.offset, Width: iw, Height: tableH, Focused: focused, SortCol: -1,
	}
	p.Body = t.Render()
	return p.Render()
}

func statusWord(s service.Status) string {
	if s == service.StatusUnknown {
		return "pending"
	}
	return s.String()
}

func latencyColor(sv service.State) lipgloss.Color {
	if sv.Def.DegradedLatency > 0 && sv.LastLatency > sv.Def.DegradedLatency {
		return theme.Warn
	}
	return theme.Subtle
}

func (s *servicesView) detail(v vctx, sv service.State, w, h int, focused bool) string {
	p := c.Panel{Title: sv.Def.Name, Marker: serviceLevel(sv.Status).Color(), Width: w, Height: h, Focused: focused}
	p.Badge = c.Pill(strings.ToUpper(statusWord(sv.Status)), colorOr(sv.Status == service.StatusUnknown, theme.BorderHi, serviceLevel(sv.Status).Color()))
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	lw := 13
	val := func(s string) string { return theme.C(theme.Text, s) }
	body := []string{
		theme.C(theme.Subtle, sv.Def.Target),
		theme.C(theme.Muted, fmt.Sprintf("%s · every %s · timeout %s", strings.ToUpper(string(sv.Def.Kind)), sv.Def.Interval, sv.Def.Timeout)),
		"",
		kv("last check", lw, val(c.Ago(sv.LastCheck, v.now))),
		kv("last success", lw, colorText(sv.LastSuccess.IsZero(), c.Ago(sv.LastSuccess, v.now))),
	}
	if !sv.LastFailure.IsZero() {
		body = append(body, kv("last failure", lw, theme.C(theme.Crit, c.Ago(sv.LastFailure, v.now))))
		for i, l := range wrap(sv.LastError, iw-lw) {
			if i == 2 {
				break
			}
			if i == 0 {
				body = append(body, kv("error", lw, theme.C(theme.Crit, l)))
			} else {
				body = append(body, strings.Repeat(" ", lw)+theme.C(theme.Crit, l))
			}
		}
	}
	if sv.ConsecutiveFailures > 0 {
		body = append(body, kv("failing", lw, theme.C(theme.Warn, fmt.Sprintf("%d consecutive checks (down after %d)", sv.ConsecutiveFailures, sv.Def.FailureThreshold))))
	}
	if sv.LastStatusCode > 0 {
		body = append(body, kv("http status", lw, val(itoa(sv.LastStatusCode))))
	}

	lat := latencies(sv.History)
	peak := maxF(lat)
	minL, avgL, n := math.Inf(1), 0.0, 0
	for _, l := range lat {
		if !math.IsNaN(l) {
			minL = math.Min(minL, l)
			avgL += l
			n++
		}
	}
	chartH := ih - len(body) - 6
	if chartH >= 3 {
		body = append(body, "", theme.C(theme.Muted, "latency"))
		if n == 0 {
			body = append(body, c.Empty(iw, chartH+1, "∅", "no successful checks yet", "")...)
		} else {
			avgL /= float64(n)
			top := c.NiceCeil(peak)
			label := fmt.Sprintf("%.0fms", top)
			cw := c.ChartWidth(iw, label)
			chart := c.Area(widen(lat, cw, v.mode), cw, chartH, top, c.Gradient{Lo: theme.LatDeep, Hi: theme.Latency}, v.mode)
			body = append(body, c.Axis(chart, label, "", "0", fmt.Sprintf("last %d checks", len(lat)), "", "now", iw)...)
			body = append(body, theme.C(theme.Muted, "min ")+val(fmt.Sprintf("%.0fms", minL))+
				theme.C(theme.Muted, "   avg ")+val(fmt.Sprintf("%.0fms", avgL))+
				theme.C(theme.Muted, "   max ")+val(fmt.Sprintf("%.0fms", peak)))
		}
	}
	for len(body) < ih-2 {
		body = append(body, "")
	}
	uptime := ""
	if pct, ok := sv.Uptime(); ok {
		uptime = theme.C(theme.Muted, fmt.Sprintf("%.1f%% of %d checks ok", pct, len(sv.History)))
	}
	body = append(body, c.Spread(theme.C(theme.Muted, "health"), uptime, iw), healthStrip(sv.History, iw))
	p.Body = body
	return p.Render()
}

func widen(vals []float64, cells int, mode c.Mode) []float64 {
	slots := cells * 2
	if mode == c.Blocks {
		slots = cells
	}
	if len(vals) == 0 {
		return vals
	}
	k := min(max(slots/len(vals), 1), 6)
	if k == 1 {
		return vals
	}
	out := make([]float64, 0, len(vals)*k)
	for _, v := range vals {
		for i := 0; i < k; i++ {
			out = append(out, v)
		}
	}
	return out
}

func colorText(missing bool, s string) string {
	if missing {
		return theme.C(theme.Faint, s)
	}
	return theme.C(theme.OK, s)
}

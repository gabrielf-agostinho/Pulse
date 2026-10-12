package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

var ranges = []struct {
	label string
	d     time.Duration
}{
	{"15m", 15 * time.Minute}, {"1h", time.Hour}, {"6h", 6 * time.Hour}, {"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour},
}

type (
	seriesMsg struct {
		list []history.SeriesInfo
		err  error
	}
	rangeReqMsg struct{ seq int }
	rangeMsg    struct {
		seq     int
		key     string
		from    time.Time
		to      time.Time
		buckets []history.Bucket
		err     error
		took    time.Duration
	}
)

type historyView struct {
	series    []history.SeriesInfo
	seriesErr string
	loaded    bool

	sel     int
	offset  int
	visible int
	rng     int
	focusR  bool
	want    string

	seq     int
	loading bool
	result  *rangeMsg
}

func newHistory() *historyView { return &historyView{rng: 0} }

func (h *historyView) bindings() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, kRangeNext, keys.Refresh}
}
func (h *historyView) capturing() bool   { return false }
func (h *historyView) modal(vctx) string { return "" }

func (h *historyView) init(m *Model) tea.Cmd { return h.listSeries(m) }

func (h *historyView) activated(m *Model) tea.Cmd { return h.listSeries(m) }

func (h *historyView) preselect(key string) { h.want = key }

func (h *historyView) listSeries(m *Model) tea.Cmd {
	if !m.be.HistoryAvailable() {
		return nil
	}
	be := m.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := be.ListSeries(ctx)
		return seriesMsg{list: list, err: err}
	}
}

func (h *historyView) load(m *Model) tea.Cmd {
	h.seq++
	seq := h.seq
	h.loading = true
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return rangeReqMsg{seq: seq} })
}

func (h *historyView) query(m *Model) tea.Cmd {
	if h.sel >= len(h.series) {
		h.loading = false
		return nil
	}
	key := h.series[h.sel].Key
	seq := h.seq
	to := time.Now()
	from := to.Add(-ranges[h.rng].d)
	buckets := max(m.width*2, 120)
	be := m.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		start := time.Now()
		b, err := be.QueryRange(ctx, key, from, to, buckets)
		return rangeMsg{seq: seq, key: key, from: from, to: to, buckets: b, err: err, took: time.Since(start)}
	}
}

func (h *historyView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case seriesMsg:
		h.loaded = true
		if msg.err != nil {
			h.seriesErr = msg.err.Error()
			return nil
		}
		h.seriesErr = ""
		prev := ""
		if h.sel < len(h.series) {
			prev = h.series[h.sel].Key
		}
		h.series = msg.list
		target := h.want
		if target == "" {
			target = prev
		}
		h.want = ""
		h.sel = 0
		for i, s := range h.series {
			if s.Key == target {
				h.sel = i
			}
		}
		return h.load(m)
	case rangeReqMsg:
		if msg.seq != h.seq {
			return nil
		}
		return h.query(m)
	case rangeMsg:
		if msg.seq != h.seq {
			return nil
		}
		h.loading = false
		r := msg
		h.result = &r
		return nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Up):
			if h.sel > 0 {
				h.sel--
				return h.load(m)
			}
		case key.Matches(msg, keys.Down):
			if h.sel < len(h.series)-1 {
				h.sel++
				return h.load(m)
			}
		case key.Matches(msg, keys.Top):
			h.sel = 0
			return h.load(m)
		case key.Matches(msg, keys.Bottom):
			h.sel = max(len(h.series)-1, 0)
			return h.load(m)
		case key.Matches(msg, kRangeNext):
			h.rng = (h.rng + 1) % len(ranges)
			return h.load(m)
		case key.Matches(msg, kRangePrev):
			h.rng = (h.rng + len(ranges) - 1) % len(ranges)
			return h.load(m)
		case key.Matches(msg, keys.Focus):
			h.focusR = !h.focusR
		case key.Matches(msg, keys.Refresh):
			return h.listSeries(m)
		}
	}
	return nil
}

func (h *historyView) view(v vctx) string {
	sv := v.st.Storage
	if !sv.Enabled || sv.InitErr != "" {
		p := c.Panel{Title: "History", Marker: theme.HistHue, Width: v.w, Height: v.h, Focused: true}
		iw, ih := c.InnerWidth(v.w), c.InnerHeight(v.h)
		if !sv.Enabled {
			p.Body = c.Empty(iw, ih, "○", "History is disabled", "set storage.enabled: true to record metrics")
		} else {
			p.Body = c.Empty(iw, ih, theme.C(theme.Crit, "✗"), "The history database could not be opened", "")
			for _, l := range wrap(sv.InitErr, iw-8) {
				p.Body = append(p.Body, c.Center(theme.C(theme.Crit, l), iw))
			}
		}
		return p.Render()
	}
	listW := min(max(v.w*30/100, 28), 44)
	if v.w < 90 {
		listW = 0
	}
	chartW := v.w
	if listW > 0 {
		chartW = v.w - listW - 1
	}
	chart := h.chartPanel(v, chartW, v.h)
	if listW == 0 {
		return chart
	}
	return c.HJoin(1, []string{h.listPanel(v, listW, v.h), chart}, []int{listW, chartW})
}

func (h *historyView) listPanel(v vctx, w, hgt int) string {
	p := c.Panel{Title: "Series", Marker: theme.HistHue, Width: w, Height: hgt, Focused: !h.focusR}
	iw, ih := c.InnerWidth(w), c.InnerHeight(hgt)
	switch {
	case h.seriesErr != "":
		p.Body = []string{errLine(h.seriesErr, iw)}
		return p.Render()
	case !h.loaded:
		p.Body = c.Empty(iw, ih, "◌", v.spin+" loading…", "")
		return p.Render()
	case len(h.series) == 0:
		p.Body = c.Empty(iw, ih, "∅", "Nothing recorded yet", "first points land after "+c.Span(v.cfg.Storage.Resolution))
		return p.Render()
	}
	p.Badge = theme.C(theme.Muted, itoa(len(h.series)))
	rows := make([][]string, len(h.series))
	for i, s := range h.series {
		g := metricColor(s.Key)
		rows[i] = []string{theme.C(g.Hi, "■") + " " + theme.C(theme.Text, metricLabel(s.Key))}
	}
	h.visible = ih - 1
	h.offset = c.Clamp(h.sel, h.offset, h.visible, len(rows))
	t := c.Table{Columns: []c.Column{{Title: "METRIC", Flex: true}}, Rows: rows, Selected: h.sel, Offset: h.offset,
		Width: iw, Height: ih, Focused: !h.focusR, SortCol: -1}
	p.Body = t.Render()
	return p.Render()
}

func (h *historyView) chartPanel(v vctx, w, hgt int) string {
	title := "History"
	key := ""
	if h.sel < len(h.series) {
		key = h.series[h.sel].Key
		title = metricLabel(key)
	}
	g := metricColor(key)
	p := c.Panel{Title: title, Marker: g.Hi, Width: w, Height: hgt, Focused: h.focusR}
	iw, ih := c.InnerWidth(w), c.InnerHeight(hgt)

	var pills []string
	for i, r := range ranges {
		if i == h.rng {
			pills = append(pills, c.Pill(r.label, theme.Accent))
		} else {
			pills = append(pills, " "+theme.C(theme.Muted, r.label)+" ")
		}
	}
	status := ""
	if h.loading {
		status = theme.C(theme.Muted, v.spin+" querying")
	} else if h.result != nil && h.result.err == nil {
		status = theme.C(theme.Faint, fmt.Sprintf("%d buckets · %s", len(h.result.buckets), h.result.took.Round(time.Millisecond)))
	}
	body := []string{c.Spread(strings.Join(pills, ""), status, iw), ""}

	res := h.result
	switch {
	case key == "":
		body = append(body, c.Empty(iw, ih-2, "∅", "Select a series", "")...)
	case res == nil || res.key != key:
		body = append(body, c.Empty(iw, ih-2, "◌", v.spin+" loading…", "")...)
	case res.err != nil:
		body = append(body, errLine("query failed: "+res.err.Error(), iw))
	case len(res.buckets) == 0:
		body = append(body, c.Empty(iw, ih-2, "∅", "No samples in the last "+ranges[h.rng].label,
			"Pulse persists one point every "+c.Span(v.cfg.Storage.Resolution))...)
	default:
		body = append(body, h.chart(v, res, key, iw, ih-2, g)...)
	}
	p.Body = body
	return p.Render()
}

func (h *historyView) chart(v vctx, res *rangeMsg, key string, iw, ih int, g c.Gradient) []string {
	span := res.to.Sub(res.from)
	width := c.ChartWidth(iw, "100.0M")
	slots := width * 2
	if v.mode == c.Blocks {
		slots = width
	}
	vals := make([]float64, slots)
	for i := range vals {
		vals[i] = math.NaN()
	}
	minV, maxV, sum, cnt := math.Inf(1), math.Inf(-1), 0.0, int64(0)
	for _, b := range res.buckets {
		i := int(float64(b.At.Sub(res.from)) / float64(span) * float64(slots))
		if i < 0 || i >= slots {
			continue
		}
		if math.IsNaN(vals[i]) {
			vals[i] = b.Avg
		} else {
			vals[i] = (vals[i] + b.Avg) / 2
		}
		minV, maxV = math.Min(minV, b.Min), math.Max(maxV, b.Max)
		sum += b.Avg * float64(b.Count)
		cnt += b.Count
	}
	slotDur := span / time.Duration(slots)
	hold := 1
	if slotDur > 0 {
		hold = max(int(math.Ceil(float64(v.cfg.Storage.Resolution)*1.5/float64(slotDur))), 1)
	}
	lastIdx := -1
	for i := range vals {
		if !math.IsNaN(vals[i]) {
			lastIdx = i
			continue
		}
		if lastIdx >= 0 && i-lastIdx <= hold {
			vals[i] = vals[lastIdx]
		}
	}
	last := res.buckets[len(res.buckets)-1].Avg
	avg := sum / math.Max(float64(cnt), 1)
	top, topLabel := axisMax(key, maxV)
	chartH := max(ih-3, 2)
	lines := c.Area(vals, width, chartH, top, g, v.mode)
	tf := "15:04"
	if span > 36*time.Hour {
		tf = "Mon 15:04"
	}
	mid := res.from.Add(span / 2).Format(tf)
	out := c.Axis(lines, topLabel, "", "0", res.from.Format(tf), mid, "now", iw)
	stat := func(label, value string, col theme.Level) string {
		return theme.C(theme.Muted, label+" ") + theme.B(col.Color(), value)
	}
	out = append(out, "", strings.Join([]string{
		stat("last", formatValue(key, last), theme.LevelInfo),
		stat("avg", formatValue(key, avg), theme.LevelNone),
		stat("min", formatValue(key, minV), theme.LevelOK),
		stat("max", formatValue(key, maxV), theme.LevelWarn),
		theme.C(theme.Muted, "samples ") + theme.C(theme.Subtle, c.Count(cnt)),
	}, theme.C(theme.Faint, "   ")))
	return out
}

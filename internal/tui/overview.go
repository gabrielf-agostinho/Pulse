package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

const (
	ovCPU = iota
	ovMem
	ovNet
	ovDisk
	ovServices
	ovAlerts
	ovProcs
	ovCount
)

type overview struct {
	focus int
}

func (o *overview) bindings() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "open panel"))}
}
func (o *overview) capturing() bool   { return false }
func (o *overview) modal(vctx) string { return "" }

func (o *overview) update(m *Model, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "tab", "right", "l", "down", "j":
		o.focus = (o.focus + 1) % ovCount
	case "shift+tab", "left", "h", "up", "k":
		o.focus = (o.focus + ovCount - 1) % ovCount
	case "enter":
		switch o.focus {
		case ovProcs:
			return m.switchTo(scrProcesses)
		case ovServices:
			return m.switchTo(scrServices)
		case ovAlerts:
			return m.switchTo(scrAlerts)
		default:
			keyFor := map[int]string{ovCPU: metrics.CPUUsage, ovMem: metrics.MemUsedPct, ovNet: metrics.NetRxBps, ovDisk: metrics.DiskReadBps}
			m.screens[scrHistory].(*historyView).preselect(keyFor[o.focus])
			return m.switchTo(scrHistory)
		}
	}
	return nil
}

func (o *overview) view(v vctx) string {
	switch {
	case v.w >= 100 && v.h >= 28:
		return o.full(v)
	case v.w >= 60 && v.h >= 16:
		return o.compact(v)
	}
	return o.tiny(v)
}

func (o *overview) full(v vctx) string {
	hs := c.Split(v.h, 40, 28, 32)
	if hs[2] < 8 {
		hs = c.Split(v.h, 38, 30, 32)
	}
	top := c.Split(v.w-1, 58, 42)
	mid := c.Split(v.w-1, 58, 42)
	rows := []string{
		c.HJoin(1, []string{o.cpu(v, top[0], hs[0]), o.memory(v, top[1], hs[0])}, top),
		c.HJoin(1, []string{o.network(v, mid[0], hs[1]), o.disk(v, mid[1], hs[1])}, mid),
	}
	if v.w >= 110 {
		bot := c.Split(v.w-2, 34, 33, 33)
		rows = append(rows, c.HJoin(1, []string{o.services(v, bot[0], hs[2]), o.alerts(v, bot[1], hs[2]), o.procs(v, bot[2], hs[2])}, bot))
	} else {
		bot := c.Split(v.w-1, 50, 50)
		rows = append(rows, c.HJoin(1, []string{o.services(v, bot[0], hs[2]), o.alerts(v, bot[1], hs[2])}, bot))
	}
	return strings.Join(rows, "\n")
}

func (o *overview) compact(v vctx) string {
	type item struct {
		h      int
		render func(w, h int) string
	}
	items := []item{
		{max(v.h*40/100, 7), func(w, h int) string { return o.cpu(v, w, h) }},
		{4, func(w, h int) string { return o.memory(v, w, h) }},
		{max(v.h*22/100, 5), func(w, h int) string { return o.network(v, w, h) }},
		{min(len(v.st.Disk.Volumes), 3) + 4, func(w, h int) string { return o.disk(v, w, h) }},
		{4, func(w, h int) string { return o.alerts(v, w, h) }},
	}
	var out []string
	left := v.h
	for i, it := range items {
		if left < 4 {
			break
		}
		h := min(it.h, left)
		if i == len(items)-1 || left-h < 4 {
			h = left
		}
		out = append(out, it.render(v.w, h))
		left -= h
	}
	return c.Box(strings.Join(out, "\n"), v.w, v.h)
}

func (o *overview) tiny(v vctx) string {
	st := v.st
	lines := []string{
		kv("cpu", 6, valueOr(st.CPU.OK(), theme.B(theme.Heat(st.CPU.Total), c.Pct(st.CPU.Total)))) + "  " +
			c.Gauge(st.CPU.Total, max(v.w-18, 4)),
		kv("mem", 6, theme.B(theme.Heat(st.Memory.UsedPct), c.Pct(st.Memory.UsedPct))) + "  " +
			c.Gauge(st.Memory.UsedPct, max(v.w-18, 4)),
		kv("net", 6, theme.C(theme.Rx, "↓ "+c.Rate(st.Network.RxBps))+"  "+theme.C(theme.Tx, "↑ "+c.Rate(st.Network.TxBps))),
	}
	crit, warn, _ := alertCounts(st.Alerts.Active)
	lines = append(lines, kv("alerts", 8, fmt.Sprintf("%s %d  %s %d", theme.C(theme.Crit, "◆"), crit, theme.C(theme.Warn, "▲"), warn)))
	return c.Box(strings.Join(lines, "\n"), v.w, v.h)
}

func valueOr(ok bool, s string) string {
	if !ok {
		return theme.C(theme.Muted, "—")
	}
	return s
}

func (o *overview) cpu(v vctx, w, h int) string {
	st := v.st
	p := c.Panel{Title: "CPU", Marker: theme.CPU, Width: w, Height: h, Focused: o.focus == ovCPU}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	switch st.CPU.Status {
	case metrics.StatusPending:
		p.Body = c.Empty(iw, ih, "◌", v.spin+" Sampling CPU…", "first reading needs two samples")
		return p.Render()
	}
	if st.CPU.OK() || len(st.CPU.PerCore) > 0 {
		p.Badge = theme.B(theme.Heat(st.CPU.Total), c.Pct(st.CPU.Total))
	}

	var body []string
	if st.CPU.Status == metrics.StatusError {
		body = append(body, errLine("collection failed: "+st.CPU.Err, iw))
	}

	info := cpuInfo(st)
	cores := coreGrid(st.CPU.PerCore, iw, max((ih-len(body)-2)*2/5, 1))
	chartH := ih - len(body) - len(cores) - 1 - 1
	if len(cores) > 0 {
		chartH--
	}
	if chartH < 2 {
		cores = nil
		chartH = ih - len(body) - 2
	}
	ring := st.Ring(metrics.CPUUsage)
	var vals []float64
	var pts []metrics.Point
	if ring != nil {
		vals, pts = ring.Values(), ring.Points()
	}
	cw := c.ChartWidth(iw, "100%")
	chart := c.Area(vals, cw, max(chartH, 1), 100, c.Gradient{Lo: theme.CPUDeep, Hi: theme.CPU}, v.mode)
	body = append(body, c.Axis(chart, "100%", "50%", "0%", "-"+c.Span(spanOf(pts, v.now)), "", "now", iw)...)
	if len(cores) > 0 {
		body = append(body, "")
		body = append(body, cores...)
	}
	body = append(body, c.Fit(info, iw))
	p.Body = body
	return p.Render()
}

func cpuInfo(st *monitoring.State) string {
	var parts []string
	if h := st.Host; h.CPUModel != "" {
		parts = append(parts, theme.C(theme.Subtle, shortModel(h.CPUModel)))
	}
	if h := st.Host; h.LogicalCores > 0 {
		s := fmt.Sprintf("%d threads", h.LogicalCores)
		if h.PhysicalCores > 0 {
			s = fmt.Sprintf("%d cores · %d threads", h.PhysicalCores, h.LogicalCores)
		}
		parts = append(parts, theme.C(theme.Muted, s))
	}
	if st.CPU.LoadSupported {
		l := st.CPU.Load
		parts = append(parts, theme.C(theme.Muted, "load ")+theme.C(theme.Subtle, fmt.Sprintf("%.2f %.2f %.2f", l[0], l[1], l[2])))
	}
	return strings.Join(parts, theme.C(theme.Faint, " · "))
}

func shortModel(m string) string {
	r := strings.NewReplacer("(R)", "", "(TM)", "", "(tm)", "", " CPU", "", "  ", " ")
	m = r.Replace(m)
	if i := strings.Index(m, " @ "); i > 0 {
		m = m[:i]
	}
	return strings.TrimSpace(m)
}

func coreGrid(per []float64, w, maxRows int) []string {
	n := len(per)
	if n == 0 {
		return nil
	}
	label := len(itoa(n - 1))
	const minBar = 6
	cell := label + 1 + minBar + 1 + 4
	cols := max((w+2)/(cell+2), 1)
	rows := (n + cols - 1) / cols
	if rows > maxRows {
		return heatStrip(per, w, maxRows)
	}
	cols = (n + rows - 1) / rows
	cellW := (w - (cols-1)*2) / cols
	bar := cellW - label - 1 - 1 - 4
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var parts []string
		for col := 0; col < cols; col++ {
			i := col*rows + r
			if i >= n {
				break
			}
			pct := per[i]
			parts = append(parts, theme.C(theme.Muted, fmt.Sprintf("%*d", label, i))+" "+c.Gauge(pct, bar)+" "+
				theme.C(theme.Heat(pct), c.FitRight(c.Pct(pct), 4)))
		}
		out[r] = strings.Join(parts, "  ")
	}
	return out
}

func heatStrip(per []float64, w, maxRows int) []string {
	perRow := max(w/2, 1)
	var out []string
	for start := 0; start < len(per) && len(out) < maxRows; start += perRow {
		var b strings.Builder
		for _, pct := range per[start:min(start+perRow, len(per))] {
			lvl := []rune("▁▂▃▄▅▆▇█")[min(int(pct/12.5), 7)]
			b.WriteString(theme.C(theme.Heat(pct), string(lvl)) + " ")
		}
		out = append(out, b.String())
	}
	return out
}

func (o *overview) memory(v vctx, w, h int) string {
	st := v.st
	mem := st.Memory
	p := c.Panel{Title: "Memory", Marker: theme.Mem, Width: w, Height: h, Focused: o.focus == ovMem}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if mem.Status == metrics.StatusPending {
		p.Body = c.Empty(iw, ih, "◌", v.spin+" Reading memory…", "")
		return p.Render()
	}
	p.Badge = theme.B(theme.Heat(mem.UsedPct), c.Pct(mem.UsedPct))
	var body []string
	if mem.Status == metrics.StatusError {
		body = append(body, errLine(mem.Err, iw))
	}
	body = append(body,
		c.Spread(theme.C(theme.Muted, "used ")+theme.B(theme.Text, c.Bytes(mem.Used))+theme.C(theme.Muted, " of "+c.Bytes(mem.Total)),
			theme.C(theme.Muted, "free ")+theme.C(theme.Subtle, c.Bytes(mem.Available)), iw),
		c.Gauge(mem.UsedPct, iw),
	)
	if ih >= 6 {
		sw := mem.Swap
		switch {
		case sw.Status == metrics.StatusError:
			body = append(body, "", warnLine("swap unavailable: "+sw.Err, iw))
		case sw.OK() && sw.Total == 0:
			body = append(body, "", theme.C(theme.Muted, "swap  ")+theme.C(theme.Faint, "not configured"))
		case sw.OK():
			body = append(body, "",
				c.Spread(theme.C(theme.Muted, "swap ")+theme.C(theme.Subtle, c.Bytes(sw.Used))+theme.C(theme.Muted, " of "+c.Bytes(sw.Total)),
					theme.C(theme.Heat(sw.UsedPct), c.Pct(sw.UsedPct)), iw),
				c.GaugeColor(sw.UsedPct, iw, theme.Swap),
			)
		}
	}
	if left := ih - len(body) - 2; left >= 2 {
		ring := st.Ring(metrics.MemUsedPct)
		var vals []float64
		var pts []metrics.Point
		if ring != nil {
			vals, pts = ring.Values(), ring.Points()
		}
		chart := c.Area(vals, c.ChartWidth(iw, "100%"), left, 100, c.Gradient{Lo: theme.MemDeep, Hi: theme.Mem}, v.mode)
		body = append(body, "")
		body = append(body, c.Axis(chart, "100%", "", "0%", "-"+c.Span(spanOf(pts, v.now)), "", "now", iw)...)
	}
	p.Body = body
	return p.Render()
}

func (o *overview) network(v vctx, w, h int) string {
	st := v.st
	n := st.Network
	p := c.Panel{Title: "Network", Marker: theme.Rx, Width: w, Height: h, Focused: o.focus == ovNet}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if n.Status == metrics.StatusPending || (!n.Primed && n.Status != metrics.StatusError) {
		p.Body = c.Empty(iw, ih, "◌", v.spin+" Measuring throughput…", "")
		return p.Render()
	}
	p.Badge = theme.B(theme.Rx, "↓ "+c.Rate(n.RxBps)) + "  " + theme.B(theme.Tx, "↑ "+c.Rate(n.TxBps))
	var body []string
	if n.Status == metrics.StatusError {
		body = append(body, errLine(n.Err, iw))
	}
	rx, tx := st.Ring(metrics.NetRxBps), st.Ring(metrics.NetTxBps)
	var rv, tv []float64
	var pts []metrics.Point
	if rx != nil && tx != nil {
		rv, tv, pts = rx.Values(), tx.Values(), rx.Points()
	}
	peak := math.Max(maxF(rv), maxF(tv))
	top := c.NiceBytesCeil(peak)
	chartH := max(ih-len(body)-2, 2)
	upLabel, downLabel := "↓"+rateShort(top), "↑"+rateShort(top)
	cw := c.ChartWidth(iw, upLabel, downLabel)
	chart := c.Mirror(rv, tv, cw, chartH, top,
		c.Gradient{Lo: theme.RxDeep, Hi: theme.Rx}, c.Gradient{Lo: theme.TxDeep, Hi: theme.Tx}, v.mode)
	body = append(body, c.Axis(chart, upLabel, "0", downLabel, "-"+c.Span(spanOf(pts, v.now)), "", "now", iw)...)

	var ifs []string
	for _, i := range n.Interfaces {
		if i.Loopback || !i.Up || len(ifs) == 3 {
			continue
		}
		ifs = append(ifs, theme.C(theme.Subtle, i.Name)+" "+theme.C(theme.Rx, "↓"+rateShort(i.RxBps))+" "+theme.C(theme.Tx, "↑"+rateShort(i.TxBps)))
	}
	total := theme.C(theme.Muted, "total ") + theme.C(theme.Rx, "↓"+c.Bytes(n.RxTotal)) + " " + theme.C(theme.Tx, "↑"+c.Bytes(n.TxTotal))
	body = append(body, c.Spread(strings.Join(ifs, theme.C(theme.Faint, " · ")), total, iw))
	p.Body = body
	return p.Render()
}

func maxF(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		if !math.IsNaN(x) && x > m {
			m = x
		}
	}
	return m
}

func (o *overview) disk(v vctx, w, h int) string {
	st := v.st
	d := st.Disk
	p := c.Panel{Title: "Disk", Marker: theme.Read, Width: w, Height: h, Focused: o.focus == ovDisk}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if d.Status == metrics.StatusPending {
		p.Body = c.Empty(iw, ih, "◌", v.spin+" Scanning volumes…", "")
		return p.Render()
	}
	var body []string
	if d.Status == metrics.StatusError {
		body = append(body, errLine(d.Err, iw))
	}
	ioLines := diskIO(v, iw)
	room := ih - len(body) - len(ioLines) - 1
	vols := append([]metrics.Volume(nil), d.Volumes...)
	sort.SliceStable(vols, func(i, j int) bool { return vols[i].UsedPct > vols[j].UsedPct })
	mountW := 0
	for _, vol := range vols {
		mountW = max(mountW, c.Width(vol.Mount))
	}
	mountW = min(mountW, max(iw/4, 6))
	shown := 0
	for _, vol := range vols {
		if shown >= room {
			break
		}
		if shown == room-1 && len(vols)-shown > 1 {
			body = append(body, theme.C(theme.Muted, fmt.Sprintf("+%d more volumes", len(vols)-shown)))
			break
		}
		name := c.Fit(theme.C(theme.Subtle, vol.Mount), mountW)
		if vol.Err != "" {
			body = append(body, name+"  "+theme.C(theme.Warn, "▲ "+vol.Err))
		} else {
			size := theme.C(theme.Muted, c.Bytes(vol.Used)+" / "+c.Bytes(vol.Total))
			pct := theme.B(theme.Heat(vol.UsedPct), c.FitRight(c.Pct(vol.UsedPct), 4))
			barW := iw - mountW - 2 - 4 - 2 - c.Width(size) - 1
			if barW < 6 {
				size, barW = "", iw-mountW-2-4-1
			}
			body = append(body, name+"  "+c.Gauge(vol.UsedPct, barW)+" "+pct+"  "+size)
		}
		shown++
	}
	if len(vols) == 0 && d.OK() {
		body = append(body, theme.C(theme.Muted, "no volumes visible"))
	}
	if free := ih - len(body) - 1; free >= 6 && d.IO.OK() && d.IO.Primed {
		ioLines = diskIOChart(v, iw, free)
	}
	for len(body) < ih-len(ioLines) {
		body = append(body, "")
	}
	body = append(body, ioLines...)
	p.Body = body
	p.Badge = theme.C(theme.Read, "R "+rateShort(d.IO.ReadBps)) + " " + theme.C(theme.Write, "W "+rateShort(d.IO.WriteBps))
	if !d.IO.OK() || !d.IO.Primed {
		p.Badge = ""
	}
	return p.Render()
}

func diskIO(v vctx, iw int) []string {
	io := v.st.Disk.IO
	switch {
	case io.Status == metrics.StatusUnsupported:
		return []string{theme.C(theme.Muted, "I/O counters not available on this system")}
	case io.Status == metrics.StatusError:
		return []string{warnLine("I/O: "+io.Err, iw)}
	case !io.Primed:
		return []string{theme.C(theme.Muted, v.spin+" measuring I/O…")}
	}
	r, wr := v.st.Ring(metrics.DiskReadBps), v.st.Ring(metrics.DiskWriteBps)
	var rv, wv []float64
	if r != nil && wr != nil {
		rv, wv = r.Values(), wr.Values()
	}
	peak := c.NiceBytesCeil(math.Max(maxF(rv), maxF(wv)))
	valW := 11
	sparkW := iw - 6 - valW - 1
	return []string{
		theme.C(theme.Muted, c.Fit("read", 6)) + c.Spark(rv, sparkW, peak, theme.Read) + " " + c.FitRight(theme.C(theme.Subtle, c.Rate(io.ReadBps)), valW),
		theme.C(theme.Muted, c.Fit("write", 6)) + c.Spark(wv, sparkW, peak, theme.Write) + " " + c.FitRight(theme.C(theme.Subtle, c.Rate(io.WriteBps)), valW),
	}
}

func diskIOChart(v vctx, iw, h int) []string {
	r, wr := v.st.Ring(metrics.DiskReadBps), v.st.Ring(metrics.DiskWriteBps)
	var rv, wv []float64
	var pts []metrics.Point
	if r != nil && wr != nil {
		rv, wv, pts = r.Values(), wr.Values(), r.Points()
	}
	top := c.NiceBytesCeil(math.Max(maxF(rv), maxF(wv)))
	upL, downL := "R "+rateShort(top), "W "+rateShort(top)
	cw := c.ChartWidth(iw, upL, downL)
	chart := c.Mirror(rv, wv, cw, h-2, top,
		c.Gradient{Lo: theme.Blend(theme.Read, theme.Ink, 0.55), Hi: theme.Read},
		c.Gradient{Lo: theme.Blend(theme.Write, theme.Ink, 0.55), Hi: theme.Write}, v.mode)
	out := c.Axis(chart, upL, "0", downL, "-"+c.Span(spanOf(pts, v.now)), "", "now", iw)
	io := v.st.Disk.IO
	legend := theme.C(theme.Read, "■ ") + theme.C(theme.Muted, "read ") + theme.C(theme.Subtle, c.Rate(io.ReadBps)) +
		theme.C(theme.Faint, "   ") + theme.C(theme.Write, "■ ") + theme.C(theme.Muted, "write ") + theme.C(theme.Subtle, c.Rate(io.WriteBps))
	return append(out, legend)
}

func (o *overview) services(v vctx, w, h int) string {
	st := v.st
	p := c.Panel{Title: "Services", Marker: theme.Latency, Width: w, Height: h, Focused: o.focus == ovServices}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if len(st.Services) == 0 {
		p.Body = c.Empty(iw, ih, "◇", "No services configured", "add services.targets to the config")
		return p.Render()
	}
	up := 0
	for _, s := range st.Services {
		if n := len(s.History); n > 0 && s.History[n-1].OK {
			up++
		}
	}
	p.Badge = theme.B(colorOr(up == len(st.Services), theme.OK, theme.Warn), fmt.Sprintf("%d/%d up", up, len(st.Services)))
	stripW := min(max(iw/4, 6), 16)
	var body []string
	for i, s := range st.Services {
		if i >= ih {
			break
		}
		lat := theme.C(theme.Faint, "—")
		if s.Status != service.StatusDown && s.LastSuccess.After(s.LastFailure) {
			lat = theme.C(theme.Subtle, c.Millis(s.LastLatency))
		} else if s.Status == service.StatusDown {
			lat = theme.C(theme.Crit, "down")
		}
		nameW := iw - 2 - 7 - 1 - stripW - 1
		body = append(body, c.Dot(serviceLevel(s.Status))+" "+c.Fit(theme.C(theme.Text, s.Def.Name), nameW)+" "+
			c.FitRight(lat, 7)+" "+healthStrip(s.History, stripW))
	}
	p.Body = body
	return p.Render()
}

func (o *overview) alerts(v vctx, w, h int) string {
	st := v.st
	p := c.Panel{Title: "Alerts", Marker: theme.Warn, Width: w, Height: h, Focused: o.focus == ovAlerts}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if !v.cfg.Alerts.Enabled {
		p.Body = c.Empty(iw, ih, "○", "Alerting disabled", "alerts.enabled: false")
		return p.Render()
	}
	active := st.Alerts.Active
	if len(active) == 0 {
		n := len(st.Alerts.Rules)
		p.Badge = theme.C(theme.OK, "✓ clear")
		p.Body = c.Empty(iw, ih, theme.C(theme.OK, "✓"), theme.C(theme.OK, "All clear"), fmt.Sprintf("%d rules watching", n))
		return p.Render()
	}
	crit, _, _ := alertCounts(active)
	p.Badge = theme.B(colorOr(crit > 0, theme.Crit, theme.Warn), fmt.Sprintf("%d active", len(active)))
	var body []string
	for i, inc := range active {
		if i >= ih {
			break
		}
		dur := theme.C(theme.Muted, c.Duration(inc.Duration(v.now)))
		ack := " "
		if inc.Acknowledged() {
			ack = theme.C(theme.OK, "✓")
		}
		nameW := iw - 2 - 8 - 3
		body = append(body, sevGlyph(inc.Severity)+" "+c.Fit(theme.C(theme.Text, inc.RuleName), nameW)+" "+c.FitRight(dur, 8)+" "+ack)
	}
	p.Body = body
	return p.Render()
}

func (o *overview) procs(v vctx, w, h int) string {
	st := v.st
	p := c.Panel{Title: "Top processes", Marker: theme.ProcHue, Width: w, Height: h, Focused: o.focus == ovProcs}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	if !st.Processes.OK && len(st.Processes.Processes) == 0 {
		msg := v.spin + " Reading process table…"
		if st.Processes.Err != "" {
			msg = "process list unavailable"
		}
		p.Body = c.Empty(iw, ih, "◌", msg, st.Processes.Err)
		return p.Render()
	}
	list := make([]process.Info, 0, len(st.Processes.Processes))
	for _, pr := range st.Processes.Processes {
		if pr.CPUOK || pr.MemOK {
			list = append(list, pr)
		}
	}
	process.Sort(list, process.SortMemory, true)
	process.Sort(list, process.SortCPU, true)
	p.Badge = theme.C(theme.Muted, fmt.Sprintf("%d total", st.Processes.Total))
	var body []string
	for _, pr := range list {
		if len(body) >= ih {
			break
		}
		cpu := theme.C(theme.Faint, c.FitRight("—", 5))
		if pr.CPUOK {
			cpu = theme.C(theme.Heat(pr.CPU*2), c.FitRight(fmt.Sprintf("%.1f%%", pr.CPU), 5))
		}
		mem := theme.C(theme.Faint, c.FitRight("—", 9))
		if pr.MemOK {
			mem = theme.C(theme.Muted, c.FitRight(c.Bytes(pr.RSS), 9))
		}
		nameW := iw - 5 - 9 - 2
		body = append(body, c.Fit(theme.C(theme.Text, pr.Name), nameW)+" "+cpu+" "+mem)
	}
	p.Body = body
	return p.Render()
}

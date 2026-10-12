package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"

	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

func itoa(n int) string { return strconv.Itoa(n) }

func alertCounts(incs []alert.Incident) (crit, warn, info int) {
	for _, i := range incs {
		switch i.Severity {
		case alert.SeverityCritical:
			crit++
		case alert.SeverityWarning:
			warn++
		default:
			info++
		}
	}
	return
}

func (m *Model) header() string {
	pulse := theme.Brand1
	switch {
	case m.paused:
		pulse = theme.Warn
	case !m.haveState || m.now.Sub(m.lastUpdate) > 5*time.Second:
		pulse = theme.Muted
	}
	logo := " " + theme.C(pulse, "●") + " " + theme.Gradient("PULSE", theme.Brand1, theme.Brand2, true)

	crit, warn, _ := alertCounts(m.st.Alerts.Active)
	down := 0
	for _, s := range m.st.Services {
		if s.Status == service.StatusDown {
			down++
		}
	}
	badge := func(i int, compact bool) string {
		n, col := 0, theme.Crit
		switch i {
		case scrAlerts:
			n = crit + warn
			if crit == 0 {
				col = theme.Warn
			}
		case scrServices:
			n = down
		}
		switch {
		case n == 0:
			return ""
		case compact:
			return theme.C(col, "•")
		}
		return theme.B(col, itoa(n))
	}

	right := ""
	if h := m.st.Host; h.Hostname != "" {
		right = theme.C(theme.Subtle, h.Hostname) + theme.C(theme.Faint, " · ")
	}
	right += theme.C(theme.Muted, m.now.Format("15:04:05")) + " "

	for _, style := range []int{0, 1, 2} {
		var tabs []string
		for i := 0; i < numScreens; i++ {
			name := screenNames[i]
			if style == 2 && i != m.active {
				name = ""
			}
			label := itoa(i + 1)
			if name != "" {
				label += " " + name
			}
			b := badge(i, style == 2)
			if b != "" && style < 2 {
				b = " " + b
			}
			if i == m.active {
				tabs = append(tabs, c.Pill(label, theme.Accent)+b)
			} else {
				num := theme.C(theme.Faint, itoa(i+1))
				rest := strings.TrimPrefix(label, itoa(i+1))
				tabs = append(tabs, " "+num+theme.C(theme.Muted, rest)+b+" ")
			}
		}
		gap := "  "
		if style > 0 {
			gap = " "
		}
		left := logo + "   " + strings.Join(tabs, gap)
		if style == 1 {
			left = logo + "  " + strings.Join(tabs, gap)
		}
		if c.Width(left)+c.Width(right)+2 <= m.width {
			return c.Spread(left, right, m.width)
		}
		if style == 2 {
			return c.Fit(left, m.width)
		}
	}
	return ""
}

func (m *Model) hints() string {
	if m.toast != "" && m.now.Before(m.toastUntil) {
		mark, col := "✓ ", theme.OK
		if m.toastErr {
			mark, col = "✗ ", theme.Crit
		}
		return c.Fit(" "+theme.C(col, mark)+theme.C(theme.Text, m.toast), m.width)
	}
	b := append([]key.Binding{}, m.screens[m.active].bindings()...)
	b = append(b, keys.Focus, keys.Help, keys.Quit)
	m.help.Width = m.width - 2
	return c.Fit(" "+m.help.ShortHelpView(b), m.width)
}

func (m *Model) statusBar() string {
	st := &m.st
	sep := theme.C(theme.Faint, "  │  ")
	var segs []string

	mode := c.Pill("LIVE", theme.OK)
	if m.paused {
		mode = c.Pill("PAUSED", theme.Warn)
	}
	segs = append(segs, mode)

	if st.CPU.OK() {
		segs = append(segs, theme.C(theme.Muted, "cpu ")+theme.B(theme.Heat(st.CPU.Total), c.Pct(st.CPU.Total))+
			theme.C(theme.Muted, "  mem ")+theme.B(theme.Heat(st.Memory.UsedPct), c.Pct(st.Memory.UsedPct)))
	}
	if st.Network.Primed {
		segs = append(segs, theme.C(theme.Rx, "↓ "+c.Rate(st.Network.RxBps))+" "+theme.C(theme.Tx, "↑ "+c.Rate(st.Network.TxBps)))
	}

	crit, warn, info := alertCounts(st.Alerts.Active)
	switch {
	case !m.cfg.Alerts.Enabled:
		segs = append(segs, theme.C(theme.Muted, "alerts off"))
	case crit+warn+info == 0:
		segs = append(segs, theme.C(theme.OK, "✓ ")+theme.C(theme.Subtle, "all clear"))
	default:
		s := ""
		if crit > 0 {
			s += theme.B(theme.Crit, fmt.Sprintf("◆ %d critical ", crit))
		}
		if warn > 0 {
			s += theme.B(theme.Warn, fmt.Sprintf("▲ %d warning ", warn))
		}
		if info > 0 {
			s += theme.C(theme.Info, fmt.Sprintf("● %d info", info))
		}
		segs = append(segs, strings.TrimSpace(s))
	}

	sv := st.Storage
	switch {
	case !sv.Enabled:
		segs = append(segs, theme.C(theme.Muted, "history off"))
	case sv.InitErr != "":
		segs = append(segs, theme.C(theme.Crit, "✗ history unavailable"))
	case sv.Degraded():
		segs = append(segs, theme.C(theme.Warn, "▲ db degraded"))
	default:
		size := ""
		if sv.DB.SizeBytes > 0 {
			size = " " + c.Bytes(uint64(sv.DB.SizeBytes+sv.DB.WALBytes))
		}
		segs = append(segs, theme.C(theme.Muted, "db")+theme.C(theme.Subtle, size)+theme.C(theme.OK, " ✓"))
	}

	left := " " + strings.Join(segs, sep)
	right := theme.C(theme.Muted, "updated "+c.Ago(m.lastUpdate, m.now)) + "  "
	if !m.haveState {
		right = ""
	}
	for c.Width(left)+c.Width(right)+1 > m.width && len(segs) > 1 {
		segs = segs[:len(segs)-1]
		left = " " + strings.Join(segs, sep)
	}
	return c.Highlight(c.Spread(left, right, m.width), theme.Surface)
}

func (m *Model) helpView(v vctx) string {
	groups := []helpGroup{globalHelp()}
	if b := m.screens[m.active].bindings(); len(b) > 0 {
		groups = append(groups, helpGroup{screenNames[m.active], b})
	}
	render := func(g helpGroup, w int) []string {
		out := []string{theme.B(theme.Accent, g.title), ""}
		for _, b := range g.bindings {
			h := b.Help()
			if h.Key == "" {
				continue
			}
			out = append(out, c.Fit(theme.C(theme.Text, h.Key), 10)+c.Fit(theme.C(theme.Muted, h.Desc), w-10))
		}
		return out
	}
	w := min(v.w-4, 80)
	colW := (c.InnerWidth(w) - 3) / 2
	left := render(groups[0], colW)
	var right []string
	if len(groups) > 1 {
		right = render(groups[1], colW)
	}
	rows := max(len(left), len(right))
	body := make([]string, 0, rows+2)
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		body = append(body, c.Fit(l, colW)+theme.C(theme.Faint, " │ ")+c.Fit(r, colW))
	}
	body = append(body, "", c.Center(theme.C(theme.Muted, "press ? or esc to close"), c.InnerWidth(w)))
	h := min(len(body)+2, v.h)
	return c.Panel{Title: "Keyboard", Marker: theme.Accent, Body: body, Width: w, Height: h, Focused: true}.Render()
}

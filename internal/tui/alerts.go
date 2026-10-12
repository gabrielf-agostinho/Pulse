package tui

import (
	"context"
	"fmt"

	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gabrielagostinho/pulse/internal/application/alerts"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

const (
	secActive = iota
	secHistory
	secRules
)

type ackMsg struct {
	name string
	err  error
}

type alertsView struct {
	section int
	sel     [3]int
	offset  [3]int
	visible int
	sev     int
	detail  *alert.Incident
}

func (a *alertsView) bindings() []key.Binding {
	b := []key.Binding{keys.Up, keys.Down, keys.Enter, kSeverity}
	if a.section == secActive {
		b = append(b, kAck)
	}
	return b
}
func (a *alertsView) capturing() bool { return false }

func (a *alertsView) items(v *vctx) []alert.Incident {
	src := v.st.Alerts.Active
	if a.section == secHistory {
		src = v.st.Alerts.Recent
	}
	if a.sev < 0 {
		return src
	}
	out := make([]alert.Incident, 0, len(src))
	for _, i := range src {
		if int(i.Severity) == a.sev {
			out = append(out, i)
		}
	}
	return out
}

func (a *alertsView) count(m *Model) int {
	v := m.vctx()
	if a.section == secRules {
		return len(m.st.Alerts.Rules)
	}
	return len(a.items(&v))
}

func (a *alertsView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ackMsg:
		if msg.err != nil {
			return toast(msg.err.Error(), true)
		}
		return toast("Acknowledged “"+msg.name+"”", false)
	case tea.KeyMsg:
		if a.detail != nil {
			if key.Matches(msg, keys.Back, keys.Enter, kCancel) {
				a.detail = nil
			} else if key.Matches(msg, kAck) && a.detail.Active() {
				inc := *a.detail
				a.detail = nil
				return a.ack(m, inc)
			}
			return nil
		}
		n := a.count(m)
		s := &a.sel[a.section]
		switch {
		case key.Matches(msg, keys.Focus):
			if msg.String() == "shift+tab" {
				a.section = (a.section + 2) % 3
			} else {
				a.section = (a.section + 1) % 3
			}
		case key.Matches(msg, keys.Up):
			*s = max(*s-1, 0)
		case key.Matches(msg, keys.Down):
			*s = min(*s+1, max(n-1, 0))
		case key.Matches(msg, keys.PageDown):
			*s = min(*s+max(a.visible-1, 1), max(n-1, 0))
		case key.Matches(msg, keys.PageUp):
			*s = max(*s-max(a.visible-1, 1), 0)
		case key.Matches(msg, keys.Top):
			*s = 0
		case key.Matches(msg, keys.Bottom):
			*s = max(n-1, 0)
		case key.Matches(msg, kSeverity):
			a.sev++
			if a.sev > int(alert.SeverityCritical) {
				a.sev = -1
			}
			a.sel = [3]int{}
		case key.Matches(msg, keys.Enter):
			if a.section != secRules {
				v := m.vctx()
				if items := a.items(&v); *s < len(items) {
					inc := items[*s]
					a.detail = &inc
				}
			}
		case key.Matches(msg, kAck):
			if a.section == secActive {
				v := m.vctx()
				if items := a.items(&v); *s < len(items) {
					return a.ack(m, items[*s])
				}
			}
		}
	}
	return nil
}

func (a *alertsView) ack(m *Model, inc alert.Incident) tea.Cmd {
	if inc.Acknowledged() {
		return toast("Already acknowledged", false)
	}
	be := m.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return ackMsg{name: inc.RuleName, err: be.Acknowledge(ctx, inc.ID)}
	}
}

func (a *alertsView) view(v vctx) string {
	p := c.Panel{Title: "Alerts", Marker: theme.Warn, Width: v.w, Height: v.h, Focused: true}
	iw, ih := c.InnerWidth(v.w), c.InnerHeight(v.h)
	if !v.cfg.Alerts.Enabled {
		p.Body = c.Empty(iw, ih, "○", "Alerting is disabled", "set alerts.enabled: true in the configuration")
		return p.Render()
	}
	al := v.st.Alerts
	sevName := "all severities"
	if a.sev >= 0 {
		sevName = alert.Severity(a.sev).String() + " only"
	}
	p.Badge = theme.C(theme.Muted, "filter ") + theme.C(theme.Accent, sevName)
	if al.PersistErr != "" {
		p.Footer = theme.C(theme.Warn, "▲ incidents not persisted: "+al.PersistErr)
	}

	tab := func(i int, label string, n int) string {
		text := fmt.Sprintf("%s %d", label, n)
		if i == a.section {
			return c.Pill(text, theme.Accent)
		}
		return " " + theme.C(theme.Muted, text) + " "
	}
	crit, warn, _ := alertCounts(al.Active)
	summary := ""
	if crit > 0 {
		summary += theme.B(theme.Crit, fmt.Sprintf("◆ %d critical", crit)) + "  "
	}
	if warn > 0 {
		summary += theme.B(theme.Warn, fmt.Sprintf("▲ %d warning", warn))
	}
	body := []string{
		c.Spread(tab(secActive, "Active", len(al.Active))+" "+tab(secHistory, "History", len(al.Recent))+" "+tab(secRules, "Rules", len(al.Rules)), summary, iw),
		"",
	}
	tableH := ih - len(body)
	a.visible = tableH - 1
	sel := &a.sel[a.section]
	off := &a.offset[a.section]

	switch a.section {
	case secRules:
		rows := make([][]string, len(al.Rules))
		for i, r := range al.Rules {
			rows[i] = ruleRow(r)
		}
		*sel = min(*sel, max(len(rows)-1, 0))
		*off = c.Clamp(*sel, *off, a.visible, len(rows))
		if len(rows) == 0 {
			body = append(body, c.Empty(iw, tableH, "∅", "No alert rules configured", "add rules under alerts.rules")...)
			break
		}
		t := c.Table{Columns: []c.Column{
			{Title: "STATE", Width: 9},
			{Title: "RULE", Flex: true},
			{Title: "CONDITION", Width: 34, HideBelow: 100},
			{Title: "SEVERITY", Width: 8},
			{Title: "SERIES", Width: 6, Right: true, HideBelow: 80},
			{Title: "LAST VALUE", Width: 11, Right: true, HideBelow: 70},
		}, Rows: rows, Selected: *sel, Offset: *off, Width: iw, Height: tableH, Focused: true, SortCol: -1}
		body = append(body, t.Render()...)
	default:
		items := a.items(&v)
		*sel = min(*sel, max(len(items)-1, 0))
		*off = c.Clamp(*sel, *off, a.visible, len(items))
		if len(items) == 0 {
			if a.section == secActive {
				body = append(body, c.Empty(iw, tableH, theme.C(theme.OK, "✓"), theme.B(theme.OK, "All clear"),
					fmt.Sprintf("%d rules active, no incidents", len(al.Rules)))...)
			} else {
				body = append(body, c.Empty(iw, tableH, "∅", "No resolved incidents yet", "resolved incidents are kept for "+c.Span(v.cfg.Storage.Retention.Alerts))...)
			}
			break
		}
		rows := make([][]string, len(items))
		for i, inc := range items {
			rows[i] = incidentRow(inc, v, a.section == secActive)
		}
		cols := []c.Column{
			{Title: "SEVERITY", Width: 8},
			{Title: "INCIDENT", Flex: true},
			{Title: "VALUE", Width: 18, Right: true, HideBelow: 90},
			{Title: "STARTED", Width: 12, Right: true, HideBelow: 74},
			{Title: "DURATION", Width: 9, Right: true},
			{Title: "ACK", Width: 3},
		}
		if a.section == secActive {
			cols[2].Title = "NOW / PEAK"
		}
		t := c.Table{Columns: cols, Rows: rows, Selected: *sel, Offset: *off, Width: iw, Height: tableH, Focused: true, SortCol: -1}
		body = append(body, t.Render()...)
	}
	p.Body = body
	return p.Render()
}

func incidentRow(inc alert.Incident, v vctx, active bool) []string {
	value := theme.C(theme.Subtle, alert.FormatValue(inc.LastValue, inc.Unit)) + theme.C(theme.Muted, " / ") + theme.C(theme.Text, alert.FormatValue(inc.PeakValue, inc.Unit))
	if !active {
		value = theme.C(theme.Muted, "peak ") + theme.C(theme.Subtle, alert.FormatValue(inc.PeakValue, inc.Unit))
	}
	started := inc.StartedAt.Format("15:04:05")
	if v.now.Sub(inc.StartedAt) > 20*time.Hour {
		started = inc.StartedAt.Format("Jan 02 15:04")
	}
	dur := c.Duration(inc.Duration(v.now))
	durCol := theme.Subtle
	if active {
		durCol = severityLevel(inc.Severity).Color()
	}
	ack := theme.C(theme.Faint, "—")
	if inc.Acknowledged() {
		ack = theme.C(theme.OK, "✓")
	}
	name := theme.C(theme.Text, inc.RuleName)
	if !active {
		name = theme.C(theme.Subtle, inc.RuleName)
	}
	return []string{sevTag(inc.Severity), name, value, theme.C(theme.Muted, started), theme.C(durCol, dur), ack}
}

func ruleRow(r alerts.RuleStatus) []string {
	state := theme.C(theme.OK, "● ok")
	switch r.Phase {
	case alert.PhasePending:
		state = theme.C(theme.Info, "◔ pending")
	case alert.PhaseFiring:
		state = theme.B(severityLevel(r.Rule.Severity).Color(), "◆ firing")
	case alert.PhaseResolving:
		state = theme.C(theme.Warn, "◑ clearing")
	}
	if !r.Rule.Enabled {
		state = theme.C(theme.Faint, "○ off")
	}
	if r.Series == 0 && r.Rule.Enabled {
		state = theme.C(theme.Muted, "○ no data")
	}
	last := theme.C(theme.Faint, "—")
	if r.HasValue {
		last = theme.C(theme.Subtle, alert.FormatValue(r.LastValue, r.Rule.Unit))
	}
	series := theme.C(theme.Muted, itoa(r.Series))
	if r.Firing > 0 {
		series = theme.C(theme.Crit, fmt.Sprintf("%d/%d", r.Firing, r.Series))
	}
	return []string{state, theme.C(theme.Text, r.Rule.Name), theme.C(theme.Muted, r.Rule.Describe()),
		theme.C(severityLevel(r.Rule.Severity).Color(), r.Rule.Severity.String()), series, last}
}

func (a *alertsView) modal(v vctx) string {
	if a.detail == nil {
		return ""
	}
	inc := *a.detail
	for _, live := range v.st.Alerts.Active {
		if live.ID == inc.ID {
			inc = live
		}
	}
	w := min(v.w-4, 72)
	iw := c.InnerWidth(w)
	lw := 14
	val := func(s string) string { return theme.C(theme.Text, s) }
	state := c.Pill("ACTIVE", severityLevel(inc.Severity).Color())
	if !inc.Active() {
		state = c.Pill("RESOLVED", theme.OK)
	}
	body := []string{
		c.Spread(sevTag(inc.Severity)+"  "+theme.B(theme.Text, inc.RuleName), state, iw),
		"",
	}
	for _, l := range wrap(inc.Message, iw) {
		body = append(body, theme.C(theme.Subtle, l))
	}
	body = append(body, "",
		kv("metric", lw, val(inc.Metric)),
		kv("threshold", lw, val(alert.FormatValue(inc.Threshold, inc.Unit))),
		kv("peak", lw, val(alert.FormatValue(inc.PeakValue, inc.Unit))),
		kv("last value", lw, val(alert.FormatValue(inc.LastValue, inc.Unit))),
		kv("started", lw, val(inc.StartedAt.Format("2006-01-02 15:04:05"))),
	)
	if inc.Active() {
		body = append(body, kv("open for", lw, theme.C(severityLevel(inc.Severity).Color(), c.Duration(inc.Duration(v.now)))))
	} else {
		body = append(body, kv("resolved", lw, val(inc.EndedAt.Format("2006-01-02 15:04:05"))), kv("lasted", lw, val(c.Duration(inc.Duration(v.now)))))
	}
	if inc.Acknowledged() {
		body = append(body, kv("acknowledged", lw, theme.C(theme.OK, "✓ "+inc.AckedAt.Format("2006-01-02 15:04:05"))))
	} else {
		body = append(body, kv("acknowledged", lw, theme.C(theme.Faint, "not yet")))
	}
	hint := theme.C(theme.Muted, "esc close")
	if inc.Active() && !inc.Acknowledged() {
		hint = c.Key("a", "acknowledge") + theme.C(theme.Faint, "  ·  ") + hint
	}
	body = append(body, "", c.Center(hint, iw))
	return c.Panel{Title: "Incident", Marker: severityLevel(inc.Severity).Color(), Body: body, Width: w, Height: min(len(body)+2, v.h), Focused: true}.Render()
}

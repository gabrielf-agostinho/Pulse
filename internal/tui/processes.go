package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gabrielagostinho/pulse/internal/domain/process"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type (
	detailsReqMsg struct{ pid int32 }
	detailsMsg    struct {
		pid int32
		d   process.Details
		err error
	}
	killMsg struct {
		pid  int32
		name string
		err  error
	}
)

type processesView struct {
	input     textinput.Model
	filtering bool
	sortKey   process.SortKey
	desc      bool

	rows    []process.Info
	sel     int
	offset  int
	selPID  int32
	visible int

	detail     bool
	details    *process.Details
	detailsErr string
	loading    int32

	confirm *process.Info
}

func newProcesses() *processesView {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "name or pid (pid:123 for exact)"
	ti.PromptStyle = theme.Fg(theme.Accent)
	ti.TextStyle = theme.Fg(theme.Text)
	ti.PlaceholderStyle = theme.Fg(theme.Faint)
	ti.Cursor.Style = theme.Fg(theme.Accent)
	ti.CharLimit = 64
	return &processesView{input: ti, sortKey: process.SortCPU, desc: true}
}

func (p *processesView) bindings() []key.Binding {
	if p.filtering {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "apply")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
		}
	}
	return []key.Binding{keys.Up, keys.Down, keys.Enter, kFilter, kSort, kReverse, kSortCPU, kSortMem, kKill}
}

func (p *processesView) capturing() bool { return p.filtering }

func (p *processesView) refresh(m *Model) {
	list := process.Filter(m.st.Processes.Processes, p.input.Value())
	rows := append([]process.Info(nil), list...)
	process.Sort(rows, p.sortKey, p.desc)
	p.rows = rows
	p.sel = min(p.sel, max(len(rows)-1, 0))
	if p.selPID != 0 {
		for i, r := range rows {
			if r.PID == p.selPID {
				p.sel = i
				break
			}
		}
	}
	if len(rows) > 0 {
		p.selPID = rows[p.sel].PID
	}
}

func (p *processesView) selected() (process.Info, bool) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return process.Info{}, false
	}
	return p.rows[p.sel], true
}

func (p *processesView) move(m *Model, delta int) tea.Cmd {
	if len(p.rows) == 0 {
		return nil
	}
	p.sel = max(0, min(len(p.rows)-1, p.sel+delta))
	prev := p.selPID
	p.selPID = p.rows[p.sel].PID
	if p.detail && prev != p.selPID {
		return p.requestDetails(p.selPID)
	}
	return nil
}

func (p *processesView) requestDetails(pid int32) tea.Cmd {
	p.loading = pid
	return tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg { return detailsReqMsg{pid: pid} })
}

func (p *processesView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case detailsReqMsg:
		if msg.pid != p.selPID || !p.detail {
			return nil
		}
		be := m.be
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			d, err := be.Inspect(ctx, msg.pid)
			return detailsMsg{pid: msg.pid, d: d, err: err}
		}
	case detailsMsg:
		if msg.pid != p.selPID {
			return nil
		}
		p.loading = 0
		if msg.err != nil {
			p.details, p.detailsErr = nil, msg.err.Error()
		} else {
			d := msg.d
			p.details, p.detailsErr = &d, ""
		}
		return nil
	case killMsg:
		if msg.err != nil {
			return toast(msg.err.Error(), true)
		}
		return toast(fmt.Sprintf("Sent terminate to %s (pid %d)", msg.name, msg.pid), false)
	case tea.KeyMsg:
		return p.key(m, msg)
	default:
		if p.filtering {
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return cmd
		}
	}
	return nil
}

func (p *processesView) key(m *Model, msg tea.KeyMsg) tea.Cmd {
	if p.confirm != nil {
		switch {
		case key.Matches(msg, kConfirm):
			target := *p.confirm
			p.confirm = nil
			be := m.be
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return killMsg{pid: target.PID, name: target.Name, err: be.Terminate(ctx, target.PID)}
			}
		case key.Matches(msg, kCancel):
			p.confirm = nil
		}
		return nil
	}
	if p.filtering {
		switch msg.String() {
		case "esc":
			p.input.SetValue("")
			p.input.Blur()
			p.filtering = false
		case "enter":
			p.input.Blur()
			p.filtering = false
		case "up", "down":
			p.filtering = false
			p.input.Blur()
			return p.key(m, msg)
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			p.sel, p.offset = 0, 0
			p.selPID = 0
			p.refresh(m)
			return cmd
		}
		p.refresh(m)
		return nil
	}

	page := max(p.visible-1, 1)
	switch {
	case key.Matches(msg, keys.Up):
		return p.move(m, -1)
	case key.Matches(msg, keys.Down):
		return p.move(m, 1)
	case key.Matches(msg, keys.PageUp):
		return p.move(m, -page)
	case key.Matches(msg, keys.PageDown):
		return p.move(m, page)
	case key.Matches(msg, keys.Top):
		return p.move(m, -len(p.rows))
	case key.Matches(msg, keys.Bottom):
		return p.move(m, len(p.rows))
	case key.Matches(msg, kFilter):
		p.filtering = true
		return p.input.Focus()
	case key.Matches(msg, keys.Back):
		if p.detail {
			p.detail = false
		} else if p.input.Value() != "" {
			p.input.SetValue("")
			p.refresh(m)
		}
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Focus):
		p.detail = !p.detail
		if p.detail {
			p.details, p.detailsErr = nil, ""
			return p.requestDetails(p.selPID)
		}
	case key.Matches(msg, kSort):
		p.sortKey = p.sortKey.Next()
		p.desc = p.sortKey.DefaultDesc()
		p.refresh(m)
	case key.Matches(msg, kReverse):
		p.desc = !p.desc
		p.refresh(m)
	case key.Matches(msg, kSortCPU):
		p.setSort(m, process.SortCPU)
	case key.Matches(msg, kSortMem):
		p.setSort(m, process.SortMemory)
	case key.Matches(msg, kKill):
		info, ok := p.selected()
		if !ok {
			return nil
		}
		if !m.cfg.Collectors.Process.AllowKill {
			return toast("Terminating processes is disabled (collectors.processes.allow_kill)", true)
		}
		if process.Protected(info.PID, int32(selfPID)) {
			return toast(fmt.Sprintf("%s (pid %d) is protected and cannot be terminated from Pulse", info.Name, info.PID), true)
		}
		p.confirm = &info
	}
	return nil
}

func (p *processesView) setSort(m *Model, k process.SortKey) {
	if p.sortKey == k {
		p.desc = !p.desc
	} else {
		p.sortKey, p.desc = k, k.DefaultDesc()
	}
	p.refresh(m)
}

func sortColumn(k process.SortKey) int {
	switch k {
	case process.SortPID:
		return 0
	case process.SortName:
		return 1
	case process.SortCPU:
		return 2
	case process.SortMemory:
		return 3
	case process.SortThreads:
		return 5
	}
	return -1
}

func (p *processesView) view(v vctx) string {
	st := v.st.Processes
	sideW := 0
	if p.detail && v.w >= 120 {
		sideW = min(max(v.w*36/100, 44), 64)
	}
	w := v.w
	if sideW > 0 {
		w = v.w - sideW - 1
	}
	panel := c.Panel{Title: "Processes", Marker: theme.ProcHue, Width: w, Height: v.h, Focused: !p.detail || sideW == 0}
	iw, ih := c.InnerWidth(w), c.InnerHeight(v.h)
	badge := theme.C(theme.Subtle, c.Count(int64(st.Total))) + theme.C(theme.Muted, " processes")
	if st.Restricted > 0 {
		badge += theme.C(theme.Faint, " · ") + theme.C(theme.Warn, itoa(st.Restricted)) + theme.C(theme.Muted, " restricted")
	}
	panel.Badge = badge
	panel.Footer = theme.C(theme.Muted, "sort ") + theme.C(theme.Accent, p.sortKey.String()) + theme.C(theme.Muted, map[bool]string{true: " ▼", false: " ▲"}[p.desc])

	var body []string
	switch {
	case p.filtering:
		p.input.Width = iw - 4
		body = append(body, p.input.View())
	case p.input.Value() != "":
		body = append(body, c.Spread(theme.C(theme.Accent, "/ ")+theme.C(theme.Text, p.input.Value())+
			theme.C(theme.Muted, fmt.Sprintf("  %d matches", len(p.rows))), theme.C(theme.Muted, "esc clear"), iw))
	default:
		body = append(body, theme.C(theme.Faint, "/ filter by name or pid"))
	}
	if st.Err != "" {
		body = append(body, errLine("process collection failed: "+st.Err, iw))
	}

	tableH := ih - len(body)
	p.visible = max(tableH-1, 1)
	p.offset = c.Clamp(p.sel, p.offset, p.visible, len(p.rows))
	switch {
	case !st.OK && len(st.Processes) == 0:
		body = append(body, c.Empty(iw, tableH, "◌", v.spin+" Reading process table…", "")...)
	case len(p.rows) == 0:
		body = append(body, c.Empty(iw, tableH, "∅", "No processes match “"+p.input.Value()+"”", "esc to clear the filter")...)
	default:
		rows := make([][]string, len(p.rows))
		for i, r := range p.rows {
			rows[i] = processRow(r)
		}
		t := c.Table{
			Columns: []c.Column{
				{Title: "PID", Width: 7, Right: true},
				{Title: "NAME", Flex: true},
				{Title: "CPU", Width: 13, Right: true},
				{Title: "MEMORY", Width: 10, Right: true},
				{Title: "MEM%", Width: 6, Right: true, HideBelow: 90},
				{Title: "THREADS", Width: 7, Right: true, HideBelow: 80},
				{Title: "STATUS", Width: 9, HideBelow: 104},
			},
			Rows: rows, Selected: p.sel, Offset: p.offset, Width: iw, Height: tableH,
			Focused: true, SortCol: sortColumn(p.sortKey), SortDesc: p.desc,
		}
		body = append(body, t.Render()...)
	}
	panel.Body = body
	out := panel.Render()
	if sideW > 0 {
		out = c.HJoin(1, []string{out, p.detailPanel(v, sideW, v.h, true)}, []int{w, sideW})
	}
	return out
}

func processRow(r process.Info) []string {
	name := theme.C(theme.Text, r.Name)
	if r.Restricted {
		name = theme.C(theme.Subtle, r.Name) + theme.C(theme.Faint, " ⁎")
	}
	cpu := theme.C(theme.Faint, "—")
	if r.CPUOK {
		cpu = c.GaugeColor(r.CPU, 6, theme.Heat(r.CPU*2)) + " " + theme.C(theme.Heat(r.CPU*2), c.FitRight(fmt.Sprintf("%.1f%%", r.CPU), 6))
	}
	mem, memPct := theme.C(theme.Faint, "—"), theme.C(theme.Faint, "—")
	if r.MemOK {
		mem = theme.C(theme.Subtle, c.Bytes(r.RSS))
		memPct = theme.C(theme.Muted, fmt.Sprintf("%.1f", r.MemPct))
	}
	thr := theme.C(theme.Muted, "—")
	if r.Threads > 0 {
		thr = theme.C(theme.Muted, itoa(int(r.Threads)))
	}
	return []string{theme.C(theme.Muted, itoa(int(r.PID))), name, cpu, mem, memPct, thr, theme.C(theme.Muted, r.Status)}
}

func (p *processesView) detailPanel(v vctx, w, h int, focused bool) string {
	panel := c.Panel{Title: "Details", Marker: theme.ProcHue, Width: w, Height: h, Focused: focused}
	iw, ih := c.InnerWidth(w), c.InnerHeight(h)
	info, ok := p.selected()
	if !ok {
		panel.Body = c.Empty(iw, ih, "∅", "No process selected", "")
		return panel.Render()
	}
	lw := 10
	val := func(s string) string { return theme.C(theme.Text, s) }
	body := []string{
		theme.B(theme.Text, info.Name) + theme.C(theme.Muted, "  pid "+itoa(int(info.PID))),
		"",
	}
	cpu, mem := theme.C(theme.Faint, "unavailable"), theme.C(theme.Faint, "unavailable")
	if info.CPUOK {
		cpu = theme.B(theme.Heat(info.CPU*2), fmt.Sprintf("%.1f%%", info.CPU)) + " " + c.GaugeColor(info.CPU, max(iw-lw-8, 4), theme.Heat(info.CPU*2))
	}
	if info.MemOK {
		mem = val(c.Bytes(info.RSS)) + theme.C(theme.Muted, fmt.Sprintf("  %.1f%% of RAM", info.MemPct))
	}
	body = append(body, kv("cpu", lw, cpu), kv("memory", lw, mem))
	if info.Threads > 0 {
		body = append(body, kv("threads", lw, val(itoa(int(info.Threads)))))
	}
	if !info.Started.IsZero() {
		body = append(body, kv("started", lw, val(info.Started.Format("Jan 02 15:04"))+theme.C(theme.Muted, "  "+c.Ago(info.Started, v.now))))
	}
	body = append(body, kv("parent", lw, val(itoa(int(info.PPID)))))

	switch {
	case p.loading == info.PID && p.details == nil:
		body = append(body, "", theme.C(theme.Muted, v.spin+" loading details…"))
	case p.detailsErr != "":
		body = append(body, "", errLine(p.detailsErr, iw))
	case p.details != nil && p.details.PID == info.PID:
		d := p.details
		field := func(label, name, value string) {
			if e, bad := d.FieldErrors[name]; bad {
				body = append(body, kv(label, lw, theme.C(theme.Faint, e)))
				return
			}
			if value == "" {
				return
			}
			lines := wrap(value, iw-lw)
			body = append(body, kv(label, lw, val(lines[0])))
			for _, l := range lines[1:min(len(lines), 4)] {
				body = append(body, strings.Repeat(" ", lw)+val(l))
			}
		}
		body = append(body, "")
		field("user", "user", d.Username)
		if d.Status != "" {
			body = append(body, kv("status", lw, val(d.Status)))
		}
		field("exe", "exe", d.Exe)
		field("command", "cmdline", d.Cmdline)
		field("cwd", "cwd", d.Cwd)
		if d.NumFDs > 0 {
			body = append(body, kv("open fds", lw, val(itoa(int(d.NumFDs)))))
		}
		if len(d.Children) > 0 {
			kids := make([]string, 0, len(d.Children))
			for _, k := range d.Children[:min(len(d.Children), 8)] {
				kids = append(kids, itoa(int(k)))
			}
			body = append(body, kv("children", lw, val(strings.Join(kids, " "))))
		}
		if d.Restricted {
			body = append(body, "", theme.C(theme.Faint, "⁎ some fields need elevated privileges"))
		}
	}
	if len(body) < ih {
		for len(body) < ih-1 {
			body = append(body, "")
		}
		body = append(body, theme.C(theme.Muted, "x terminate · esc close"))
	}
	panel.Body = body
	return panel.Render()
}

func (p *processesView) modal(v vctx) string {
	if p.confirm != nil {
		info := p.confirm
		w := min(v.w-4, 62)
		iw := c.InnerWidth(w)
		body := []string{
			"",
			c.Center(theme.B(theme.Text, "Terminate "+info.Name+"?"), iw),
			c.Center(theme.C(theme.Muted, "pid "+itoa(int(info.PID))), iw),
			"",
		}
		for _, l := range wrap("Pulse will ask the process to exit (SIGTERM on Linux/macOS, TerminateProcess on Windows). Unsaved work in it may be lost.", iw) {
			body = append(body, theme.C(theme.Subtle, l))
		}
		body = append(body, "",
			c.Center(c.Pill("y", theme.Crit)+theme.C(theme.Text, " terminate     ")+c.Pill("n", theme.BorderHi)+theme.C(theme.Text, " cancel"), iw), "")
		return c.Panel{Title: "Confirm", Marker: theme.Crit, Body: body, Width: w, Height: len(body) + 2, Focused: true}.Render()
	}
	if p.detail && v.w < 120 {
		return p.detailPanel(v, min(v.w-4, 70), min(v.h-2, 22), true)
	}
	return ""
}

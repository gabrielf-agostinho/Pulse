package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/config"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

const (
	scrOverview = iota
	scrProcesses
	scrServices
	scrAlerts
	scrHistory
	scrSettings
	numScreens
)

var screenNames = [numScreens]string{"Overview", "Processes", "Services", "Alerts", "History", "Settings"}

type vctx struct {
	st   *monitoring.State
	now  time.Time
	w, h int
	mode c.Mode
	cfg  *config.Config
	spin string
}

type screen interface {
	update(m *Model, msg tea.Msg) tea.Cmd
	view(v vctx) string
	bindings() []key.Binding
	capturing() bool
	modal(v vctx) string
}

type (
	stateMsg struct{ st monitoring.State }
	tickMsg  time.Time
	toastMsg struct {
		text string
		err  bool
	}
)

type Model struct {
	be   Backend
	cfg  config.Config
	mode c.Mode

	width, height int
	active        int
	screens       [numScreens]screen

	st         monitoring.State
	haveState  bool
	paused     bool
	lastUpdate time.Time
	now        time.Time

	showHelp bool
	help     help.Model
	spinner  spinner.Model

	toast      string
	toastErr   bool
	toastUntil time.Time

	minFrame time.Duration
}

func New(be Backend) *Model {
	cfg := be.Config()
	mode := c.Braille
	if cfg.UI.Charts == "blocks" {
		mode = c.Blocks
	}
	h := help.New()
	h.ShortSeparator = theme.C(theme.Faint, " · ")
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(theme.Subtle)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(theme.Muted)
	h.Styles.Ellipsis = lipgloss.NewStyle().Foreground(theme.Faint)

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(theme.Accent)))
	m := &Model{
		be: be, cfg: cfg, mode: mode, help: h, spinner: sp, now: time.Now(),
		minFrame: time.Second / time.Duration(max(cfg.UI.FPS, 1)),
	}
	m.screens = [numScreens]screen{
		&overview{},
		newProcesses(),
		&servicesView{},
		&alertsView{sev: -1},
		newHistory(),
		newSettings(),
	}
	for i, n := range config.Screens {
		if n == cfg.UI.StartScreen {
			m.active = i
		}
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.waitForState(time.Time{}),
		tick(),
		m.spinner.Tick,
		m.screens[scrHistory].(*historyView).init(m),
	)
}

func (m *Model) waitForState(last time.Time) tea.Cmd {
	be, minFrame := m.be, m.minFrame
	return func() tea.Msg {
		<-be.Changed()
		if wait := minFrame - time.Since(last); wait > 0 {
			time.Sleep(wait)
		}
		return stateMsg{st: be.Snapshot()}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func toast(text string, err bool) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, err: err} }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		return m, nil

	case stateMsg:
		now := time.Now()
		if !m.paused {
			m.st = msg.st
			m.haveState = true
			m.lastUpdate = now
			m.now = now
			m.broadcastState()
		}
		return m, m.waitForState(now)

	case tickMsg:
		m.now = time.Time(msg)
		return m, tick()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case toastMsg:
		m.toast, m.toastErr, m.toastUntil = msg.text, msg.err, time.Now().Add(4*time.Second)
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	var cmds []tea.Cmd
	for _, s := range m.screens {
		cmds = append(cmds, s.update(m, msg))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) broadcastState() {
	for _, s := range m.screens {
		if r, ok := s.(interface{ refresh(m *Model) }); ok {
			r.refresh(m)
		}
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	scr := m.screens[m.active]
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
		return tea.Quit
	}
	if m.showHelp {
		if key.Matches(msg, keys.Help, keys.Back, keys.Quit) {
			m.showHelp = false
		}
		return nil
	}
	if scr.capturing() || scr.modal(m.vctx()) != "" {
		return scr.update(m, msg)
	}
	switch {
	case key.Matches(msg, keys.Quit):
		return tea.Quit
	case key.Matches(msg, keys.Help):
		m.showHelp = true
		return nil
	case key.Matches(msg, keys.Screens):
		return m.switchTo(int(msg.Runes[0] - '1'))
	case key.Matches(msg, keys.Next):
		return m.switchTo((m.active + 1) % numScreens)
	case key.Matches(msg, keys.Prev):
		return m.switchTo((m.active + numScreens - 1) % numScreens)
	case key.Matches(msg, keys.Pause):
		m.paused = !m.paused
		if m.paused {
			return toast("Paused (still collecting)", false)
		}
		return toast("Live updates resumed", false)
	case key.Matches(msg, keys.Refresh):
		m.be.Refresh()
		cmd := scr.update(m, msg)
		return tea.Batch(cmd, toast("Refreshing all collectors…", false))
	}
	return scr.update(m, msg)
}

func (m *Model) switchTo(i int) tea.Cmd {
	if i < 0 || i >= numScreens || i == m.active {
		return nil
	}
	m.active = i
	if a, ok := m.screens[i].(interface{ activated(m *Model) tea.Cmd }); ok {
		return a.activated(m)
	}
	return nil
}

func (m *Model) vctx() vctx {
	return vctx{st: &m.st, now: m.now, w: m.width, h: m.bodyHeight(), mode: m.mode, cfg: &m.cfg, spin: m.spinner.View()}
}

func (m *Model) bodyHeight() int {
	h := m.height - 3
	if m.height >= 26 {
		h--
	}
	return max(h, 0)
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 44 || m.height < 12 {
		return m.tooSmall()
	}
	v := m.vctx()
	var body string
	if !m.haveState {
		body = strings.Join(c.Empty(v.w, v.h, "◌", v.spin+" Starting collectors…", ""), "\n")
	} else {
		body = m.screens[m.active].view(v)
	}
	if mod := m.screens[m.active].modal(v); mod != "" {
		body = c.Overlay(body, mod, v.w, v.h)
	}
	if m.showHelp {
		body = c.Overlay(body, m.helpView(v), v.w, v.h)
	}
	parts := []string{m.header()}
	if m.height >= 26 {
		parts = append(parts, "")
	}
	parts = append(parts, c.Box(body, m.width, v.h), m.hints(), m.statusBar())
	return c.Box(strings.Join(parts, "\n"), m.width, m.height)
}

func (m *Model) tooSmall() string {
	lines := c.Empty(m.width, m.height, "◆", "Pulse needs a bit more room",
		"at least 44×12, current "+itoa(m.width)+"×"+itoa(m.height))
	return strings.Join(c.BoxLines(lines, m.width, m.height), "\n")
}

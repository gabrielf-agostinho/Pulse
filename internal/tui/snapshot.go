package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type Snapshot struct {
	model *Model
}

func NewSnapshot(be Backend, w, h int) *Snapshot {
	m := New(be)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	s := &Snapshot{model: m}
	s.drive(m.screens[scrHistory].(*historyView).init(m))
	return s
}

func (s *Snapshot) Render(screen int, keys []tea.KeyMsg) string {
	m := s.model
	m.Update(stateMsg{st: m.be.Snapshot()})
	m.now = time.Now()
	s.drive(m.switchTo(screen))
	for _, k := range keys {
		_, cmd := m.Update(k)
		s.drive(cmd)
	}
	m.Update(stateMsg{st: m.be.Snapshot()})
	m.toast = ""
	return m.View()
}

func (s *Snapshot) drive(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(1500 * time.Millisecond):
		return
	}
	if strings.HasPrefix(fmt.Sprintf("%T", msg), "cursor.") {
		return
	}
	switch msg := msg.(type) {
	case nil, stateMsg, tickMsg, spinner.TickMsg:
		return
	case tea.BatchMsg:
		for _, c := range msg {
			s.drive(c)
		}
	default:
		_, next := s.model.Update(msg)
		s.drive(next)
	}
}

func ParseKeys(spec string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for _, k := range strings.Fields(spec) {
		switch k {
		case "enter":
			out = append(out, tea.KeyMsg{Type: tea.KeyEnter})
		case "tab":
			out = append(out, tea.KeyMsg{Type: tea.KeyTab})
		case "down":
			out = append(out, tea.KeyMsg{Type: tea.KeyDown})
		case "up":
			out = append(out, tea.KeyMsg{Type: tea.KeyUp})
		case "right":
			out = append(out, tea.KeyMsg{Type: tea.KeyRight})
		case "esc":
			out = append(out, tea.KeyMsg{Type: tea.KeyEsc})
		default:
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
	return out
}

func ScreenIndex(name string) int {
	for i, n := range screenNames {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	return -1
}

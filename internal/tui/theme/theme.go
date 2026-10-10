package theme

import (
	"math"
	"sync"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

var (
	Surface   = lipgloss.Color("#151A23")
	Selection = lipgloss.Color("#1E2A38")
	Border    = lipgloss.Color("#323C4E")
	BorderHi  = lipgloss.Color("#3A4558")
	Text      = lipgloss.Color("#E6EAF2")
	Subtle    = lipgloss.Color("#A3ACBD")
	Muted     = lipgloss.Color("#6E798C")
	Faint     = lipgloss.Color("#3C4659")
	Ink       = lipgloss.Color("#0B0F16")

	Brand1 = lipgloss.Color("#34D399")
	Brand2 = lipgloss.Color("#22D3EE")
	Accent = lipgloss.Color("#5EEAD4")

	OK   = lipgloss.Color("#34D399")
	Warn = lipgloss.Color("#FBBF24")
	Crit = lipgloss.Color("#F87171")
	Info = lipgloss.Color("#60A5FA")

	CPU      = lipgloss.Color("#22D3EE")
	CPUDeep  = lipgloss.Color("#0E7490")
	Mem      = lipgloss.Color("#A78BFA")
	MemDeep  = lipgloss.Color("#5B3FB8")
	Swap     = lipgloss.Color("#F0ABFC")
	Rx       = lipgloss.Color("#2DD4BF")
	RxDeep   = lipgloss.Color("#0F766E")
	Tx       = lipgloss.Color("#818CF8")
	TxDeep   = lipgloss.Color("#3730A3")
	Read     = lipgloss.Color("#60A5FA")
	Write    = lipgloss.Color("#F472B6")
	Latency  = lipgloss.Color("#38BDF8")
	LatDeep  = lipgloss.Color("#075985")
	ProcHue  = lipgloss.Color("#FCD34D")
	HistHue  = lipgloss.Color("#34D399")
	HistDeep = lipgloss.Color("#065F46")
)

var styles sync.Map

func Fg(c lipgloss.Color) lipgloss.Style {
	if s, ok := styles.Load(c); ok {
		return s.(lipgloss.Style)
	}
	s := lipgloss.NewStyle().Foreground(c)
	styles.Store(c, s)
	return s
}

func C(c lipgloss.Color, s string) string { return Fg(c).Render(s) }

func B(c lipgloss.Color, s string) string { return Fg(c).Bold(true).Render(s) }

func OnBg(fg, bg lipgloss.Color, s string) string {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Render(s)
}

func Blend(a, b lipgloss.Color, t float64) lipgloss.Color {
	ca, err1 := colorful.Hex(string(a))
	cb, err2 := colorful.Hex(string(b))
	if err1 != nil || err2 != nil {
		return a
	}
	t = math.Max(0, math.Min(1, t))
	return lipgloss.Color(ca.BlendOkLab(cb, t).Clamped().Hex())
}

var heatStops = []struct {
	at float64
	c  lipgloss.Color
}{
	{0, lipgloss.Color("#34D399")},
	{0.55, lipgloss.Color("#A3E635")},
	{0.75, lipgloss.Color("#FBBF24")},
	{0.9, lipgloss.Color("#FB923C")},
	{1, lipgloss.Color("#F87171")},
}

func Heat(pct float64) lipgloss.Color {
	t := math.Max(0, math.Min(1, pct/100))
	for i := 1; i < len(heatStops); i++ {
		if t <= heatStops[i].at {
			lo, hi := heatStops[i-1], heatStops[i]
			return Blend(lo.c, hi.c, (t-lo.at)/(hi.at-lo.at))
		}
	}
	return heatStops[len(heatStops)-1].c
}

type Level uint8

const (
	LevelNone Level = iota
	LevelOK
	LevelInfo
	LevelWarn
	LevelCrit
)

func (l Level) Color() lipgloss.Color {
	switch l {
	case LevelOK:
		return OK
	case LevelInfo:
		return Info
	case LevelWarn:
		return Warn
	case LevelCrit:
		return Crit
	}
	return Muted
}

func Gradient(s string, from, to lipgloss.Color, bold bool) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	out := ""
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		st := Fg(Blend(from, to, t))
		if bold {
			st = st.Bold(true)
		}
		out += st.Render(string(r))
	}
	return out
}

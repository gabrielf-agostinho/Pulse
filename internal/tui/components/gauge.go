package components

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

func Gauge(pct float64, w int) string {
	return gauge(pct, w, func(t float64) lipgloss.Color { return theme.Heat(t * 100) })
}

func GaugeColor(pct float64, w int, c lipgloss.Color) string {
	return gauge(pct, w, func(float64) lipgloss.Color { return c })
}

func gauge(pct float64, w int, color func(float64) lipgloss.Color) string {
	if w <= 0 {
		return ""
	}
	if math.IsNaN(pct) {
		return theme.C(theme.Faint, strings.Repeat("╌", w))
	}
	halves := int(math.Round(math.Max(0, math.Min(100, pct)) / 100 * float64(w*2)))
	if halves == 0 && pct > 0 {
		halves = 1
	}
	var b strings.Builder
	full := halves / 2
	for i := 0; i < full; i++ {
		b.WriteString(theme.C(color(float64(i)/float64(max(w-1, 1))), "━"))
	}
	used := full
	if halves%2 == 1 {
		b.WriteString(theme.C(color(float64(full)/float64(max(w-1, 1))), "╸"))
		used++
	}
	if rest := w - used; rest > 0 {
		b.WriteString(theme.C(theme.Faint, strings.Repeat("━", rest)))
	}
	return b.String()
}

func Spark(values []float64, w int, maxV float64, c lipgloss.Color) string {
	if w <= 0 {
		return ""
	}
	cols := Resample(values, w)
	if maxV <= 0 {
		maxV = maxOf(values)
		if maxV <= 0 {
			maxV = 1
		}
	}
	var b strings.Builder
	for _, v := range cols {
		if math.IsNaN(v) {
			b.WriteByte(' ')
			continue
		}
		idx := int(math.Round(math.Max(0, math.Min(1, v/maxV)) * 7))
		b.WriteRune(eighths[idx+1])
	}
	return theme.C(c, b.String())
}

func Dot(level theme.Level) string {
	if level == theme.LevelNone {
		return theme.C(theme.Muted, "○")
	}
	return theme.C(level.Color(), "●")
}

func Pill(text string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(theme.Ink).Background(bg).Bold(true).Render(" " + text + " ")
}

func Key(key, desc string) string {
	return theme.C(theme.Subtle, key) + " " + theme.C(theme.Muted, desc)
}

package components

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type Mode uint8

const (
	Braille Mode = iota
	Blocks
)

type Gradient struct{ Lo, Hi lipgloss.Color }

func Resample(values []float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	if n <= 0 || len(values) == 0 {
		return out
	}
	if len(values) <= n {
		copy(out[n-len(values):], values)
		return out
	}
	step := float64(len(values)) / float64(n)
	for i := 0; i < n; i++ {
		lo := int(math.Floor(float64(i) * step))
		hi := int(math.Floor(float64(i+1) * step))
		hi = max(hi, lo+1)
		sum, cnt := 0.0, 0
		for _, v := range values[lo:min(hi, len(values))] {
			if !math.IsNaN(v) {
				sum += v
				cnt++
			}
		}
		if cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

var dotBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

func rowColors(h int, g Gradient) []lipgloss.Color {
	out := make([]lipgloss.Color, h)
	for r := range out {
		t := 1.0
		if h > 1 {
			t = 1 - float64(r)/float64(h-1)
		}
		out[r] = theme.Blend(g.Lo, g.Hi, t)
	}
	return out
}

func Area(values []float64, w, h int, maxV float64, g Gradient, mode Mode) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	if maxV <= 0 {
		maxV = maxOf(values)
		if maxV <= 0 {
			maxV = 1
		}
	}
	if mode == Blocks {
		return blockArea(Resample(values, w), w, h, maxV, g)
	}
	cols := Resample(values, w*2)
	grid := make([][]rune, h)
	for r := range grid {
		grid[r] = make([]rune, w)
	}
	dotsH := h * 4
	for x, v := range cols {
		if math.IsNaN(v) {
			continue
		}
		filled := int(math.Round(math.Max(0, math.Min(1, v/maxV)) * float64(dotsH)))
		if filled == 0 && v > 0 {
			filled = 1
		}
		for d := 0; d < filled; d++ {
			y := dotsH - 1 - d
			grid[y/4][x/2] |= dotBits[x%2][y%4]
		}
	}
	colors := rowColors(h, g)
	out := make([]string, h)
	for r := range grid {
		out[r] = paintRow(grid[r], colors[r], 0x2800)
	}
	return out
}

func Mirror(up, down []float64, w, h int, maxV float64, gu, gd Gradient, mode Mode) []string {
	if w <= 0 || h < 2 {
		return Area(up, w, h, maxV, gu, mode)
	}
	if maxV <= 0 {
		maxV = math.Max(maxOf(up), maxOf(down))
		if maxV <= 0 {
			maxV = 1
		}
	}
	hu := h / 2
	hd := h - hu
	top := Area(up, w, hu, maxV, gu, mode)
	bottom := areaFlipped(down, w, hd, maxV, Gradient{Lo: gd.Lo, Hi: gd.Hi}, mode)
	return append(top, bottom...)
}

func areaFlipped(values []float64, w, h int, maxV float64, g Gradient, mode Mode) []string {
	if mode == Blocks {
		cols := Resample(values, w)
		grid := make([][]rune, h)
		for r := range grid {
			grid[r] = []rune(strings.Repeat(" ", w))
		}
		for x, v := range cols {
			if math.IsNaN(v) {
				continue
			}
			cells := math.Max(0, math.Min(1, v/maxV)) * float64(h)
			for r := 0; r < h; r++ {
				fill := cells - float64(r)
				switch {
				case fill >= 1:
					grid[r][x] = '█'
				case fill > 0.5:
					grid[r][x] = '▀'
				case fill > 0:
					grid[r][x] = '▔'
				}
			}
		}
		colors := rowColors(h, g)
		out := make([]string, h)
		for r := range grid {
			out[r] = paintRow(grid[r], colors[h-1-r], 0)
		}
		return out
	}
	cols := Resample(values, w*2)
	grid := make([][]rune, h)
	for r := range grid {
		grid[r] = make([]rune, w)
	}
	dotsH := h * 4
	for x, v := range cols {
		if math.IsNaN(v) {
			continue
		}
		filled := int(math.Round(math.Max(0, math.Min(1, v/maxV)) * float64(dotsH)))
		if filled == 0 && v > 0 {
			filled = 1
		}
		for y := 0; y < filled; y++ {
			grid[y/4][x/2] |= dotBits[x%2][y%4]
		}
	}
	colors := rowColors(h, g)
	out := make([]string, h)
	for r := range grid {
		out[r] = paintRow(grid[r], colors[h-1-r], 0x2800)
	}
	return out
}

var eighths = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func blockArea(cols []float64, w, h int, maxV float64, g Gradient) []string {
	grid := make([][]rune, h)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(" ", w))
	}
	for x, v := range cols {
		if math.IsNaN(v) {
			continue
		}
		units := int(math.Round(math.Max(0, math.Min(1, v/maxV)) * float64(h*8)))
		if units == 0 && v > 0 {
			units = 1
		}
		for r := h - 1; r >= 0 && units > 0; r-- {
			n := min(units, 8)
			grid[r][x] = eighths[n]
			units -= n
		}
	}
	colors := rowColors(h, g)
	out := make([]string, h)
	for r := range grid {
		out[r] = paintRow(grid[r], colors[r], 0)
	}
	return out
}

func paintRow(cells []rune, c lipgloss.Color, base rune) string {
	var b strings.Builder
	st := theme.Fg(c)
	run := strings.Builder{}
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(st.Render(run.String()))
			run.Reset()
		}
	}
	for _, cell := range cells {
		empty := cell == 0 || cell == ' '
		if empty {
			flush()
			b.WriteByte(' ')
			continue
		}
		run.WriteRune(base + cell)
	}
	flush()
	return b.String()
}

func maxOf(values []float64) float64 {
	m := 0.0
	for _, v := range values {
		if !math.IsNaN(v) && v > m {
			m = v
		}
	}
	return m
}

func Axis(lines []string, top, mid, bottom string, xLeft, xMid, xRight string, w int) []string {
	lw := max(Width(top), Width(mid), Width(bottom))
	out := make([]string, 0, len(lines)+1)
	axis := theme.Fg(theme.Faint)
	label := theme.Fg(theme.Muted)
	midRow := len(lines) / 2
	for i, l := range lines {
		lab, tick := strings.Repeat(" ", lw), "│"
		switch i {
		case 0:
			lab, tick = FitRight(top, lw), "┤"
		case len(lines) - 1:
			lab, tick = FitRight(bottom, lw), "┤"
		case midRow:
			if mid != "" {
				lab, tick = FitRight(mid, lw), "┤"
			}
		}
		out = append(out, label.Render(lab)+axis.Render(tick)+Fit(l, w-lw-1))
	}
	if xLeft != "" || xRight != "" {
		inner := w - lw - 2
		row := Spread(xLeft, xRight, inner)
		if xMid != "" && inner > Width(xLeft)+Width(xRight)+Width(xMid)+4 {
			left := Fit(xLeft, (inner-Width(xMid))/2)
			row = left + xMid
			row = Spread(row, xRight, inner)
		}
		out = append(out, strings.Repeat(" ", lw)+axis.Render("└")+label.Render(" "+Fit(row, inner)))
	}
	return out
}

func ChartWidth(w int, labels ...string) int {
	lw := 0
	for _, l := range labels {
		lw = max(lw, Width(l))
	}
	return max(w-lw-1, 1)
}

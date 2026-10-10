package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type Column struct {
	Title     string
	Width     int
	Right     bool
	Flex      bool
	HideBelow int
}

type Table struct {
	Columns  []Column
	Rows     [][]string
	Selected int
	Offset   int
	Width    int
	Height   int
	Focused  bool
	SortCol  int
	SortDesc bool
}

func (t Table) Visible() int { return max(t.Height-1, 0) }

func Clamp(selected, offset, visible, total int) int {
	if visible <= 0 {
		return 0
	}
	if selected < offset {
		offset = selected
	}
	if selected >= offset+visible {
		offset = selected - visible + 1
	}
	offset = min(offset, max(total-visible, 0))
	return max(offset, 0)
}

func (t Table) layout() ([]int, []bool) {
	widths := make([]int, len(t.Columns))
	show := make([]bool, len(t.Columns))
	scroll := 0
	if len(t.Rows) > t.Visible() {
		scroll = 1
	}
	avail := t.Width - 2 - scroll
	fixed, flex := 0, -1
	for i, c := range t.Columns {
		if c.HideBelow > 0 && t.Width < c.HideBelow {
			continue
		}
		show[i] = true
		if c.Flex {
			flex = i
			continue
		}
		widths[i] = c.Width
		fixed += c.Width + 1
	}
	if flex >= 0 {
		widths[flex] = max(avail-fixed, 4)
	}
	return widths, show
}

func (t Table) Render() []string {
	widths, show := t.layout()
	out := make([]string, 0, t.Height)

	var hdr strings.Builder
	hdr.WriteString("  ")
	first := true
	for i, c := range t.Columns {
		if !show[i] {
			continue
		}
		if !first {
			hdr.WriteByte(' ')
		}
		first = false
		title := c.Title
		if i == t.SortCol {
			if t.SortDesc {
				title += " ▼"
			} else {
				title += " ▲"
			}
		}
		cell := Fit(title, widths[i])
		if c.Right {
			cell = FitRight(title, widths[i])
		}
		color := theme.Muted
		if i == t.SortCol {
			color = theme.Accent
		}
		hdr.WriteString(theme.B(color, cell))
	}
	out = append(out, Fit(hdr.String(), t.Width))

	visible := t.Visible()
	thumbStart, thumbLen := scrollThumb(len(t.Rows), visible, t.Offset)
	for r := 0; r < visible; r++ {
		idx := t.Offset + r
		bar := ""
		if len(t.Rows) > visible {
			if r >= thumbStart && r < thumbStart+thumbLen {
				bar = theme.C(theme.BorderHi, "┃")
			} else {
				bar = theme.C(theme.Faint, "│")
			}
		}
		if idx >= len(t.Rows) {
			out = append(out, Fit("", t.Width-Width(bar))+bar)
			continue
		}
		var row strings.Builder
		selected := idx == t.Selected
		if selected {
			mark := theme.Muted
			if t.Focused {
				mark = theme.Accent
			}
			row.WriteString(theme.C(mark, "▌") + " ")
		} else {
			row.WriteString("  ")
		}
		first := true
		for i, c := range t.Columns {
			if !show[i] {
				continue
			}
			if !first {
				row.WriteByte(' ')
			}
			first = false
			cell := ""
			if i < len(t.Rows[idx]) {
				cell = t.Rows[idx][i]
			}
			if c.Right {
				row.WriteString(FitRight(cell, widths[i]))
			} else {
				row.WriteString(Fit(cell, widths[i]))
			}
		}
		line := Fit(row.String(), t.Width-Width(bar))
		if selected {
			line = Highlight(line, theme.Selection)
		}
		out = append(out, line+bar)
	}
	return out
}

func scrollThumb(total, visible, offset int) (int, int) {
	if total <= visible || visible <= 0 {
		return 0, 0
	}
	length := max(visible*visible/total, 1)
	start := offset * (visible - length) / max(total-visible, 1)
	return start, length
}

var bgCache = map[lipgloss.Color]string{}

func Highlight(line string, bg lipgloss.Color) string {
	prefix, ok := bgCache[bg]
	if !ok {
		probe := lipgloss.NewStyle().Background(bg).Render("\x00")
		prefix, _, _ = strings.Cut(probe, "\x00")
		bgCache[bg] = prefix
	}
	if prefix == "" {
		return lipgloss.NewStyle().Reverse(true).Render(line)
	}
	return prefix + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+prefix) + "\x1b[0m"
}

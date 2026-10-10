package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

type Panel struct {
	Title   string
	Marker  lipgloss.Color
	Badge   string
	Footer  string
	Body    []string
	Width   int
	Height  int
	Focused bool
}

func InnerWidth(w int) int { return max(w-4, 0) }

func InnerHeight(h int) int { return max(h-2, 0) }

func (p Panel) Render() string {
	w, h := p.Width, p.Height
	if w < 4 || h < 2 {
		return Box("", w, h)
	}
	bc := theme.Border
	tc := theme.Subtle
	if p.Focused {
		bc, tc = theme.Accent, theme.Text
	}
	border := theme.Fg(bc)

	title := ""
	if p.Title != "" {
		if p.Marker != "" {
			title = theme.C(p.Marker, "■") + " "
		}
		title += theme.B(tc, strings.ToUpper(p.Title))
		title = " " + title + " "
	}
	badge := ""
	if p.Badge != "" {
		badge = " " + p.Badge + " "
	}
	fill := w - 2 - 1 - Width(title) - Width(badge) - 1
	if fill < 1 {
		badge, fill = "", w-2-1-Width(title)-1
	}
	if fill < 1 {
		title = Fit(title, max(w-4, 0))
		fill = w - 2 - 1 - Width(title)
	}
	top := border.Render("╭─") + title + border.Render(strings.Repeat("─", max(fill, 0))) + badge + border.Render("─╮")
	top = Fit(top, w)

	iw, ih := InnerWidth(w), InnerHeight(h)
	body := BoxLines(p.Body, iw, ih)
	lines := make([]string, 0, h)
	lines = append(lines, top)
	side := border.Render("│")
	for _, l := range body {
		lines = append(lines, side+" "+l+" "+side)
	}
	bottom := ""
	if p.Footer != "" && Width(p.Footer)+6 < w {
		f := " " + p.Footer + " "
		bottom = border.Render("╰"+strings.Repeat("─", w-3-Width(f))) + f + border.Render("─╯")
	} else {
		bottom = border.Render("╰" + strings.Repeat("─", w-2) + "╯")
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

func Empty(w, h int, icon, title, hint string) []string {
	lines := make([]string, h)
	content := []string{theme.C(theme.Faint, icon), theme.C(theme.Subtle, title)}
	if hint != "" {
		content = append(content, theme.C(theme.Muted, hint))
	}
	start := max((h-len(content))/2, 0)
	for i := range lines {
		lines[i] = strings.Repeat(" ", w)
	}
	for i, c := range content {
		if start+i < h {
			lines[start+i] = Center(c, w)
		}
	}
	return lines
}

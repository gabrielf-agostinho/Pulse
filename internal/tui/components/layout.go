package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func Width(s string) int { return ansi.StringWidth(s) }

func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := Width(s)
	if sw > w {
		s = ansi.Truncate(s, w, "…")
		sw = Width(s)
	}
	if sw < w {
		s += strings.Repeat(" ", w-sw)
	}
	return s
}

func FitRight(s string, w int) string {
	sw := Width(s)
	if sw >= w {
		return Fit(s, w)
	}
	return strings.Repeat(" ", w-sw) + s
}

func Center(s string, w int) string {
	sw := Width(s)
	if sw >= w {
		return Fit(s, w)
	}
	left := (w - sw) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-sw-left)
}

func Spread(left, right string, w int) string {
	lw, rw := Width(left), Width(right)
	if lw+rw+1 > w {
		if rw >= w {
			return Fit(right, w)
		}
		return Fit(left, w-rw-1) + " " + right
	}
	return left + strings.Repeat(" ", w-lw-rw) + right
}

func Lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func Box(s string, w, h int) string {
	return strings.Join(BoxLines(Lines(s), w, h), "\n")
}

func BoxLines(lines []string, w, h int) []string {
	out := make([]string, h)
	blank := strings.Repeat(" ", max(w, 0))
	for i := range out {
		if i < len(lines) {
			out[i] = Fit(lines[i], w)
		} else {
			out[i] = blank
		}
	}
	return out
}

func HJoin(gap int, blocks []string, widths []int) string {
	h := 0
	split := make([][]string, len(blocks))
	for i, b := range blocks {
		split[i] = Lines(b)
		h = max(h, len(split[i]))
	}
	pad := strings.Repeat(" ", gap)
	var sb strings.Builder
	for row := 0; row < h; row++ {
		for i := range blocks {
			if i > 0 {
				sb.WriteString(pad)
			}
			line := ""
			if row < len(split[i]) {
				line = split[i][row]
			}
			sb.WriteString(Fit(line, widths[i]))
		}
		if row < h-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func VJoin(blocks ...string) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b != "" {
			parts = append(parts, b)
		}
	}
	return strings.Join(parts, "\n")
}

func Split(total int, weights ...int) []int {
	sum := 0
	for _, w := range weights {
		sum += w
	}
	out := make([]int, len(weights))
	if sum == 0 || total <= 0 {
		return out
	}
	used := 0
	for i, w := range weights {
		out[i] = total * w / sum
		used += out[i]
	}
	for i := 0; used < total; i = (i + 1) % len(out) {
		out[i]++
		used++
	}
	return out
}

func Overlay(bg, fg string, w, h int) string {
	bgl := BoxLines(Lines(bg), w, h)
	fgl := Lines(fg)
	fw := 0
	for _, l := range fgl {
		fw = max(fw, Width(l))
	}
	fw = min(fw, w)
	top := max((h-len(fgl))/2, 0)
	left := max((w-fw)/2, 0)
	for i, l := range fgl {
		row := top + i
		if row >= h {
			break
		}
		base := bgl[row]
		leftPart := ansi.Truncate(base, left, "")
		rightPart := ansi.TruncateLeft(base, left+fw, "")
		bgl[row] = leftPart + "\x1b[0m" + Fit(l, fw) + "\x1b[0m" + rightPart
	}
	return strings.Join(bgl, "\n")
}

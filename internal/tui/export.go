package tui

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func ExportHTML(frame, title string, cols, rows int) string {
	const cellW, cellH = 9, 19
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title><style>
html,body{margin:0;background:#06080c}
.win{display:inline-block;margin:0;border-radius:10px;overflow:hidden;background:#0b0f16;
  box-shadow:0 0 0 1px #1f2633}
.bar{height:30px;background:#121722;display:flex;align-items:center;padding:0 14px;gap:8px;
  font:12px "Segoe UI",system-ui,sans-serif;color:#6e798c}
.dot{width:12px;height:12px;border-radius:50%%}
.ttl{flex:1;text-align:center;margin-right:52px}
.term{padding:12px 14px;font-family:"Cascadia Mono","Cascadia Code","JetBrains Mono",Menlo,Consolas,monospace;
  font-size:15.5px;color:#e6eaf2;white-space:pre}
.l{height:%dpx;line-height:%dpx}
.l span{display:inline-block;width:%dpx;height:%dpx;text-align:center;overflow:visible;vertical-align:top}
.b{font-weight:700}
</style></head><body><div class="win"><div class="bar">
<span class="dot" style="background:#f87171"></span><span class="dot" style="background:#fbbf24"></span>
<span class="dot" style="background:#34d399"></span><span class="ttl">%s</span></div><div class="term">`,
		html.EscapeString(title), cellH, cellH, cellW, cellH, html.EscapeString(title))

	lines := strings.Split(frame, "\n")
	for r := 0; r < rows; r++ {
		line := ""
		if r < len(lines) {
			line = lines[r]
		}
		b.WriteString(`<div class="l">`)
		writeCells(&b, line, cols)
		b.WriteString("</div>")
	}
	b.WriteString("</div></div></body></html>")
	return b.String()
}

type sgr struct {
	fg, bg string
	bold   bool
	rev    bool
}

func writeCells(b *strings.Builder, line string, cols int) {
	var st sgr
	n := 0
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	state := byte(0)
	for len(line) > 0 && n < cols {
		seq, width, consumed, newState := ansi.DecodeSequence(line, state, p)
		state = newState
		line = line[consumed:]
		if width == 0 {
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				st = applySGR(st, seq[2:len(seq)-1])
			}
			continue
		}
		fg, bg := st.fg, st.bg
		if st.rev {
			fg, bg = bg, fg
			if fg == "" {
				fg = "#0b0f16"
			}
			if bg == "" {
				bg = "#e6eaf2"
			}
		}
		style := ""
		if fg != "" {
			style += "color:" + fg + ";"
		}
		if bg != "" {
			style += "background:" + bg + ";"
		}
		cls := ""
		if st.bold {
			cls = ` class="b"`
		}
		text := html.EscapeString(seq)
		if seq == " " {
			text = "&nbsp;"
		}
		if style != "" {
			fmt.Fprintf(b, `<span%s style="%s">%s</span>`, cls, style, text)
		} else {
			fmt.Fprintf(b, `<span%s>%s</span>`, cls, text)
		}
		n += width
	}
	for ; n < cols; n++ {
		b.WriteString("<span>&nbsp;</span>")
	}
}

func applySGR(st sgr, params string) sgr {
	if params == "" {
		return sgr{}
	}
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		v, _ := strconv.Atoi(ps[i])
		switch {
		case v == 0:
			st = sgr{}
		case v == 1:
			st.bold = true
		case v == 22:
			st.bold = false
		case v == 7:
			st.rev = true
		case v == 27:
			st.rev = false
		case v == 39:
			st.fg = ""
		case v == 49:
			st.bg = ""
		case (v == 38 || v == 48) && i+4 < len(ps) && ps[i+1] == "2":
			r, _ := strconv.Atoi(ps[i+2])
			g, _ := strconv.Atoi(ps[i+3])
			bl, _ := strconv.Atoi(ps[i+4])
			col := fmt.Sprintf("#%02x%02x%02x", r, g, bl)
			if v == 38 {
				st.fg = col
			} else {
				st.bg = col
			}
			i += 4
		}
	}
	return st
}

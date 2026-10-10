package components

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

var g = Gradient{Lo: theme.CPUDeep, Hi: theme.CPU}

func assertBox(t *testing.T, name string, lines []string, w, h int) {
	t.Helper()
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want %d", name, len(lines), h)
	}
	for i, l := range lines {
		if got := Width(l); got != w {
			t.Fatalf("%s: line %d width %d, want %d: %q", name, i, got, w, ansi.Strip(l))
		}
	}
}

func TestFitAndSpread(t *testing.T) {
	if Fit("hello", 3) != "he…" || Width(Fit("hi", 6)) != 6 {
		t.Fatal("fit")
	}
	if s := Spread("a", "b", 5); s != "a   b" {
		t.Fatalf("spread = %q", s)
	}
	if Width(Fit(theme.C(theme.OK, "colourful text"), 8)) != 8 {
		t.Fatal("fit must ignore ANSI")
	}
}

func TestSplitSumsToTotal(t *testing.T) {
	for _, total := range []int{7, 80, 133} {
		parts := Split(total, 3, 2, 2)
		sum := 0
		for _, p := range parts {
			sum += p
		}
		if sum != total {
			t.Fatalf("split(%d) = %v", total, parts)
		}
	}
}

func TestResampleRightAlignsAndAverages(t *testing.T) {
	r := Resample([]float64{1, 2}, 4)
	if !math.IsNaN(r[0]) || !math.IsNaN(r[1]) || r[2] != 1 || r[3] != 2 {
		t.Fatalf("right align = %v", r)
	}
	r = Resample([]float64{1, 3, 5, 7}, 2)
	if r[0] != 2 || r[1] != 6 {
		t.Fatalf("average = %v", r)
	}
}

func TestChartsHaveExactDimensions(t *testing.T) {
	vals := make([]float64, 300)
	for i := range vals {
		vals[i] = 50 + 50*math.Sin(float64(i)/10)
	}
	for _, mode := range []Mode{Braille, Blocks} {
		assertBox(t, "area", Area(vals, 40, 6, 100, g, mode), 40, 6)
		assertBox(t, "mirror", Mirror(vals, vals, 40, 7, 0, g, g, mode), 40, 7)
		assertBox(t, "area-empty", Area(nil, 10, 3, 0, g, mode), 10, 3)
	}
	ax := Axis(Area(vals, 30, 5, 100, g, Braille), "100%", "50%", "0%", "-5m", "", "now", 35)
	assertBox(t, "axis", ax, 35, 6)
}

func TestAreaFullAndEmpty(t *testing.T) {
	full := Area([]float64{100, 100}, 1, 2, 100, g, Braille)
	if !strings.Contains(full[0], "⣿") || !strings.Contains(full[1], "⣿") {
		t.Fatalf("full = %q", full)
	}
	zero := Area([]float64{0, 0}, 1, 2, 100, g, Braille)
	if strings.TrimSpace(ansi.Strip(zero[0]+zero[1])) != "" {
		t.Fatal("zero values draw nothing")
	}
	tiny := Area([]float64{0.1, 0.1}, 1, 1, 100, g, Braille)
	if strings.TrimSpace(ansi.Strip(tiny[0])) == "" {
		t.Fatal("tiny non-zero values stay visible")
	}
}

func TestGaugeAndSpark(t *testing.T) {
	for _, p := range []float64{0, 1, 33.3, 50, 99.9, 100, 140, math.NaN()} {
		if w := Width(Gauge(p, 12)); w != 12 {
			t.Fatalf("gauge(%v) width %d", p, w)
		}
	}
	if strings.Count(ansi.Strip(Gauge(50, 10)), "━") != 10 {
		t.Fatal("gauge uses a full track")
	}
	if Width(Spark([]float64{1, 2, 3}, 8, 0, theme.CPU)) != 8 {
		t.Fatal("spark width")
	}
}

func TestPanelAndTableFitExactly(t *testing.T) {
	p := Panel{Title: "cpu", Marker: theme.CPU, Badge: "23%", Body: []string{"a very long line that must be truncated cleanly", "b"}, Width: 30, Height: 6, Focused: true}
	assertBox(t, "panel", Lines(p.Render()), 30, 6)
	plain := Lines(Panel{Title: "no badge", Width: 30, Height: 4}.Render())
	if !strings.HasSuffix(ansi.Strip(plain[0]), "─╮") || Width(plain[0]) != 30 {
		t.Fatalf("top border must reach the corner: %q", ansi.Strip(plain[0]))
	}
	tiny := Panel{Title: "a very long title indeed", Badge: "badge", Width: 12, Height: 3}
	assertBox(t, "tiny panel", Lines(tiny.Render()), 12, 3)

	rows := make([][]string, 50)
	for i := range rows {
		rows[i] = []string{"1234", theme.C(theme.OK, "process-name"), "12%"}
	}
	tb := Table{
		Columns:  []Column{{Title: "PID", Width: 6, Right: true}, {Title: "NAME", Flex: true}, {Title: "CPU", Width: 5, Right: true, HideBelow: 30}},
		Rows:     rows,
		Selected: 3, Width: 40, Height: 10, SortCol: 2, SortDesc: true,
	}
	assertBox(t, "table", tb.Render(), 40, 10)
	tb.Width = 20
	assertBox(t, "narrow table", tb.Render(), 20, 10)
}

func TestClamp(t *testing.T) {
	if Clamp(15, 0, 10, 50) != 6 || Clamp(2, 6, 10, 50) != 2 || Clamp(49, 45, 10, 50) != 40 {
		t.Fatal("clamp")
	}
}

func TestOverlayKeepsDimensions(t *testing.T) {
	bg := strings.Repeat(strings.Repeat("x", 40)+"\n", 9) + strings.Repeat("x", 40)
	fg := Panel{Title: "modal", Width: 20, Height: 4}.Render()
	out := Lines(Overlay(bg, fg, 40, 10))
	assertBox(t, "overlay", out, 40, 10)
	if !strings.Contains(ansi.Strip(out[4]), "MODAL") && !strings.Contains(ansi.Strip(out[3]), "MODAL") {
		t.Fatalf("modal not drawn:\n%s", ansi.Strip(strings.Join(out, "\n")))
	}
}

func TestFormatting(t *testing.T) {
	cases := map[string]string{
		Bytes(0):                        "0 B",
		Bytes(1536):                     "1.50 KiB",
		Bytes(10 << 30):                 "10.0 GiB",
		Rate(2.5 * 1024 * 1024):         "2.50 MiB/s",
		Pct(42.42):                      "42%",
		Pct(4.24):                       "4.2%",
		Duration(90 * time.Hour):        "3d 18h",
		Duration(75 * time.Second):      "1m 15s",
		Millis(1500 * time.Microsecond): "1.5ms",
		Count(1234567):                  "1,234,567",
		Span(5 * time.Minute):           "5m",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if NiceCeil(73) != 100 || NiceCeil(0.3) != 0.5 || NiceCeil(180) != 200 {
		t.Fatal("nice ceil")
	}
}

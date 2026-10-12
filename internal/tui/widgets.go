package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
	c "github.com/gabrielagostinho/pulse/internal/tui/components"
	"github.com/gabrielagostinho/pulse/internal/tui/theme"
)

func rateShort(v float64) string {
	units := []string{"B", "K", "M", "G", "T"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 10 || v == math.Trunc(v) {
		return fmt.Sprintf("%.0f%s", v, units[i])
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}

func serviceLevel(s service.Status) theme.Level {
	switch s {
	case service.StatusHealthy:
		return theme.LevelOK
	case service.StatusDegraded:
		return theme.LevelWarn
	case service.StatusDown:
		return theme.LevelCrit
	}
	return theme.LevelNone
}

func severityLevel(s alert.Severity) theme.Level {
	switch s {
	case alert.SeverityCritical:
		return theme.LevelCrit
	case alert.SeverityWarning:
		return theme.LevelWarn
	}
	return theme.LevelInfo
}

func sevTag(s alert.Severity) string {
	switch s {
	case alert.SeverityCritical:
		return theme.B(theme.Crit, "◆ CRIT")
	case alert.SeverityWarning:
		return theme.B(theme.Warn, "▲ WARN")
	}
	return theme.B(theme.Info, "● INFO")
}

func sevGlyph(s alert.Severity) string {
	switch s {
	case alert.SeverityCritical:
		return theme.C(theme.Crit, "◆")
	case alert.SeverityWarning:
		return theme.C(theme.Warn, "▲")
	}
	return theme.C(theme.Info, "●")
}

func healthStrip(hist []service.CheckResult, w int) string {
	if w <= 0 {
		return ""
	}
	start := max(len(hist)-w, 0)
	var b strings.Builder
	for i := 0; i < w-(len(hist)-start); i++ {
		b.WriteString(theme.C(theme.Faint, "·"))
	}
	for _, h := range hist[start:] {
		b.WriteString(theme.C(serviceLevel(h.Status).Color(), "▌"))
	}
	return b.String()
}

func latencies(hist []service.CheckResult) []float64 {
	out := make([]float64, len(hist))
	for i, h := range hist {
		if h.OK {
			out[i] = float64(h.Latency) / float64(time.Millisecond)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

func metricLabel(key string) string {
	name, rest, _ := strings.Cut(key, "{")
	labels := strings.TrimSuffix(rest, "}")
	base := map[string]string{
		metrics.CPUUsage:       "CPU usage",
		metrics.CPUCoreUsage:   "CPU core",
		metrics.CPULoad1:       "Load average (1m)",
		metrics.MemUsedPct:     "Memory used",
		metrics.MemUsedBytes:   "Memory used (bytes)",
		metrics.SwapUsedPct:    "Swap used",
		metrics.DiskUsedPct:    "Volume used",
		metrics.DiskReadBps:    "Disk read",
		metrics.DiskWriteBps:   "Disk write",
		metrics.NetRxBps:       "Network in",
		metrics.NetTxBps:       "Network out",
		metrics.ServiceUp:      "Service up",
		metrics.ServiceLatency: "Service latency",
		metrics.ProcessCount:   "Process count",
	}[name]
	if base == "" {
		base = name
	}
	if labels != "" {
		var vals []string
		for _, kv := range strings.Split(labels, ",") {
			if _, v, ok := strings.Cut(kv, "="); ok {
				vals = append(vals, v)
			}
		}
		base += " · " + strings.Join(vals, " ")
	}
	return base
}

func metricColor(key string) c.Gradient {
	name, _, _ := strings.Cut(key, "{")
	switch name {
	case metrics.CPUUsage, metrics.CPUCoreUsage, metrics.CPULoad1:
		return c.Gradient{Lo: theme.CPUDeep, Hi: theme.CPU}
	case metrics.MemUsedPct, metrics.MemUsedBytes, metrics.SwapUsedPct:
		return c.Gradient{Lo: theme.MemDeep, Hi: theme.Mem}
	case metrics.NetRxBps:
		return c.Gradient{Lo: theme.RxDeep, Hi: theme.Rx}
	case metrics.NetTxBps:
		return c.Gradient{Lo: theme.TxDeep, Hi: theme.Tx}
	case metrics.DiskReadBps:
		return c.Gradient{Lo: theme.Blend(theme.Read, theme.Ink, 0.6), Hi: theme.Read}
	case metrics.DiskWriteBps:
		return c.Gradient{Lo: theme.Blend(theme.Write, theme.Ink, 0.6), Hi: theme.Write}
	case metrics.ServiceLatency:
		return c.Gradient{Lo: theme.LatDeep, Hi: theme.Latency}
	}
	return c.Gradient{Lo: theme.HistDeep, Hi: theme.HistHue}
}

func formatValue(key string, v float64) string {
	if math.IsNaN(v) {
		return "—"
	}
	name, _, _ := strings.Cut(key, "{")
	switch {
	case strings.HasSuffix(name, "_bps"):
		return c.Rate(v)
	case strings.HasSuffix(name, "_bytes"):
		return c.BytesF(v)
	case strings.HasSuffix(name, "_pct"), name == metrics.CPUUsage, name == metrics.CPUCoreUsage:
		return c.Pct(v)
	case strings.HasSuffix(name, "_ms"):
		return c.Millis(time.Duration(v * float64(time.Millisecond)))
	case name == metrics.ServiceUp:
		if v >= 0.5 {
			return "up"
		}
		return "down"
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func axisMax(key string, peak float64) (float64, string) {
	name, _, _ := strings.Cut(key, "{")
	switch {
	case strings.HasSuffix(name, "_pct"), name == metrics.CPUUsage, name == metrics.CPUCoreUsage:
		return 100, "100%"
	case strings.HasSuffix(name, "_bps"):
		m := c.NiceBytesCeil(peak)
		return m, rateShort(m)
	case strings.HasSuffix(name, "_bytes"):
		m := c.NiceBytesCeil(peak)
		return m, rateShort(m)
	case name == metrics.ServiceUp:
		return 1, "up"
	case strings.HasSuffix(name, "_ms"):
		m := c.NiceCeil(peak)
		return m, fmt.Sprintf("%.0fms", m)
	}
	m := c.NiceCeil(peak)
	return m, formatValue(key, m)
}

func spanOf(pts []metrics.Point, now time.Time) time.Duration {
	if len(pts) == 0 {
		return 0
	}
	return now.Sub(pts[0].At)
}

func errLine(msg string, w int) string {
	return c.Fit(theme.C(theme.Crit, "✗ ")+theme.C(theme.Subtle, msg), w)
}

func warnLine(msg string, w int) string {
	return c.Fit(theme.C(theme.Warn, "▲ ")+theme.C(theme.Subtle, msg), w)
}

func kv(label string, labelW int, value string) string {
	return c.Fit(theme.C(theme.Muted, label), labelW) + value
}

func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		r := []rune(para)
		for len(r) > w {
			out = append(out, string(r[:w]))
			r = r[w:]
		}
		out = append(out, string(r))
	}
	return out
}

func colorOr(cond bool, a, b lipgloss.Color) lipgloss.Color {
	if cond {
		return a
	}
	return b
}

var selfPID = os.Getpid()

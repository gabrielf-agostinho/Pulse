package components

import (
	"fmt"
	"math"
	"strings"
	"time"
)

var iec = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}

func Bytes(n uint64) string { return BytesF(float64(n)) }

func BytesF(v float64) string {
	if v < 0 || math.IsNaN(v) {
		return "—"
	}
	i := 0
	for v >= 1024 && i < len(iec)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%.0f %s", v, iec[i])
	case v >= 100:
		return fmt.Sprintf("%.0f %s", v, iec[i])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, iec[i])
	}
	return fmt.Sprintf("%.2f %s", v, iec[i])
}

func Rate(v float64) string { return BytesF(v) + "/s" }

func Pct(v float64) string {
	if v >= 99.95 {
		return "100%"
	}
	if v >= 10 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

func Millis(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	switch {
	case ms >= 10000:
		return fmt.Sprintf("%.0fs", ms/1000)
	case ms >= 1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 100:
		return fmt.Sprintf("%.0fms", ms)
	case ms >= 10:
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fms", ms)
}

func Elapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d.Seconds())
	days, s := s/86400, s%86400
	hours, s := s/3600, s%3600
	mins, secs := s/60, s%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %02ds", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

func Ago(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	if d < time.Second {
		return "just now"
	}
	return Duration(d) + " ago"
}

func Span(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(math.Round(d.Hours()/24)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(math.Round(d.Hours())))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%dm", int(math.Round(d.Minutes())))
	}
	return fmt.Sprintf("%ds", int(math.Round(d.Seconds())))
}

func Count(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 4 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func NiceCeil(v float64) float64 {
	if v <= 0 || math.IsNaN(v) {
		return 1
	}
	exp := math.Floor(math.Log10(v))
	base := math.Pow(10, exp)
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if v <= m*base {
			return m * base
		}
	}
	return 10 * base
}

func NiceBytesCeil(v float64) float64 {
	if v <= 1024 {
		return 1024
	}
	unit := 1.0
	for v/unit >= 1024 {
		unit *= 1024
	}
	return NiceCeil(v/unit) * unit
}

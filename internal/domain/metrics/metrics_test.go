package metrics

import (
	"math"
	"testing"
	"time"
)

func TestSeriesKeySortsLabels(t *testing.T) {
	got := SeriesKey("disk.used_pct", map[string]string{"mount": "/", "dev": "sda1"})
	if want := "disk.used_pct{dev=sda1,mount=/}"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := SeriesKey("cpu.usage", nil); got != "cpu.usage" {
		t.Fatalf("unlabelled key = %q", got)
	}
}

func TestCPUPercent(t *testing.T) {
	prev := CPUTimes{User: 10, System: 5, Idle: 85}
	cur := CPUTimes{User: 20, System: 10, Idle: 105, Iowait: 5}
	pct, ok := CPUPercent(prev, cur)
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(pct-37.5) > 1e-9 {
		t.Fatalf("pct = %v", pct)
	}
	if _, ok := CPUPercent(cur, cur); ok {
		t.Fatal("zero elapsed must not be ok")
	}
	if _, ok := CPUPercent(cur, prev); ok {
		t.Fatal("counters going backwards must not be ok")
	}
}

func TestRate(t *testing.T) {
	r, ok := Rate(1000, 3000, 2*time.Second)
	if !ok || r != 1000 {
		t.Fatalf("rate = %v ok=%v", r, ok)
	}
	if _, ok := Rate(3000, 1000, time.Second); ok {
		t.Fatal("counter reset must not be ok")
	}
	if _, ok := Rate(0, 10, 0); ok {
		t.Fatal("zero elapsed must not be ok")
	}
}

func TestPercent(t *testing.T) {
	if Percent(1, 0) != 0 {
		t.Fatal("zero whole")
	}
	if Percent(50, 200) != 25 {
		t.Fatal("25%")
	}
	if Percent(300, 200) != 100 {
		t.Fatal("clamp")
	}
}

func TestRingEvictsOldest(t *testing.T) {
	r := NewRing(3)
	if _, ok := r.Last(); ok {
		t.Fatal("empty ring has no last")
	}
	for i := 1; i <= 5; i++ {
		r.Push(Point{Value: float64(i)})
	}
	vals := r.Values()
	if len(vals) != 3 || vals[0] != 3 || vals[2] != 5 {
		t.Fatalf("values = %v", vals)
	}
	last, _ := r.Last()
	if last.Value != 5 {
		t.Fatalf("last = %v", last.Value)
	}
	c := r.Clone()
	r.Push(Point{Value: 6})
	if c.Values()[2] != 5 {
		t.Fatal("clone must be independent")
	}
}

func TestHostUptime(t *testing.T) {
	now := time.Now()
	h := Host{BootTime: now.Add(-90 * time.Minute)}
	if h.Uptime(now) != 90*time.Minute {
		t.Fatal("uptime")
	}
	if (Host{}).Uptime(now) != 0 {
		t.Fatal("unknown boot time")
	}
}

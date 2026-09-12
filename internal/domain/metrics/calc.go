package metrics

import "time"

type CPUTimes struct {
	User, System, Idle, Nice, Iowait, Irq, Softirq, Steal float64
}

func (t CPUTimes) Busy() float64 {
	return t.User + t.System + t.Nice + t.Irq + t.Softirq + t.Steal
}

func (t CPUTimes) Total() float64 {
	return t.Busy() + t.Idle + t.Iowait
}

func CPUPercent(prev, cur CPUTimes) (pct float64, ok bool) {
	dTotal := cur.Total() - prev.Total()
	dBusy := cur.Busy() - prev.Busy()
	if dTotal <= 0 || dBusy < 0 {
		return 0, false
	}
	return clampPct(dBusy / dTotal * 100), true
}

func Rate(prev, cur uint64, elapsed time.Duration) (rate float64, ok bool) {
	if elapsed <= 0 || cur < prev {
		return 0, false
	}
	return float64(cur-prev) / elapsed.Seconds(), true
}

func Percent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return clampPct(float64(part) / float64(whole) * 100)
}

func clampPct(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return v
}

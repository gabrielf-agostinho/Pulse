package cpu

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/platform"
)

type Collector struct {
	prev      []metrics.CPUTimes
	prevTotal metrics.CPUTimes
	now       func() time.Time
}

func New() *Collector { return &Collector{now: time.Now} }

func (c *Collector) Collect(ctx context.Context) (metrics.CPU, error) {
	out := metrics.CPU{Section: metrics.Section{At: c.now()}}
	per, err := cpu.TimesWithContext(ctx, true)
	if err != nil {
		return out, fmt.Errorf("cpu times: %w", err)
	}
	if len(per) == 0 {
		return out, fmt.Errorf("cpu times: no processors reported")
	}
	cur := make([]metrics.CPUTimes, len(per))
	var total metrics.CPUTimes
	for i, t := range per {
		ct := convert(t)
		cur[i] = ct
		total = add(total, ct)
	}

	if len(c.prev) == len(cur) {
		out.PerCore = make([]float64, len(cur))
		for i := range cur {
			out.PerCore[i], _ = metrics.CPUPercent(c.prev[i], cur[i])
		}
		if pct, ok := metrics.CPUPercent(c.prevTotal, total); ok {
			out.Total = pct
			out.Status = metrics.StatusOK
		}
	}
	c.prev, c.prevTotal = cur, total

	if platform.LoadAvgSupported {
		if avg, err := load.AvgWithContext(ctx); err == nil {
			out.Load = [3]float64{avg.Load1, avg.Load5, avg.Load15}
			out.LoadSupported = true
		}
	}
	return out, nil
}

func convert(t cpu.TimesStat) metrics.CPUTimes {
	return metrics.CPUTimes{
		User: t.User, System: t.System, Idle: t.Idle, Nice: t.Nice,
		Iowait: t.Iowait, Irq: t.Irq, Softirq: t.Softirq, Steal: t.Steal,
	}
}

func add(a, b metrics.CPUTimes) metrics.CPUTimes {
	return metrics.CPUTimes{
		User: a.User + b.User, System: a.System + b.System, Idle: a.Idle + b.Idle, Nice: a.Nice + b.Nice,
		Iowait: a.Iowait + b.Iowait, Irq: a.Irq + b.Irq, Softirq: a.Softirq + b.Softirq, Steal: a.Steal + b.Steal,
	}
}

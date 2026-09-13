package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/mem"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

type Collector struct{}

func New() *Collector { return &Collector{} }

func (c *Collector) Collect(ctx context.Context) (metrics.Memory, error) {
	now := time.Now()
	out := metrics.Memory{Section: metrics.Section{At: now}}
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return out, fmt.Errorf("virtual memory: %w", err)
	}
	out.Total, out.Available = vm.Total, vm.Available
	if vm.Available <= vm.Total {
		out.Used = vm.Total - vm.Available
	} else {
		out.Used = vm.Used
	}
	out.UsedPct = metrics.Percent(out.Used, out.Total)
	out.Status = metrics.StatusOK

	out.Swap.At = now
	sw, err := mem.SwapMemoryWithContext(ctx)
	switch {
	case err != nil:
		out.Swap.Status, out.Swap.Err = metrics.StatusError, err.Error()
	default:
		out.Swap.Status = metrics.StatusOK
		out.Swap.Total, out.Swap.Used = sw.Total, sw.Used
		out.Swap.UsedPct = metrics.Percent(sw.Used, sw.Total)
	}
	return out, nil
}

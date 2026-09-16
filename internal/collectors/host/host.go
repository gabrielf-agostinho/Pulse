package host

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

type Collector struct {
	model             string
	physical, logical int
	cpuInfoLoaded     bool
}

func New() *Collector { return &Collector{} }

func (c *Collector) Collect(ctx context.Context) (metrics.Host, error) {
	out := metrics.Host{Section: metrics.Section{At: time.Now()}, Arch: runtime.GOARCH}
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return out, fmt.Errorf("host info: %w", err)
	}
	out.Hostname = info.Hostname
	out.OS = info.OS
	out.Platform = prettyPlatform(info.Platform)
	out.PlatformVersion = info.PlatformVersion
	out.KernelVersion = info.KernelVersion
	if info.KernelArch != "" {
		out.Arch = info.KernelArch
	}
	if info.BootTime > 0 {
		out.BootTime = time.Unix(int64(info.BootTime), 0)
	}

	if !c.cpuInfoLoaded {
		c.loadCPUInfo(ctx)
	}
	out.CPUModel, out.PhysicalCores, out.LogicalCores = c.model, c.physical, c.logical
	out.Status = metrics.StatusOK
	return out, nil
}

func (c *Collector) loadCPUInfo(ctx context.Context) {
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		c.logical = n
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		c.physical = n
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		c.model = strings.Join(strings.Fields(infos[0].ModelName), " ")
	}
	c.cpuInfoLoaded = c.logical > 0
}

func prettyPlatform(p string) string {
	if p == "" {
		return p
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

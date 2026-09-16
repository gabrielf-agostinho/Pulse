package disk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

const usageTimeout = 1500 * time.Millisecond

const partitionsTTL = 30 * time.Second

type Options struct {
	ExcludeFSTypes []string
	ExcludeMounts  []string
}

type Collector struct {
	opts Options

	parts     []disk.PartitionStat
	partsAt   time.Time
	prevRead  uint64
	prevWrite uint64
	prevAt    time.Time

	mu       sync.Mutex
	inflight map[string]bool
}

func New(opts Options) *Collector {
	return &Collector{opts: opts, inflight: map[string]bool{}}
}

func (c *Collector) Collect(ctx context.Context) (metrics.Disk, error) {
	now := time.Now()
	out := metrics.Disk{Section: metrics.Section{At: now}}

	if c.parts == nil || now.Sub(c.partsAt) > partitionsTTL {
		parts, err := disk.PartitionsWithContext(ctx, false)
		if err != nil && len(parts) == 0 {
			return out, fmt.Errorf("partitions: %w", err)
		}
		c.parts, c.partsAt = c.filter(parts), now
	}

	type result struct {
		i   int
		vol metrics.Volume
		ok  bool
	}
	results := make(chan result, len(c.parts))
	pending := 0
	busy := map[int]bool{}
	for i, p := range c.parts {
		if !c.claim(p.Mountpoint) {
			busy[i] = true
			continue
		}
		pending++
		go func(i int, p disk.PartitionStat) {
			defer c.release(p.Mountpoint)
			uctx, cancel := context.WithTimeout(ctx, usageTimeout)
			defer cancel()
			u, err := disk.UsageWithContext(uctx, p.Mountpoint)
			v := metrics.Volume{Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype}
			if err != nil || u.Total == 0 {
				results <- result{i: i, vol: v}
				return
			}
			v.Total, v.Used, v.Free = u.Total, u.Used, u.Free
			v.UsedPct = metrics.Percent(u.Used, u.Total)
			results <- result{i: i, vol: v, ok: true}
		}(i, p)
	}

	timeout := time.NewTimer(usageTimeout + 200*time.Millisecond)
	defer timeout.Stop()
	got := map[int]metrics.Volume{}
	answered := map[int]bool{}
collect:
	for pending > 0 {
		select {
		case r := <-results:
			pending--
			answered[r.i] = true
			if r.ok {
				got[r.i] = r.vol
			}
		case <-timeout.C:
			break collect
		case <-ctx.Done():
			break collect
		}
	}
	for i, p := range c.parts {
		switch {
		case got[i].Mount != "":
			out.Volumes = append(out.Volumes, got[i])
		case busy[i] || !answered[i]:
			out.Volumes = append(out.Volumes, metrics.Volume{Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype, Err: "not responding"})
		}
	}
	sort.SliceStable(out.Volumes, func(i, j int) bool { return out.Volumes[i].Mount < out.Volumes[j].Mount })
	out.Status = metrics.StatusOK

	out.IO = c.io(ctx, now)
	return out, nil
}

func (c *Collector) io(ctx context.Context, now time.Time) metrics.DiskIO {
	io := metrics.DiskIO{Section: metrics.Section{At: now}}
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			io.Status, io.Err = metrics.StatusError, err.Error()
		} else {
			io.Status, io.Err = metrics.StatusUnsupported, err.Error()
		}
		return io
	}
	var read, write uint64
	for name, ct := range counters {
		if !countDevice(name) {
			continue
		}
		read += ct.ReadBytes
		write += ct.WriteBytes
	}
	if !c.prevAt.IsZero() {
		elapsed := now.Sub(c.prevAt)
		r, okR := metrics.Rate(c.prevRead, read, elapsed)
		w, okW := metrics.Rate(c.prevWrite, write, elapsed)
		if okR && okW {
			io.ReadBps, io.WriteBps, io.Primed = r, w, true
		}
	}
	c.prevRead, c.prevWrite, c.prevAt = read, write, now
	io.Status = metrics.StatusOK
	return io
}

func (c *Collector) filter(parts []disk.PartitionStat) []disk.PartitionStat {
	seen := map[string]bool{}
	mounts := map[string]bool{}
	out := make([]disk.PartitionStat, 0, len(parts))
	for _, p := range parts {
		fs := strings.ToLower(p.Fstype)
		switch {
		case p.Mountpoint == "" || mounts[p.Mountpoint]:
			continue
		case slices.Contains(c.opts.ExcludeFSTypes, fs),
			slices.Contains(c.opts.ExcludeMounts, p.Mountpoint),
			ignoreMount(p.Mountpoint):
			continue
		}
		if p.Device != "" && seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		mounts[p.Mountpoint] = true
		out = append(out, p)
	}
	return out
}

func (c *Collector) claim(mount string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inflight[mount] {
		return false
	}
	c.inflight[mount] = true
	return true
}

func (c *Collector) release(mount string) {
	c.mu.Lock()
	delete(c.inflight, mount)
	c.mu.Unlock()
}

package network

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/net"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

const ifaceTTL = 30 * time.Second

type ifaceInfo struct {
	up, loopback bool
	addrs        []string
}

type Collector struct {
	exclude []string

	prev   map[string][2]uint64
	prevAt time.Time

	info   map[string]ifaceInfo
	infoAt time.Time
}

func New(exclude []string) *Collector {
	return &Collector{exclude: exclude, prev: map[string][2]uint64{}}
}

func (c *Collector) Collect(ctx context.Context) (metrics.Network, error) {
	now := time.Now()
	out := metrics.Network{Section: metrics.Section{At: now}}
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return out, fmt.Errorf("network counters: %w", err)
	}
	if c.info == nil || now.Sub(c.infoAt) > ifaceTTL {
		c.refreshInfo(ctx, now)
	}

	elapsed := now.Sub(c.prevAt)
	primed := !c.prevAt.IsZero()
	next := make(map[string][2]uint64, len(counters))
	for _, ct := range counters {
		if slices.Contains(c.exclude, ct.Name) {
			continue
		}
		info, known := c.info[ct.Name]
		ifc := metrics.Interface{
			Name: ct.Name, RxTotal: ct.BytesRecv, TxTotal: ct.BytesSent,
			Up: !known || info.up, Loopback: info.loopback || isLoopbackName(ct.Name), Addrs: info.addrs,
		}
		next[ct.Name] = [2]uint64{ct.BytesRecv, ct.BytesSent}
		if p, ok := c.prev[ct.Name]; ok && primed {
			rx, okR := metrics.Rate(p[0], ct.BytesRecv, elapsed)
			tx, okT := metrics.Rate(p[1], ct.BytesSent, elapsed)
			if okR && okT {
				ifc.RxBps, ifc.TxBps = rx, tx
			}
		}
		if ifc.RxTotal == 0 && ifc.TxTotal == 0 {
			continue
		}
		if !ifc.Loopback && ifc.Up {
			out.RxBps += ifc.RxBps
			out.TxBps += ifc.TxBps
			out.RxTotal += ifc.RxTotal
			out.TxTotal += ifc.TxTotal
		}
		out.Interfaces = append(out.Interfaces, ifc)
	}
	sort.SliceStable(out.Interfaces, func(i, j int) bool {
		a, b := out.Interfaces[i], out.Interfaces[j]
		if a.Loopback != b.Loopback {
			return !a.Loopback
		}
		if ta, tb := a.RxBps+a.TxBps, b.RxBps+b.TxBps; ta != tb {
			return ta > tb
		}
		return a.RxTotal+a.TxTotal > b.RxTotal+b.TxTotal
	})
	c.prev, c.prevAt = next, now
	out.Primed = primed
	out.Status = metrics.StatusOK
	if !primed {
		out.Status = metrics.StatusPending
	}
	return out, nil
}

func (c *Collector) refreshInfo(ctx context.Context, now time.Time) {
	ifs, err := net.InterfacesWithContext(ctx)
	if err != nil {
		return
	}
	info := make(map[string]ifaceInfo, len(ifs))
	for _, i := range ifs {
		ii := ifaceInfo{up: slices.Contains(i.Flags, "up"), loopback: slices.Contains(i.Flags, "loopback")}
		for _, a := range i.Addrs {
			ii.addrs = append(ii.addrs, a.Addr)
		}
		info[i.Name] = ii
	}
	c.info, c.infoAt = info, now
}

func isLoopbackName(name string) bool {
	n := strings.ToLower(name)
	return n == "lo" || n == "lo0" || strings.HasPrefix(n, "loopback")
}

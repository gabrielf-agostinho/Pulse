package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	gproc "github.com/shirou/gopsutil/v4/process"

	"github.com/gabrielagostinho/pulse/internal/domain/process"
)

type prevTimes struct {
	created int64
	cpu     float64
}

type identity struct {
	created int64
	name    string
}

type Collector struct {
	memTotal func() uint64
	numCPU   int

	prev   map[int32]prevTimes
	prevAt time.Time
	names  map[int32]identity
}

func New(memTotal func() uint64) *Collector {
	return &Collector{
		memTotal: memTotal,
		numCPU:   max(runtime.NumCPU(), 1),
		prev:     map[int32]prevTimes{},
		names:    map[int32]identity{},
	}
}

func (c *Collector) Collect(ctx context.Context) (process.Snapshot, error) {
	now := time.Now()
	snap := process.Snapshot{At: now}
	procs, err := gproc.ProcessesWithContext(ctx)
	if err != nil {
		snap.Err = err.Error()
		return snap, fmt.Errorf("list processes: %w", err)
	}
	var memTotal uint64
	if c.memTotal != nil {
		memTotal = c.memTotal()
	}
	elapsed := now.Sub(c.prevAt).Seconds()
	fast := basics()
	nextPrev := make(map[int32]prevTimes, len(procs))
	nextNames := make(map[int32]identity, len(procs))
	snap.Processes = make([]process.Info, 0, len(procs))

	for i, p := range procs {
		if i%64 == 0 && ctx.Err() != nil {
			return snap, ctx.Err()
		}
		info := process.Info{PID: p.Pid}
		created, cerr := p.CreateTimeWithContext(ctx)
		if cerr == nil {
			info.Started = time.UnixMilli(created)
		}
		b, haveBasic := fast[p.Pid]
		if id, ok := c.names[p.Pid]; ok && id.created == created && id.name != "" {
			info.Name = id.name
		} else if haveBasic && b.name != "" {
			info.Name = b.name
		} else if name, err := p.NameWithContext(ctx); err == nil {
			info.Name = name
		} else {
			info.Restricted = true
		}
		if info.Name == "" {
			info.Name = fmt.Sprintf("[pid %d]", p.Pid)
		}
		nextNames[p.Pid] = identity{created: created, name: info.Name}

		if t, err := p.TimesWithContext(ctx); err == nil {
			total := t.User + t.System
			if prev, ok := c.prev[p.Pid]; ok && prev.created == created && elapsed > 0 && total >= prev.cpu {
				info.CPU = min((total-prev.cpu)/elapsed/float64(c.numCPU)*100, 100)
				info.CPUOK = true
			}
			nextPrev[p.Pid] = prevTimes{created: created, cpu: total}
		} else {
			info.Restricted = true
		}
		if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
			info.RSS, info.MemOK = m.RSS, true
			if memTotal > 0 {
				info.MemPct = float64(m.RSS) / float64(memTotal) * 100
			}
		} else {
			info.Restricted = true
		}
		if haveBasic {
			info.Threads, info.PPID = b.threads, b.ppid
		} else {
			if n, err := p.NumThreadsWithContext(ctx); err == nil {
				info.Threads = n
			}
			if pp, err := p.PpidWithContext(ctx); err == nil {
				info.PPID = pp
			}
		}
		if st, err := p.StatusWithContext(ctx); err == nil && len(st) > 0 {
			info.Status = st[0]
		}
		if info.Restricted {
			snap.Restricted++
		}
		snap.Processes = append(snap.Processes, info)
	}
	c.prev, c.names, c.prevAt = nextPrev, nextNames, now
	snap.Total = len(snap.Processes)
	snap.OK = true
	return snap, nil
}

type Inspector struct{}

func (Inspector) Inspect(ctx context.Context, pid int32) (process.Details, error) {
	p, err := gproc.NewProcessWithContext(ctx, pid)
	if err != nil {
		return process.Details{}, fmt.Errorf("process %d: %w", pid, err)
	}
	d := process.Details{Info: process.Info{PID: pid}, FieldErrors: map[string]string{}}
	note := func(field string, err error) {
		if err != nil {
			d.FieldErrors[field] = permissionText(err)
		}
	}
	var e error
	d.Name, e = p.NameWithContext(ctx)
	note("name", e)
	d.Exe, e = p.ExeWithContext(ctx)
	note("exe", e)
	d.Cmdline, e = p.CmdlineWithContext(ctx)
	note("cmdline", e)
	d.Username, e = p.UsernameWithContext(ctx)
	note("user", e)
	d.Cwd, e = p.CwdWithContext(ctx)
	note("cwd", e)
	d.PPID, e = p.PpidWithContext(ctx)
	note("ppid", e)
	d.Threads, e = p.NumThreadsWithContext(ctx)
	note("threads", e)
	if created, err := p.CreateTimeWithContext(ctx); err == nil {
		d.Started = time.UnixMilli(created)
	} else {
		note("started", err)
	}
	if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
		d.RSS, d.MemOK = m.RSS, true
	} else {
		note("memory", err)
	}
	if st, err := p.StatusWithContext(ctx); err == nil && len(st) > 0 {
		d.Status = st[0]
	}
	if n, err := p.NumFDsWithContext(ctx); err == nil {
		d.NumFDs = n
	}
	if kids, err := p.ChildrenWithContext(ctx); err == nil {
		for _, k := range kids {
			d.Children = append(d.Children, k.Pid)
		}
	}
	d.Restricted = len(d.FieldErrors) > 0
	return d, nil
}

func (Inspector) Terminate(ctx context.Context, pid int32) error {
	if process.Protected(pid, int32(os.Getpid())) {
		return fmt.Errorf("process %d is protected", pid)
	}
	p, err := gproc.NewProcessWithContext(ctx, pid)
	if err != nil {
		return fmt.Errorf("process %d: %w", pid, err)
	}
	if err := p.TerminateWithContext(ctx); err != nil {
		return fmt.Errorf("terminate %d: %s", pid, permissionText(err))
	}
	return nil
}

func permissionText(err error) string {
	if errors.Is(err, os.ErrPermission) || isAccessDenied(err) {
		return "access denied (requires elevated privileges)"
	}
	if errors.Is(err, gproc.ErrorNotPermitted) {
		return "not supported on this platform"
	}
	return err.Error()
}

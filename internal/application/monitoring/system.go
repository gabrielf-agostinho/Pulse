package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
)

type Collector[T any] interface {
	Collect(ctx context.Context) (T, error)
}

type SampleSink func(ctx context.Context, samples []metrics.Sample)

type Monitor struct {
	Hub  *Hub
	Sink SampleSink
	Log  *slog.Logger
}

func Run[T any](m *Monitor, c Collector[T], apply func(*State, T, error, int), samples func(T) []metrics.Sample) func(context.Context) error {
	return func(ctx context.Context) error {
		v, err := c.Collect(ctx)
		if err != nil && errors.Is(ctx.Err(), context.Canceled) {
			return err
		}
		m.Hub.Update(func(s *State) { apply(s, v, err, m.Hub.ringCap) })
		if err == nil && samples != nil && m.Sink != nil {
			if ss := samples(v); len(ss) > 0 {
				m.Sink(ctx, ss)
			}
		}
		return err
	}
}

func failed(sec *metrics.Section, err error) {
	sec.Status = metrics.StatusError
	sec.Err = err.Error()
}

func ApplyCPU(s *State, v metrics.CPU, err error, capacity int) {
	if err != nil {
		failed(&s.CPU.Section, err)
		return
	}
	s.CPU = v
	if v.OK() {
		s.push(metrics.CPUUsage, capacity, v.At, v.Total)
		for i, pct := range v.PerCore {
			s.push(coreKey(i), capacity, v.At, pct)
		}
	}
}

func coreKey(i int) string {
	return metrics.SeriesKey(metrics.CPUCoreUsage, map[string]string{"core": strconv.Itoa(i)})
}

func CoreKey(i int) string { return coreKey(i) }

func CPUSamples(v metrics.CPU) []metrics.Sample {
	if !v.OK() {
		return nil
	}
	out := []metrics.Sample{{Name: metrics.CPUUsage, Value: v.Total, Unit: metrics.UnitPercent, Source: "cpu", At: v.At}}
	if v.LoadSupported {
		out = append(out, metrics.Sample{Name: metrics.CPULoad1, Value: v.Load[0], Source: "cpu", At: v.At})
	}
	return out
}

func ApplyMemory(s *State, v metrics.Memory, err error, capacity int) {
	if err != nil {
		failed(&s.Memory.Section, err)
		return
	}
	s.Memory = v
	s.push(metrics.MemUsedPct, capacity, v.At, v.UsedPct)
	if v.Swap.OK() && v.Swap.Total > 0 {
		s.push(metrics.SwapUsedPct, capacity, v.At, v.Swap.UsedPct)
	}
}

func MemorySamples(v metrics.Memory) []metrics.Sample {
	out := []metrics.Sample{
		{Name: metrics.MemUsedPct, Value: v.UsedPct, Unit: metrics.UnitPercent, Source: "memory", At: v.At},
		{Name: metrics.MemUsedBytes, Value: float64(v.Used), Unit: metrics.UnitBytes, Source: "memory", At: v.At},
	}
	if v.Swap.OK() && v.Swap.Total > 0 {
		out = append(out, metrics.Sample{Name: metrics.SwapUsedPct, Value: v.Swap.UsedPct, Unit: metrics.UnitPercent, Source: "memory", At: v.At})
	}
	return out
}

func ApplyDisk(s *State, v metrics.Disk, err error, capacity int) {
	if err != nil {
		failed(&s.Disk.Section, err)
		return
	}
	s.Disk = v
	if v.IO.OK() && v.IO.Primed {
		s.push(metrics.DiskReadBps, capacity, v.At, v.IO.ReadBps)
		s.push(metrics.DiskWriteBps, capacity, v.At, v.IO.WriteBps)
	}
}

func DiskSamples(v metrics.Disk) []metrics.Sample {
	var out []metrics.Sample
	for _, vol := range v.Volumes {
		if vol.Err != "" || vol.Total == 0 {
			continue
		}
		out = append(out, metrics.Sample{
			Name: metrics.DiskUsedPct, Labels: map[string]string{"mount": vol.Mount},
			Value: vol.UsedPct, Unit: metrics.UnitPercent, Source: "disk", At: v.At,
		})
	}
	if v.IO.OK() && v.IO.Primed {
		out = append(out,
			metrics.Sample{Name: metrics.DiskReadBps, Value: v.IO.ReadBps, Unit: metrics.UnitBytesPerSec, Source: "disk", At: v.At},
			metrics.Sample{Name: metrics.DiskWriteBps, Value: v.IO.WriteBps, Unit: metrics.UnitBytesPerSec, Source: "disk", At: v.At},
		)
	}
	return out
}

func ApplyNetwork(s *State, v metrics.Network, err error, capacity int) {
	if err != nil {
		failed(&s.Network.Section, err)
		return
	}
	s.Network = v
	if v.Primed {
		s.push(metrics.NetRxBps, capacity, v.At, v.RxBps)
		s.push(metrics.NetTxBps, capacity, v.At, v.TxBps)
	}
}

func NetworkSamples(v metrics.Network) []metrics.Sample {
	if !v.Primed {
		return nil
	}
	return []metrics.Sample{
		{Name: metrics.NetRxBps, Value: v.RxBps, Unit: metrics.UnitBytesPerSec, Source: "network", At: v.At},
		{Name: metrics.NetTxBps, Value: v.TxBps, Unit: metrics.UnitBytesPerSec, Source: "network", At: v.At},
	}
}

func ApplyHost(s *State, v metrics.Host, err error, _ int) {
	if err != nil {
		failed(&s.Host.Section, err)
		return
	}
	s.Host = v
}

func ApplyProcesses(s *State, v process.Snapshot, err error, _ int) {
	if err != nil {
		s.Processes.OK = false
		s.Processes.Err = err.Error()
		return
	}
	s.Processes = v
}

func ProcessSamples(v process.Snapshot) []metrics.Sample {
	return []metrics.Sample{{Name: metrics.ProcessCount, Value: float64(v.Total), Unit: metrics.UnitCount, Source: "processes", At: v.At}}
}

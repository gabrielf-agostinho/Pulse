package metrics

import (
	"sort"
	"strings"
	"time"
)

type Status uint8

const (
	StatusPending Status = iota
	StatusOK
	StatusUnsupported
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusUnsupported:
		return "unsupported"
	case StatusError:
		return "error"
	default:
		return "pending"
	}
}

type Section struct {
	Status Status
	Err    string
	At     time.Time
}

func (s Section) OK() bool { return s.Status == StatusOK }

type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
	Unit   string
	Source string
	At     time.Time
}

func (s Sample) Key() string { return SeriesKey(s.Name, s.Labels) }

func SeriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	b.WriteByte('}')
	return b.String()
}

const (
	CPUUsage        = "cpu.usage"
	CPUCoreUsage    = "cpu.core.usage"
	CPULoad1        = "cpu.load1"
	MemUsedPct      = "mem.used_pct"
	MemUsedBytes    = "mem.used_bytes"
	SwapUsedPct     = "swap.used_pct"
	DiskUsedPct     = "disk.used_pct"
	DiskReadBps     = "disk.read_bps"
	DiskWriteBps    = "disk.write_bps"
	NetRxBps        = "net.rx_bps"
	NetTxBps        = "net.tx_bps"
	ServiceUp       = "service.up"
	ServiceLatency  = "service.latency_ms"
	ProcessCount    = "proc.count"
	UnitPercent     = "%"
	UnitBytes       = "B"
	UnitBytesPerSec = "B/s"
	UnitMillis      = "ms"
	UnitCount       = "count"
)

type CPU struct {
	Section
	Total         float64
	PerCore       []float64
	Load          [3]float64
	LoadSupported bool
}

type Memory struct {
	Section
	Total     uint64
	Used      uint64
	Available uint64
	UsedPct   float64

	Swap SwapInfo
}

type SwapInfo struct {
	Section
	Total   uint64
	Used    uint64
	UsedPct float64
}

type Volume struct {
	Mount   string
	Device  string
	FSType  string
	Total   uint64
	Used    uint64
	Free    uint64
	UsedPct float64
	Err     string
}

type Disk struct {
	Section
	Volumes []Volume
	IO      DiskIO
}

type DiskIO struct {
	Section
	ReadBps  float64
	WriteBps float64
	Primed   bool
}

type Interface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []string
	RxBps    float64
	TxBps    float64
	RxTotal  uint64
	TxTotal  uint64
}

type Network struct {
	Section
	Interfaces []Interface
	RxBps      float64
	TxBps      float64
	RxTotal    uint64
	TxTotal    uint64
	Primed     bool
}

type Host struct {
	Section
	Hostname        string
	OS              string
	Platform        string
	PlatformVersion string
	KernelVersion   string
	Arch            string
	BootTime        time.Time
	CPUModel        string
	PhysicalCores   int
	LogicalCores    int
}

func (h Host) Uptime(now time.Time) time.Duration {
	if h.BootTime.IsZero() {
		return 0
	}
	return now.Sub(h.BootTime)
}

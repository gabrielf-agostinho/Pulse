package process

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	PID        int32
	PPID       int32
	Name       string
	CPU        float64
	CPUOK      bool
	RSS        uint64
	MemPct     float64
	MemOK      bool
	Threads    int32
	Status     string
	Started    time.Time
	Restricted bool
}

type Details struct {
	Info
	Exe         string
	Cmdline     string
	Username    string
	Cwd         string
	Children    []int32
	NumFDs      int32
	FieldErrors map[string]string
}

type Snapshot struct {
	At         time.Time
	Processes  []Info
	Total      int
	Restricted int
	Err        string
	OK         bool
}

type SortKey uint8

const (
	SortCPU SortKey = iota
	SortMemory
	SortPID
	SortName
	SortThreads
)

var SortKeys = []SortKey{SortCPU, SortMemory, SortPID, SortName, SortThreads}

func (k SortKey) String() string {
	switch k {
	case SortMemory:
		return "memory"
	case SortPID:
		return "pid"
	case SortName:
		return "name"
	case SortThreads:
		return "threads"
	default:
		return "cpu"
	}
}

func (k SortKey) Next() SortKey {
	for i, s := range SortKeys {
		if s == k {
			return SortKeys[(i+1)%len(SortKeys)]
		}
	}
	return SortCPU
}

func (k SortKey) DefaultDesc() bool {
	return k == SortCPU || k == SortMemory || k == SortThreads
}

func Sort(list []Info, key SortKey, desc bool) {
	less := func(a, b Info) int {
		switch key {
		case SortCPU:
			return cmpFloat(a.CPU, b.CPU)
		case SortMemory:
			return cmpUint(a.RSS, b.RSS)
		case SortName:
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case SortThreads:
			return cmpInt(int64(a.Threads), int64(b.Threads))
		default:
			return cmpInt(int64(a.PID), int64(b.PID))
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		c := less(list[i], list[j])
		if c == 0 {
			return list[i].PID < list[j].PID
		}
		if desc {
			return c > 0
		}
		return c < 0
	})
}

func Filter(list []Info, query string) []Info {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return list
	}
	if rest, ok := strings.CutPrefix(q, "pid:"); ok {
		pid, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 32)
		if err != nil {
			return nil
		}
		for _, p := range list {
			if int64(p.PID) == pid {
				return []Info{p}
			}
		}
		return nil
	}
	_, numErr := strconv.Atoi(q)
	out := make([]Info, 0, len(list)/4)
	for _, p := range list {
		if strings.Contains(strings.ToLower(p.Name), q) ||
			(numErr == nil && strings.HasPrefix(strconv.Itoa(int(p.PID)), q)) {
			out = append(out, p)
		}
	}
	return out
}

func Protected(pid, self int32) bool {
	return pid <= 4 || pid == self
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

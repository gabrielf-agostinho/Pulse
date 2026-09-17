package process

import "testing"

func sample() []Info {
	return []Info{
		{PID: 10, Name: "postgres", CPU: 5, RSS: 300},
		{PID: 2, Name: "Chrome", CPU: 40, RSS: 900},
		{PID: 33, Name: "code", CPU: 40, RSS: 100},
		{PID: 104, Name: "bash", CPU: 0.1, RSS: 10, Threads: 9},
	}
}

func pids(list []Info) []int32 {
	out := make([]int32, len(list))
	for i, p := range list {
		out[i] = p.PID
	}
	return out
}

func eq(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSortCPUDescBreaksTiesByPID(t *testing.T) {
	l := sample()
	Sort(l, SortCPU, true)
	if got := pids(l); !eq(got, []int32{2, 33, 10, 104}) {
		t.Fatalf("got %v", got)
	}
}

func TestSortNameAscCaseInsensitive(t *testing.T) {
	l := sample()
	Sort(l, SortName, false)
	if got := pids(l); !eq(got, []int32{104, 2, 33, 10}) {
		t.Fatalf("got %v", got)
	}
}

func TestSortMemoryAndThreads(t *testing.T) {
	l := sample()
	Sort(l, SortMemory, true)
	if l[0].PID != 2 {
		t.Fatalf("memory: %v", pids(l))
	}
	Sort(l, SortThreads, true)
	if l[0].PID != 104 {
		t.Fatalf("threads: %v", pids(l))
	}
}

func TestFilter(t *testing.T) {
	l := sample()
	cases := map[string][]int32{
		"":        {10, 2, 33, 104},
		"CHR":     {2},
		"o":       {10, 2, 33},
		"10":      {10, 104},
		"pid:33":  {33},
		"pid:999": {},
		"pid:abc": {},
	}
	for q, want := range cases {
		if got := pids(Filter(l, q)); !eq(got, want) {
			t.Errorf("Filter(%q) = %v want %v", q, got, want)
		}
	}
}

func TestSortKeyCycle(t *testing.T) {
	k := SortCPU
	seen := map[SortKey]bool{}
	for range SortKeys {
		seen[k] = true
		k = k.Next()
	}
	if len(seen) != len(SortKeys) || k != SortCPU {
		t.Fatal("cycle must visit every key and wrap")
	}
}

func TestProtected(t *testing.T) {
	if !Protected(0, 100) || !Protected(4, 100) || !Protected(100, 100) {
		t.Fatal("system and self must be protected")
	}
	if Protected(500, 100) {
		t.Fatal("regular pid")
	}
}

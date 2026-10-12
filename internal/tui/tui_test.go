package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gabrielagostinho/pulse/internal/application/alerts"
	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/application/monitoring"
	"github.com/gabrielagostinho/pulse/internal/config"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/process"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
)

type fakeBackend struct {
	mu         sync.Mutex
	st         monitoring.State
	cfg        config.Config
	changed    chan struct{}
	terminated []int32
	acked      []string
	refreshed  int
}

func fixture(t *testing.T) *fakeBackend {
	t.Helper()
	t.Setenv("PULSE_DATA_DIR", t.TempDir())
	t.Setenv("PULSE_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	st := monitoring.State{Started: now.Add(-time.Hour), Series: map[string]*metrics.Ring{}}
	ok := metrics.Section{Status: metrics.StatusOK, At: now}
	st.CPU = metrics.CPU{Section: ok, Total: 37.5, PerCore: []float64{12, 88, 45, 3, 67, 21, 99, 5}}
	st.Memory = metrics.Memory{Section: ok, Total: 16 << 30, Used: 11 << 30, Available: 5 << 30, UsedPct: 68.75,
		Swap: metrics.SwapInfo{Section: ok, Total: 4 << 30, Used: 1 << 30, UsedPct: 25}}
	st.Disk = metrics.Disk{Section: ok, Volumes: []metrics.Volume{
		{Mount: "/", Total: 500 << 30, Used: 410 << 30, UsedPct: 82},
		{Mount: "/data", Total: 2 << 40, Used: 1 << 40, UsedPct: 50},
		{Mount: "/mnt/nas", Err: "not responding"},
	}, IO: metrics.DiskIO{Section: ok, ReadBps: 12 << 20, WriteBps: 3 << 20, Primed: true}}
	st.Network = metrics.Network{Section: ok, Primed: true, RxBps: 2 << 20, TxBps: 300 << 10, RxTotal: 9 << 30, TxTotal: 1 << 30,
		Interfaces: []metrics.Interface{{Name: "eth0", Up: true, RxBps: 2 << 20, TxBps: 300 << 10}, {Name: "lo", Up: true, Loopback: true}}}
	st.Host = metrics.Host{Section: ok, Hostname: "workstation", Platform: "Ubuntu", PlatformVersion: "24.04", BootTime: now.Add(-50 * time.Hour),
		CPUModel: "AMD Ryzen 7 7840U", PhysicalCores: 8, LogicalCores: 16}
	for i := 0; i < 300; i++ {
		at := now.Add(time.Duration(i-300) * time.Second)
		push := func(k string, v float64) {
			if st.Series[k] == nil {
				st.Series[k] = metrics.NewRing(300)
			}
			st.Series[k].Push(metrics.Point{At: at, Value: v})
		}
		push(metrics.CPUUsage, 40+30*math.Sin(float64(i)/20))
		push(metrics.MemUsedPct, 68)
		push(metrics.NetRxBps, float64(1<<20)*(1+math.Sin(float64(i)/7)))
		push(metrics.NetTxBps, float64(200<<10)*(1+math.Cos(float64(i)/9)))
		push(metrics.DiskReadBps, float64(i%30)*float64(1<<20))
		push(metrics.DiskWriteBps, float64(i%10)*float64(1<<19))
	}
	for i := 0; i < 400; i++ {
		st.Processes.Processes = append(st.Processes.Processes, process.Info{
			PID: int32(100 + i), Name: fmt.Sprintf("worker-%03d", i), CPU: float64(i%50) / 3, CPUOK: i%7 != 0,
			RSS: uint64(i) << 20, MemOK: i%7 != 0, Threads: int32(i % 40), Restricted: i%7 == 0,
		})
	}
	st.Processes.Processes = append(st.Processes.Processes, process.Info{PID: 4, Name: "System", Restricted: true})
	st.Processes.Total, st.Processes.OK = len(st.Processes.Processes), true

	defs := []service.Definition{
		{ID: "api", Name: "Public API", Kind: service.KindHTTP, Target: "https://api.example.com/health", Interval: 30 * time.Second, Timeout: 5 * time.Second, DegradedLatency: time.Second, FailureThreshold: 2},
		{ID: "db", Name: "Postgres", Kind: service.KindTCP, Target: "localhost:5432", Interval: 10 * time.Second, Timeout: 2 * time.Second, FailureThreshold: 2},
	}
	for _, d := range defs {
		s := service.NewState(d)
		for i := 0; i < 50; i++ {
			s.Apply(service.Probe{At: now.Add(time.Duration(i-50) * 30 * time.Second), OK: d.ID == "api" || i < 40, Latency: time.Duration(40+i) * time.Millisecond, Err: "connection refused"})
		}
		st.Services = append(st.Services, s)
	}
	rules, _ := cfg.AlertRules()
	st.Alerts = alerts.View{Enabled: true,
		Active: []alert.Incident{{ID: "i1", RuleID: "service-down", RuleName: "Service down · db", Metric: "service.up{service=db}", Severity: alert.SeverityCritical,
			Message: "Service down · db: 0 < 1", Threshold: 1, StartedAt: now.Add(-5 * time.Minute)}},
		Recent: []alert.Incident{{ID: "i0", RuleID: "cpu-high", RuleName: "CPU saturated", Metric: "cpu.usage", Severity: alert.SeverityWarning,
			StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-110 * time.Minute), PeakValue: 97, Unit: "%"}},
	}
	for _, r := range rules {
		st.Alerts.Rules = append(st.Alerts.Rules, alerts.RuleStatus{Rule: r, Series: 1})
	}
	st.Storage = monitoring.StorageView{Enabled: true}
	return &fakeBackend{st: st, cfg: cfg, changed: make(chan struct{}, 1)}
}

func (f *fakeBackend) Snapshot() monitoring.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st.Clone()
}
func (f *fakeBackend) Changed() <-chan struct{} { return f.changed }
func (f *fakeBackend) Refresh()                 { f.mu.Lock(); f.refreshed++; f.mu.Unlock() }
func (f *fakeBackend) Config() config.Config    { return f.cfg }
func (f *fakeBackend) HistoryAvailable() bool   { return true }
func (f *fakeBackend) ListSeries(context.Context) ([]history.SeriesInfo, error) {
	return []history.SeriesInfo{{Key: metrics.CPUUsage, Name: metrics.CPUUsage}, {Key: "service.latency_ms{service=api}", Name: metrics.ServiceLatency}}, nil
}
func (f *fakeBackend) QueryRange(_ context.Context, key string, from, to time.Time, n int) ([]history.Bucket, error) {
	var out []history.Bucket
	step := to.Sub(from) / time.Duration(n)
	for i := 0; i < n; i++ {
		v := 50 + 40*math.Sin(float64(i)/15)
		out = append(out, history.Bucket{At: from.Add(time.Duration(i) * step), Avg: v, Min: v - 5, Max: v + 5, Count: 3})
	}
	return out, nil
}
func (f *fakeBackend) Inspect(_ context.Context, pid int32) (process.Details, error) {
	return process.Details{Info: process.Info{PID: pid, Name: "worker"}, Exe: "/usr/bin/worker", Cmdline: "worker --serve",
		FieldErrors: map[string]string{"cwd": "access denied"}}, nil
}
func (f *fakeBackend) Terminate(_ context.Context, pid int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminated = append(f.terminated, pid)
	return nil
}
func (f *fakeBackend) Acknowledge(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "i1" {
		return errors.New("unknown")
	}
	f.acked = append(f.acked, id)
	return nil
}

func drive(m *Model, cmd tea.Cmd) {
	s := &Snapshot{model: m}
	s.drive(cmd)
}

func setup(t *testing.T, w, h int) (*Model, *fakeBackend) {
	be := fixture(t)
	m := New(be)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(stateMsg{st: be.Snapshot()})
	drive(m, m.screens[scrHistory].(*historyView).init(m))
	return m, be
}

func press(m *Model, keys string) {
	for _, k := range ParseKeys(keys) {
		_, cmd := m.Update(k)
		drive(m, cmd)
	}
}

func assertFrame(t *testing.T, name string, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want %d", name, len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("%s: line %d is %d cells wide, want %d:\n%q", name, i, got, w, ansi.Strip(l))
		}
	}
}

func TestEveryScreenFitsEveryTerminalSize(t *testing.T) {
	sizes := [][2]int{{44, 12}, {60, 16}, {80, 24}, {100, 30}, {120, 40}, {160, 46}, {230, 64}, {30, 8}}
	for _, sz := range sizes {
		m, _ := setup(t, sz[0], sz[1])
		for scr := 0; scr < numScreens; scr++ {
			drive(m, m.switchTo(scr))
			assertFrame(t, fmt.Sprintf("%s@%dx%d", screenNames[scr], sz[0], sz[1]), m.View(), sz[0], sz[1])
		}
		press(m, "?")
		assertFrame(t, fmt.Sprintf("help@%dx%d", sz[0], sz[1]), m.View(), sz[0], sz[1])
		press(m, "esc")
	}
}

func TestBlocksModeAndModalsFit(t *testing.T) {
	be := fixture(t)
	be.cfg.UI.Charts = "blocks"
	m := New(be)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(stateMsg{st: be.Snapshot()})
	for scr := 0; scr < numScreens; scr++ {
		drive(m, m.switchTo(scr))
		assertFrame(t, "blocks/"+screenNames[scr], m.View(), 100, 30)
	}
	drive(m, m.switchTo(scrProcesses))
	press(m, "enter")
	assertFrame(t, "process detail modal", m.View(), 100, 30)
	press(m, "esc x")
	assertFrame(t, "kill confirm", m.View(), 100, 30)
	drive(m, m.switchTo(scrAlerts))
}

func TestNavigationKeys(t *testing.T) {
	m, be := setup(t, 120, 40)
	press(m, "3")
	if m.active != scrServices {
		t.Fatalf("active = %d", m.active)
	}
	press(m, "] ]")
	if m.active != scrHistory {
		t.Fatalf("] should advance, active = %d", m.active)
	}
	press(m, "[")
	if m.active != scrAlerts {
		t.Fatal("[ should go back")
	}
	press(m, "r")
	if be.refreshed != 1 {
		t.Fatal("r must trigger a refresh")
	}
	press(m, "p")
	if !m.paused {
		t.Fatal("p pauses")
	}
	m.Update(stateMsg{st: monitoring.State{}})
	if m.st.CPU.Total == 0 {
		t.Fatal("paused display must not take new snapshots")
	}
	press(m, "p ?")
	if !m.showHelp {
		t.Fatal("? opens help")
	}
	press(m, "1")
	if m.active != scrAlerts {
		t.Fatal("keys are swallowed while help is open")
	}
	press(m, "esc")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("q quits")
	}
}

func TestProcessFilterSortAndTerminate(t *testing.T) {
	m, be := setup(t, 140, 40)
	press(m, "2")
	p := m.screens[scrProcesses].(*processesView)
	if p.rows[0].CPU < p.rows[len(p.rows)-1].CPU {
		t.Fatal("default sort is CPU descending")
	}
	press(m, "/ w o r k e r - 0 4 2 enter")
	if len(p.rows) != 1 || p.rows[0].Name != "worker-042" {
		t.Fatalf("filter rows = %d", len(p.rows))
	}
	if !strings.Contains(ansi.Strip(m.View()), "worker-042") {
		t.Fatal("filtered row must be visible")
	}
	press(m, "/ q")
	if m.active != scrProcesses || p.input.Value() != "worker-042q" {
		t.Fatalf("filter must capture keys, value=%q", p.input.Value())
	}
	press(m, "esc")
	if p.input.Value() != "" || len(p.rows) != 401 {
		t.Fatal("esc clears the filter")
	}
	press(m, "m")
	if p.sortKey != process.SortMemory || p.rows[0].RSS < p.rows[1].RSS {
		t.Fatal("m sorts by memory")
	}
	press(m, "S")
	if p.desc {
		t.Fatal("S reverses")
	}
	press(m, "c")
	target := p.rows[0].PID
	press(m, "x")
	if p.confirm == nil {
		t.Fatal("x asks for confirmation")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Terminate") {
		t.Fatal("confirmation dialog must be drawn")
	}
	press(m, "n")
	if len(be.terminated) != 0 || p.confirm != nil {
		t.Fatal("n cancels without terminating")
	}
	press(m, "x y")
	if len(be.terminated) != 1 || be.terminated[0] != target {
		t.Fatalf("y terminates the selected process, got %v", be.terminated)
	}
	press(m, "/ p i d : 4 enter x")
	if p.confirm != nil {
		t.Fatal("protected process must not be offered for termination")
	}
}

func TestProcessDetailsLoadAsync(t *testing.T) {
	m, _ := setup(t, 160, 46)
	press(m, "2 enter")
	p := m.screens[scrProcesses].(*processesView)
	if p.details == nil || p.details.Exe != "/usr/bin/worker" {
		t.Fatalf("details = %+v", p.details)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "DETAILS") || !strings.Contains(view, "access denied") {
		t.Fatal("detail pane must show fields and permission errors")
	}
}

func TestAlertsAcknowledgeAndFilter(t *testing.T) {
	m, be := setup(t, 120, 40)
	press(m, "4")
	if !strings.Contains(ansi.Strip(m.View()), "Service down · db") {
		t.Fatal("active incident must be listed with the default (all) filter")
	}
	press(m, "a")
	if len(be.acked) != 1 {
		t.Fatal("a acknowledges the selected incident")
	}
	a := m.screens[scrAlerts].(*alertsView)
	press(m, "f")
	m.toast = ""
	if a.sev != int(alert.SeverityInfo) {
		t.Fatalf("f cycles severity, got %d", a.sev)
	}
	if strings.Contains(ansi.Strip(m.View()), "Service down · db") {
		t.Fatal("info filter hides critical incidents")
	}
	press(m, "f f f tab enter")
	if a.section != secHistory || a.detail == nil {
		t.Fatal("tab switches to history, enter opens details")
	}
	if !strings.Contains(ansi.Strip(m.View()), "RESOLVED") {
		t.Fatal("incident modal shows resolution state")
	}
}

func TestHistoryLoadsSeriesAndRange(t *testing.T) {
	m, _ := setup(t, 140, 40)
	press(m, "5")
	h := m.screens[scrHistory].(*historyView)
	if len(h.series) != 2 || h.result == nil || len(h.result.buckets) == 0 {
		t.Fatalf("history not loaded: series=%d result=%v", len(h.series), h.result)
	}
	press(m, "right")
	if h.rng != 1 || h.result.to.Sub(h.result.from) != time.Hour {
		t.Fatal("→ selects the next range and re-queries")
	}
	press(m, "down")
	if h.result.key != "service.latency_ms{service=api}" {
		t.Fatalf("↓ selects next series, result key=%s", h.result.key)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"SERVICE LATENCY", "min", "max", "samples"} {
		if !strings.Contains(view, want) {
			t.Fatalf("history view missing %q", want)
		}
	}
}

func TestDegradedStatesAreExplicit(t *testing.T) {
	be := fixture(t)
	be.st.CPU = metrics.CPU{Section: metrics.Section{Status: metrics.StatusPending}}
	be.st.Disk.IO = metrics.DiskIO{Section: metrics.Section{Status: metrics.StatusUnsupported}}
	be.st.Memory.Swap = metrics.SwapInfo{Section: metrics.Section{Status: metrics.StatusError, Err: "pagefile query failed"}}
	be.st.Storage = monitoring.StorageView{Enabled: true, InitErr: "storage /x/pulse.db: database is locked (hint: another process holds a lock)"}
	m := New(be)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 46})
	m.Update(stateMsg{st: be.Snapshot()})
	view := ansi.Strip(m.View())
	for _, want := range []string{"Sampling CPU", "I/O counters not available", "swap unavailable", "history unavailable"} {
		if !strings.Contains(view, want) {
			t.Errorf("overview must say %q", want)
		}
	}
	press(m, "5")
	if !strings.Contains(ansi.Strip(m.View()), "could not be opened") {
		t.Error("history screen must explain the storage failure")
	}
}

func BenchmarkOverviewView(b *testing.B) {
	t := &testing.T{}
	be := fixture(t)
	m := New(be)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 46})
	m.Update(stateMsg{st: be.Snapshot()})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

package alert

import (
	"fmt"
	"testing"
	"time"
)

func seqID() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("inc-%d", n) }
}

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func cpuRule() Rule {
	return Rule{
		ID: "cpu-high", Name: "CPU high", Metric: "cpu.usage", Op: OpGT,
		Threshold: 90, For: 10 * time.Second, Hysteresis: 5, ClearFor: 5 * time.Second,
		Severity: SeverityCritical, Enabled: true, Unit: "%",
	}
}

func TestOperatorBreaches(t *testing.T) {
	cases := []struct {
		op   Operator
		v    float64
		want bool
	}{
		{OpGT, 91, true}, {OpGT, 90, false},
		{OpGE, 90, true}, {OpLT, 89, true},
		{OpLT, 90, false}, {OpLE, 90, true},
	}
	for _, c := range cases {
		if got := c.op.Breaches(c.v, 90); got != c.want {
			t.Errorf("%v %v 90 = %v", c.v, c.op, got)
		}
	}
	if Operator("!=").Valid() {
		t.Fatal("unknown operator must be invalid")
	}
}

func TestDurationGateOpensSingleIncident(t *testing.T) {
	tr := NewTracker(cpuRule(), seqID())
	var opened []Transition
	for s := 0; s <= 60; s++ {
		if tr := tr.Observe(95, at(s)); tr.Kind == TransitionOpened {
			opened = append(opened, tr)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("expected exactly one incident for a sustained breach, got %d", len(opened))
	}
	inc := opened[0].Incident
	if !inc.StartedAt.Equal(at(0)) {
		t.Fatalf("incident should start when the breach started, got %v", inc.StartedAt)
	}
	if tr.Phase != PhaseFiring {
		t.Fatalf("phase = %v", tr.Phase)
	}
}

func TestShortSpikeDoesNotFire(t *testing.T) {
	tr := NewTracker(cpuRule(), seqID())
	for s := 0; s < 9; s++ {
		if res := tr.Observe(99, at(s)); res.Kind != TransitionNone {
			t.Fatal("must not fire before For elapses")
		}
	}
	tr.Observe(10, at(9))
	if tr.Phase != PhaseInactive {
		t.Fatalf("spike should reset to inactive, got %v", tr.Phase)
	}
	for s := 10; s < 19; s++ {
		if res := tr.Observe(99, at(s)); res.Kind != TransitionNone {
			t.Fatal("pending window must restart after a reset")
		}
	}
}

func TestHysteresisPreventsFlapping(t *testing.T) {
	tr := NewTracker(cpuRule(), seqID())
	for s := 0; s <= 10; s++ {
		tr.Observe(95, at(s))
	}
	for s := 11; s < 100; s++ {
		v := 88.0
		if s%2 == 0 {
			v = 92
		}
		if res := tr.Observe(v, at(s)); res.Kind != TransitionNone {
			t.Fatalf("unexpected transition %v at %ds", res.Kind, s)
		}
	}
	if tr.Phase != PhaseFiring || tr.Incident == nil {
		t.Fatalf("incident must remain open, phase=%v", tr.Phase)
	}
	if tr.Incident.PeakValue != 95 {
		t.Fatalf("peak = %v", tr.Incident.PeakValue)
	}
}

func TestResolveRequiresClearForAndRelapseKeepsIncident(t *testing.T) {
	tr := NewTracker(cpuRule(), seqID())
	for s := 0; s <= 10; s++ {
		tr.Observe(95, at(s))
	}
	id := tr.Incident.ID
	for s := 11; s <= 13; s++ {
		if res := tr.Observe(50, at(s)); res.Kind != TransitionNone {
			t.Fatal("must not resolve before ClearFor")
		}
	}
	if tr.Phase != PhaseResolving {
		t.Fatalf("phase = %v", tr.Phase)
	}
	tr.Observe(97, at(14))
	if tr.Phase != PhaseFiring || tr.Incident.ID != id {
		t.Fatal("relapse continues the same incident")
	}
	var resolved *Transition
	for s := 15; s <= 30; s++ {
		if res := tr.Observe(40, at(s)); res.Kind == TransitionResolved {
			resolved = &res
			break
		}
	}
	if resolved == nil {
		t.Fatal("expected resolution")
	}
	if resolved.Incident.ID != id || resolved.Incident.EndedAt.Before(at(20)) {
		t.Fatalf("resolved = %+v", resolved.Incident)
	}
	if resolved.Incident.PeakValue != 97 {
		t.Fatalf("peak = %v", resolved.Incident.PeakValue)
	}
	if tr.Phase != PhaseInactive || tr.Incident != nil {
		t.Fatal("tracker must reset after resolve")
	}
	var second Transition
	for s := 40; s <= 52; s++ {
		if res := tr.Observe(99, at(s)); res.Kind == TransitionOpened {
			second = res
		}
	}
	if second.Incident.ID == id || second.Incident.ID == "" {
		t.Fatalf("expected a distinct second incident, got %q", second.Incident.ID)
	}
}

func TestDownwardRule(t *testing.T) {
	r := Rule{ID: "svc", Name: "API down", Metric: "service.up{service=api}", Op: OpLT, Threshold: 1, Enabled: true}
	tr := NewTracker(r, seqID())
	if res := tr.Observe(0, at(0)); res.Kind != TransitionOpened {
		t.Fatal("zero duration fires immediately")
	}
	if res := tr.Observe(1, at(1)); res.Kind != TransitionResolved {
		t.Fatal("zero clear duration resolves immediately")
	}
}

func TestDisabledRuleNeverFires(t *testing.T) {
	r := cpuRule()
	r.Enabled = false
	tr := NewTracker(r, seqID())
	for s := 0; s < 60; s++ {
		if res := tr.Observe(100, at(s)); res.Kind != TransitionNone {
			t.Fatal("disabled")
		}
	}
}

func TestRestoreContinuesIncident(t *testing.T) {
	tr := NewTracker(cpuRule(), seqID())
	tr.Restore(Incident{ID: "old", RuleID: "cpu-high", StartedAt: at(-100), PeakValue: 99})
	if res := tr.Observe(96, at(0)); res.Kind != TransitionNone {
		t.Fatal("restored incident must not be reopened")
	}
	var res Transition
	for s := 1; s <= 10; s++ {
		res = tr.Observe(10, at(s))
		if res.Kind == TransitionResolved {
			break
		}
	}
	if res.Kind != TransitionResolved || res.Incident.ID != "old" {
		t.Fatalf("restored incident should resolve, got %+v", res)
	}
}

func TestIncidentDurationAndFormat(t *testing.T) {
	inc := Incident{StartedAt: at(0)}
	if inc.Duration(at(30)) != 30*time.Second || !inc.Active() {
		t.Fatal("active duration")
	}
	inc.EndedAt = at(10)
	if inc.Duration(at(30)) != 10*time.Second {
		t.Fatal("ended duration")
	}
	if FormatValue(90, "%") != "90%" || FormatValue(1.25, "ms") != "1.2 ms" && FormatValue(1.25, "ms") != "1.3 ms" {
		t.Fatalf("format: %s %s", FormatValue(90, "%"), FormatValue(1.25, "ms"))
	}
	if _, err := ParseSeverity("nope"); err == nil {
		t.Fatal("severity validation")
	}
}

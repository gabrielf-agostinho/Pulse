package alert

import (
	"fmt"
	"strings"
	"time"
)

type Severity uint8

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	default:
		return "info"
	}
}

func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return SeverityInfo, nil
	case "warning", "warn":
		return SeverityWarning, nil
	case "critical", "crit":
		return SeverityCritical, nil
	}
	return 0, fmt.Errorf("unknown severity %q (want info, warning or critical)", s)
}

type Operator string

const (
	OpGT Operator = ">"
	OpGE Operator = ">="
	OpLT Operator = "<"
	OpLE Operator = "<="
)

func (o Operator) Valid() bool {
	switch o {
	case OpGT, OpGE, OpLT, OpLE:
		return true
	}
	return false
}

func (o Operator) Breaches(v, threshold float64) bool {
	switch o {
	case OpGT:
		return v > threshold
	case OpGE:
		return v >= threshold
	case OpLT:
		return v < threshold
	case OpLE:
		return v <= threshold
	}
	return false
}

func (o Operator) upward() bool { return o == OpGT || o == OpGE }

type Rule struct {
	ID         string
	Name       string
	Metric     string
	Op         Operator
	Threshold  float64
	For        time.Duration
	Hysteresis float64
	ClearFor   time.Duration
	Severity   Severity
	Enabled    bool
	Unit       string
}

func (r Rule) Cleared(v float64) bool {
	switch r.Op {
	case OpGT:
		return v <= r.Threshold-r.Hysteresis
	case OpGE:
		return v < r.Threshold-r.Hysteresis
	case OpLT:
		return v >= r.Threshold+r.Hysteresis
	case OpLE:
		return v > r.Threshold+r.Hysteresis
	}
	return true
}

func (r Rule) Describe() string {
	s := fmt.Sprintf("%s %s %s", r.Metric, r.Op, FormatValue(r.Threshold, r.Unit))
	if r.For > 0 {
		s += " for " + r.For.String()
	}
	return s
}

func FormatValue(v float64, unit string) string {
	num := fmt.Sprintf("%.1f", v)
	num = strings.TrimSuffix(num, ".0")
	switch unit {
	case "", "count":
		return num
	case "%":
		return num + "%"
	default:
		return num + " " + unit
	}
}

type Phase uint8

const (
	PhaseInactive Phase = iota
	PhasePending
	PhaseFiring
	PhaseResolving
)

func (p Phase) String() string {
	switch p {
	case PhasePending:
		return "pending"
	case PhaseFiring:
		return "firing"
	case PhaseResolving:
		return "resolving"
	default:
		return "ok"
	}
}

type Incident struct {
	ID         string
	RuleID     string
	RuleName   string
	Metric     string
	Severity   Severity
	Message    string
	Threshold  float64
	Unit       string
	StartedAt  time.Time
	EndedAt    time.Time
	AckedAt    time.Time
	PeakValue  float64
	LastValue  float64
	LastUpdate time.Time
}

func (i Incident) Active() bool { return i.EndedAt.IsZero() }

func (i Incident) Acknowledged() bool { return !i.AckedAt.IsZero() }

func (i Incident) Duration(now time.Time) time.Duration {
	if i.Active() {
		return now.Sub(i.StartedAt)
	}
	return i.EndedAt.Sub(i.StartedAt)
}

type TransitionKind uint8

const (
	TransitionNone TransitionKind = iota
	TransitionOpened
	TransitionResolved
)

type Transition struct {
	Kind     TransitionKind
	Incident Incident
}

type Tracker struct {
	Rule      Rule
	Phase     Phase
	Since     time.Time
	LastValue float64
	LastSeen  time.Time
	Incident  *Incident
	newID     func() string
}

func NewTracker(r Rule, newID func() string) *Tracker {
	return &Tracker{Rule: r, newID: newID}
}

func (t *Tracker) Restore(inc Incident) {
	c := inc
	t.Incident = &c
	t.Phase = PhaseFiring
	t.Since = inc.StartedAt
}

func (t *Tracker) Observe(v float64, at time.Time) Transition {
	t.LastValue, t.LastSeen = v, at
	r := t.Rule
	if !r.Enabled {
		return Transition{}
	}
	breach := r.Op.Breaches(v, r.Threshold)

	switch t.Phase {
	case PhaseInactive:
		if breach {
			t.Phase, t.Since = PhasePending, at
			return t.maybeFire(v, at)
		}
	case PhasePending:
		if !breach {
			t.Phase, t.Since = PhaseInactive, at
			return Transition{}
		}
		return t.maybeFire(v, at)
	case PhaseFiring:
		t.track(v, at)
		if r.Cleared(v) {
			t.Phase, t.Since = PhaseResolving, at
			return t.maybeResolve(at)
		}
	case PhaseResolving:
		t.track(v, at)
		if !r.Cleared(v) {
			t.Phase, t.Since = PhaseFiring, at
			return Transition{}
		}
		return t.maybeResolve(at)
	}
	return Transition{}
}

func (t *Tracker) maybeFire(v float64, at time.Time) Transition {
	if at.Sub(t.Since) < t.Rule.For {
		return Transition{}
	}
	r := t.Rule
	inc := &Incident{
		ID:         t.newID(),
		RuleID:     r.ID,
		RuleName:   r.Name,
		Metric:     r.Metric,
		Severity:   r.Severity,
		Threshold:  r.Threshold,
		Unit:       r.Unit,
		StartedAt:  t.Since,
		PeakValue:  v,
		LastValue:  v,
		LastUpdate: at,
	}
	inc.Message = fmt.Sprintf("%s: %s %s %s", r.Name, FormatValue(v, r.Unit), r.Op, FormatValue(r.Threshold, r.Unit))
	if r.For > 0 {
		inc.Message += " for " + r.For.String()
	}
	t.Incident = inc
	t.Phase = PhaseFiring
	return Transition{Kind: TransitionOpened, Incident: *inc}
}

func (t *Tracker) maybeResolve(at time.Time) Transition {
	if at.Sub(t.Since) < t.Rule.ClearFor {
		return Transition{}
	}
	inc := t.Incident
	t.Incident = nil
	t.Phase, t.Since = PhaseInactive, at
	if inc == nil {
		return Transition{}
	}
	inc.EndedAt = at
	inc.LastUpdate = at
	return Transition{Kind: TransitionResolved, Incident: *inc}
}

func (t *Tracker) track(v float64, at time.Time) {
	if t.Incident == nil {
		return
	}
	t.Incident.LastValue = v
	t.Incident.LastUpdate = at
	if t.Rule.Op.upward() && v > t.Incident.PeakValue || !t.Rule.Op.upward() && v < t.Incident.PeakValue {
		t.Incident.PeakValue = v
	}
}

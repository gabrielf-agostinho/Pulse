package service

import (
	"time"
)

type Kind string

const (
	KindHTTP Kind = "http"
	KindTCP  Kind = "tcp"
)

type Status uint8

const (
	StatusUnknown Status = iota
	StatusHealthy
	StatusDegraded
	StatusDown
)

func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusDown:
		return "down"
	default:
		return "unknown"
	}
}

func ParseStatus(s string) Status {
	switch s {
	case "healthy":
		return StatusHealthy
	case "degraded":
		return StatusDegraded
	case "down":
		return StatusDown
	}
	return StatusUnknown
}

type Definition struct {
	ID       string
	Name     string
	Kind     Kind
	Target   string
	Interval time.Duration
	Timeout  time.Duration

	Method        string
	Headers       map[string]string
	ExpectStatus  []int
	TLSSkipVerify bool

	DegradedLatency  time.Duration
	FailureThreshold int
}

func (d Definition) AcceptsStatus(code int) bool {
	if len(d.ExpectStatus) == 0 {
		return code >= 200 && code < 400
	}
	for _, c := range d.ExpectStatus {
		if c == code {
			return true
		}
	}
	return false
}

type Probe struct {
	At         time.Time
	OK         bool
	Latency    time.Duration
	StatusCode int
	Err        string
}

type CheckResult struct {
	ServiceID  string
	At         time.Time
	Status     Status
	OK         bool
	Latency    time.Duration
	StatusCode int
	Err        string
}

const HistorySize = 120

type State struct {
	Def                 Definition
	Status              Status
	LastCheck           time.Time
	LastLatency         time.Duration
	LastSuccess         time.Time
	LastFailure         time.Time
	LastError           string
	LastStatusCode      int
	ConsecutiveFailures int
	History             []CheckResult
}

func NewState(def Definition) State {
	return State{Def: def, Status: StatusUnknown}
}

func (s *State) Apply(p Probe) CheckResult {
	threshold := s.Def.FailureThreshold
	if threshold < 1 {
		threshold = 1
	}
	res := CheckResult{
		ServiceID:  s.Def.ID,
		At:         p.At,
		OK:         p.OK,
		Latency:    p.Latency,
		StatusCode: p.StatusCode,
		Err:        p.Err,
	}
	s.LastCheck = p.At
	s.LastStatusCode = p.StatusCode
	if p.OK {
		s.ConsecutiveFailures = 0
		s.LastSuccess = p.At
		s.LastLatency = p.Latency
		res.Status = StatusHealthy
		if s.Def.DegradedLatency > 0 && p.Latency > s.Def.DegradedLatency {
			res.Status = StatusDegraded
		}
	} else {
		s.ConsecutiveFailures++
		s.LastFailure = p.At
		s.LastError = p.Err
		res.Status = StatusDegraded
		if s.ConsecutiveFailures >= threshold {
			res.Status = StatusDown
		}
	}
	s.Status = res.Status
	s.History = append(s.History, res)
	if over := len(s.History) - HistorySize; over > 0 {
		s.History = append(s.History[:0:0], s.History[over:]...)
	}
	return res
}

func (s State) Uptime() (pct float64, ok bool) {
	if len(s.History) == 0 {
		return 0, false
	}
	n := 0
	for _, c := range s.History {
		if c.OK {
			n++
		}
	}
	return float64(n) / float64(len(s.History)) * 100, true
}

func (s State) Clone() State {
	c := s
	c.History = append([]CheckResult(nil), s.History...)
	if s.Def.Headers != nil {
		c.Def.Headers = make(map[string]string, len(s.Def.Headers))
		for k, v := range s.Def.Headers {
			c.Def.Headers[k] = v
		}
	}
	c.Def.ExpectStatus = append([]int(nil), s.Def.ExpectStatus...)
	return c
}

package service

import (
	"testing"
	"time"
)

func TestAcceptsStatus(t *testing.T) {
	d := Definition{}
	if !d.AcceptsStatus(204) || !d.AcceptsStatus(302) || d.AcceptsStatus(404) || d.AcceptsStatus(500) {
		t.Fatal("default range is 200..399")
	}
	d.ExpectStatus = []int{404}
	if !d.AcceptsStatus(404) || d.AcceptsStatus(200) {
		t.Fatal("explicit list")
	}
}

func TestApplyClassification(t *testing.T) {
	now := time.Now()
	s := NewState(Definition{ID: "api", DegradedLatency: 200 * time.Millisecond, FailureThreshold: 2})

	if r := s.Apply(Probe{At: now, OK: true, Latency: 50 * time.Millisecond}); r.Status != StatusHealthy {
		t.Fatalf("fast ok => healthy, got %v", r.Status)
	}
	if r := s.Apply(Probe{At: now, OK: true, Latency: 900 * time.Millisecond}); r.Status != StatusDegraded {
		t.Fatalf("slow ok => degraded, got %v", r.Status)
	}
	if r := s.Apply(Probe{At: now, Err: "refused"}); r.Status != StatusDegraded {
		t.Fatalf("first failure => degraded, got %v", r.Status)
	}
	if r := s.Apply(Probe{At: now, Err: "refused"}); r.Status != StatusDown {
		t.Fatalf("second failure => down, got %v", r.Status)
	}
	if s.ConsecutiveFailures != 2 || s.LastError != "refused" {
		t.Fatalf("state = %+v", s)
	}
	if r := s.Apply(Probe{At: now, OK: true, Latency: time.Millisecond}); r.Status != StatusHealthy || s.ConsecutiveFailures != 0 {
		t.Fatal("recovery resets failures")
	}
	if pct, ok := s.Uptime(); !ok || pct != 60 {
		t.Fatalf("uptime = %v", pct)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	s := NewState(Definition{ID: "x"})
	for i := 0; i < HistorySize+30; i++ {
		s.Apply(Probe{OK: true, At: time.Unix(int64(i), 0)})
	}
	if len(s.History) != HistorySize {
		t.Fatalf("len = %d", len(s.History))
	}
	if s.History[0].At.Unix() != 30 {
		t.Fatalf("oldest = %v", s.History[0].At.Unix())
	}
	c := s.Clone()
	c.History[0].OK = false
	if !s.History[0].OK {
		t.Fatal("clone must not alias history")
	}
}

func TestParseStatusRoundTrip(t *testing.T) {
	for _, st := range []Status{StatusUnknown, StatusHealthy, StatusDegraded, StatusDown} {
		if ParseStatus(st.String()) != st {
			t.Fatalf("round trip %v", st)
		}
	}
}

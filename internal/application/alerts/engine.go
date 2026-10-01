package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gabrielagostinho/pulse/internal/application/history"
	"github.com/gabrielagostinho/pulse/internal/domain/alert"
	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

const recentLimit = 200

const staleAfter = 10 * time.Minute

type Enqueuer interface {
	Enqueue(ctx context.Context, recs ...history.Record) error
}

type RuleStatus struct {
	Rule      alert.Rule
	Phase     alert.Phase
	Series    int
	Firing    int
	LastValue float64
	HasValue  bool
	LastSeen  time.Time
}

type View struct {
	Enabled    bool
	Rules      []RuleStatus
	Active     []alert.Incident
	Recent     []alert.Incident
	PersistErr string
}

func (v View) Clone() View {
	c := v
	c.Rules = append([]RuleStatus(nil), v.Rules...)
	c.Active = append([]alert.Incident(nil), v.Active...)
	c.Recent = append([]alert.Incident(nil), v.Recent...)
	return c
}

type Engine struct {
	log     *slog.Logger
	store   Enqueuer
	publish func(View)
	now     func() time.Time

	mu         sync.Mutex
	rules      []alert.Rule
	trackers   map[string]*alert.Tracker
	recent     []alert.Incident
	acked      map[string]time.Time
	persistErr string
}

func New(rules []alert.Rule, store Enqueuer, publish func(View), log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if publish == nil {
		publish = func(View) {}
	}
	return &Engine{
		log: log, store: store, publish: publish, now: time.Now,
		rules: rules, trackers: map[string]*alert.Tracker{}, acked: map[string]time.Time{},
	}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func matches(r alert.Rule, s metrics.Sample) bool {
	if strings.Contains(r.Metric, "{") {
		return r.Metric == s.Key()
	}
	return r.Metric == s.Name
}

func trackerKey(ruleID, series string) string { return ruleID + "|" + series }

func seriesRule(r alert.Rule, s metrics.Sample) alert.Rule {
	key := s.Key()
	r.Metric = key
	if !strings.Contains(r.Name, "·") && len(s.Labels) > 0 {
		vals := make([]string, 0, len(s.Labels))
		for _, k := range sortedKeys(s.Labels) {
			vals = append(vals, s.Labels[k])
		}
		r.Name = r.Name + " · " + strings.Join(vals, " ")
	}
	return r
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (e *Engine) Restore(ctx context.Context, open, recent []alert.Incident) {
	e.mu.Lock()
	var events []history.Record
	now := e.now()
	for _, inc := range open {
		rule, ok := e.rule(inc.RuleID)
		if !ok || !rule.Enabled {
			inc.EndedAt = now
			inc.Message += " (rule removed)"
			events = append(events, incidentRecord(history.IncidentResolved, inc))
			e.remember(inc)
			continue
		}
		r := rule
		r.Metric, r.Name = inc.Metric, inc.RuleName
		tr := alert.NewTracker(r, newID)
		tr.Restore(inc)
		tr.LastSeen = now
		e.trackers[trackerKey(inc.RuleID, inc.Metric)] = tr
		if inc.Acknowledged() {
			e.acked[inc.ID] = inc.AckedAt
		}
	}
	for i := len(recent) - 1; i >= 0; i-- {
		if !recent[i].Active() {
			e.remember(recent[i])
		}
	}
	view := e.viewLocked()
	e.mu.Unlock()
	e.persist(ctx, events)
	e.publish(view)
}

func (e *Engine) rule(id string) (alert.Rule, bool) {
	for _, r := range e.rules {
		if r.ID == id {
			return r, true
		}
	}
	return alert.Rule{}, false
}

func (e *Engine) Observe(ctx context.Context, samples []metrics.Sample) {
	if len(e.rules) == 0 || len(samples) == 0 {
		return
	}
	var events []history.Record
	changed := false
	e.mu.Lock()
	for _, s := range samples {
		for _, r := range e.rules {
			if !r.Enabled || !matches(r, s) {
				continue
			}
			key := trackerKey(r.ID, s.Key())
			tr := e.trackers[key]
			if tr == nil {
				tr = alert.NewTracker(seriesRule(r, s), newID)
				e.trackers[key] = tr
			}
			before := tr.Phase
			res := tr.Observe(s.Value, s.At)
			if tr.Phase != before {
				changed = true
			}
			switch res.Kind {
			case alert.TransitionOpened:
				e.log.Info("incident opened", "rule", r.ID, "series", s.Key(), "value", s.Value)
				events = append(events, incidentRecord(history.IncidentOpened, res.Incident))
			case alert.TransitionResolved:
				inc := e.withAck(res.Incident)
				e.log.Info("incident resolved", "rule", r.ID, "series", s.Key(), "duration", inc.Duration(inc.EndedAt))
				events = append(events, incidentRecord(history.IncidentResolved, inc))
				e.remember(inc)
			}
		}
	}
	var view View
	if changed || len(events) > 0 {
		view = e.viewLocked()
	}
	e.mu.Unlock()
	e.persist(ctx, events)
	if changed || len(events) > 0 {
		e.publish(view)
	}
}

func (e *Engine) Sweep(ctx context.Context) {
	now := e.now()
	var events []history.Record
	e.mu.Lock()
	for key, tr := range e.trackers {
		if now.Sub(tr.LastSeen) < staleAfter {
			continue
		}
		if tr.Incident != nil {
			inc := e.withAck(*tr.Incident)
			inc.EndedAt = now
			inc.Message += " (no data)"
			events = append(events, incidentRecord(history.IncidentResolved, inc))
			e.remember(inc)
		}
		delete(e.trackers, key)
	}
	view := e.viewLocked()
	e.mu.Unlock()
	e.persist(ctx, events)
	if len(events) > 0 {
		e.publish(view)
	}
}

func (e *Engine) Acknowledge(ctx context.Context, id string) error {
	now := e.now()
	e.mu.Lock()
	var target *alert.Incident
	for _, tr := range e.trackers {
		if tr.Incident != nil && tr.Incident.ID == id {
			target = tr.Incident
		}
	}
	if target == nil {
		e.mu.Unlock()
		return fmt.Errorf("incident %s is not active", id)
	}
	if _, done := e.acked[id]; done {
		e.mu.Unlock()
		return nil
	}
	e.acked[id] = now
	inc := *target
	inc.AckedAt = now
	view := e.viewLocked()
	e.mu.Unlock()
	e.log.Info("incident acknowledged", "id", id, "rule", inc.RuleID)
	e.persist(ctx, []history.Record{incidentRecord(history.IncidentAcked, inc)})
	e.publish(view)
	return nil
}

func (e *Engine) View() View {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.viewLocked()
}

func (e *Engine) withAck(inc alert.Incident) alert.Incident {
	if at, ok := e.acked[inc.ID]; ok {
		inc.AckedAt = at
		delete(e.acked, inc.ID)
	}
	return inc
}

func (e *Engine) remember(inc alert.Incident) {
	e.recent = append([]alert.Incident{inc}, e.recent...)
	if len(e.recent) > recentLimit {
		e.recent = e.recent[:recentLimit]
	}
}

func (e *Engine) viewLocked() View {
	v := View{Enabled: true, PersistErr: e.persistErr}
	byRule := map[string]*RuleStatus{}
	for _, r := range e.rules {
		rs := &RuleStatus{Rule: r}
		byRule[r.ID] = rs
	}
	for _, tr := range e.trackers {
		rs := byRule[tr.Rule.ID]
		if rs == nil {
			continue
		}
		rs.Series++
		if severity(tr.Phase) > severity(rs.Phase) {
			rs.Phase = tr.Phase
		}
		if tr.LastSeen.After(rs.LastSeen) {
			rs.LastSeen, rs.LastValue, rs.HasValue = tr.LastSeen, tr.LastValue, true
		}
		if tr.Incident != nil {
			rs.Firing++
			inc := *tr.Incident
			if at, ok := e.acked[inc.ID]; ok {
				inc.AckedAt = at
			}
			v.Active = append(v.Active, inc)
		}
	}
	for _, r := range e.rules {
		v.Rules = append(v.Rules, *byRule[r.ID])
	}
	sort.Slice(v.Active, func(i, j int) bool {
		a, b := v.Active[i], v.Active[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		return a.StartedAt.After(b.StartedAt)
	})
	v.Recent = append([]alert.Incident(nil), e.recent...)
	return v
}

func severity(p alert.Phase) int {
	switch p {
	case alert.PhaseFiring:
		return 3
	case alert.PhaseResolving:
		return 2
	case alert.PhasePending:
		return 1
	}
	return 0
}

func incidentRecord(kind history.IncidentEventKind, inc alert.Incident) history.Record {
	return history.Record{Incident: &history.IncidentEvent{Kind: kind, Incident: inc}}
}

func (e *Engine) persist(ctx context.Context, events []history.Record) {
	if e.store == nil || len(events) == 0 {
		return
	}
	err := e.store.Enqueue(ctx, events...)
	e.mu.Lock()
	if err != nil {
		e.persistErr = err.Error()
		e.log.Error("failed to persist incident transitions", "events", len(events), "err", err)
	} else {
		e.persistErr = ""
	}
	e.mu.Unlock()
}

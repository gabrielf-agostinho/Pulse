package monitoring

import (
	"sync"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
	"github.com/gabrielagostinho/pulse/internal/domain/service"
)

type Hub struct {
	mu      sync.Mutex
	st      State
	ringCap int
	notify  chan struct{}
}

func NewHub(ringCap int, services []service.Definition) *Hub {
	h := &Hub{
		ringCap: ringCap,
		notify:  make(chan struct{}, 1),
		st: State{
			Started: time.Now(),
			Series:  map[string]*metrics.Ring{},
		},
	}
	for _, d := range services {
		h.st.Services = append(h.st.Services, service.NewState(d))
	}
	return h
}

func (h *Hub) Update(fn func(*State)) {
	h.mu.Lock()
	fn(&h.st)
	h.st.Version++
	h.mu.Unlock()
	select {
	case h.notify <- struct{}{}:
	default:
	}
}

func (h *Hub) Snapshot() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st.Clone()
}

func (h *Hub) MemTotal() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st.Memory.Total
}

func (h *Hub) Changed() <-chan struct{} { return h.notify }

func (s *State) push(key string, capacity int, at time.Time, v float64) {
	r := s.Series[key]
	if r == nil {
		r = metrics.NewRing(capacity)
		s.Series[key] = r
	}
	r.Push(metrics.Point{At: at, Value: v})
}

func (h *Hub) Push(s *State, key string, at time.Time, v float64) {
	s.push(key, h.ringCap, at, v)
}

package metrics

import "time"

type Point struct {
	At    time.Time
	Value float64
}

type Ring struct {
	buf   []Point
	start int
	n     int
}

func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]Point, capacity)}
}

func (r *Ring) Push(p Point) {
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = p
		r.n++
		return
	}
	r.buf[r.start] = p
	r.start = (r.start + 1) % len(r.buf)
}

func (r *Ring) Len() int { return r.n }

func (r *Ring) Cap() int { return len(r.buf) }

func (r *Ring) Last() (Point, bool) {
	if r.n == 0 {
		return Point{}, false
	}
	return r.buf[(r.start+r.n-1)%len(r.buf)], true
}

func (r *Ring) Points() []Point {
	out := make([]Point, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}

func (r *Ring) Values() []float64 {
	out := make([]float64, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)].Value
	}
	return out
}

func (r *Ring) Clone() *Ring {
	if r == nil {
		return nil
	}
	c := &Ring{buf: make([]Point, len(r.buf)), start: r.start, n: r.n}
	copy(c.buf, r.buf)
	return c
}

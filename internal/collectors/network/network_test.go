package network

import (
	"context"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

func TestCollectRealNetwork(t *testing.T) {
	c := New(nil)
	first, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Primed || first.Status != metrics.StatusPending {
		t.Fatal("first reading cannot have rates")
	}
	time.Sleep(100 * time.Millisecond)
	second, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Primed || !second.OK() || second.RxBps < 0 || second.TxBps < 0 {
		t.Fatalf("second reading = %+v", second)
	}
}

func TestLoopbackNames(t *testing.T) {
	for _, n := range []string{"lo", "lo0", "Loopback Pseudo-Interface 1"} {
		if !isLoopbackName(n) {
			t.Errorf("%s should be loopback", n)
		}
	}
	if isLoopbackName("eth0") {
		t.Error("eth0 is not loopback")
	}
}

package cpu

import (
	"context"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/metrics"
)

func TestCollectRealCPU(t *testing.T) {
	c := New()
	first, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if first.Status != metrics.StatusPending {
		t.Fatalf("first reading must be pending, got %v", first.Status)
	}
	time.Sleep(150 * time.Millisecond)
	second, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != metrics.StatusOK {
		t.Fatalf("second reading status = %v", second.Status)
	}
	if second.Total < 0 || second.Total > 100 || len(second.PerCore) == 0 {
		t.Fatalf("implausible reading %+v", second)
	}
}

func BenchmarkCollect(b *testing.B) {
	c := New()
	for i := 0; i < b.N; i++ {
		if _, err := c.Collect(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

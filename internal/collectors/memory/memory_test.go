package memory

import (
	"context"
	"testing"
)

func TestCollectRealMemory(t *testing.T) {
	m, err := New().Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !m.OK() || m.Total == 0 || m.Used > m.Total || m.UsedPct <= 0 || m.UsedPct > 100 {
		t.Fatalf("implausible memory reading %+v", m)
	}
}

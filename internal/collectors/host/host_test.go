package host

import (
	"context"
	"testing"
	"time"
)

func TestCollectRealHost(t *testing.T) {
	h, err := New().Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Hostname == "" || h.LogicalCores == 0 {
		t.Fatalf("implausible host %+v", h)
	}
	if up := h.Uptime(time.Now()); up <= 0 {
		t.Fatalf("uptime = %v", up)
	}
}

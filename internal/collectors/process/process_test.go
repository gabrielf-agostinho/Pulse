package process

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCollectRealProcesses(t *testing.T) {
	c := New(func() uint64 { return 16 << 30 })
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	snap, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OK || snap.Total == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
	self := int32(os.Getpid())
	found := false
	for _, p := range snap.Processes {
		if p.PID == self {
			found = true
			if !p.CPUOK || !p.MemOK || p.RSS == 0 || p.Name == "" {
				t.Fatalf("own process must be fully readable: %+v", p)
			}
		}
		if p.CPU < 0 || p.CPU > 100 {
			t.Fatalf("cpu out of range: %+v", p)
		}
	}
	if !found {
		t.Fatal("own process not listed")
	}
}

func TestInspectSelfAndProtectedTerminate(t *testing.T) {
	var in Inspector
	d, err := in.Inspect(context.Background(), int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name == "" || d.Exe == "" {
		t.Fatalf("details = %+v", d)
	}
	if err := in.Terminate(context.Background(), int32(os.Getpid())); err == nil {
		t.Fatal("terminating self must be refused")
	}
}

func TestCollectHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(nil).Collect(ctx); err == nil {
		t.Fatal("cancelled context must abort collection")
	}
}

func BenchmarkCollect(b *testing.B) {
	c := New(func() uint64 { return 16 << 30 })
	for i := 0; i < b.N; i++ {
		if _, err := c.Collect(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

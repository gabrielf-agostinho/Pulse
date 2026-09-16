package disk

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestCollectRealDisks(t *testing.T) {
	c := New(Options{})
	d, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Volumes) == 0 {
		t.Skip("no volumes visible in this environment")
	}
	for _, v := range d.Volumes {
		if v.Err == "" && (v.Total == 0 || v.UsedPct < 0 || v.UsedPct > 100) {
			t.Fatalf("implausible volume %+v", v)
		}
	}
	time.Sleep(100 * time.Millisecond)
	d, _ = c.Collect(context.Background())
	if d.IO.OK() && !d.IO.Primed {
		t.Fatal("second reading must produce IO rates")
	}
}

func TestFilterExcludesAndDedupes(t *testing.T) {
	c := New(Options{ExcludeFSTypes: []string{"tmpfs"}, ExcludeMounts: []string{"/mnt/skip"}})
	parts := []disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/data", Fstype: "ext4"},
		{Device: "tmpfs", Mountpoint: "/tmp", Fstype: "tmpfs"},
		{Device: "/dev/sdb1", Mountpoint: "/mnt/skip", Fstype: "ext4"},
		{Device: "/dev/sda1", Mountpoint: "/data-bind", Fstype: "ext4"},
		{Device: "", Mountpoint: "", Fstype: ""},
		{Device: "", Mountpoint: "", Fstype: ""},
	}
	got := c.filter(parts)
	if len(got) != 1 || got[0].Mountpoint != "/data" {
		t.Fatalf("filtered = %+v", got)
	}
}

func BenchmarkCollect(b *testing.B) {
	c := New(Options{})
	for i := 0; i < b.N; i++ {
		_, _ = c.Collect(context.Background())
	}
}

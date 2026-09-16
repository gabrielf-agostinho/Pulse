//go:build linux

package disk

import (
	"os"
	"strings"
)

func ignoreMount(m string) bool {
	for _, p := range []string{"/proc", "/sys", "/dev", "/run", "/snap", "/var/lib/docker", "/var/snap"} {
		if m == p || strings.HasPrefix(m, p+"/") {
			return true
		}
	}
	return false
}

func countDevice(name string) bool {
	for _, p := range []string{"loop", "ram", "dm-", "md", "zram", "sr"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	_, err := os.Stat("/sys/block/" + name)
	return err == nil
}

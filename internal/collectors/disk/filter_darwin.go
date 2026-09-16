//go:build darwin

package disk

import "strings"

func ignoreMount(m string) bool {
	if m == "/System/Volumes/Data" {
		return false
	}
	return strings.HasPrefix(m, "/System/Volumes/") || strings.HasPrefix(m, "/private/var/vm") ||
		strings.HasPrefix(m, "/Library/Developer/CoreSimulator/")
}

func countDevice(string) bool { return true }

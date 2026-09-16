//go:build !linux && !darwin

package disk

func ignoreMount(string) bool { return false }

func countDevice(string) bool { return true }

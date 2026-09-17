//go:build !windows

package process

type basic struct {
	ppid    int32
	threads int32
	name    string
}

func basics() map[int32]basic { return nil }

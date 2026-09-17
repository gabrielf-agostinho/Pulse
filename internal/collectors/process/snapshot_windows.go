//go:build windows

package process

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type basic struct {
	ppid    int32
	threads int32
	name    string
}

func basics() map[int32]basic {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	out := make(map[int32]basic, 512)
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[int32(e.ProcessID)] = basic{
			ppid:    int32(e.ParentProcessID),
			threads: int32(e.Threads),
			name:    windows.UTF16ToString(e.ExeFile[:]),
		}
	}
	return out
}

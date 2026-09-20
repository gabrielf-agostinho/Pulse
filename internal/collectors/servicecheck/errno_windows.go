//go:build windows

package servicecheck

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isRefused(err error) bool { return errors.Is(err, windows.WSAECONNREFUSED) }

func isReset(err error) bool {
	return errors.Is(err, windows.WSAECONNRESET) || errors.Is(err, windows.WSAECONNABORTED)
}

func isUnreachable(err error) bool {
	return errors.Is(err, windows.WSAENETUNREACH) || errors.Is(err, windows.WSAEHOSTUNREACH)
}

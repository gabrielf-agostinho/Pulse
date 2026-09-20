//go:build !windows

package servicecheck

import (
	"errors"
	"syscall"
)

func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

func isReset(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED)
}

func isUnreachable(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)
}

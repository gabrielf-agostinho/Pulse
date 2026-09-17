package process

import (
	"errors"
	"strings"
	"syscall"
)

func isAccessDenied(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.EACCES || errno == syscall.EPERM) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") || strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted")
}

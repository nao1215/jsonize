//go:build windows

package cli

import (
	"errors"
	"syscall"
)

// Windows reports a reader that has gone as one of two errors: the pipe
// was closed before the write (ERROR_BROKEN_PIPE) or while it was being
// closed (ERROR_NO_DATA).
const (
	errBrokenPipe = syscall.Errno(109)
	errNoData     = syscall.Errno(232)
)

// outputClosed reports whether a write failed because the reader of
// standard output has gone.
func outputClosed(err error) bool {
	return errors.Is(err, errBrokenPipe) || errors.Is(err, errNoData) || errors.Is(err, syscall.EPIPE)
}

//go:build !windows

package cli

import (
	"errors"
	"syscall"
)

// outputClosed reports whether a write failed because the reader of
// standard output has gone: the pipe is broken.
func outputClosed(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

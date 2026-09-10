//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// keepGoingOnClosedOutput turns a closed standard output into a write
// error instead of an immediate death by SIGPIPE, so that jz gets to
// stop the command it started before it ends. The status it then ends
// with is the same 141 a filter killed by the signal would have.
func keepGoingOnClosedOutput() {
	signal.Ignore(syscall.SIGPIPE)
}

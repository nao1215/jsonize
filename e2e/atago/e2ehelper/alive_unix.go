//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// alive reports whether a process with this id exists. A child jz has
// already waited for is gone; one it left behind is still here.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// signalProcess sends p the signal a feed script names.
func signalProcess(p *os.Process, name string) error {
	switch name {
	case "INT":
		return p.Signal(syscall.SIGINT)
	case "TERM":
		return p.Signal(syscall.SIGTERM)
	}
	return fmt.Errorf("unknown signal %q (INT or TERM)", name)
}

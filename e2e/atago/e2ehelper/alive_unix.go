//go:build !windows

package main

import "syscall"

// alive reports whether a process with this id exists. A child jz has
// already waited for is gone; one it left behind is still here.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

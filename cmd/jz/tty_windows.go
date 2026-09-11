package main

import (
	"os"
	"syscall"
)

// isTerminal reports whether f is a console. NUL is a character device
// as a console is, and reading it is reading nothing.
func isTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}

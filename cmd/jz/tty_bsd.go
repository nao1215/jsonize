//go:build darwin || freebsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal reports whether f is a terminal: one that answers the
// request for its terminal attributes. Being a character device is not
// enough, since /dev/null is one too.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t))) //nolint:gosec // the ioctl fills the termios it is handed

	return errno == 0
}

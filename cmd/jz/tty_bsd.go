//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal reports whether f is a terminal: one that answers the
// request for its terminal attributes. Being a character device is not
// enough, since /dev/null is one too. Every BSD spells the request
// TIOCGETA, so they share this one.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t))) //nolint:gosec // the ioctl fills the termios it is handed

	return errno == 0
}

//go:build !linux && !darwin && !freebsd && !windows

package main

import "os"

// isTerminal falls back to a character device where jz has no way to
// ask for terminal attributes. It is only ever consulted to decide
// whether `jz` alone prints its help.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

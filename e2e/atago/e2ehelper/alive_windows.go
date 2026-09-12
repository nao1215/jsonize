//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// alive reports whether a process with this id is still running. On
// Windows a process that has ended can still be opened while a handle
// to it is held, so the exit code is what says whether it has ended.
func alive(pid int) bool {
	const stillActive = 259
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid)) //nolint:gosec // pid is what the producer wrote
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h) //nolint:errcheck // nothing to do with a failure to close
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// signalProcess is not available on Windows, which has no signals to send
// another process; a scenario that needs one is POSIX only.
func signalProcess(*os.Process, string) error {
	return errors.New("signal steps are POSIX only")
}

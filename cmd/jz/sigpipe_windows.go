//go:build windows

package main

// keepGoingOnClosedOutput is a no-op: Windows has no SIGPIPE, and a write
// to a closed pipe is already an error.
func keepGoingOnClosedOutput() {}

//go:build !windows

package runner

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// hasSignals reports that a child ended by a signal is seen as such, so
// a stop the runner asked for is told apart from the child's own status.
const hasSignals = true

// terminate asks the child to exit gracefully when the context is
// cancelled; exec.Cmd.WaitDelay kills it if it ignores the request.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

// interruptReached reports that the terminal delivered the interrupt jz
// received to p as well: jz's process group is the foreground group of
// its controlling terminal, and p is in that group. A process with no
// controlling terminal, as under a supervisor or in CI, gets 0 for the
// foreground group, and the interrupt is passed on.
//
// What is given up is an interrupt sent with kill to jz alone while it
// runs in the foreground of a terminal; a signal sent to the process
// group, or SIGTERM, still reaches the command.
func interruptReached(p *os.Process) bool {
	if p == nil {
		return false
	}
	command, err := syscall.Getpgid(p.Pid)
	if err != nil {
		command = 0
	}
	return terminalDelivered(foregroundGroup(), syscall.Getpgrp(), command)
}

// foregroundGroup returns the foreground process group of the controlling
// terminal, or 0 when there is none.
func foregroundGroup() int {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return 0
	}
	defer tty.Close()
	var pgrp int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgrp))) //nolint:gosec // the ioctl fills the process group id it is handed
	if errno != 0 {
		return 0
	}
	return int(pgrp)
}

// forward relays a signal received by jz to the child.
func forward(p *os.Process, s os.Signal) error {
	if p == nil {
		return nil
	}
	return p.Signal(s)
}

func signalName(ps *os.ProcessState) (sigInfo, bool) {
	if ps == nil {
		return sigInfo{}, false
	}
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return sigInfo{}, false
	}
	sig := ws.Signal()
	name, ok := signalNames[sig]
	if !ok {
		name = fmt.Sprintf("SIG%d", int(sig))
	}
	return sigInfo{name: name, number: int(sig)}, true
}

// signalNames covers the signals that commonly terminate a child. Names
// are spelled the way shells report them.
var signalNames = map[syscall.Signal]string{
	syscall.SIGHUP:  "SIGHUP",
	syscall.SIGINT:  "SIGINT",
	syscall.SIGQUIT: "SIGQUIT",
	syscall.SIGILL:  "SIGILL",
	syscall.SIGABRT: "SIGABRT",
	syscall.SIGFPE:  "SIGFPE",
	syscall.SIGKILL: "SIGKILL",
	syscall.SIGSEGV: "SIGSEGV",
	syscall.SIGPIPE: "SIGPIPE",
	syscall.SIGALRM: "SIGALRM",
	syscall.SIGTERM: "SIGTERM",
}

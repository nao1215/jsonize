//go:build !windows

package runner

import (
	"fmt"
	"os"
	"syscall"
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

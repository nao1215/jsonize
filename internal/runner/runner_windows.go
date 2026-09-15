//go:build windows

package runner

import "os"

// hasSignals is false: a killed child reports a plain exit status.
const hasSignals = false

// terminate kills the child; Windows has no graceful signal equivalent.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// forward relays an interrupt by killing the child, which is the only
// portable way to stop a console process on Windows.
// interruptReached is false: Windows has no process groups to share an
// interrupt, and jz ends the child itself.
func interruptReached(*os.Process) bool { return false }

func forward(p *os.Process, _ os.Signal) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

func signalName(_ *os.ProcessState) (sigInfo, bool) {
	return sigInfo{}, false
}

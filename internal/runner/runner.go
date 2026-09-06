// Package runner executes the target command in exec mode and captures its
// standard output for parsing while streaming standard error through to
// the user untouched.
//
// Commands are started directly (no shell), with LC_ALL=C forced so that
// output is stable across locales unless the caller opts out.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// DefaultMaxOutput bounds captured stdout.
const DefaultMaxOutput = 64 * 1024 * 1024

// ErrOutputTooLarge is returned when stdout exceeds the limit. The child
// is killed in that case.
var ErrOutputTooLarge = errors.New("command output exceeds the size limit")

// Command describes what to run.
type Command struct {
	Name string
	Args []string
	// Env holds extra variables (NAME=value) applied over the inherited
	// environment and the locale defaults. Later entries win.
	Env []string
	// KeepLocale disables forcing LC_ALL=C / LANG=C.
	KeepLocale bool
	// MaxOutput bounds stdout in bytes (0 = DefaultMaxOutput).
	MaxOutput int64
	// Dir is the working directory (empty = inherit).
	Dir string
	// Stdin is fed to the child (nil = no input).
	Stdin io.Reader
}

// Result is the outcome of a run.
type Result struct {
	Stdout []byte
	// ExitCode is the child's exit status, or 128+signal when the child was
	// terminated by a signal.
	ExitCode int
	// Signal names the terminating signal ("SIGTERM") when applicable.
	Signal string
	// Duration is the wall-clock time of the child.
	Duration time.Duration
}

// Run executes cmd. stderr receives the child's stderr as it is produced.
// Signals received on sigs are forwarded to the child. A non-zero exit
// status is reported in Result, not as an error; errors are reserved for
// failures to start the command or to capture its output.
func Run(ctx context.Context, cmd Command, stderr io.Writer, sigs <-chan os.Signal) (*Result, error) {
	if strings.TrimSpace(cmd.Name) == "" {
		return nil, errors.New("no command given")
	}
	path, err := exec.LookPath(cmd.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot run %q: %w", cmd.Name, err)
	}
	limit := cmd.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	// A derived context lets the output limiter stop the child; the
	// cause tells us afterwards why the run ended.
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	out := &limitedBuffer{limit: limit, cancel: cancel}

	c := exec.CommandContext(runCtx, path, cmd.Args...)
	c.Env = BuildEnv(os.Environ(), cmd.KeepLocale, cmd.Env)
	c.Dir = cmd.Dir
	c.Stdin = cmd.Stdin
	c.Stdout = out
	c.Stderr = stderr
	c.Cancel = func() error { return terminate(c.Process) }
	// If the child exits but a grandchild keeps the pipes open, give up on
	// them after this delay instead of hanging.
	c.WaitDelay = 3 * time.Second
	start := time.Now()
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("cannot run %q: %w", cmd.Name, err)
	}
	done := make(chan struct{})
	if sigs != nil {
		go func() {
			for {
				select {
				case s := <-sigs:
					_ = forward(c.Process, s)
				case <-done:
					return
				}
			}
		}()
	}
	waitErr := c.Wait()
	close(done)
	res := &Result{Stdout: out.buf.Bytes(), Duration: time.Since(start)}
	if errors.Is(context.Cause(runCtx), ErrOutputTooLarge) {
		return nil, fmt.Errorf("%w (%d bytes)", ErrOutputTooLarge, limit)
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			return nil, fmt.Errorf("waiting for %q: %w", cmd.Name, waitErr)
		}
		res.ExitCode = ee.ExitCode()
		if sig, ok := signalName(ee.ProcessState); ok {
			res.Signal = sig.name
			res.ExitCode = 128 + sig.number
		}
	}
	return res, nil
}

// limitedBuffer collects stdout and cancels the run once the limit is
// exceeded.
type limitedBuffer struct {
	buf    bytes.Buffer
	limit  int64
	cancel context.CancelCauseFunc
	over   bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.over {
		return len(p), nil
	}
	if int64(l.buf.Len())+int64(len(p)) > l.limit {
		l.over = true
		l.cancel(ErrOutputTooLarge)
		return len(p), nil
	}
	return l.buf.Write(p)
}

// BuildEnv merges the base environment, the locale defaults and extra
// NAME=value entries. Later entries override earlier ones and the result is
// sorted for reproducibility.
func BuildEnv(base []string, keepLocale bool, extra []string) []string {
	m := map[string]string{}
	order := []string{}
	set := func(kv string) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return
		}
		if _, seen := m[k]; !seen {
			order = append(order, k)
		}
		m[k] = v
	}
	for _, kv := range base {
		set(kv)
	}
	if !keepLocale {
		set("LC_ALL=C")
		set("LANG=C")
		delete(m, "LANGUAGE")
	}
	for _, kv := range extra {
		set(kv)
	}
	out := make([]string, 0, len(m))
	for _, k := range order {
		if v, ok := m[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	sort.Strings(out)
	return out
}

type sigInfo struct {
	name   string
	number int
}

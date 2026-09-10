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
	"os/signal"
	"sort"
	"strings"
	"syscall"
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
	// Stopped reports that the child was ended by the runner because the
	// consumer of its output stopped, so the status is the runner's doing
	// rather than anything the child said about itself.
	Stopped bool
}

// Run executes cmd. stderr receives the child's stderr as it is produced.
// An interrupt jz receives while the child runs is forwarded to it. A
// non-zero exit status is reported in Result, not as an error; errors are
// reserved for failures to start the command or to capture its output.
func Run(ctx context.Context, cmd Command, stderr io.Writer) (*Result, error) {
	return run(ctx, cmd, stderr, forwarded())
}

// run is Run with the signal source given, so a test can send one.
func run(ctx context.Context, cmd Command, stderr io.Writer, sigs *relay) (*Result, error) {
	defer sigs.stop()
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
	sigs.forwardTo(c.Process)
	waitErr := c.Wait()
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

// Stream runs cmd and hands its standard output to consume while the
// command is still producing it. There is no total output limit here:
// nothing is held, so nothing can grow without bound.
//
// consume is called once, on this goroutine, with a reader over the
// child's stdout. A consumer that returns nil before the output has
// ended (input.select.until) has seen what it needs and the child is
// left to finish: whatever it prints after that is drained so it cannot
// block on a full pipe, and its own status is reported. A consumer that
// returns an error will write nothing more, so the child is stopped
// rather than left to run for nobody, and Result.Stopped says so.
func Stream(ctx context.Context, cmd Command, stderr io.Writer, consume func(io.Reader) error) (*Result, error) {
	return stream(ctx, cmd, stderr, forwarded(), consume)
}

// stream is Stream with the signal source given, so a test can send one.
func stream(ctx context.Context, cmd Command, stderr io.Writer, sigs *relay, consume func(io.Reader) error) (*Result, error) {
	defer sigs.stop()
	if strings.TrimSpace(cmd.Name) == "" {
		return nil, errors.New("no command given")
	}
	path, err := exec.LookPath(cmd.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot run %q: %w", cmd.Name, err)
	}
	// Stopping the child goes through the context so that a child which
	// ignores the request is killed after WaitDelay, the same as a
	// timeout.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	c := exec.CommandContext(runCtx, path, cmd.Args...)
	c.Env = BuildEnv(os.Environ(), cmd.KeepLocale, cmd.Env)
	c.Dir = cmd.Dir
	c.Stdin = cmd.Stdin
	c.Stderr = stderr
	c.Cancel = func() error { return terminate(c.Process) }
	c.WaitDelay = 3 * time.Second
	// The pipe is jz's own rather than the one StdoutPipe would set up,
	// so that waiting for the command is waiting for the command: a
	// process it started and left behind may hold the writing end open,
	// and nothing more will be read from it once the command has ended.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("cannot read the output of %q: %w", cmd.Name, err)
	}
	defer pr.Close()
	c.Stdout = pw
	start := time.Now()
	if err := c.Start(); err != nil {
		_ = pw.Close()
		return nil, fmt.Errorf("cannot run %q: %w", cmd.Name, err)
	}
	_ = pw.Close()
	sigs.forwardTo(c.Process)
	consumeErr := consume(pr)
	if consumeErr != nil {
		stop()
	}
	// Whatever the consumer left is drained while the command finishes,
	// so it cannot block on a full pipe; the drain ends with the pipe,
	// which is closed once the command has, whoever else holds it.
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	waitErr := c.Wait()
	res := &Result{Duration: time.Since(start)}
	if waitErr != nil {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			if consumeErr != nil {
				return res, consumeErr
			}
			return nil, fmt.Errorf("waiting for %q: %w", cmd.Name, waitErr)
		}
		res.ExitCode = ee.ExitCode()
		if sig, ok := signalName(ee.ProcessState); ok {
			res.Signal = sig.name
			res.ExitCode = 128 + sig.number
		}
		// A child that ended by itself, however it ended, keeps its
		// status; only one that had to be stopped is the runner's doing.
		// On Windows there is no signal to see, so a kill is a plain
		// non-zero status and the stop is what tells the two apart.
		res.Stopped = consumeErr != nil && ctx.Err() == nil && (res.Signal != "" || !hasSignals)
	}
	return res, consumeErr
}

// relay carries the signals jz forwards to its child. It is subscribed
// before the child starts and unsubscribed once the child has been
// waited for, so an interrupt that arrives while jz has no child takes
// the default action and ends jz: a filter with nothing to forward to
// should stop when it is told to.
type relay struct {
	ch   chan os.Signal
	done chan struct{}
}

// forwarded subscribes to the interrupts jz forwards.
func forwarded() *relay {
	r := &relay{ch: make(chan os.Signal, 4), done: make(chan struct{})}
	signal.Notify(r.ch, os.Interrupt, syscall.SIGTERM)
	return r
}

// forwardTo relays every signal received to p until stop is called.
func (r *relay) forwardTo(p *os.Process) {
	if r == nil {
		return
	}
	go func() {
		for {
			select {
			case s := <-r.ch:
				_ = forward(p, s)
			case <-r.done:
				return
			}
		}
	}()
}

func (r *relay) stop() {
	if r == nil {
		return
	}
	signal.Stop(r.ch)
	close(r.done)
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

package main

// feed and relay are the two ends of a conversation with a filter that
// is not over: input handed over a piece at a time, and the output that
// piece should have produced waited for before the next is sent. It is
// how a scenario shows that jz writes a record when the lines of that
// record have come, rather than when its input ends, without a fixed
// sleep standing in for "a while".
//
//	e2ehelper feed [-deadline D] -script FILE -- COMMAND [ARG...]
//
// runs COMMAND with a pipe on its standard input and follows the script,
// one step per line:
//
//	send TEXT      write TEXT to COMMAND (\n \r \t \\ and \xHH are escapes)
//	sendfile PATH  write the bytes of PATH
//	expect N       wait up to the deadline for N more complete lines of
//	               COMMAND's standard output
//	close          end COMMAND's standard input
//	wait           let COMMAND end, up to the deadline
//	signal NAME    send COMMAND the signal (INT or TERM; POSIX only)
//
// Blank lines and lines starting with # are left out. What it prints is
// a transcript: "> expect N" and then the N lines that answered it, so a
// line written too early shows up under the wrong step and a line that
// never comes ends the script with "timeout" and status 1. "status=N"
// closes the transcript once COMMAND has ended; the lines it printed
// after the last expect come before it. COMMAND's standard error goes to
// the helper's own.
//
//	e2ehelper relay
//
// copies standard input to standard output as it arrives, which is what
// a command jz runs has to do for a feed to reach jz through it. The
// helper does the same when it is started under any other name, so a
// copy named ping or vmstat is a stand-in jz run lands on by name;
// standin.go says what else a stand-in can be told to do.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// feed runs the script against a command.
func feed(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("feed", flag.ContinueOnError)
	deadline := fs.Duration("deadline", 10*time.Second, "how long one expect or wait may take")
	script := fs.String("script", "", "the steps to follow")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 || *script == "" {
		return errors.New("feed: -script and a command are required")
	}
	steps, err := readScript(*script)
	if err != nil {
		return err
	}
	c, err := startFed(fs.Args(), stdout, *deadline)
	if err != nil {
		return err
	}
	for i, st := range steps {
		if err := c.do(st); err != nil {
			return c.fail(fmt.Errorf("step %d: %w", i+1, err))
		}
	}
	return nil
}

// fed is a command being fed, and where its output goes.
type fed struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	lines    chan string
	out      io.Writer
	deadline time.Duration
}

func startFed(argv []string, stdout io.Writer, deadline time.Duration) (*fed, error) {
	cmd := exec.CommandContext(context.Background(), argv[0], argv[1:]...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &fed{cmd: cmd, in: in, lines: make(chan string, 1024), out: stdout, deadline: deadline}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	return c, nil
}

// fail stops the command and hands back err.
func (c *fed) fail(err error) error {
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	return err
}

// do carries out one step.
func (c *fed) do(st step) error {
	switch st.op {
	case "send":
		_, err := io.WriteString(c.in, st.arg)
		return err
	case "sendfile":
		b, err := os.ReadFile(st.arg)
		if err != nil {
			return err
		}
		_, err = c.in.Write(b)
		return err
	case "expect":
		n, err := strconv.Atoi(st.arg)
		if err != nil {
			return fmt.Errorf("expect needs a number: %w", err)
		}
		return c.expect(n)
	case "close":
		return c.in.Close()
	case "signal":
		return signalProcess(c.cmd.Process, st.arg)
	case "wait":
		return c.wait()
	}
	return fmt.Errorf("unknown step %q", st.op)
}

// expect takes the next n lines of output, each within the deadline.
func (c *fed) expect(n int) error {
	fmt.Fprintf(c.out, "> expect %d\n", n)
	for got := range n {
		select {
		case l, ok := <-c.lines:
			if !ok {
				fmt.Fprintf(c.out, "eof after %d of %d lines\n", got, n)
				return fmt.Errorf("the output ended after %d of %d lines", got, n)
			}
			fmt.Fprintln(c.out, l)
		case <-time.After(c.deadline):
			fmt.Fprintf(c.out, "timeout after %d of %d lines\n", got, n)
			return fmt.Errorf("%d of %d lines within %s", got, n, c.deadline)
		}
	}
	return nil
}

// wait writes what the command printed after the last expect, in the
// order it came, and then how the command ended.
func (c *fed) wait() error {
	drained := make(chan struct{})
	go func() {
		for l := range c.lines {
			fmt.Fprintln(c.out, l)
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(c.deadline):
		fmt.Fprintln(c.out, "timeout waiting for the output to end")
		return fmt.Errorf("the output did not end within %s", c.deadline)
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		fmt.Fprintf(c.out, "status=%d\n", statusOf(err))
		return nil
	case <-time.After(c.deadline):
		_ = c.cmd.Process.Kill()
		fmt.Fprintln(c.out, "status=hung")
		return fmt.Errorf("%s did not end within %s", c.cmd.Path, c.deadline)
	}
}

type step struct {
	op, arg string
}

func readScript(path string) ([]step, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var steps []step
	for n, raw := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		op, arg, _ := strings.Cut(raw, " ")
		switch op {
		case "send":
			text, err := unescape(arg)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n+1, err)
			}
			arg = text
		case "sendfile", "expect", "close", "wait", "signal":
		default:
			return nil, fmt.Errorf("%s:%d: unknown step %q", path, n+1, op)
		}
		steps = append(steps, step{op: op, arg: arg})
	}
	return steps, nil
}

// unescape reads the escapes a send step may carry.
func unescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", errors.New("a backslash at the end of a send")
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case 'x':
			if i+2 >= len(s) {
				return "", errors.New(`\x needs two hex digits`)
			}
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", fmt.Errorf(`\x%s: %w`, s[i+1:i+3], err)
			}
			b.WriteByte(byte(v))
			i += 2
		default:
			return "", fmt.Errorf(`unknown escape \%c`, s[i])
		}
	}
	return b.String(), nil
}

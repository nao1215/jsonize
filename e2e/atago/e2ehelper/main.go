// Command e2ehelper stands in for the two ends of a pipe in the
// end-to-end suite, on every operating system jz runs on.
//
// It exists because the scenarios about a command that keeps printing
// need a producer that prints a burst and then keeps printing at a
// pace, and a reader that takes a few lines and leaves, and need to know afterwards what became
// of both. A shell script can be that on POSIX; there is no portable
// shell on Windows, and a Go program is the one thing every runner of
// this suite already has the means to build.
//
//	e2ehelper emit [-lines N] [-burst K] [-interval D] [-pid FILE]
//
// prints N lines in the shape of du output: the first K at once, the
// rest one every D, the way a monitoring command prints a sample per
// interval. It records its process id in FILE first, so the reader can
// tell afterwards whether it was stopped. The burst is what gets the
// reader its lines; the paced lines after it are what jz's next write
// fails on once the reader has gone, and what is left unprinted when
// the producer is stopped.
//
//	e2ehelper pipe [-take N] [-deadline D] [-pid FILE] -- COMMAND [ARG...]
//
// runs COMMAND with its standard output piped here, reads N lines, then
// closes its end of the pipe the way `head` does and waits for COMMAND
// to end. It prints what it saw, one fact per line, for a scenario to
// assert on: how many lines it took, whether the process named in FILE
// was still running when it had them, COMMAND's status, and whether that
// process was gone once COMMAND had ended. A COMMAND that does not end
// within the deadline is reported as hung and the helper exits 1.
//
//	e2ehelper gone -pid FILE [-deadline D]
//
// waits for the process recorded in FILE to end and prints whether it
// did, for a scenario that ran the pipeline some other way.
package main

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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: e2ehelper emit|pipe [options]")
		return 2
	}
	var err error
	switch args[0] {
	case "emit":
		err = emit(args[1:], stdout)
	case "pipe":
		err = pipe(args[1:], stdout)
	case "gone":
		err = gone(args[1:], stdout)
	default:
		err = fmt.Errorf("unknown mode %q", args[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "e2ehelper: %v\n", err)
		return 1
	}
	return 0
}

// emit prints du-shaped lines: a size, a tab and a path.
func emit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	lines := fs.Int("lines", 10, "how many lines to print in all")
	burst := fs.Int("burst", 0, "how many lines to print at once before pacing the rest")
	interval := fs.Duration("interval", 0, "how long to wait between the lines after the burst")
	pidFile := fs.String("pid", "", "write the process id here before printing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pidFile != "" {
		if err := os.WriteFile(*pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return err
		}
	}
	w := bufio.NewWriter(stdout)
	for i := 1; i <= *lines; i++ {
		if i > *burst {
			// A paced line is written on its own, so the reader sees it
			// when it is printed and not when the buffer happens to fill.
			if err := w.Flush(); err != nil {
				return err
			}
			time.Sleep(*interval)
		}
		fmt.Fprintf(w, "%d\t/mnt/%d\n", i*4, i)
	}
	return w.Flush()
}

// pipe runs a command, takes a few lines of its output and leaves.
func pipe(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pipe", flag.ContinueOnError)
	take := fs.Int("take", 1, "how many lines to read before closing the pipe")
	deadline := fs.Duration("deadline", 10*time.Second, "how long the command may take to end")
	pidFile := fs.String("pid", "", "the file the producer records its process id in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("pipe: no command given")
	}
	cmd := exec.CommandContext(context.Background(), fs.Arg(0), fs.Args()[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The command's own stdout stays open on the command's side; the
	// reading end is what this closes, so the next write the command
	// makes has nowhere to go.
	sc := bufio.NewScanner(out)
	got := 0
	for got < *take && sc.Scan() {
		got++
	}
	fmt.Fprintf(stdout, "lines=%d\n", got)
	if *pidFile != "" {
		fmt.Fprintf(stdout, "producer_alive_at_take=%t\n", aliveNow(*pidFile))
	}
	_ = out.Close()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		fmt.Fprintf(stdout, "status=%d\n", statusOf(err))
	case <-time.After(*deadline):
		_ = cmd.Process.Kill()
		fmt.Fprintln(stdout, "status=hung")
		return fmt.Errorf("%s did not end within %s of its output being closed", fs.Arg(0), *deadline)
	}
	if *pidFile != "" {
		fmt.Fprintf(stdout, "producer=%s\n", goneWithin(*pidFile, *deadline))
	}
	return nil
}

// gone waits for the process a file names to end.
func gone(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gone", flag.ContinueOnError)
	deadline := fs.Duration("deadline", 10*time.Second, "how long to wait for the process to end")
	pidFile := fs.String("pid", "", "the file the process recorded its id in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pidFile == "" {
		return errors.New("gone: -pid is required")
	}
	fmt.Fprintf(stdout, "producer=%s\n", goneWithin(*pidFile, *deadline))
	return nil
}

// statusOf reads an exit status the way a shell reports it.
func statusOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// aliveNow reports whether the process recorded in pidFile is running
// right now.
func aliveNow(pidFile string) bool {
	pid, err := readPID(pidFile)
	if err != nil {
		return false
	}
	return alive(pid)
}

// goneWithin polls until the process recorded in pidFile has ended, or
// the deadline passes.
func goneWithin(pidFile string, deadline time.Duration) string {
	pid, err := readPID(pidFile)
	if err != nil {
		return "unknown"
	}
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if !alive(pid) {
			return "gone"
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "alive"
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

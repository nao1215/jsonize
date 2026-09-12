package main

// A stand-in is a copy of the helper under the name of a command, which
// is how jz run reaches it: jz chooses the parser from the name of what it
// runs. It is the command a scenario needs on every operating system, in
// place of a shell script, which Windows would not run. What the copy does
// is set by the environment jz passes on to it, in this order:
//
//	E2EHELPER_PRINT_ARGS  when set, print "args=" and the arguments,
//	                      joined by spaces
//	E2EHELPER_PRINT_ENV   variable names, comma-separated, each printed
//	                      as NAME=VALUE
//	E2EHELPER_STDERR      text written to standard error
//
// then standard input is copied to standard output as it arrives, and
//
//	E2EHELPER_LEAVE       a duration: start a process that holds standard
//	                      output open that long, and end without waiting
//	                      for it, the way `sleep 8 &` in a script does
//	E2EHELPER_EXIT        the status to end with
//
//	e2ehelper as NAME DIR
//
// copies the helper to DIR/NAME, with the extension Windows runs a
// program by, and prints the path. A scenario makes its stand-ins in the
// suite's setup, before anything else runs.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// holdKey marks the process a stand-in leaves behind: it keeps the
// output it was given open for this long and does nothing else.
const holdKey = "E2EHELPER_HOLD"

// relay is what a stand-in does.
func relay(stdin io.Reader, stdout, stderr io.Writer) int {
	if d, err := time.ParseDuration(os.Getenv(holdKey)); err == nil {
		time.Sleep(d)
		return 0
	}
	if os.Getenv("E2EHELPER_PRINT_ARGS") != "" {
		fmt.Fprintf(stdout, "args=%s\n", strings.Join(os.Args[1:], " "))
	}
	if names := os.Getenv("E2EHELPER_PRINT_ENV"); names != "" {
		for _, name := range strings.Split(names, ",") {
			fmt.Fprintf(stdout, "%s=%s\n", name, os.Getenv(name))
		}
	}
	if msg := os.Getenv("E2EHELPER_STDERR"); msg != "" {
		fmt.Fprintln(stderr, msg)
	}
	buf := make([]byte, 4096)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			if _, werr := stdout.Write(buf[:n]); werr != nil {
				return 141
			}
		}
		if err != nil {
			break
		}
	}
	if d, err := time.ParseDuration(os.Getenv("E2EHELPER_LEAVE")); err == nil {
		if err := leave(d, stdout); err != nil {
			fmt.Fprintf(stderr, "e2ehelper: %v\n", err)
			return 1
		}
	}
	if code, err := strconv.Atoi(os.Getenv("E2EHELPER_EXIT")); err == nil {
		return code
	}
	return 0
}

// leave starts a copy of the helper that holds stdout open for d. Its
// standard error goes nowhere: that stream is the caller's capture, and
// only the output is meant to be held.
func leave(d time.Duration, stdout io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self) //nolint:noctx // the process is left to run on its own
	cmd.Stdout = stdout
	cmd.Env = append(withoutKnobs(os.Environ()), holdKey+"="+d.String())
	return cmd.Start()
}

// withoutKnobs drops the stand-in's own settings from env.
func withoutKnobs(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(strings.ToUpper(kv), "E2EHELPER_") {
			out = append(out, kv)
		}
	}
	return out
}

// as copies the helper to DIR/NAME.
func as(args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New("as: NAME and DIR are required")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	name := args[0]
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(args[1], name)
	if err := copyFile(self, dst); err != nil {
		return err
	}
	fmt.Fprintln(stdout, dst)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // a program is meant to be run
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

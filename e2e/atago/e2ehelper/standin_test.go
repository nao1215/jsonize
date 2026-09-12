package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRelayStatusAndStandardError(t *testing.T) {
	t.Setenv("E2EHELPER_EXIT", "3")
	t.Setenv("E2EHELPER_STDERR", "warning: something")
	var out, stderr bytes.Buffer
	if code := relay(strings.NewReader("x\ny"), &out, &stderr); code != 3 {
		t.Errorf("code = %d", code)
	}
	if out.String() != "x\ny" || stderr.String() != "warning: something\n" {
		t.Errorf("out %q, stderr %q", out.String(), stderr.String())
	}
}

// What a stand-in was started with comes before what it relays.
func TestRelayPrintsItsArgumentsAndEnvironment(t *testing.T) {
	t.Setenv("E2EHELPER_PRINT_ARGS", "1")
	t.Setenv("E2EHELPER_PRINT_ENV", "LC_ALL,E2EHELPER_UNSET")
	t.Setenv("LC_ALL", "C")
	var out bytes.Buffer
	if code := relay(strings.NewReader("a=b\n"), &out, os.Stderr); code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := "args=" + strings.Join(os.Args[1:], " ") + "\nLC_ALL=C\nE2EHELPER_UNSET=\na=b\n"
	if out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

// The process a stand-in leaves holds the output open after the stand-in
// has ended: a reader of the pipe sees its end only when that process
// has gone too.
func TestRelayLeavesAProcessHoldingItsOutput(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), os.Args[0])
	cmd.Env = append(os.Environ(), "E2EHELPER_ARGS=relay", "E2EHELPER_LEAVE=1s")
	cmd.Stdin = strings.NewReader("a=b\n")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	held := time.Since(start)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if string(b) != "a=b\n" {
		t.Errorf("output = %q", b)
	}
	if held < time.Second {
		t.Errorf("the output ended after %s, before the process left behind had", held)
	}
}

// A copy made by as is a program the system runs by its name.
func TestAsCopiesTheHelperUnderAName(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if code := run([]string{"as", "standin", dir}, &out, os.Stderr); code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := filepath.Join(dir, "standin")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if strings.TrimSpace(out.String()) != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
	path, err := exec.LookPath(filepath.Join(dir, "standin"))
	if err != nil {
		t.Fatalf("the copy is not a program: %v", err)
	}
	cmd := exec.CommandContext(context.Background(), path, "-x")
	cmd.Env = append(os.Environ(), "E2EHELPER_ARGS=relay", "E2EHELPER_PRINT_ARGS=1")
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "args=-x\n" {
		t.Errorf("the copy printed %q", b)
	}
	if code := run([]string{"as", "standin"}, &out, &bytes.Buffer{}); code != 1 {
		t.Errorf("as without a directory: code = %d", code)
	}
}

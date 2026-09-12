package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for the helper when asked to, so pipe can be
// tried against a real process without building anything first.
func TestMain(m *testing.M) {
	if args := os.Getenv("E2EHELPER_ARGS"); args != "" {
		os.Exit(run(strings.Fields(args), os.Stdout, os.Stderr))
	}
	// The process a stand-in leaves behind is a copy of this binary too.
	if os.Getenv(holdKey) != "" {
		os.Exit(relay(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestEmitPrintsDuShapedLines(t *testing.T) {
	var out bytes.Buffer
	pid := filepath.Join(t.TempDir(), "pid")
	if code := run([]string{"emit", "-lines", "3", "-pid", pid}, &out, os.Stderr); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if out.String() != "4\t/mnt/1\n8\t/mnt/2\n12\t/mnt/3\n" {
		t.Errorf("stdout = %q", out.String())
	}
	if got, err := readPID(pid); err != nil || got != os.Getpid() {
		t.Errorf("pid file = %d, %v; want %d", got, err, os.Getpid())
	}
}

// pipe takes its lines while the producer is still running, closes the
// pipe, and reports that the producer ended. The producer here is the
// helper itself, which ends on its own: the first paced write after the
// pipe is closed fails because nobody reads it.
func TestPipeTakesLinesAndWaits(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "pid")
	var out bytes.Buffer
	t.Setenv("E2EHELPER_ARGS", "emit -lines 1000 -burst 250 -interval 100ms -pid "+pid)
	start := time.Now()
	if code := run([]string{"pipe", "-take", "2", "-pid", pid, "-deadline", "20s", "--", os.Args[0]}, &out, os.Stderr); code != 0 {
		t.Fatalf("code = %d, out = %s", code, out.String())
	}
	for _, want := range []string{"lines=2\n", "producer_alive_at_take=true\n", "producer=gone\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out = %q, want %q in it", out.String(), want)
		}
	}
	if !strings.Contains(out.String(), "status=") {
		t.Errorf("out = %q, want a status", out.String())
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

func TestUsageErrors(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, os.Stdout, &stderr); code != 2 {
		t.Errorf("no mode: code = %d", code)
	}
	if code := run([]string{"nope"}, os.Stdout, &stderr); code != 1 {
		t.Errorf("unknown mode: code = %d", code)
	}
	if code := run([]string{"pipe"}, os.Stdout, &stderr); code != 1 {
		t.Errorf("pipe without a command: code = %d", code)
	}
	if code := run([]string{"gone"}, os.Stdout, &stderr); code != 1 {
		t.Errorf("gone without a pid file: code = %d", code)
	}
}

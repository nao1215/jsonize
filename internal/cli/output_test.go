package cli

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// closedOutput is a standard output whose reader has gone: the first
// write fails the way a broken pipe does, and every write after it too.
type closedOutput struct{}

func (closedOutput) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// A reader that has gone away is not a failure to report: the document
// cannot be finished, so jz says nothing and ends with 128+SIGPIPE, the
// status a filter killed by the signal would carry.
func TestClosedOutputIsSilent(t *testing.T) {
	df := "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs 100 10 90 10% /run\n"
	t.Run("whole document", func(t *testing.T) {
		h := newHarness(t)
		h.env.Stdout = closedOutput{}
		if code := h.pipe(df); code != ExitOutputClosed {
			t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
		}
		if h.stderr.Len() != 0 {
			t.Errorf("stderr = %q, want nothing", h.stderr.String())
		}
	})
	t.Run("stream", func(t *testing.T) {
		h := newHarness(t)
		h.env.Stdout = closedOutput{}
		if code := h.pipe(df, "--stream"); code != ExitOutputClosed {
			t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
		}
		if h.stderr.Len() != 0 {
			t.Errorf("stderr = %q, want nothing", h.stderr.String())
		}
	})
	t.Run("a data file stream and jz new --each", func(t *testing.T) {
		for _, args := range [][]string{{"--format", "lines", "--stream"}, {"new", "--each", "line=@-"}} {
			h := newHarness(t)
			h.env.Stdout = closedOutput{}
			if code := h.pipe("a\nb\n", args...); code != ExitOutputClosed || h.stderr.Len() != 0 {
				t.Errorf("%v: code = %d, stderr = %q", args, code, h.stderr.String())
			}
		}
	})
	t.Run("list", func(t *testing.T) {
		h := newHarness(t)
		h.env.Stdout = closedOutput{}
		if code := h.run("list", "--json"); code != ExitOutputClosed {
			t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
		}
	})
}

// With --stream the reader can go while the command is still printing.
// The command is stopped rather than left to print for nobody, and the
// stop is jz's own doing, so it is not reported as the command's status.
func TestClosedOutputStopsTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	h.env.Stdout = closedOutput{}
	start := time.Now()
	// Detection holds back a window of lines before it commits, so the
	// command prints past it before it hangs.
	code := h.run("run", "--stream", "sh", "-c", "i=0; while [ $i -lt 100 ]; do echo a$i=1; i=$((i+1)); done; exec sleep 30")
	if code != ExitOutputClosed {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", h.stderr.String())
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %s: the command was not stopped", time.Since(start))
	}
}

// A stream that fails for a reason of its own (here a key the format
// does not produce) will write nothing more, so the command is stopped
// and the failure is what is returned: the stop is not the command's
// status, and it is not reported as if it were.
func TestStreamFailureStopsTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	start := time.Now()
	code := h.run("run", "--stream", "--extract", "nope", "sh", "-c", "i=0; while [ $i -lt 100 ]; do echo a$i=1; i=$((i+1)); done; exec sleep 30")
	if code != ExitUsage {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), `no key "nope"`) || strings.Contains(h.stderr.String(), "terminated") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %s: the command was not stopped", time.Since(start))
	}
	// A command that had already ended keeps its own status.
	code = h.run("run", "--stream", "--extract", "nope", "sh", "-c", "echo a=1; exit 7")
	if code != 7 || !strings.Contains(h.stderr.String(), "exited with status 7") {
		t.Errorf("code = %d, stderr = %q", code, h.stderr.String())
	}
}

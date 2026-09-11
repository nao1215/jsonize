package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBuildEnv(t *testing.T) {
	t.Parallel()
	base := []string{"PATH=/bin", "LANG=ja_JP.UTF-8", "LC_ALL=ja_JP.UTF-8", "LANGUAGE=ja", "BROKEN", "=novar"}
	got := BuildEnv(base, false, []string{"TZ=UTC", "PATH=/usr/bin"})
	want := "LANG=C LC_ALL=C PATH=/usr/bin TZ=UTC"
	if strings.Join(got, " ") != want {
		t.Errorf("BuildEnv = %v, want %s", got, want)
	}
	got = BuildEnv(base, true, nil)
	if !strings.Contains(strings.Join(got, " "), "LANG=ja_JP.UTF-8") || !strings.Contains(strings.Join(got, " "), "LANGUAGE=ja") {
		t.Errorf("keep locale: %v", got)
	}
}

func TestRunCapturesStdoutAndStderr(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	var stderr bytes.Buffer
	res, err := Run(context.Background(), Command{Name: "go", Args: []string{"env", "GOOS"}}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(res.Stdout)) != runtime.GOOS || res.ExitCode != 0 || res.Signal != "" {
		t.Errorf("result = %+v", res)
	}
	if res.Duration <= 0 {
		t.Error("duration not recorded")
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()
	if _, err := Run(context.Background(), Command{Name: "  "}, os.Stderr); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := Run(context.Background(), Command{Name: "jsonize-definitely-missing-binary"}, os.Stderr); err == nil || !strings.Contains(err.Error(), "cannot run") {
		t.Errorf("missing binary: %v", err)
	}
}

func TestRunPosix(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	t.Run("exit code passthrough with stderr", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}, &stderr)
		if err != nil {
			t.Fatal(err)
		}
		if string(res.Stdout) != "out\n" || res.ExitCode != 3 || res.Cut || stderr.String() != "err\n" {
			t.Errorf("result = %+v stderr=%q", res, stderr.String())
		}
	})
	t.Run("locale is forced and env applied", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo $LC_ALL $JZ_TEST"}, Env: []string{"JZ_TEST=yes"}}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if string(res.Stdout) != "C yes\n" {
			t.Errorf("stdout = %q", res.Stdout)
		}
	})
	t.Run("stdin is passed", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "cat"}, Stdin: strings.NewReader("ping")}, os.Stderr)
		if err != nil || string(res.Stdout) != "ping" {
			t.Errorf("stdin: %q %v", res.Stdout, err)
		}
	})
	t.Run("output limit kills the child", func(t *testing.T) {
		t.Parallel()
		_, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "yes | head -c 100000"}, MaxOutput: 1000}, os.Stderr)
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Errorf("expected ErrOutputTooLarge, got %v", err)
		}
	})
	t.Run("child killed by signal reports 128+n", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "kill -TERM $$"}}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 143 || res.Signal != "SIGTERM" || !res.Cut {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("forwarded signal reaches the child", func(t *testing.T) {
		t.Parallel()
		sigs := testRelay()
		go func() {
			time.Sleep(200 * time.Millisecond)
			sigs.ch <- syscall.SIGTERM
		}()
		res, err := run(context.Background(), Command{Name: "sh", Args: []string{"-c", "exec sleep 10"}}, os.Stderr, sigs)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 143 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("context cancellation terminates the child", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		res, err := Run(ctx, Command{Name: "sh", Args: []string{"-c", "exec sleep 10"}}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if res.Signal != "SIGTERM" || !res.Cut {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestSignalNameNil(t *testing.T) {
	t.Parallel()
	if _, ok := signalName(nil); ok {
		t.Error("nil state should not be signalled")
	}
	if err := terminate(nil); err != nil {
		t.Error(err)
	}
	if err := forward(nil, os.Interrupt); err != nil {
		t.Error(err)
	}
}

func TestStreamHandsOutputOverAsItArrives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	t.Parallel()
	var stderr bytes.Buffer
	var got []byte
	res, err := Stream(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "echo one; echo two; echo problem >&2; exit 4"},
	}, &stderr, func(r io.Reader) error {
		var err error
		got, err = io.ReadAll(r)
		return err
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if string(got) != "one\ntwo\n" {
		t.Errorf("stdout = %q", got)
	}
	if res.ExitCode != 4 || !strings.Contains(stderr.String(), "problem") {
		t.Errorf("res = %+v stderr = %q", res, stderr.String())
	}
}

// A command ended from outside stops wherever its output had got to,
// in the middle of a line as often as not. The reader says so at the
// end instead of passing the cut for the end the command chose.
func TestStreamSaysWhenTheOutputWasCutShort(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	tests := []struct {
		name   string
		script string
		ctx    time.Duration
	}{
		{"killed by a signal", "printf 'one\\ntw'; kill -TERM $$", 0},
		{"stopped at the deadline", "printf 'one\\ntw'; exec sleep 30", 300 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.ctx > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctx)
				defer cancel()
			}
			var got []byte
			var readErr error
			res, err := Stream(ctx, Command{Name: "sh", Args: []string{"-c", tt.script}}, io.Discard, func(r io.Reader) error {
				got, readErr = io.ReadAll(r)
				return nil
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			if string(got) != "one\ntw" || !errors.Is(readErr, ErrCut) {
				t.Errorf("read %q, %v; want the text and ErrCut", got, readErr)
			}
			if res == nil || !res.Cut || res.Stopped || res.Signal != "SIGTERM" {
				t.Errorf("res = %+v", res)
			}
		})
	}
}

// A command that ends and leaves a process behind holding its output
// open has still ended. What it printed is its output; waiting for the
// process it left would wait as long as that runs, which for a daemon is
// for ever.
func TestCommandThatLeavesItsOutputOpen(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	script := "sleep 6 & echo out"
	t.Run("whole", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", script}}, io.Discard)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if string(res.Stdout) != "out\n" || res.ExitCode != 0 || !res.LeftOpen || res.Cut {
			t.Errorf("res = %+v", res)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("waited %s for the process the command left", time.Since(start))
		}
	})
	t.Run("stream", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		var got []byte
		var readErr error
		res, err := Stream(context.Background(), Command{Name: "sh", Args: []string{"-c", script}}, io.Discard, func(r io.Reader) error {
			got, readErr = io.ReadAll(r)
			return nil
		})
		if err != nil || readErr != nil {
			t.Fatalf("Stream: %v %v", err, readErr)
		}
		if string(got) != "out\n" || res.ExitCode != 0 || !res.LeftOpen || res.Cut {
			t.Errorf("read %q, res = %+v", got, res)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("waited %s for the process the command left", time.Since(start))
		}
	})
}

// A consumer that has seen enough and returns nil must not leave the
// child blocked on a full pipe: whatever it left is drained before the
// wait, and the child's own status is what comes back.
func TestStreamDrainsWhatTheConsumerLeft(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	t.Parallel()
	res, err := Stream(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "i=0; while [ $i -lt 20000 ]; do echo line$i; i=$((i+1)); done; exit 3"},
	}, io.Discard, func(r io.Reader) error {
		buf := make([]byte, 8)
		_, _ = r.Read(buf)
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res == nil || res.ExitCode != 3 || res.Stopped {
		t.Errorf("res = %+v", res)
	}
}

// A consumer that fails will write nothing more, so the child is stopped
// instead of being left to run for nobody: a command that never ends
// would otherwise keep jz waiting forever.
func TestStreamStopsTheChildWhenTheConsumerFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	t.Parallel()
	stop := errors.New("stop")
	start := time.Now()
	res, err := Stream(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "echo first; exec sleep 30"},
	}, io.Discard, func(r io.Reader) error {
		buf := make([]byte, 6)
		_, _ = io.ReadFull(r, buf)
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v", err)
	}
	if res == nil || !res.Stopped || res.Signal != "SIGTERM" || res.ExitCode != 143 {
		t.Errorf("res = %+v", res)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("waited %s for a child that should have been stopped", time.Since(start))
	}
}

// A child that had already ended when the consumer failed keeps its own
// status: the failure did not stop it.
func TestStreamKeepsTheStatusOfAChildThatEndedByItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	t.Parallel()
	stop := errors.New("stop")
	res, err := Stream(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "echo only; exit 7"},
	}, io.Discard, func(r io.Reader) error {
		_, _ = io.ReadAll(r)
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v", err)
	}
	if res == nil || res.Stopped || res.ExitCode != 7 {
		t.Errorf("res = %+v", res)
	}
}

// testRelay is a signal source a test feeds by hand; nothing is
// subscribed, so stop has nothing to undo.
func testRelay() *relay {
	return &relay{ch: make(chan os.Signal, 1), done: make(chan struct{})}
}

func TestStreamErrors(t *testing.T) {
	t.Parallel()
	if _, err := Stream(context.Background(), Command{Name: "  "}, io.Discard, func(io.Reader) error { return nil }); err == nil {
		t.Error("empty command should fail")
	}
	if _, err := Stream(context.Background(), Command{Name: "definitely-missing-binary"}, io.Discard, func(io.Reader) error { return nil }); err == nil {
		t.Error("missing binary should fail")
	}
}

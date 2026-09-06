package runner

import (
	"bytes"
	"context"
	"errors"
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
	res, err := Run(context.Background(), Command{Name: "go", Args: []string{"env", "GOOS"}}, &stderr, nil)
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
	if _, err := Run(context.Background(), Command{Name: "  "}, os.Stderr, nil); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := Run(context.Background(), Command{Name: "jsonize-definitely-missing-binary"}, os.Stderr, nil); err == nil || !strings.Contains(err.Error(), "cannot run") {
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
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}, &stderr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(res.Stdout) != "out\n" || res.ExitCode != 3 || stderr.String() != "err\n" {
			t.Errorf("result = %+v stderr=%q", res, stderr.String())
		}
	})
	t.Run("locale is forced and env applied", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo $LC_ALL $JZ_TEST"}, Env: []string{"JZ_TEST=yes"}}, os.Stderr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(res.Stdout) != "C yes\n" {
			t.Errorf("stdout = %q", res.Stdout)
		}
	})
	t.Run("stdin is passed", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "cat"}, Stdin: strings.NewReader("ping")}, os.Stderr, nil)
		if err != nil || string(res.Stdout) != "ping" {
			t.Errorf("stdin: %q %v", res.Stdout, err)
		}
	})
	t.Run("output limit kills the child", func(t *testing.T) {
		t.Parallel()
		_, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "yes | head -c 100000"}, MaxOutput: 1000}, os.Stderr, nil)
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Errorf("expected ErrOutputTooLarge, got %v", err)
		}
	})
	t.Run("child killed by signal reports 128+n", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "kill -TERM $$"}}, os.Stderr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 143 || res.Signal != "SIGTERM" {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("forwarded signal reaches the child", func(t *testing.T) {
		t.Parallel()
		sigs := make(chan os.Signal, 1)
		go func() {
			time.Sleep(200 * time.Millisecond)
			sigs <- syscall.SIGTERM
		}()
		res, err := Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "exec sleep 10"}}, os.Stderr, sigs)
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
		res, err := Run(ctx, Command{Name: "sh", Args: []string{"-c", "exec sleep 10"}}, os.Stderr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Signal != "SIGTERM" {
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

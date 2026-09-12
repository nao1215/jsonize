package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The helper feeds itself: the test binary started with "relay" copies
// what it is sent, so each expect is answered by the send before it.
func TestFeedWaitsForEachAnswer(t *testing.T) {
	t.Setenv("E2EHELPER_ARGS", "relay")
	script := writeScript(t, "# comment\nsend a\\n\nexpect 1\nsend b\\x0Ac\\n\nexpect 2\nsend tail\\n\nclose\nwait\n")
	var out bytes.Buffer
	if code := run([]string{"feed", "-deadline", "10s", "-script", script, "--", os.Args[0]}, &out, os.Stderr); code != 0 {
		t.Fatalf("code = %d, out = %s", code, out.String())
	}
	want := "> expect 1\na\n> expect 2\nb\nc\ntail\nstatus=0\n"
	if out.String() != want {
		t.Errorf("transcript = %q, want %q", out.String(), want)
	}
}

// A line that never comes ends the script at the deadline instead of
// hanging it.
func TestFeedTimesOut(t *testing.T) {
	t.Setenv("E2EHELPER_ARGS", "relay")
	script := writeScript(t, "send no newline yet\nexpect 1\n")
	var out, stderr bytes.Buffer
	if code := run([]string{"feed", "-deadline", "200ms", "-script", script, "--", os.Args[0]}, &out, &stderr); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "timeout after 0 of 1 lines") {
		t.Errorf("transcript = %q", out.String())
	}
}

func TestReadScriptRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, body := range []string{"shout x\n", "send \\q\n", "send \\x4\n", "send end\\\n"} {
		if _, err := readScript(writeScript(t, body)); err == nil {
			t.Errorf("%q was accepted", body)
		}
	}
}

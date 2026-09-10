package main

import (
	"os"
	"path/filepath"
	"testing"
)

// /dev/null is a character device, as a terminal is, and reading it is
// reading nothing: cron, a CI step and `ssh -n` leave it on standard
// input. Taking it for a terminal printed the help on standard output
// with status 0, where empty input is refused.
func TestOnlyATerminalIsATerminal(t *testing.T) {
	t.Parallel()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if isTerminal(null) {
		t.Errorf("%s was taken for a terminal", os.DevNull)
	}
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if isTerminal(file) {
		t.Error("a regular file was taken for a terminal")
	}
}

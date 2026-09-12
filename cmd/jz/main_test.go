package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// jz reads text and local directories and nothing else: conversion, jz
// run's reading of a command's output and shell completion alike. No
// package that can open a connection is compiled in, on any system jz
// is built for, which is a stronger statement than a test that watches
// one run for traffic.
func TestNoNetworkPackages(t *testing.T) {
	t.Parallel()
	gotool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command lists the packages jz is built from: %v", err)
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd := exec.CommandContext(t.Context(), gotool, "list", "-deps", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (%s): %v", goos, err)
		}
		for _, pkg := range strings.Fields(string(out)) {
			if pkg == "net" || strings.HasPrefix(pkg, "net/") || pkg == "crypto/tls" {
				t.Errorf("%s: jz is built with %s", goos, pkg)
			}
		}
	}
}

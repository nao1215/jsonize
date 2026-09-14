package cli

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --define reads the input with a definition written on the command line
// instead of a registered one. Nothing is selected, so nothing can be
// selected wrongly: the caller states the format.
func TestDefineReadsWithAGivenDefinition(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe("a,b\n1,\"x,y\"\n", "--define", "parse: {type: csv}"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 1 || rows[0]["b"] != "x,y" {
		t.Errorf("rows = %v", rows)
	}
	// It composes with the options that decide how the JSON is written.
	if code := h.pipe("a,b\n1,2\n3,4\n", "--define", "parse: {type: csv}", "--stream"); code != ExitOK {
		t.Fatalf("stream: %d %s", code, h.stderr.String())
	}
	if got := len(h.records()); got != 2 {
		t.Errorf("streamed %d records", got)
	}
	// A key the definition does not produce is refused before a record is
	// streamed, as it is from a registered definition.
	if code := h.pipe("a b\n1 2\n", "--define", "parse: {type: table, header: {columns: [a, b]}}", "--stream", "--extract", "c"); code != ExitUsage || h.stdout.Len() != 0 {
		t.Errorf("stream, unknown key: %d stdout=%q stderr=%s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("a,b\n1,2\n", "--define", "parse: {type: csv}", "--extract", "a"); code != ExitOK {
		t.Fatalf("extract: %d %s", code, h.stderr.String())
	}
	if rows := h.rows(); len(rows) != 1 || len(rows[0]) != 1 {
		t.Errorf("rows = %v", rows)
	}
	// The definition it used is what --explain has to say.
	if code := h.pipe("a,b\n1,2\n", "--define", "parse: {type: csv}", "--explain"); code != ExitOK ||
		!strings.Contains(h.stderr.String(), "jz: explain: defined inline/inline: the definition was given with --define") {
		t.Errorf("explain: %d %s", code, h.stderr.String())
	}
}

func TestDefineRefuses(t *testing.T) {
	h := newHarness(t)
	// --define is the definition, so there is nothing left to choose.
	for _, args := range [][]string{
		{"--define", "parse: {type: csv}", "--parser", "df"},
		{"--define", "parse: {type: csv}", "--variant", "gnu"},
	} {
		if code := h.pipe("a,b\n", args...); code != ExitUsage ||
			!strings.Contains(h.stderr.String(), "cannot be used together") {
			t.Errorf("%v: %d %s", args, code, h.stderr.String())
		}
	}
	// A body that cannot be read is a registry problem, and the message
	// names the key rather than a line number of something the user did
	// not write.
	for _, body := range []string{
		"parse: {type: nope}",
		"parse: {type: table, split: sideways}",
		"nonsense",
		"parse: {type: csv, delimiter: \",,\"}",
	} {
		if code := h.pipe("a,b\n", "--define", body); code != ExitRegistry {
			t.Errorf("%q: code = %d, stderr = %s", body, code, h.stderr.String())
		}
		if !strings.Contains(h.stderr.String(), "--define") {
			t.Errorf("%q: stderr does not name the option: %s", body, h.stderr.String())
		}
	}
	// The four keys that place a definition in a registry are explained
	// rather than reported as unknown.
	for _, body := range []string{"format: 1\nparse: {type: csv}", "command: df\nparse: {type: csv}",
		"variant: gnu\nparse: {type: csv}", "detect: {os: [linux]}\nparse: {type: csv}"} {
		if code := h.pipe("a,b\n", "--define", body); code != ExitRegistry ||
			!strings.Contains(h.stderr.String(), "does not live in a registry") {
			t.Errorf("%q: %d %s", body, code, h.stderr.String())
		}
	}
}

// A definition that describes a shape is in the registry under a name
// that says so, and it is never chosen on its own.
func TestGenericDefinitionsAreNamedOrNothing(t *testing.T) {
	h := newHarness(t)
	const report = "region  orders  revenue\nnorth   1204    88210\nsouth   937     71455\n"
	if code := h.pipe(report, "--parser", "table", "--variant", "whitespace"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 2 || rows[0]["region"] != "north" {
		t.Errorf("rows = %v", rows)
	}
	// Every value is text: a shape says nothing about what its columns
	// mean, so there is nothing to convert them with.
	if rows[0]["orders"] != "1204" {
		t.Errorf("orders = %#v", rows[0]["orders"])
	}
	// Automatic detection still reads df output as df output.
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatal(code)
	}
	if got := h.rows()[0]["1k_blocks"]; got != float64(1000000) {
		t.Errorf("automatic detection landed on a shape: %#v", got)
	}
	if code := h.pipe("k1=v1\nk2=v2\n", "--parser", "kv", "--variant", "equals"); code != ExitOK {
		t.Fatalf("kv: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("[a]\nk = 1\n", "--parser", "ini"); code != ExitOK {
		t.Fatalf("ini: %d %s", code, h.stderr.String())
	}
	var obj map[string]any
	h.json(&obj)
	if a, ok := obj["a"].(map[string]any); !ok || a["k"] != "1" {
		t.Errorf("ini = %v", obj)
	}
}

// A definition given on the command line is the whole of what is
// needed, so the registries are not read: a registry that cannot be
// loaded does not stand in the way of a definition that does not use
// it, and the definition's own exec.env is what jz run applies.
func TestDefineDoesNotReadTheRegistries(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.home, "reg")
	writeRegistry(t, dir, map[string]string{"registry.yaml": "format: 999\n"})
	h.registryPath = dir
	// The registry is refused where it is read.
	if code := h.pipe("a,b\n1,2\n"); code != ExitRegistry {
		t.Fatalf("without --define: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("a,b\n1,2\n", "--define", "parse: {type: csv}"); code != ExitOK || h.stderr.Len() != 0 {
		t.Fatalf("--define: %d %s", code, h.stderr.String())
	}
	if rows := h.rows(); len(rows) != 1 || rows[0]["b"] != "2" {
		t.Errorf("rows = %v", rows)
	}
	if code := h.pipe("a,b\n1,2\n", "--define", "parse: {type: csv}", "--stream"); code != ExitOK || len(h.records()) != 1 {
		t.Errorf("--define --stream: %d %s", code, h.stderr.String())
	}
	// --columns names the columns of the definition given, and needs no
	// registry either.
	if code := h.pipe("1,2\n", "--define", "parse: {type: csv, header: {none: true}}", "--columns", "x,y"); code != ExitOK || h.rows()[0]["y"] != "2" {
		t.Errorf("--define --columns: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	if runtime.GOOS == "windows" {
		return
	}
	if code := h.run("run", "--define", "parse: {type: kv}\nexec: {env: {JZ_INLINE: given}}", "--", "sh", "-c", "echo x=$JZ_INLINE"); code != ExitOK {
		t.Fatalf("run --define: %d %s", code, h.stderr.String())
	}
	if rows := h.rows(); len(rows) != 1 || rows[0]["value"] != "given" {
		t.Errorf("exec.env of the definition given: %v", rows)
	}
	if code := h.run("run", "--define", "parse: {type: kv}", "--stream", "--", "sh", "-c", "echo x=1; echo y=2"); code != ExitOK || len(h.records()) != 2 {
		t.Errorf("run --define --stream: %d %s", code, h.stderr.String())
	}
}

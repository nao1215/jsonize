package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// greetRegistry lays out a registry directory with one definition and its
// fixture, and returns the directory.
func greetRegistry(t *testing.T, def, fixture, golden string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"registry.yaml":                  "format: 1\nname: local\n",
		"parsers/greet/x/parser.yaml":    def,
		"parsers/greet/x/testdata/a.txt": fixture,
	}
	if golden != "" {
		files["parsers/greet/x/testdata/a.json"] = golden
	}
	writeRegistry(t, dir, files)
	return dir
}

const (
	greetDef = "format: 1\ncommand: greet\nvariant: x\ndetect: {signature: {all: ['^hello \\S+$']}}\n" +
		"parse: {type: regex, pattern: '^hello (?P<name>\\S+)$'}\n"
	// The same definition with a signature wide enough to read df output.
	greetWideDef = "format: 1\ncommand: greet\nvariant: x\ndetect: {signature: {all: ['^Filesystem']}}\n" +
		"parse: {type: regex, pattern: '^(?P<line>.+)$'}\n"
)

func TestTestPasses(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "atago"}]`)
	if code := h.run("test", dir); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if got := h.stdout.String(); got != "" {
		t.Errorf("stdout is not empty: %q", got)
	}
	if !strings.Contains(h.stderr.String(), "1 passed, 0 failed") {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

func TestTestReportsGoldenMismatch(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "somebody else"}]`)
	if code := h.run("test", dir); code != ExitError {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "output differs from a.json") {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

// The point of building the official fixtures into jz: a definition whose
// signature reads df output fails without a copy of the repository.
func TestTestCatchesOfficialFixture(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetWideDef, "Filesystem here\n", "")
	if code := h.run("test", dir); code != ExitError {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "a fixture of df/gnu") {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

func TestTestUpdateWritesGolden(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", "")
	if code := h.run("test", "--update", dir); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	golden := filepath.Join(dir, "parsers", "greet", "x", "testdata", "a.json")
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name": "atago"`) {
		t.Errorf("golden=%s", data)
	}
	if !strings.Contains(h.stderr.String(), "golden files written under "+dir) {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

func TestTestDecoys(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "atago"}]`)
	decoys := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoys, "greeting.txt"), []byte("hello atago\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("test", "--decoys", decoys, dir); code != ExitError {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "read the decoy greeting.txt") {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

func TestTestUsageErrors(t *testing.T) {
	h := newHarness(t)
	// A path that is not a registry.
	if code := h.run("test", filepath.Join(t.TempDir(), "nowhere")); code != ExitRegistry {
		t.Errorf("missing directory: code=%d stderr=%s", code, h.stderr.String())
	}
	// No directory and no registry of the user's own.
	if code := h.run("test"); code != ExitUsage {
		t.Errorf("no registry: code=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "no definitions to check") {
		t.Errorf("stderr=%q", h.stderr.String())
	}
	// An empty decoy directory says nothing was read rather than passing.
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "atago"}]`)
	if code := h.run("test", "--decoys", t.TempDir(), dir); code != ExitUsage {
		t.Errorf("empty decoys: code=%d stderr=%s", code, h.stderr.String())
	}
	// A decoy directory that is not there is named as such, not as the
	// system call that found it missing.
	h.stderr.Reset()
	missing := filepath.Join(t.TempDir(), "nodecoys")
	if code := h.run("test", "--decoys", missing, dir); code != ExitUsage {
		t.Errorf("missing decoys: code=%d stderr=%s", code, h.stderr.String())
	}
	if got := h.stderr.String(); !strings.Contains(got, "--decoys "+missing+" does not exist") || strings.Contains(got, "lstat") {
		t.Errorf("stderr=%q", got)
	}
}

// A warning from the registries under test is said once.
func TestTestWarnsOnce(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "atago"}]`)
	if err := os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte("format: 1\nname: local\ndisable: [nosuchcmd]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("test", dir); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if n := strings.Count(h.stderr.String(), `disable "nosuchcmd" matches no definition`); n != 1 {
		t.Errorf("warning said %d times: %q", n, h.stderr.String())
	}
}

// With no directory the user registry is what gets checked, which is what
// the loop in the README does.
func TestTestChecksUserRegistry(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.home, "config", "jsonize", "registry")
	data := filepath.Join(dir, "parsers", "greet", "x", "testdata")
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parsers", "greet", "x", "parser.yaml"), []byte(greetDef), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "a.txt"), []byte("hello atago\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("test", "--update"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(data, "a.json")); err != nil {
		t.Errorf("golden not written to the user registry: %v", err)
	}
	if !strings.Contains(h.stderr.String(), "golden files written under "+dir) {
		t.Errorf("stderr=%q", h.stderr.String())
	}
}

// --json writes the same facts the lines carry, in a shape a CI job can
// count: the totals, and one entry per failure saying what it failed at.
func TestTestWritesAJSONReport(t *testing.T) {
	h := newHarness(t)
	dir := greetRegistry(t, greetDef, "hello atago\n", `[{"name": "atago"}]`)
	if code := h.run("test", "--json", dir); code != ExitOK {
		t.Fatalf("passing: code=%d stderr=%s", code, h.stderr.String())
	}
	var report struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Failures []struct {
			Definition string `json:"definition"`
			Case       string `json:"case"`
			Path       string `json:"path"`
			Source     string `json:"source"`
			Kind       string `json:"kind"`
			Message    string `json:"message"`
		} `json:"failures"`
	}
	read := func(what string) {
		t.Helper()
		report.Passed, report.Failed, report.Failures = 0, 0, nil
		if err := json.Unmarshal(h.stdout.Bytes(), &report); err != nil {
			t.Fatalf("%s: %v\n%s", what, err, h.stdout.String())
		}
	}
	read("passing")
	if report.Passed != 1 || report.Failed != 0 || len(report.Failures) != 0 {
		t.Errorf("passing: %+v", report)
	}
	// The lines on standard error are the same with the report as without.
	if !strings.Contains(h.stderr.String(), "1 passed, 0 failed") {
		t.Errorf("passing: stderr=%q", h.stderr.String())
	}
	// A failure names the definition, the fixture, what it failed at and
	// the message the line carries.
	dir = greetRegistry(t, greetDef, "hello atago\n", `[{"name": "somebody else"}]`)
	if code := h.run("test", "--json", dir); code != ExitError {
		t.Fatalf("golden: code=%d stderr=%s", code, h.stderr.String())
	}
	read("golden")
	if report.Passed != 0 || report.Failed != 1 || len(report.Failures) != 1 {
		t.Fatalf("golden: %+v", report)
	}
	f := report.Failures[0]
	if f.Definition != "greet/x" || f.Case != "a" || f.Kind != "golden" ||
		f.Path != "parsers/greet/x/testdata/a.txt" || f.Source != dir ||
		!strings.Contains(f.Message, "output differs from a.json") {
		t.Errorf("golden: %+v", f)
	}
	// A definition that reads another definition's fixtures failed at
	// choosing, not at reading its own.
	dir = greetRegistry(t, greetWideDef, "Filesystem here\n", "")
	if code := h.run("test", "--json", dir); code != ExitError {
		t.Fatalf("select: code=%d stderr=%s", code, h.stderr.String())
	}
	read("select")
	kinds := map[string]int{}
	for _, f := range report.Failures {
		kinds[f.Kind]++
	}
	if kinds["select"] == 0 {
		t.Errorf("select: kinds=%v", kinds)
	}
	// A definition that did not load has no identity yet, so the file is
	// what names it.
	dir = greetRegistry(t, "format: 1\ncommand: greet\nvariant: x\nparse: {type: table, pattern: x}\n", "hello\n", "[]")
	if code := h.run("test", "--json", dir); code != ExitError {
		t.Fatalf("load: code=%d stderr=%s", code, h.stderr.String())
	}
	read("load")
	if len(report.Failures) != 1 || report.Failures[0].Kind != "load" ||
		report.Failures[0].Definition != "" ||
		report.Failures[0].Path != "parsers/greet/x/parser.yaml" {
		t.Errorf("load: %+v", report.Failures)
	}
}

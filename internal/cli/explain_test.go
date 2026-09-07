package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
)

// --explain says which definition was chosen and what it was chosen on.
// Nothing about the answer changes: standard output and the exit status
// are what they are without it.
func TestExplainReportsTheChoice(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--explain"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	plain := h.stdout.String()
	lines := strings.Split(strings.TrimSpace(h.stderr.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	if lines[0] != "jz: df/gnu from embedded" {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "jz: matched: signature.all[0] /") {
		t.Errorf("second line = %q", lines[1])
	}
	// No clock reading: two runs of the same input explain themselves
	// identically, so an explanation can be compared and pasted.
	before := h.stderr.String()
	if code := h.pipe(gnuDF, "--explain"); code != ExitOK || h.stderr.String() != before {
		t.Errorf("second run differs:\n%s\n%s", before, h.stderr.String())
	}
	if code := h.pipe(gnuDF); code != ExitOK || h.stdout.String() != plain || h.stderr.Len() != 0 {
		t.Errorf("--explain changed the answer: %q vs %q", plain, h.stdout.String())
	}
}

// A scoped search reports every variant it left out, which is what makes
// a wrong variant traceable.
func TestExplainListsTheVariantsItLeftOut(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--explain", "--parser", "df"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	got := h.stderr.String()
	for _, want := range []string{"rejected: ", "df/gnu-human (signature.all[0]", "did not match"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr has no %q:\n%s", want, got)
		}
	}
}

// Text nothing describes still exits 4 with the message it always had;
// --explain adds the definitions that came close underneath it.
func TestExplainOnUnidentifiedInput(t *testing.T) {
	h := newHarness(t)
	// A group line has four colon-separated fields where passwd has
	// seven, so etc/passwd fits the text and is only held back by
	// auto_detect.
	const passwd = "root:x:0:0:root:/root:/bin/bash\ndaemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n"
	if code := h.pipe(passwd, "--explain"); code != ExitSelect {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	got := h.stderr.String()
	if !strings.Contains(got, "unable to identify the input format") {
		t.Errorf("the ordinary message is gone:\n%s", got)
	}
	if !strings.Contains(got, "rejected: ") || !strings.Contains(got, "etc/passwd (needs --parser etc)") {
		t.Errorf("no near miss listed:\n%s", got)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// jz run knows what it started, so it says so as well.
func TestExplainOnRun(t *testing.T) {
	h := newHarness(t)
	shellRegistry(t, h)
	code := h.run("run", "--explain", "--parser", "df", "--", "sh", "-c", `printf '%s' "$0"`, gnuDF)
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	got := h.stderr.String()
	if !strings.Contains(got, "jz: df/gnu from embedded") {
		t.Errorf("no definition line:\n%s", got)
	}
	if !strings.Contains(got, "jz: command: sh -c ") || !strings.Contains(got, "(exit 0)") {
		t.Errorf("no command line:\n%s", got)
	}
}

// A path is evidence about the text in it: the directory names the
// parser and the file names the variant. It is what makes the formats
// that are too unremarkable to claim on sight readable without naming
// them, and it can only add an answer, never replace one.
func TestFilePathNamesTheDefinition(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(t.TempDir(), "etc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const fstab = "/dev/sda1 / ext4 defaults 0 1\nUUID=abcd /boot vfat umask=0077 0 2\n"
	path := filepath.Join(dir, "fstab")
	if err := os.WriteFile(path, []byte(fstab), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--explain", "--file", path); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "etc/fstab from embedded") {
		t.Errorf("stderr = %s", h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 2 || rows[0]["mount_point"] != "/" {
		t.Errorf("rows = %v", rows)
	}
	// The same text with no such path is unidentifiable, which is what
	// the path added.
	if code := h.pipe(fstab); code != ExitSelect {
		t.Errorf("piped: %d", code)
	}
}

// A path that names a definition the text does not fit is dropped, and
// the text decides on its own terms. Evidence that could make the answer
// worse would not be worth having.
func TestFilePathNeverMakesTheAnswerWorse(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(t.TempDir(), "etc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fstab")
	if err := os.WriteFile(path, []byte(gnuDF), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--file", path); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if rows := h.rows(); len(rows) != 2 || rows[0]["filesystem"] != "tmpfs" {
		t.Errorf("rows = %v", rows)
	}
}

func TestParserFromPath(t *testing.T) {
	t.Parallel()
	reg, err := registry.Load(registry.Source{Name: SourceEmbedded, FS: official.FS()})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path            string
		parser, variant string
		ok              bool
	}{
		{path: "/etc/fstab", parser: "etc", variant: "fstab", ok: true},
		{path: "/proc/meminfo", parser: "proc", variant: "meminfo", ok: true},
		{path: "/etc/../etc/passwd", parser: "etc", variant: "passwd", ok: true},
		// A capture is a file name, not a variant name.
		{path: "/tmp/etc/fstab.txt"},
		// The directory names no parser.
		{path: "/home/me/fstab"},
		// Nothing to read a directory from.
		{path: "fstab"},
		{path: "/fstab"},
		// The parser exists, the variant does not.
		{path: "/etc/not-a-variant"},
	}
	for _, tt := range tests {
		p, v, ok := parserFromPath(reg, tt.path)
		if ok != tt.ok || p != tt.parser || v != tt.variant {
			t.Errorf("%s: got %q %q %v, want %q %q %v", tt.path, p, v, ok, tt.parser, tt.variant, tt.ok)
		}
	}
}

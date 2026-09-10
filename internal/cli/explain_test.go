package cli

import (
	"os"
	"path/filepath"
	"runtime"
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
	if len(lines) < 3 {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	if lines[0] != "jz: explain: chose df/gnu from embedded" {
		t.Errorf("first line = %q", lines[0])
	}
	if lines[1] != "jz: explain: scope: every definition in the registry, by its signature alone" {
		t.Errorf("second line = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "jz: explain: matched: signature.all[0] /") {
		t.Errorf("third line = %q", lines[2])
	}
	if last := lines[len(lines)-1]; last != "jz: explain: read: 3 lines: 3 read" {
		t.Errorf("last line = %q", last)
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
	for _, want := range []string{
		"jz: explain: scope: the variants of df, named by --parser\n",
		"jz: explain: rejected: df/gnu-human: signature.all[0]",
		"did not match",
	} {
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
	for _, want := range []string{
		"jz: explain: unidentified: no definition fits the text\n",
		"jz: explain: held back: etc/passwd: its signature fits, but it is only used when named (--parser etc)\n",
		"jz: explain: not considered: ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q:\n%s", want, got)
		}
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// jz run knows what it started, so it says so as well, and says that the
// parser came from the command's name. The definition is one of the test
// registry's, which claims no operating system: jz run filters on the
// system it is running on, so a Linux-only definition would make this a
// test of that filter.
func TestExplainOnRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	code := h.run("run", "--explain", "--", "sh", "-c", "echo n=1")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	got := h.stderr.String()
	for _, want := range []string{
		"jz: explain: chose sh/default from ",
		"jz: explain: scope: the variants of sh, from the name of the command jz ran\n",
		"jz: explain: scope: narrowed by the system it ran on (" + runtime.GOOS + ") and its arguments (-c echo n=1)\n",
		"jz: explain: rejected: sh/alt: signature.all[0] /^ALT/ did not match\n",
		"jz: explain: command: sh -c echo n=1 (exit 0)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q:\n%s", want, got)
		}
	}
	// A wrapper names the parser itself, and the explanation says the
	// name did not come from the command.
	code = h.run("run", "--explain", "--parser", "wrapper", "--", "sh", "-c", "echo wrapped=1")
	if code != ExitOK || !strings.Contains(h.stderr.String(), "jz: explain: scope: the variants of wrapper, named by --parser\n") {
		t.Errorf("code=%d stderr=%s", code, h.stderr.String())
	}
}

// A command that prints nothing gives jz no text to choose by, and the
// answer rests on the variants its name and arguments leave instead. The
// explanation says that, and says what the command did, rather than
// staying silent while `[]` or a refusal comes back.
func TestExplainOnRunThatPrintsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	expect := func(code, want int, lines ...string) {
		t.Helper()
		if code != want {
			t.Errorf("code=%d, want %d; stderr=%s", code, want, h.stderr.String())
		}
		got := h.stderr.String()
		for _, l := range lines {
			if !strings.Contains(got, l) {
				t.Errorf("no %q:\n%s", l, got)
			}
		}
	}
	const empty = "jz: explain: empty: the command printed nothing, so no definition was chosen by its text\n"

	expect(h.run("run", "--explain", "--parser", "sh", "--variant", "alt", "--", "sh", "-c", "true"), ExitOK,
		empty,
		"jz: explain: scope: sh/alt, named by --parser and --variant\n",
		"jz: explain: candidates: sh/alt\n",
		"jz: explain: command: sh -c true (exit 0)\n")
	if h.stdout.String() != "[]\n" {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	// One object has no empty form, so that answer is refused, and the
	// explanation says what it was judged against.
	writeRegistry(t, h.registryPath, map[string]string{
		"parsers/one/default/parser.yaml": "format: 1\ncommand: one\nvariant: default\n" +
			"detect: {signature: {all: ['=']}}\nparse: {type: kv, as: map}\n",
	})
	expect(h.run("run", "--explain", "--parser", "one", "--", "sh", "-c", "true"), ExitSelect,
		"reads a format that has no empty form",
		empty,
		"jz: explain: candidates: one/default\n",
		"jz: explain: command: sh -c true (exit 0)\n")
	expect(h.run("run", "--explain", "--stream", "--parser", "sh", "--variant", "alt", "--", "sh", "-c", "true"), ExitOK,
		empty,
		"jz: explain: candidates: sh/alt\n",
		"jz: explain: command: sh -c true (exit 0)\n")
	// A command that failed without printing anything is its own answer.
	expect(h.run("run", "--explain", "--", "sh", "-c", "exit 3"), 3,
		"jz: explain: failed before a definition was chosen\n",
		"jz: explain: command: sh -c exit 3 (exit 3)\n")
	expect(h.run("run", "--explain=json", "--parser", "sh", "--variant", "alt", "--", "sh", "-c", "true"), ExitOK,
		`"outcome":"empty"`,
		`"candidates":["sh/alt"]`,
		`"command":{"name":"sh","args":["-c","true"],"exit":0}`)
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
	if !strings.Contains(h.stderr.String(), "jz: explain: chose etc/fstab from embedded\n") ||
		!strings.Contains(h.stderr.String(), "jz: explain: scope: etc/fstab, from the file path "+path+"\n") {
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
	// Each of these is a real (parser, variant) pair, so the path names
	// something; none of them describes df output. The last four are the
	// dangerous kind: a definition that describes a shape has no
	// signature, so nothing about the text can rule it out and the
	// directory name would be the only evidence there was.
	for _, pair := range [][2]string{
		{"etc", "fstab"},
		{"proc", "meminfo"},
		{"csv", "comma"},
		{"table", "whitespace"},
		{"kv", "colon"},
		{"ini", "default"},
	} {
		dir := filepath.Join(t.TempDir(), pair[0])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pair[1])
		if err := os.WriteFile(path, []byte(gnuDF), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := h.run("--file", path); code != ExitOK {
			t.Errorf("%s/%s: code=%d stderr=%s", pair[0], pair[1], code, h.stderr.String())
			continue
		}
		rows := h.rows()
		if len(rows) != 2 || rows[0]["filesystem"] != "tmpfs" || rows[0]["1k_blocks"] != float64(1000000) {
			t.Errorf("%s/%s read df output as something else: %v", pair[0], pair[1], rows)
		}
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
		// A definition that describes a shape rather than a command has
		// no signature, so naming one from a path would put the whole
		// weight of the answer on a directory name.
		{path: "/csv/comma"},
		{path: "/table/whitespace"},
		{path: "/kv/colon"},
		{path: "/ini/default"},
	}
	for _, tt := range tests {
		p, v, ok := parserFromPath(reg, tt.path)
		if ok != tt.ok || p != tt.parser || v != tt.variant {
			t.Errorf("%s: got %q %q %v, want %q %q %v", tt.path, p, v, ok, tt.parser, tt.variant, tt.ok)
		}
	}
}

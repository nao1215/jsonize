package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
	official "github.com/nao1215/jsonize/registry"
)

type harness struct {
	t        *testing.T
	stdin    *bytes.Buffer
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	env      Env
	home     string
	terminal bool
	// registryPath is what JSONIZE_REGISTRY_PATH reports; it is the only
	// way to add a registry, so tests use it the way users do.
	registryPath string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, stdin: &bytes.Buffer{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	h.home = t.TempDir()
	h.env = Env{
		Stdin:  h.stdin,
		Stdout: h.stdout,
		Stderr: h.stderr,
		Getenv: func(k string) string {
			if k == EnvRegistryPath {
				return h.registryPath
			}
			return ""
		},
		UserConfigDir:   func() (string, error) { return filepath.Join(h.home, "config"), nil },
		StdinIsTerminal: func() bool { return h.terminal },
		GOOS:            runtime.GOOS,
		Context:         context.Background(),
		Embedded:        registry.Source{Name: SourceEmbedded, FS: official.FS()},
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return Main(args, h.env)
}

// pipe feeds text on stdin and runs jz with it.
func (h *harness) pipe(input string, args ...string) int {
	h.stdin.Reset()
	h.stdin.WriteString(input)
	return h.run(args...)
}

func (h *harness) json(v any) {
	h.t.Helper()
	if err := json.Unmarshal(h.stdout.Bytes(), v); err != nil {
		h.t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout.String())
	}
}

func (h *harness) rows() []map[string]any {
	h.t.Helper()
	var rows []map[string]any
	h.json(&rows)
	return rows
}

const (
	gnuDF   = "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs            1000000    5000    995000   1% /run\n/dev/sda1       20000000 5000000  15000000  25% /\n"
	humanDF = "Filesystem      Size  Used Avail Use% Mounted on\ntmpfs           1.0G  5.0M  995M   1% /run\n"
	//nolint:dupword // real uname output repeats the architecture
	unameA    = "Linux host1 6.8.0-45-generic #45-Ubuntu SMP PREEMPT_DYNAMIC Fri Aug 30 12:02:04 UTC 2024 x86_64 x86_64 x86_64 GNU/Linux\n"
	lsblkList = "NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nsda           8:0    0    20G  0 disk \n"
)

// TestAutoDetectFromStdin is the headline behaviour: no subcommand, no
// options, just piped output.
func TestAutoDetectFromStdin(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 2 || rows[1]["mounted_on"] != "/" || rows[1]["use_percent"] != float64(25) {
		t.Errorf("rows = %v", rows)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr should stay empty: %s", h.stderr.String())
	}
	// A different variant of the same command and a different command are
	// told apart from the text alone; the shape of the JSON is what says
	// which definition was used.
	if code := h.pipe(humanDF); code != ExitOK {
		t.Fatalf("df -h: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["size"] != "1.0G" {
		t.Errorf("df -h rows = %v", h.rows())
	}
	if code := h.pipe(unameA); code != ExitOK {
		t.Fatalf("uname: %d %s", code, h.stderr.String())
	}
	var obj map[string]any
	h.json(&obj)
	if obj["kernel_name"] != "Linux" || obj["machine"] != "x86_64" {
		t.Errorf("uname = %v", obj)
	}
}

func TestConversionOptions(t *testing.T) {
	h := newHarness(t)
	for _, opt := range []string{"--pretty", "-p"} {
		if code := h.pipe(gnuDF, opt); code != ExitOK || !strings.Contains(h.stdout.String(), "\n  {") {
			t.Errorf("%s: %d\n%s", opt, code, h.stdout.String())
		}
	}
	p := filepath.Join(h.home, "df.txt")
	if err := os.WriteFile(p, []byte(gnuDF), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, opt := range []string{"--file", "-f"} {
		if code := h.run(opt, p); code != ExitOK || len(h.rows()) != 2 {
			t.Errorf("%s: %d %s", opt, code, h.stderr.String())
		}
	}
	if code := h.run("--file", filepath.Join(h.home, "missing.txt")); code != ExitError {
		t.Errorf("missing file: %d", code)
	}
	if code := h.pipe(gnuDF, "--parser", "df"); code != ExitOK {
		t.Errorf("--parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "gnu"); code != ExitOK {
		t.Errorf("--variant: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(humanDF, "--parser", "df", "--variant", "gnu-human"); code != ExitOK {
		t.Errorf("--variant gnu-human: %d %s", code, h.stderr.String())
	}
}

// An option that takes a value, given an empty one, names nothing. It
// used to count as not given at all, so a script passing --define "$DEF"
// with DEF unset had its input read by whatever detection chose.
func TestEmptyOptionValueIsAUsageError(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"--define", ""},
		{"--parser", ""},
		{"--parser", "df", "--variant", ""},
		{"--file", ""},
		{"-f", ""},
		{"--assume-year", ""},
		{"run", "--parser", "", "df"},
	} {
		code := h.pipe(gnuDF, args...)
		if code != ExitUsage || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "empty value") {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %s", args, code, h.stdout.String(), h.stderr.String())
		}
	}
}

// An option that takes one value, given two, states two answers. The
// last one used to win without a word, so `jz -f a -f b` converted b and
// said nothing about a. An option that may be repeated, and a switch,
// are not affected.
func TestRepeatedOptionIsAUsageError(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "df.txt")
	if err := os.WriteFile(file, []byte(gnuDF), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-f", file, "-f", file},
		{"-f", file, "--file", file},
		{"--parser", "df", "--parser", "ls"},
		{"--parser", "df", "--variant", "gnu", "--variant", "gnu-human"},
		{"--assume-year", "2024", "--assume-year", "2025"},
		{"run", "--timeout", "1h", "--timeout", "1s", "df"},
		{"list", "--json", "--json"},
	} {
		code := h.pipe(gnuDF, args...)
		if args[0] == "list" {
			// --json is a switch: saying it twice says one thing.
			if code != ExitOK {
				t.Errorf("%q: code = %d, stderr = %s", args, code, h.stderr.String())
			}
			continue
		}
		if code != ExitUsage || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "was given twice") {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %s", args, code, h.stdout.String(), h.stderr.String())
		}
	}
	if code := h.pipe(gnuDF, "--extract", "filesystem", "--extract", "used", "-p", "-p"); code != ExitOK {
		t.Errorf("repeatable options: %d %s", code, h.stderr.String())
	}
}

// TestRemovedOptionsAreGone pins that the options cut before release are
// usage errors rather than silently accepted or ignored.
func TestRemovedOptionsAreGone(t *testing.T) {
	h := newHarness(t)
	removed := [][]string{
		{"-r"}, {"--meta"}, {"--os", "linux"}, {"--force"},
		{"--max-input", "1"}, {"--embedded-only"}, {"--registry", "./registry"},
	}
	for _, args := range removed {
		code := h.pipe(gnuDF, args...)
		if code != ExitUsage {
			t.Errorf("%v: code = %d, want %d", args, code, ExitUsage)
		}
		if h.stdout.Len() != 0 {
			t.Errorf("%v: stdout = %q", args, h.stdout.String())
		}
		if !strings.Contains(h.stderr.String(), "not defined") {
			t.Errorf("%v: stderr = %s", args, h.stderr.String())
		}
	}
	// The same for jz run, whose options are also a closed set.
	for _, args := range [][]string{{"--meta"}, {"--force"}, {"--registry", "x"}, {"--max-output", "1"}} {
		if code := h.run(append([]string{"run"}, append(args, "df")...)...); code != ExitUsage {
			t.Errorf("run %v: code = %d", args, code)
		}
	}
	if code := h.run("--help"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	help := h.stdout.String()
	for _, gone := range []string{"--meta", "--os", "--force", "--max-input", "--embedded-only", "--registry"} {
		if strings.Contains(help, gone) {
			t.Errorf("help still mentions %s:\n%s", gone, help)
		}
	}
}

func TestHelpListsEveryOptionOnce(t *testing.T) {
	h := newHarness(t)
	if code := h.run("--help"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	help := h.stdout.String()
	for _, want := range []string{
		"COMMAND | jz [options]",
		"jz run [options] COMMAND ...",
		"jz new [options] KEY=VALUE ...",
		"Input:",
		"Output:",
		"Choosing and checking the parser of command output:",
		"-f, --file PATH",
		"-p, --pretty",
		"    --parser NAME",
		"    --variant NAME",
		"-h, --help",
		"run a command and convert its stdout to JSON",
		"df -h | jz",
		"Documentation:   https://nao1215.github.io/jsonize/",
		"Report an issue: https://github.com/nao1215/jsonize/issues",
		"GitHub Sponsors: https://github.com/sponsors/nao1215",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
	// A short and a long form are one option, not two lines. The count is
	// taken over the options block alone, since an example may name an
	// option too.
	options := help[strings.Index(help, "Input:"):strings.Index(help, "Examples:")]
	for _, opt := range []string{"--file", "--pretty", "--parser", "--variant", "--help", "--raw", "--type"} {
		lines := 0
		for _, line := range strings.Split(options, "\n") {
			// The option column ends before the two spaces that separate
			// it from its description, so a description mentioning
			// another option does not count as a second entry.
			if column, _, ok := strings.Cut(strings.TrimSpace(line), "  "); ok && strings.Contains(column, opt) {
				lines++
			}
		}
		if lines != 1 {
			t.Errorf("%s is listed on %d lines:\n%s", opt, lines, options)
		}
	}
	// Every registered option appears in the help, and nothing else does.
	o := newOptions("jz")
	var co convertOptions
	co.bind(o)
	registered := map[string]bool{}
	o.fs.VisitAll(func(f *flag.Flag) { registered[f.Name] = true })
	documented := map[string]bool{}
	for _, d := range o.docs {
		if d.heading != "" {
			continue
		}
		documented[d.long] = true
		if d.short != "" {
			documented[d.short] = true
		}
	}
	for name := range registered {
		if !documented[name] {
			t.Errorf("option %q is registered but not documented", name)
		}
	}
	for name := range documented {
		if !registered[name] && name != "h" && name != "help" {
			t.Errorf("option %q is documented but not registered", name)
		}
	}
}

func TestAutoDetectFailures(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"unrelated text", "this is not command output\n", []string{"unable to identify the input format", "--parser"}},
		{"empty", "", []string{"unable to identify"}},
		{"whitespace only", "   \n\t\n", []string{"unable to identify"}},
		{"invalid utf-8", "\xff\xfe\x00\n", []string{"unable to identify"}},
		{"header only", "Filesystem\n", []string{"unable to identify"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if code := h.pipe(tt.input); code != ExitSelect {
				t.Fatalf("code = %d, want %d", code, ExitSelect)
			}
			if h.stdout.Len() != 0 {
				t.Errorf("stdout must stay empty: %q", h.stdout.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(h.stderr.String(), want) {
					t.Errorf("stderr missing %q:\n%s", want, h.stderr.String())
				}
			}
		})
	}
}

// TestForeignOutputIsNotClaimed pipes text that no parser in the registry
// describes and requires jz to say so. Every input here was accepted by
// some definition once: a signature loose enough to fit a build log, a
// status table or a properties file turns "COMMAND | jz" into confident
// nonsense, which is the one failure the tool exists to avoid.
func TestForeignOutputIsNotClaimed(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name  string
		input string
	}{
		// mount/bsd used to read "X on Y (Z)" as a mount table.
		{"build log", "Building foo on linux (amd64)\nBuilding bar on darwin (arm64)\n"},
		{"prose", "The meeting is on Tuesday (rescheduled)\nLunch is on Friday (maybe)\n"},
		// uname matched any line starting with the kernel name, which is
		// how /proc/version and a boot log begin too.
		{"proc version", "Linux version 7.0.0-30-generic (buildd@lcy02) #30-Ubuntu SMP\n"},
		{"kernel banner in a log", "boot log\nLinux mybox 6.8.0 #1 SMP x86_64 GNU/Linux\n"},
		{"darwin kernel banner", "Darwin Kernel Version 23.5.0: Wed May  1 20:09:52 PDT 2024\n"},
		// ip/brief-address used to read any "NAME UP ..." line.
		{"service status table", "web01 UP 3 days\ndb01 DOWN 2 hours\n"},
		{"link speed", "eth0 UP 1000Mb/s full\n"},
		// lsattr used to read any word followed by an absolute path.
		{"name and path columns", "librarypath /usr/lib\nincludepath /usr/include\n"},
		// sysctl used to read any dotted key with spaces around "=".
		{"properties file", "user.name = nao\nnet.core.x = 1\n"},
		// host/bind skips lines it cannot parse, so one sentence buried in
		// prose used to become the whole answer.
		{"prose mentioning a lookup", "Notes from today.\nwww.example.com has address 93.184.216.34\nrandom trailing text\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			code := h.pipe(tt.input)
			if code == ExitOK {
				t.Fatalf("claimed foreign input and produced JSON: %s", h.stdout.String())
			}
			if code != ExitSelect {
				t.Fatalf("code = %d (want %d, unidentified); stderr:\n%s", code, ExitSelect, h.stderr.String())
			}
			if h.stdout.Len() != 0 {
				t.Errorf("stdout must stay empty: %q", h.stdout.String())
			}
		})
	}
}

// TestOtherInvocationsOfAKnownCommand pipes what a registered command
// prints under options jz has no definition for. The command is known, so
// a signature written for its default format can still fit the leading
// lines; what must not happen is JSON that files the extra output under a
// field of the format jz did recognise. Whether the input is turned away
// by the signature or by the parser is not the point here: nothing may
// reach standard output either way.
func TestOtherInvocationsOfAKnownCommand(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name  string
		args  []string
		input string
	}{
		{
			// A script that installs a cron entry carries one line of the
			// shape a crontab has, which is all the signature looks for.
			// The other lines used to be read as entries too, with `echo`
			// as the minute.
			name: "a script that installs a cron entry",
			args: []string{"--parser", "etc", "--variant", "crontab"},
			input: "#!/bin/sh\n" +
				"30 4 * * 1 root /usr/local/bin/rotate-logs --keep 30\n" +
				"echo \"installed the entry above into /etc/cron.d/rotate\"\n",
		},
		{
			// `git log --stat` puts the diffstat inside the commit block,
			// where the message would be. It used to be read as git/log
			// with the file list reported as lines of the commit message.
			name: "git log --stat",
			input: "commit 1a348149ff7078757f307580d7846bb788b17484\n" +
				"Author: Ada Lovelace <ada@example.com>\n" +
				"Date:   Mon Sep 7 09:06:35 2026 +0900\n" +
				"\n" +
				"    add a second note\n" +
				"\n" +
				" notes/second.txt | 3 +++\n" +
				" 1 file changed, 3 insertions(+)\n",
		},
		{
			name: "git log --numstat",
			input: "commit 1a348149ff7078757f307580d7846bb788b17484\n" +
				"Author: Ada Lovelace <ada@example.com>\n" +
				"Date:   Mon Sep 7 09:06:35 2026 +0900\n" +
				"\n" +
				"    add a second note\n" +
				"\n" +
				"3\t0\tnotes/second.txt\n",
		},
		{
			name: "git log --name-only",
			input: "commit 1a348149ff7078757f307580d7846bb788b17484\n" +
				"Author: Ada Lovelace <ada@example.com>\n" +
				"Date:   Mon Sep 7 09:06:35 2026 +0900\n" +
				"\n" +
				"    add a second note\n" +
				"\n" +
				"notes/second.txt\n",
		},
		{
			name: "git log -p",
			input: "commit 1a348149ff7078757f307580d7846bb788b17484\n" +
				"Author: Ada Lovelace <ada@example.com>\n" +
				"Date:   Mon Sep 7 09:06:35 2026 +0900\n" +
				"\n" +
				"    add a second note\n" +
				"\n" +
				"diff --git a/notes/second.txt b/notes/second.txt\n" +
				"index e69de29..3b18e51 100644\n" +
				"--- a/notes/second.txt\n" +
				"+++ b/notes/second.txt\n" +
				"@@ -0,0 +1 @@\n" +
				"+hello\n",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if code := h.pipe(tt.input, tt.args...); code == ExitOK {
				t.Fatalf("read another invocation and produced JSON: %s", h.stdout.String())
			}
			if h.stdout.Len() != 0 {
				t.Errorf("stdout must stay empty: %q", h.stdout.String())
			}
		})
	}
}

// TestNamedParserStillChecksTheInput covers the definitions jz will not
// choose on its own because their shape is too plain. Naming one is a
// claim about the input, not a way past the signature, so a parser that
// is handed another command's output has to refuse it.
func TestNamedParserStillChecksTheInput(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name   string
		args   []string
		input  string
		reject string
	}{
		{
			// /etc/passwd has seven colon separated fields; the group
			// parser reads four and used to accept it, silently folding
			// the home directory and the shell into the member list.
			name:   "group must not read a passwd file",
			args:   []string{"--parser", "etc", "--variant", "group"},
			input:  "root:x:0:0:root:/root:/bin/bash\ndaemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n",
			reject: "etc/group does not describe this input",
		},
		{
			// A clock time is not an IPv6 address.
			name:   "hosts must not read an uptime line",
			args:   []string{"--parser", "etc", "--variant", "hosts"},
			input:  "10:14  up 3 days, 22:45, 2 users, load averages: 1.0 1.1 1.2\n",
			reject: "etc/hosts does not describe this input",
		},
		{
			// Prose that quotes the opening line of a report is not the
			// report. Both signatures used to look for their header line
			// alone, which a document explaining the format carries too.
			name:   "ethtool must not read a document quoting its header",
			args:   []string{"--parser", "ethtool", "--variant", "settings"},
			input:  "The settings report opens with\n\nSettings for eno1:\n\nand then lists a label and a value per line.\n",
			reject: "ethtool/settings does not describe this input",
		},
		{
			name:   "mtr must not read a document quoting its header",
			args:   []string{"--parser", "mtr", "--variant", "report"},
			input:  "The report opens with\n\nHOST: workstation                      Loss%   Snt   Last   Avg  Best  Wrst StDev\n\nand the rows that follow are indented by two spaces.\n",
			reject: "mtr/report does not describe this input",
		},
		{
			// blkid prints "path: TAG=..." which is the shape of file(1)
			// output but never a type description.
			name:   "file must not read blkid output",
			args:   []string{"--parser", "file"},
			input:  "/dev/nvme0n1p1: UUID=\"1234-ABCD\" BLOCK_SIZE=\"512\" TYPE=\"vfat\"\n",
			reject: "no file variant matches this input",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			code := h.pipe(tt.input, tt.args...)
			if code == ExitOK {
				t.Fatalf("accepted foreign input and produced JSON: %s", h.stdout.String())
			}
			if h.stdout.Len() != 0 {
				t.Errorf("stdout must stay empty: %q", h.stdout.String())
			}
			if !strings.Contains(h.stderr.String(), tt.reject) {
				t.Errorf("stderr should name %s:\n%s", tt.reject, h.stderr.String())
			}
		})
	}
}

func TestAmbiguousInput(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.home, "extra")
	writeRegistry(t, dir, map[string]string{
		"parsers/alpha/default/parser.yaml": "format: 1\ncommand: alpha\nvariant: default\ndetect: {signature: {all: ['^SHARED']}}\nparse: {type: kv}\n",
		"parsers/beta/default/parser.yaml":  "format: 1\ncommand: beta\nvariant: default\ndetect: {signature: {all: ['SHARED']}}\nparse: {type: kv}\n",
	})
	h.registryPath = dir
	code := h.pipe("SHARED=1\n")
	if code != ExitSelect || h.stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, h.stdout.String())
	}
	for _, want := range []string{"input matches multiple parsers", "alpha/default", "beta/default", "--parser alpha"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, h.stderr.String())
		}
	}
	if code := h.pipe("SHARED=1\n", "--parser", "beta"); code != ExitOK {
		t.Errorf("--parser beta: %d %s", code, h.stderr.String())
	}
}

func TestNamedParserAndVariantErrors(t *testing.T) {
	h := newHarness(t)
	// --variant without --parser cannot identify a definition.
	if code := h.pipe(gnuDF, "--variant", "gnu"); code != ExitUsage ||
		!strings.Contains(h.stderr.String(), "needs --parser") {
		t.Errorf("variant alone: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--parser", "nosuch"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), `no parser for "nosuch"`) {
		t.Errorf("unknown parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "nosuch"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "available: bsd,") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	// A variant of another command is not a variant of this one.
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "posix"); code != ExitSelect {
		t.Errorf("wrong combination: %d %s", code, h.stderr.String())
	}
	// A variant that contradicts the text is refused, and there is no way
	// to override that.
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "bsd"); code != ExitSelect {
		t.Errorf("mismatch: %d %s", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "df/bsd does not describe this input") {
		t.Errorf("mismatch message: %s", h.stderr.String())
	}
	if strings.Contains(h.stderr.String(), "force") {
		t.Errorf("no bypass may be advertised: %s", h.stderr.String())
	}
}

func TestParseFailureKeepsStdoutEmpty(t *testing.T) {
	h := newHarness(t)
	broken := "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs  abc 0 0 0% /run\n"
	if code := h.pipe(broken); code != ExitParse {
		t.Fatalf("code = %d, want %d (%s)", code, ExitParse, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), `df/gnu: line 2: field "1k_blocks": cannot convert "abc" to int`) {
		t.Errorf("stderr = %s", h.stderr.String())
	}
}

// TestInputSizeLimit pins that the 64 MiB ceiling is enforced even though
// it is no longer an option.
func TestInputSizeLimit(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(strings.Repeat("x", MaxInputSize+1)); code != ExitParse {
		t.Fatalf("code = %d, want %d (%s)", code, ExitParse, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Error("stdout must stay empty")
	}
	if !strings.Contains(h.stderr.String(), "input exceeds the 67108864 byte limit") {
		t.Errorf("stderr = %s", h.stderr.String())
	}
}

func TestUsageAndVersion(t *testing.T) {
	h := newHarness(t)
	// No arguments on a terminal: help instead of blocking on stdin.
	h.terminal = true
	if code := h.run(); code != ExitOK || !strings.Contains(h.stdout.String(), "COMMAND | jz") {
		t.Errorf("terminal help: %d\n%s", code, h.stdout.String())
	}
	h.terminal = false
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Errorf("piped: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(""); code != ExitSelect {
		t.Errorf("empty pipe: %d", code)
	}
	if code := h.run("help", "run"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz run") {
		t.Errorf("help run: %d %s", code, h.stdout.String())
	}
	if code := h.run("help", "bogus"); code != ExitOK || !strings.Contains(h.stdout.String(), "Commands:") {
		t.Errorf("help bogus: %d", code)
	}
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		if code := h.run(args...); code != ExitOK || !strings.HasPrefix(h.stdout.String(), "jz ") {
			t.Errorf("%v: %d %q", args, code, h.stdout.String())
		}
	}
	// version takes nothing, as list takes no more than it names.
	if code := h.run("version", "extra"); code != ExitUsage || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "takes no arguments") {
		t.Errorf("version extra: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	// A directory that does not exist is not described as one without
	// parsers in it.
	if code := h.run("test", filepath.Join(t.TempDir(), "absent")); code != ExitRegistry || !strings.Contains(h.stderr.String(), "does not exist") {
		t.Errorf("test absent: %d %s", code, h.stderr.String())
	}
	if code := h.run("frobnicate"); code != ExitUsage || !strings.Contains(h.stderr.String(), `unknown command "frobnicate"`) {
		t.Errorf("unknown command: %d %s", code, h.stderr.String())
	}
	// A file where a subcommand goes is a file meant to be read, and the
	// refusal says how, keeping the options it was given.
	captured := filepath.Join(t.TempDir(), "df output.txt")
	if err := os.WriteFile(captured, []byte(gnuDF), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--pretty", captured); code != ExitUsage || h.stdout.Len() != 0 ||
		!strings.Contains(h.stderr.String(), "jz --pretty --file '"+captured+"'") {
		t.Errorf("file as a command: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--pretty", "run"); code != ExitUsage || !strings.Contains(h.stderr.String(), "must come before") {
		t.Errorf("subcommand after options: %d %s", code, h.stderr.String())
	}
	for _, sub := range []string{"run", "list"} {
		if code := h.run(sub, "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz "+sub) {
			t.Errorf("%s --help: %d %q", sub, code, h.stdout.String())
		}
		// Every help ends with where to read more and where to help out.
		if !strings.Contains(h.stdout.String(), "GitHub Sponsors: https://github.com/sponsors/nao1215") ||
			!strings.Contains(h.stdout.String(), "https://nao1215.github.io/jsonize/") {
			t.Errorf("%s --help is missing the project links:\n%s", sub, h.stdout.String())
		}
		if code := h.run(sub, "--no-such-option"); code != ExitUsage {
			t.Errorf("%s bad option: %d", sub, code)
		}
	}
	// The subcommands cut before release stay gone.
	for _, gone := range []string{"parse", "show", "validate", "registry"} {
		if code := h.pipe(gnuDF, gone); code != ExitUsage {
			t.Errorf("%s should not exist: %d", gone, code)
		}
	}
}

func TestList(t *testing.T) {
	h := newHarness(t)
	if code := h.run("list"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out := h.stdout.String()
	if !strings.Contains(out, "COMMAND") || !strings.Contains(out, "bsd, bsd-human, busybox-human, freebsd, freebsd-blocks, freebsd-blocks-type, freebsd-human, freebsd-inodes, freebsd-type, freebsd-type-human, gnu") {
		t.Errorf("list:\n%s", out)
	}
	if code := h.run("list", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out = h.stdout.String()
	if !strings.Contains(out, "VARIANT") || !strings.Contains(out, "gnu-human") || !strings.Contains(out, "embedded") {
		t.Errorf("list df:\n%s", out)
	}
	if code := h.run("list", "df", "gnu-human"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out = h.stdout.String()
	for _, want := range []string{"df/gnu-human", "source:", "format:       1", "detection:", "signature", "regex each=line groups=filesystem,size"} {
		if !strings.Contains(out, want) {
			t.Errorf("list df gnu-human missing %q:\n%s", want, out)
		}
	}
	if code := h.run("list", "w", "linux"); code != ExitOK ||
		!strings.Contains(h.stdout.String(), "composite parts=uptime:regex,users:table") ||
		!strings.Contains(h.stdout.String(), "part uptime:") {
		t.Errorf("list w linux: %d\n%s", code, h.stdout.String())
	}
	if code := h.run("list", "--json"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var commands []map[string]any
	h.json(&commands)
	if len(commands) < 10 {
		t.Errorf("list --json returned %d commands", len(commands))
	}
	found := false
	for _, c := range commands {
		if c["command"] == "df" {
			found = true
			// The count grows whenever a df variant is added, so what is
			// asserted is that the listing carries the variants, not how
			// many the registry happens to hold today.
			if got := len(c["variants"].([]any)); got < 2 {
				t.Errorf("df variants = %v", c["variants"])
			}
		}
	}
	if !found {
		t.Errorf("df is missing from the list: %v", commands)
	}
	if code := h.run("list", "--json", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var variants []map[string]any
	h.json(&variants)
	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		seen[v["variant"].(string)] = true
	}
	for _, want := range []string{"bsd", "gnu", "gnu-human"} {
		if !seen[want] {
			t.Errorf("list df --json is missing %q: %v", want, variants)
		}
	}
	if code := h.run("list", "--json", "id", "posix"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var detail map[string]any
	h.json(&detail)
	if detail["variant"] != "posix" || detail["format"] != float64(1) ||
		!strings.Contains(detail["parse"].(string), "regex each=input") {
		t.Errorf("detail = %v", detail)
	}
	if code := h.run("list", "nosuch"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser for") {
		t.Errorf("list nosuch: %d %s", code, h.stderr.String())
	}
	if code := h.run("list", "df", "nosuch"); code != ExitSelect {
		t.Errorf("list df nosuch: %d", code)
	}
	if code := h.run("list", "a", "b", "c"); code != ExitUsage {
		t.Errorf("too many args: %d", code)
	}
}

func TestListSources(t *testing.T) {
	h := newHarness(t)
	if code := h.run("list", "--sources"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out := h.stdout.String()
	if !strings.Contains(out, "embedded") || !strings.Contains(out, "(built into jz)") ||
		!strings.Contains(out, EnvRegistryPath) {
		t.Errorf("sources:\n%s", out)
	}
	if strings.Contains(out, "--registry") {
		t.Errorf("sources must not advertise a removed option:\n%s", out)
	}
	if code := h.run("list", "--sources", "--json"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var srcs []map[string]any
	h.json(&srcs)
	last := srcs[len(srcs)-1]
	if last["name"] != "embedded" || last["present"] != true || last["definitions"].(float64) < 20 {
		t.Errorf("sources json = %v", srcs)
	}
	if code := h.run("list", "--sources", "df"); code != ExitUsage {
		t.Errorf("--sources with an argument: %d", code)
	}
}

func writeRegistry(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const helloDef = `format: 1
command: hello
variant: default
description: greeting lines
detect:
  signature:
    all: ['^hello \S+$']
parse:
  type: regex
  pattern: '^hello (?P<name>\S+)$'
`

func TestRegistryLayering(t *testing.T) {
	h := newHarness(t)
	user := filepath.Join(h.home, "config", "jsonize", "registry")
	writeRegistry(t, user, map[string]string{
		"registry.yaml":                     "format: 1\nname: mine\n",
		"parsers/hello/default/parser.yaml": helloDef,
		// An override of an official definition that keeps every value a string.
		"parsers/df/gnu/parser.yaml": "format: 1\ncommand: df\nvariant: gnu\ndescription: user override\n" +
			"detect: {signature: {all: ['^Filesystem\\s+1K-blocks']}}\n" +
			"parse: {type: table, header: {columns: [filesystem, blocks, used, available, use_percent, mounted_on]}}\n",
	})
	if code := h.pipe("hello world\n"); code != ExitOK {
		t.Fatalf("user parser: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["name"] != "world" {
		t.Errorf("rows = %v", h.rows())
	}
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if h.rows()[0]["blocks"] != "1000000" {
		t.Errorf("override = %v", h.rows())
	}
	if code := h.run("list", "df", "gnu"); code != ExitOK ||
		!strings.Contains(h.stdout.String(), "shadows:      the same definition in embedded") {
		t.Errorf("shadow note: %d\n%s", code, h.stdout.String())
	}

	// JSONIZE_REGISTRY_PATH is the way to add a registry, and it takes a
	// list separated the way PATH is on this platform.
	extra := filepath.Join(h.home, "extra")
	writeRegistry(t, extra, map[string]string{
		"parsers/bye/default/parser.yaml": strings.ReplaceAll(helloDef, "hello", "bye"),
	})
	h.registryPath = strings.Join([]string{extra, "", filepath.Join(h.home, "absent")}, string(os.PathListSeparator))
	if code := h.pipe("bye now\n"); code != ExitOK {
		t.Errorf("%s: %d %s", EnvRegistryPath, code, h.stderr.String())
	}
	if code := h.run("list", "--sources"); code != ExitOK || !strings.Contains(h.stdout.String(), extra) {
		t.Errorf("sources with env: %d\n%s", code, h.stdout.String())
	}
	// A broken definition is reported but does not take the rest down.
	writeRegistry(t, user, map[string]string{"parsers/broken/x/parser.yaml": "format: 1\n"})
	if code := h.pipe("hello you\n"); code != ExitOK ||
		!strings.Contains(h.stderr.String(), "warning: skipping definition") {
		t.Errorf("broken definition: %d %s", code, h.stderr.String())
	}
}

func TestParserKey(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"df": "df", "/usr/bin/df": "df", "tool.cmd": "tool", "x.bat": "x", "df.EXE": "df"} {
		if got := parserKey(in); got != want {
			t.Errorf("parserKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- exec mode -------------------------------------------------------

func shellRegistry(t *testing.T, h *harness) {
	t.Helper()
	dir := filepath.Join(h.home, "reg")
	writeRegistry(t, dir, map[string]string{
		"parsers/sh/default/parser.yaml": "format: 1\ncommand: sh\nvariant: default\n" +
			"detect: {signature: {all: ['^[A-Za-z_][A-Za-z0-9_]*=']}}\nexec: {env: {JZ_DEF: fromdef}}\n" +
			"parse: {type: kv}\nfields: {n: {type: int}}\n",
		"parsers/sh/alt/parser.yaml": "format: 1\ncommand: sh\nvariant: alt\n" +
			"detect: {signature: {all: ['^ALT']}}\nexec: {env: {JZ_DEF: other}}\n" +
			"parse: {type: regex, pattern: '^(?P<line>.*)$'}\n",
		"parsers/wrapper/default/parser.yaml": "format: 1\ncommand: wrapper\nvariant: default\n" +
			"detect: {signature: {all: ['^wrapped=']}}\nparse: {type: kv}\n",
	})
	h.registryPath = dir
}

func TestRunExecMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	if code := h.run("run"); code != ExitUsage || !strings.Contains(h.stderr.String(), "no command given") {
		t.Errorf("no command: %d", code)
	}
	if code := h.run("run", "--env", "BAD", "sh"); code != ExitUsage {
		t.Errorf("bad env: %d", code)
	}
	// Nothing runs when jz cannot parse the result anyway.
	if code := h.run("run", "nosuchcmd"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser for") {
		t.Errorf("unknown parser: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--variant", "zzz", "sh", "-c", "echo x=1"); code != ExitUsage ||
		!strings.Contains(h.stderr.String(), "needs --parser") {
		t.Errorf("variant without parser: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--parser", "sh", "--variant", "zzz", "sh", "-c", "echo x=1"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "has no variant") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	// Success: the locale is forced, --env is applied and the conflicting
	// exec.env of the two variants is dropped.
	if code := h.run("run", "--env", "JZ_X=1", "sh", "-c", "echo n=$JZ_X; echo lc=$LC_ALL; echo def=${JZ_DEF:-unset}"); code != ExitOK {
		t.Fatalf("run: %d %s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 3 || rows[0]["value"] != float64(1) || rows[1]["value"] != "C" || rows[2]["value"] != "unset" {
		t.Errorf("rows = %v", rows)
	}
	if code := h.run("run", "--keep-locale", "sh", "-c", "echo lc=${LC_ALL:-inherited}"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"inherited"`) &&
		!strings.Contains(h.stdout.String(), `"value":"`+os.Getenv("LC_ALL")+`"`) {
		t.Errorf("keep-locale: %s", h.stdout.String())
	}
	// --parser lets a wrapper be read as the tool it wraps.
	if code := h.run("run", "--parser", "wrapper", "--", "sh", "-c", "echo wrapped=yes"); code != ExitOK {
		t.Fatalf("wrapper: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["value"] != "yes" {
		t.Errorf("wrapper rows = %v", h.rows())
	}
	// The output still has to match the signature, with no way around it.
	if code := h.run("run", "sh", "-c", "echo no separator here"); code != ExitSelect {
		t.Errorf("signature mismatch: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--parser", "sh", "--variant", "default", "sh", "-c", "echo no separator here"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "does not describe this input") {
		t.Errorf("explicit variant mismatch: %d %s", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	// Exit status, stderr passthrough and signals.
	if code := h.run("run", "sh", "-c", "echo a=b; echo oops >&2; exit 7"); code != 7 {
		t.Errorf("exit passthrough: %d %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"b"`) || !strings.Contains(h.stderr.String(), "oops") ||
		!strings.Contains(h.stderr.String(), "exited with status 7") {
		t.Errorf("stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
	}
	if code := h.run("run", "sh", "-c", "exit 3"); code != 3 || h.stdout.Len() != 0 {
		t.Errorf("failure without output: %d %q", code, h.stdout.String())
	}
	if code := h.run("run", "sh", "-c", "kill -TERM $$"); code != 143 || !strings.Contains(h.stderr.String(), "SIGTERM") {
		t.Errorf("signal: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--timeout", "200ms", "sh", "-c", "exec sleep 5"); code != 143 ||
		!strings.Contains(h.stderr.String(), "timeout") {
		t.Errorf("timeout: %d %s", code, h.stderr.String())
	}
	writeRegistry(t, filepath.Join(h.home, "reg"), map[string]string{
		"parsers/definitely-missing-binary/x/parser.yaml": "format: 1\ncommand: definitely-missing-binary\nvariant: x\nparse: {type: kv}\n",
	})
	if code := h.run("run", "definitely-missing-binary"); code != ExitError ||
		!strings.Contains(h.stderr.String(), "cannot run") {
		t.Errorf("missing binary: %d %s", code, h.stderr.String())
	}
}

// TestRunArgumentBoundary pins where jz's own options stop and the
// command's begin.
func TestRunArgumentBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	dir := filepath.Join(h.home, "reg")
	writeRegistry(t, dir, map[string]string{
		"parsers/sh/default/parser.yaml": "format: 1\ncommand: sh\nvariant: default\n" +
			"detect: {signature: {all: ['^args=']}}\nparse: {type: kv}\n",
	})
	h.registryPath = dir
	script := `echo "args=$*"`
	tests := []struct {
		name     string
		args     []string
		wantArgs string
		pretty   bool
	}{
		{"a command flag is not a jz option", []string{"run", "sh", "-c", script, "sh", "-h"}, "-h", false},
		{"a jz option before the command is jz's", []string{"run", "--pretty", "sh", "-c", script, "sh", "-h"}, "-h", true},
		{"the same option after the command is the command's", []string{"run", "sh", "-c", script, "sh", "--pretty"}, "--pretty", false},
		{"--parser after the command passes through", []string{"run", "sh", "-c", script, "sh", "--parser", "x"}, "--parser x", false},
		{"-- stops jz option parsing", []string{"run", "--", "sh", "-c", script, "sh", "--pretty", "--variant"}, "--pretty --variant", false},
		{"-- after the command belongs to the command", []string{"run", "sh", "-c", script, "sh", "--", "--literal"}, "-- --literal", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := h.run(tt.args...); code != ExitOK {
				t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
			}
			if got := h.rows()[0]["value"]; got != tt.wantArgs {
				t.Errorf("args = %q, want %q", got, tt.wantArgs)
			}
			if pretty := strings.Contains(h.stdout.String(), "\n  "); pretty != tt.pretty {
				t.Errorf("pretty = %v, want %v: %s", pretty, tt.pretty, h.stdout.String())
			}
		})
	}
}

func TestRunPassesStdinToTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs POSIX wc")
	}
	h := newHarness(t)
	h.stdin.WriteString("hello world\n")
	if code := h.run("run", "wc"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 1 || rows[0]["lines"] != float64(1) || rows[0]["words"] != float64(2) || rows[0]["bytes"] != float64(12) {
		t.Errorf("wc counted %v, want 1 line, 2 words, 12 bytes", rows)
	}
}

// A command ended from outside stops wherever its output had got to, as
// often as not in the middle of a line. A stream keeps the records it has
// written but not the one the cut fell in; output that never came to its
// end is not a whole document, so none is written.
func TestRunDoesNotReadOutputCutShort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	killed := "echo a=whole; printf b=cu; kill -TERM $$"
	stalled := "echo a=whole; printf b=cu; exec sleep 5"
	define := "parse: {type: kv}"
	for _, args := range [][]string{
		{"run", "--stream", "sh", "-c", killed},
		{"run", "--stream", "--define", define, "sh", "-c", killed},
		{"run", "--stream", "--timeout", "300ms", "sh", "-c", stalled},
	} {
		code := h.run(args...)
		if code != 143 || !strings.Contains(h.stdout.String(), `"value":"whole"`) || strings.Contains(h.stdout.String(), `"cu"`) {
			t.Errorf("%v: %d stdout=%q stderr=%s", args, code, h.stdout.String(), h.stderr.String())
		}
	}
	for _, args := range [][]string{
		{"run", "sh", "-c", killed},
		{"run", "--define", define, "sh", "-c", killed},
		{"run", "--timeout", "300ms", "sh", "-c", stalled},
	} {
		code := h.run(args...)
		if code != 143 || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "--stream") {
			t.Errorf("%v: %d stdout=%q stderr=%s", args, code, h.stdout.String(), h.stderr.String())
		}
	}
}

// A command that ends and leaves a process behind holding its output
// open has still ended, and what it printed is converted. The whole
// document used to fail with "WaitDelay expired before I/O complete" and
// a stream to wait for the process it left, which for a daemon is for
// ever.
func TestRunCommandThatLeavesItsOutputOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	for _, args := range [][]string{
		{"run", "--define", "parse: {type: kv}", "--", "sh", "-c", "sleep 6 & echo a=b"},
		{"run", "--stream", "--define", "parse: {type: kv}", "--", "sh", "-c", "sleep 6 & echo a=b"},
	} {
		start := time.Now()
		code := h.run(args...)
		if code != ExitOK || !strings.Contains(h.stdout.String(), `"value":"b"`) ||
			!strings.Contains(h.stderr.String(), "sh ended, and a process it started still held its output open") {
			t.Errorf("%v: %d stdout=%q stderr=%s", args, code, h.stdout.String(), h.stderr.String())
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("%v: waited %s for the process sh left", args, time.Since(start))
		}
	}
}

// A wrapper puts its own name where jz looks for the parser. When what it
// runs is a command jz knows, the refusal says how to name that parser,
// instead of offering names that merely look like the wrapper's.
func TestRunNamesTheParserAWrapperRuns(t *testing.T) {
	h := newHarness(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "nice", "-n", "5", "df", "-h"}, "jz run --parser df -- nice -n 5 df -h"},
		{[]string{"run", "--pretty", "stdbuf", "-oL", "/usr/bin/ps", "aux"}, "jz run --pretty --parser ps -- stdbuf -oL /usr/bin/ps aux"},
	} {
		code := h.run(tt.args...)
		if code != ExitSelect || !strings.Contains(h.stderr.String(), tt.want) || strings.Contains(h.stderr.String(), "did you mean") {
			t.Errorf("%v: %d %s", tt.args, code, h.stderr.String())
		}
	}
	// Nothing jz knows among the arguments: no parser is named, and the
	// refusal says only how to name one.
	if code := h.run("run", "nice", "./script"); code != ExitSelect || strings.Contains(h.stderr.String(), " runs ") {
		t.Errorf("no known command: %d %s", code, h.stderr.String())
	}
	// A directory is something a command reads, not a command it runs,
	// whatever its name: `tree -L 2 /etc/apt` does not run apt.
	dir := filepath.Join(t.TempDir(), "apt")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := h.run("run", "nice", "-n", "5", dir); code != ExitSelect || strings.Contains(h.stderr.String(), "--parser apt") {
		t.Errorf("a directory named like a parser: %d %s", code, h.stderr.String())
	}
	// A file that is not a program is read, not run: `stat /proc/uptime`.
	file := filepath.Join(t.TempDir(), "uptime")
	if err := os.WriteFile(file, []byte("1 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run("run", "nice", file); runtime.GOOS != "windows" && (code != ExitSelect || strings.Contains(h.stderr.String(), "--parser uptime")) {
		t.Errorf("a data file named like a parser: %d %s", code, h.stderr.String())
	}
	// A command jz has no parser for may still print a format jz reads
	// under another name, and the refusal says how to name that parser
	// rather than only where the list of them is.
	if code := h.run("run", "nosuchtool-jz", "--tag", "f"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "If nosuchtool-jz prints a format jz reads under another name, name its parser: jz run --parser PARSER -- nosuchtool-jz --tag f") {
		t.Errorf("unknown command: %d %s", code, h.stderr.String())
	}
	// A value that happens to be the name of a shape (csv, table) is an
	// option's value, not a command the first one runs: systeminfo /fo csv
	// is not systeminfo running csv, and naming csv would be ambiguous
	// between its variants anyway.
	if code := h.run("run", "nosuchtool-jz", "/fo", "csv"); code != ExitSelect || strings.Contains(h.stderr.String(), "runs csv") {
		t.Errorf("a shape named as an option's value: %d %s", code, h.stderr.String())
	}
	if runtime.GOOS == "windows" {
		return
	}
	// env is a parser of its own, so jz runs it and only the output says
	// it ran something else.
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "env", "JZ_X=1", "df"}, "If env runs df, name that parser: jz run --parser df -- env JZ_X=1 df"},
		{[]string{"run", "--stream", "env", "JZ_X=1", "df"}, "If env runs df, name that parser: jz run --stream --parser df -- env JZ_X=1 df"},
	} {
		code := h.run(tt.args...)
		if code != ExitSelect || !strings.Contains(h.stderr.String(), tt.want) {
			t.Errorf("%v: %d %s", tt.args, code, h.stderr.String())
		}
	}
}

func TestRunRealCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX commands")
	}
	h := newHarness(t)
	for _, args := range [][]string{{"uname", "-a"}, {"id"}, {"env"}, {"df"}, {"df", "-h"}} {
		if code := h.run(append([]string{"run"}, args...)...); code != ExitOK {
			t.Errorf("run %v: %d %s", args, code, h.stderr.String())
		}
	}
}

// A command that prints what another one prints is read by the same
// definition, under its own name and with the arguments that name needs.
func TestRunUsesAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX commands")
	}
	h := newHarness(t)
	for _, args := range [][]string{{"printenv"}, {"getent", "passwd"}, {"getent", "group"}} {
		// getent is glibc's, so it is absent on macOS and on a musl
		// system. What the alias does is the same wherever the command
		// exists; where it does not there is nothing to run.
		if _, err := exec.LookPath(args[0]); err != nil {
			t.Logf("%s is not installed here", args[0])
			continue
		}
		if code := h.run(append([]string{"run"}, args...)...); code != ExitOK {
			t.Errorf("run %v: %d %s", args, code, h.stderr.String())
		}
	}
	// The arguments an alias states replace the ones the command needs:
	// getent takes the database name where etc takes nothing, and a
	// database no definition claims is refused rather than read with
	// whichever of them happens to fit.
	if _, err := exec.LookPath("getent"); err == nil {
		if code := h.run("run", "getent", "ahosts"); code == ExitOK {
			t.Errorf("getent ahosts has no definition and must not be read: %s", h.stdout.String())
		}
	}
	// Naming the parser by an alias reaches the same definitions.
	if code := h.pipe("root:x:0:\n", "--parser", "getent", "--variant", "group"); code != ExitOK {
		t.Errorf("--parser getent --variant group: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["name"] != "root" {
		t.Errorf("rows = %v", h.rows())
	}
}

func TestMergedExecEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRegistry(t, dir, map[string]string{
		"parsers/c/a/parser.yaml": "format: 1\ncommand: c\nvariant: a\nexec: {env: {TZ: UTC, X: '1'}}\nparse: {type: kv}\n",
		"parsers/c/b/parser.yaml": "format: 1\ncommand: c\nvariant: b\nexec: {env: {TZ: UTC, X: '2'}}\nparse: {type: kv}\n",
	})
	reg, err := registry.Load(registry.Source{Name: "t", FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(execEnv(reg, selector.Context{Parser: "c"}), ","); got != "TZ=UTC" {
		t.Errorf("execEnv = %q", got)
	}
	// A variant named on the command line is the definition that reads
	// the output, so its entries apply as they stand.
	if got := strings.Join(execEnv(reg, selector.Context{Parser: "c", Variant: "b"}), ","); got != "TZ=UTC,X=2" {
		t.Errorf("execEnv for the variant = %q", got)
	}
}

// The environment comes from the variants the arguments and the system
// leave, since those are the definitions that can read the output. A
// setting one subcommand's output needs is not given to another's: git
// log asked to leave its decorations out would otherwise put that
// setting into what git config --list prints.
func TestExecEnvFollowsTheArguments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRegistry(t, dir, map[string]string{
		"parsers/g/log/parser.yaml":    "format: 1\ncommand: g\nvariant: log\ndetect: {args: {any: [log]}}\nexec: {env: {G_DECORATE: 'no'}}\nparse: {type: kv}\n",
		"parsers/g/config/parser.yaml": "format: 1\ncommand: g\nvariant: config\ndetect: {args: {any: [config]}}\nparse: {type: kv}\n",
		"parsers/g/mac/parser.yaml":    "format: 1\ncommand: g\nvariant: mac\ndetect: {os: [darwin], args: {any: [config]}}\nexec: {env: {G_MAC: '1'}}\nparse: {type: kv}\n",
	})
	reg, err := registry.Load(registry.Source{Name: "t", FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		os   string
		want string
	}{
		{[]string{"log", "--oneline"}, "linux", "G_DECORATE=no"},
		{[]string{"config", "--list"}, "linux", ""},
		{[]string{"config", "--list"}, "darwin", "G_MAC=1"},
	} {
		got := strings.Join(execEnv(reg, selector.Context{Parser: "g", OS: tt.os, Args: tt.args}), ",")
		if got != tt.want {
			t.Errorf("%v on %s: %q, want %q", tt.args, tt.os, got, tt.want)
		}
	}
}

// --- the regressions the parser fixes are about ----------------------

// TestNoFabricatedUnits pins that a rounded, human-readable size reaches
// JSON exactly as the command printed it.
func TestNoFabricatedUnits(t *testing.T) {
	h := newHarness(t)
	si := "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1       1.1G  100M  1.0G  10% /\n"
	if code := h.pipe(si); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	rows := h.rows()
	if rows[0]["size"] != "1.1G" || rows[0]["used"] != "100M" {
		t.Errorf("sizes must stay as printed: %v", rows[0])
	}
	if rows[0]["use_percent"] != float64(10) {
		t.Errorf("a percentage is exact and stays typed: %v", rows[0])
	}
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if h.rows()[0]["1k_blocks"] != float64(1000000) {
		t.Errorf("1K blocks are exact: %v", h.rows()[0])
	}
	if code := h.pipe("               total        used        free      shared  buff/cache   available\nMem:            61Gi        34Gi       8.3Gi        17Gi        36Gi        26Gi\n"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if h.rows()[0]["total"] != "61Gi" {
		t.Errorf("free -h: %v", h.rows()[0])
	}
	if code := h.pipe(lsblkList); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if h.rows()[0]["size"] != "20G" || h.rows()[0]["mountpoint"] != nil {
		t.Errorf("lsblk: %v", h.rows()[0])
	}
}

// TestGenericFormatsNeedAnExplicitParser pins that a format jz cannot
// recognise on its own is refused, with a message naming the parser to
// pass, instead of being claimed because it was the only candidate left.
func TestGenericFormatsNeedAnExplicitParser(t *testing.T) {
	h := newHarness(t)
	// Three tab separated cells, two of them counts, is what
	// `git diff --numstat` prints and not what du prints.
	if code := h.pipe("10\t2\tmain.go\n"); code != ExitSelect || h.stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%s", code, h.stdout.String(), h.stderr.String())
	}
	if got := h.stderr.String(); !strings.Contains(got, "could be `git` output") {
		t.Errorf("numstat should be named as git:\n%s", got)
	}
	// A count and a path is du, and nothing about the text says so, so
	// the parser has to be named.
	if code := h.pipe("12\t/usr/bin\n"); code != ExitSelect || h.stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%s", code, h.stdout.String(), h.stderr.String())
	}
	for _, want := range []string{"could be `du` output", "too generic", "jz --parser du"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, h.stderr.String())
		}
	}
	if code := h.pipe("134164\t/usr/bin\n", "--parser", "du"); code != ExitOK {
		t.Fatalf("--parser du: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["size"] != float64(134164) || h.rows()[0]["name"] != "/usr/bin" {
		t.Errorf("du rows = %v", h.rows())
	}
	if code := h.pipe("not du output at all\n", "--parser", "du"); code != ExitSelect {
		t.Errorf("the signature still applies: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("      1       1       4\n"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "jz --parser wc") {
		t.Errorf("wc without a parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("      1       1       4\n", "--parser", "wc"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	rows := h.rows()
	if _, ok := rows[0]["characters"]; ok {
		t.Errorf("wc must not report characters: %v", rows[0])
	}
	if rows[0]["bytes"] != float64(4) || rows[0]["lines"] != float64(1) {
		t.Errorf("wc rows = %v", rows)
	}
}

func TestRunWithNoOutputAnswersWithAnEmptyList(t *testing.T) {
	h := newHarness(t)
	// A command that lists things prints nothing when there is nothing
	// to list. jz started this command, so it knows which format was
	// meant and can say the list is empty; on a pipe it could not. true
	// ignores its arguments, so it stands in for `git stash list`.
	if code := h.run("run", "--parser", "git", "--variant", "stash-list", "true", "stash", "list"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	if got := strings.TrimSpace(h.stdout.String()); got != "[]" {
		t.Errorf("stdout = %q", got)
	}
	// The arguments still choose the variant. Without them the command
	// was not asked for the list, and nothing printed is no answer: the
	// same arguments with output would have been refused too.
	for _, args := range [][]string{
		{"run", "--parser", "git", "--variant", "stash-list", "true"},
		{"run", "--parser", "git", "--variant", "stash-list", "--stream", "true"},
		// systemd-inhibit without --list is a wrapper around another
		// command, and what that one printed is no list of locks.
		{"run", "--parser", "systemd-inhibit", "true", "--what=sleep", "true"},
	} {
		if code := h.run(args...); code != ExitSelect || h.stdout.Len() != 0 {
			t.Errorf("%v: code=%d stdout=%q %s", args, code, h.stdout.String(), h.stderr.String())
		}
	}
	// A format that yields one object has no empty form, so the answer
	// is not knowable and jz says so rather than inventing one. id/posix
	// is read on every system and with any arguments, so what is left
	// to refuse is the missing empty form; uptime/linux would be ruled
	// out on macOS and Windows before that.
	h2 := newHarness(t)
	if code := h2.run("run", "--parser", "id", "--variant", "posix", "true"); code != ExitSelect {
		t.Fatalf("code=%d %s", code, h2.stderr.String())
	}
	if h2.stdout.Len() != 0 {
		t.Errorf("stdout must stay empty: %q", h2.stdout.String())
	}
	if !strings.Contains(h2.stderr.String(), "no empty form") {
		t.Error(h2.stderr.String())
	}
}

func TestExtractAndExclude(t *testing.T) {
	const input = "Filesystem     1K-blocks    Used Available Use% Mounted on\n" +
		"tmpfs              12882    5928     12876   1% /run\n"

	h := newHarness(t)
	if code := h.pipe(input, "--extract", "filesystem", "--extract", "mounted_on"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	// The keys keep the order the definition gives them, not the order
	// they were asked for.
	if got := strings.TrimSpace(h.stdout.String()); got != `[{"filesystem":"tmpfs","mounted_on":"/run"}]` {
		t.Errorf("extract = %s", got)
	}

	h = newHarness(t)
	if code := h.pipe(input, "--exclude", "1k_blocks", "--exclude", "used",
		"--exclude", "available", "--exclude", "use_percent"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	if got := strings.TrimSpace(h.stdout.String()); got != `[{"filesystem":"tmpfs","mounted_on":"/run"}]` {
		t.Errorf("exclude = %s", got)
	}

	// A key the format does not produce is an error rather than an empty
	// answer, and the message says what there was instead.
	h = newHarness(t)
	if code := h.pipe(input, "--extract", "nosuch"); code != ExitUsage {
		t.Fatalf("unknown key should be a usage error, got %d", code)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout must stay empty: %q", h.stdout.String())
	}
	for _, want := range []string{`no key "nosuch"`, `"filesystem"`} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, h.stderr.String())
		}
	}

	// Naming a key on both sides states two answers for it.
	h = newHarness(t)
	if code := h.pipe(input, "--extract", "filesystem", "--exclude", "used"); code != ExitUsage {
		t.Fatalf("the pair should be a usage error, got %d", code)
	}
	if !strings.Contains(h.stderr.String(), "cannot be used together") {
		t.Error(h.stderr.String())
	}
}

// A key is checked against what the definition can produce, not against
// what one input happened to hold: an optional key no row has, and any
// key of an empty listing, narrow to nothing rather than being refused,
// and a stream does not refuse a key its first record omits. A key
// outside what the definition can produce is refused before anything
// is written.
func TestExtractKnowsTheKeysOfTheFormat(t *testing.T) {
	const lo = "lo               UNKNOWN        00:00:00:00:00:00 <LOOPBACK,UP,LOWER_UP> \n"
	const veth = "veth0@if2        UP             52:54:00:10:00:06 <BROADCAST,MULTICAST,UP,LOWER_UP> \n"
	for _, tc := range []struct {
		name  string
		input string
		args  []string
		code  int
		want  string
	}{
		{"optional key no row has", lo, []string{"--extract", "peer"}, ExitOK, `[{}]`},
		{"optional key in a later record of a stream", lo + veth, []string{"--stream", "--extract", "peer"}, ExitOK, "{}\n{\"peer\":\"if2\"}"},
		{"exclude an optional key no row has", lo, []string{"--exclude", "peer"}, ExitOK,
			`[{"interface":"lo","state":"UNKNOWN","lladdr":"00:00:00:00:00:00","flags":["LOOPBACK","UP","LOWER_UP"]}]`},
		{"key the format cannot produce", lo + veth, []string{"--stream", "--extract", "nosuch"}, ExitUsage, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			args := append([]string{"--parser", "ip", "--variant", "brief-link"}, tc.args...)
			if code := h.pipe(tc.input, args...); code != tc.code {
				t.Fatalf("code=%d want %d: %s", code, tc.code, h.stderr.String())
			}
			if got := strings.TrimSpace(h.stdout.String()); got != tc.want {
				t.Errorf("stdout = %q, want %q", got, tc.want)
			}
		})
	}
	// The keys named in the refusal are the ones the format has.
	h := newHarness(t)
	if code := h.pipe(lo, "--parser", "ip", "--variant", "brief-link", "--extract", "nosuch"); code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(h.stderr.String(), `"peer"`) {
		t.Errorf("the refusal should list peer among the keys: %s", h.stderr.String())
	}
	// An empty listing has every key of its format.
	h = newHarness(t)
	if code := h.pipe("PID TTY          TIME CMD\n", "--parser", "ps", "--variant", "posix", "--extract", "pid"); code != ExitOK {
		t.Fatalf("empty listing: code=%d %s", code, h.stderr.String())
	}
	if got := strings.TrimSpace(h.stdout.String()); got != "[]" {
		t.Errorf("empty listing = %q", got)
	}
}

func TestExtractOnASingleObject(t *testing.T) {
	h := newHarness(t)
	const input = "uid=1000(alice) gid=1000(alice) groups=1000(alice)\n"
	if code := h.pipe(input, "--exclude", "groups"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	// A result that is one object rather than a list narrows the same way.
	if got := strings.TrimSpace(h.stdout.String()); strings.Contains(got, "groups") {
		t.Errorf("groups should be gone: %s", got)
	} else if !strings.Contains(got, `"uid"`) {
		t.Errorf("uid should remain: %s", got)
	}
}

func TestEnvValuesAreKeptVerbatim(t *testing.T) {
	h := newHarness(t)
	// NAME=value describes a .env file and a properties file as well, so
	// the line form is named rather than guessed; the NUL form below is
	// distinctive enough to be claimed on sight.
	if code := h.pipe("A=  padded  \nB=x\nC=\n"); code != ExitSelect {
		t.Fatalf("plain env should not be claimed: code=%d %s", code, h.stderr.String())
	}
	if code := h.pipe("A=  padded  \nB=x\nC=\n", "--parser", "env"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	rows := h.rows()
	if rows[0]["value"] != "  padded  " || rows[2]["value"] != "" {
		t.Errorf("env values = %v", rows)
	}
	// The NUL form of the same command is the lossless one: a value may
	// contain a newline, which the line form cannot express.
	if code := h.pipe("A=one\nstill A\x00B=two\x00"); code != ExitOK {
		t.Fatalf("env -0: %d %s", code, h.stderr.String())
	}
	rows = h.rows()
	if len(rows) != 2 || rows[0]["value"] != "one\nstill A" {
		t.Errorf("NUL data = %v", rows)
	}
}

func TestLsListingsAreReadOnce(t *testing.T) {
	h := newHarness(t)
	listing := "total 8\n" +
		"-rw-r--r-- 1 alice staff   42 Jan  5 10:11 a -> b\n" +
		"lrwxrwxrwx 1 alice staff    7 Jan  5 10:12 bin -> usr/bin\n"
	if code := h.pipe(listing); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	rows := h.rows()
	if rows[0]["filename"] != "a -> b" {
		t.Errorf("regular file: %v", rows[0])
	}
	if _, ok := rows[0]["link_to"]; ok {
		t.Errorf("a regular file has no target: %v", rows[0])
	}
	if rows[1]["filename"] != "bin" || rows[1]["link_to"] != "usr/bin" {
		t.Errorf("symlink: %v", rows[1])
	}
	// One parser reads both -l and -lh, so neither the sizes nor their
	// order decide whether the listing can be read at all.
	small := "-rw-r--r-- 1 u g 3 Jan  5 10:11 small.txt\n"
	// The suffixed size sits well past the lines a signature may read, so
	// a listing that has to be classified by its sizes would fail here.
	mixed := strings.Repeat(small, 25) + "-rw-r--r-- 1 u g 2.9K Jan  5 10:11 big.bin\n"
	if code := h.pipe(small); code != ExitOK {
		t.Fatalf("small only: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["size"] != "3" {
		t.Errorf("small only: %v", h.rows()[0])
	}
	if code := h.pipe(mixed); code != ExitOK {
		t.Fatalf("mixed sizes: %d %s", code, h.stderr.String())
	}
	rows = h.rows()
	if rows[0]["size"] != "3" || rows[len(rows)-1]["size"] != "2.9K" {
		t.Errorf("sizes stay as printed: %v ... %v", rows[0], rows[len(rows)-1])
	}
}

func TestTruncatedAlignedRowIsAnError(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe("NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nsda\n"); code != ExitParse {
		t.Fatalf("code = %d, want %d (%s)", code, ExitParse, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), `field "maj_min": required value is missing`) {
		t.Errorf("stderr = %s", h.stderr.String())
	}
	if code := h.pipe(lsblkList); code != ExitOK {
		t.Fatalf("unmounted device: %d %s", code, h.stderr.String())
	}
	if h.rows()[0]["mountpoint"] != nil {
		t.Errorf("mountpoint = %v", h.rows()[0]["mountpoint"])
	}
}

// TestEmptyInput pins the ways an empty input can end: unidentifiable
// when nothing was named, and a signature mismatch when something was.
func TestEmptyInput(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(""); code != ExitSelect || !strings.Contains(h.stderr.String(), "unable to identify") {
		t.Errorf("auto: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("", "--parser", "df"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "no df variant matches this input") {
		t.Errorf("--parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("", "--parser", "df", "--variant", "gnu"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "does not describe this input") {
		t.Errorf("--variant: %d %s", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// --raw reports what the definition extracted rather than what it made of
// it, which is what makes a definition debuggable: a value converted
// wrongly and a value cut from the wrong place look alike once typed.
func TestRawSkipsTheFieldRules(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--raw"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 2 || rows[0]["use_percent"] != "1%" || rows[0]["1k_blocks"] != "1000000" {
		t.Fatalf("rows = %v", rows)
	}
	// Without it the same input is typed, which is the contract raw mode
	// steps out of.
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatal(code)
	}
	if got := h.rows()[0]["use_percent"]; got != float64(1) {
		t.Errorf("typed use_percent = %#v", got)
	}
	// It composes with the options that decide how the JSON is written.
	if code := h.pipe(gnuDF, "--raw", "--pretty"); code != ExitOK || !strings.Contains(h.stdout.String(), "\"1%\"") {
		t.Errorf("pretty: %d %s", code, h.stdout.String())
	}
	if code := h.pipe(gnuDF, "--raw", "--stream"); code != ExitOK {
		t.Fatalf("stream: %d %s", code, h.stderr.String())
	}
	for _, rec := range h.records() {
		if _, ok := rec["1k_blocks"].(string); !ok {
			t.Errorf("streamed record is typed: %v", rec)
		}
	}
	// jz test compares a fixture with the JSON beside it, and that JSON
	// is the typed reading. Raw mode has nothing to say there.
	if code := h.run("test", "--raw"); code != ExitUsage {
		t.Errorf("jz test --raw: %d", code)
	}
}

// A format that prints no year, or names its zone only by an
// abbreviation, leaves the value as the text it was printed as. Dating
// it by this machine's clock, or reading an abbreviation against this
// machine's own zone, would make the same text convert differently on
// two machines.
func TestAssumptionsAboutTimestamps(t *testing.T) {
	h := newHarness(t)
	const who = "alice    seat0        Nov  4 13:17 (login screen)\n"
	if code := h.pipe(who, "--parser", "who"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if got := h.rows()[0]["time"]; got != "Nov  4 13:17" {
		t.Errorf("without a year: %#v", got)
	}
	if code := h.pipe(who, "--parser", "who", "--assume-year", "2025"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if got := h.rows()[0]["time"]; got != "2025-11-04T13:17:00Z" {
		t.Errorf("with a year: %#v", got)
	}
	// "now" is resolved once, on the command line, so nothing further
	// down reads a clock. It dates the login in the latest year that does
	// not put it after that moment, since a login that was recorded has
	// happened: within the year before, and not later than a day ahead.
	started := time.Now()
	if code := h.pipe(who, "--parser", "who", "--assume-year", "now"); code != ExitOK {
		t.Fatalf("now: %d %s", code, h.stderr.String())
	}
	got, ok := h.rows()[0]["time"].(string)
	when, err := time.Parse(time.RFC3339, got)
	if !ok || err != nil || when.After(started.Add(24*time.Hour)) || !when.After(started.AddDate(-1, 0, -1)) {
		t.Errorf("now: %#v read at %s", h.rows()[0]["time"], started.Format(time.RFC3339))
	}

	const date = "Mon Sep  7 10:02:02 JST 2026\n"
	if code := h.pipe(date, "--parser", "date"); code != ExitOK {
		t.Fatalf("date: %d %s", code, h.stderr.String())
	}
	var obj map[string]any
	h.json(&obj)
	if obj["timestamp"] != "Mon Sep  7 10:02:02 JST 2026" {
		t.Errorf("without a zone: %#v", obj["timestamp"])
	}
	if code := h.pipe(date, "--parser", "date", "--assume-zone", "JST=+0900"); code != ExitOK {
		t.Fatalf("date zone: %d %s", code, h.stderr.String())
	}
	h.json(&obj)
	if obj["timestamp"] != "2026-09-07T10:02:02+09:00" {
		t.Errorf("with a zone: %#v", obj["timestamp"])
	}

	// An assumption no field in the chosen format asks for makes no
	// difference to the output and is not an error.
	if code := h.pipe(gnuDF, "--assume-year", "2025", "--assume-zone", "JST=+0900"); code != ExitOK {
		t.Errorf("unused assumptions: %d %s", code, h.stderr.String())
	}
	// One that cannot be read is.
	for _, args := range [][]string{
		{"--assume-year", "20x5"}, {"--assume-year", "25"}, {"--assume-year", "later"},
		{"--assume-zone", "JST"}, {"--assume-zone", "JST=0900"}, {"--assume-zone", "=+0900"},
	} {
		if code := h.pipe(gnuDF, args...); code != ExitUsage {
			t.Errorf("%v: code = %d", args, code)
		}
		if h.stdout.Len() != 0 {
			t.Errorf("%v: stdout = %q", args, h.stdout.String())
		}
	}
	// Two offsets for one abbreviation state two answers.
	if code := h.pipe(gnuDF, "--assume-zone", "JST=+0900", "--assume-zone", "JST=+0100"); code != ExitUsage {
		t.Errorf("conflicting zones: %d", code)
	}
}

// A duration is reported in seconds, and the definition says what the
// last part of a bare two-part reading is.
func TestDurationFields(t *testing.T) {
	h := newHarness(t)
	const ps = "  PID TTY          TIME CMD\n 1234 pts/0    00:04:14 bash\n 5678 pts/0    00:00:00 ps\n"
	if code := h.pipe(ps); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	rows := h.rows()
	if len(rows) != 2 || rows[0]["time"] != float64(254) || rows[1]["time"] != float64(0) {
		t.Errorf("rows = %v", rows)
	}
	const uptime = " 13:42:13 up 13 days,  4:22,  0 users,  load average: 0.59, 0.70, 0.84\n"
	if code := h.pipe(uptime); code != ExitOK {
		t.Fatalf("uptime: %d %s", code, h.stderr.String())
	}
	var obj map[string]any
	h.json(&obj)
	if obj["uptime"] != float64(1138920) {
		t.Errorf("uptime = %#v", obj["uptime"])
	}
	// --raw shows the text it was made from, which is the point of that
	// option for a value the reader has to check.
	if code := h.pipe(uptime, "--raw"); code != ExitOK {
		t.Fatal(code)
	}
	h.json(&obj)
	if obj["uptime"] != "13 days,  4:22" {
		t.Errorf("raw uptime = %#v", obj["uptime"])
	}
}

// jz run reads a command's output whole or as a stream, with a registered
// variant or a --define, and each of those answers a question the way the
// others do: the same status, the same document or none, and a refusal
// the options earn given before the command is run.
func TestRunAnswersTheSameWholeAndStreamed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	// The df variants left for a system decide whether blank output has
	// an empty form; on Linux one of them has none.
	h.env.GOOS = "linux"
	shellRegistry(t, h)
	writeRegistry(t, h.registryPath, map[string]string{
		"parsers/one/default/parser.yaml": "format: 1\ncommand: one\nvariant: default\n" +
			"detect: {signature: {all: ['=']}}\nparse: {type: kv, as: map}\n",
	})
	marker := filepath.Join(t.TempDir(), "ran")
	const (
		kv  = "parse: {type: kv}"
		csv = "parse: {type: csv}"
	)
	for _, tc := range []struct {
		name   string
		opts   []string
		script string
		code   int
		// whole and stream are what standard output holds; skip leaves a
		// mode out.
		whole, stream string
		ran           bool
		// says and quiet are text standard error must and must not hold.
		says, quiet string
	}{
		{name: "a key the variant cannot have", opts: []string{"--parser", "sh", "--variant", "default", "--extract", "nokey"},
			script: "echo a=1", code: ExitUsage},
		{name: "a key the definition cannot have", opts: []string{"--define", kv, "--extract", "nokey"},
			script: "echo a=1", code: ExitUsage},
		{name: "a key no variant left can have, and no output", opts: []string{"--parser", "sh", "--variant", "default", "--extract", "nokey"},
			script: "true", code: ExitUsage},
		{name: "a failed command that printed nothing, defined", opts: []string{"--define", kv},
			script: "exit 5", code: 5, ran: true},
		{name: "a failed command that printed blank lines", opts: []string{"--parser", "df"},
			script: "echo; exit 5", code: 5, ran: true, quiet: "printed nothing"},
		{name: "a command that printed blank lines", opts: []string{"--parser", "df"},
			script: "echo", code: ExitSelect, ran: true, says: "printed nothing"},
		{name: "a key the output lacks after a failed command, registered", opts: []string{"--parser", "csv", "--variant", "comma", "--extract", "nokey"},
			script: "printf 'a,b\\n1,2\\n'; exit 5", code: 5, ran: true, whole: "", stream: "skip"},
		{name: "a key the output lacks after a failed command, defined", opts: []string{"--define", csv, "--extract", "nokey"},
			script: "printf 'a,b\\n1,2\\n'; exit 5", code: 5, ran: true, whole: "", stream: "skip"},
		{name: "a format read as data", opts: []string{"--format", "jsonl"},
			script: `echo '{"a":1}'`, code: ExitOK, ran: true, whole: "[{\"a\":1}]\n", stream: "{\"a\":1}\n"},
		{name: "a csv read as data", opts: []string{"--format", "csv", "--type", "a=int"},
			script: "printf 'a\\n7\\n'", code: ExitOK, ran: true, whole: "[{\"a\":7}]\n", stream: "{\"a\":7}\n"},
		{name: "data from a failed command", opts: []string{"--format", "jsonl"},
			script: `echo '{"a":1}'; exit 5`, code: 5, ran: true, whole: "[{\"a\":1}]\n", stream: "{\"a\":1}\n"},
		{name: "no data from a failed command", opts: []string{"--format", "jsonl"},
			script: "exit 5", code: 5, ran: true},
		{name: "data that is not the format", opts: []string{"--format", "jsonl"},
			script: `echo '{"a":1}'; echo nope`, code: ExitParse, ran: true, whole: "", stream: "{\"a\":1}\n"},
		{name: "an unknown format", opts: []string{"--format", "nope"}, script: "true", code: ExitUsage},
		{name: "a format and a parser", opts: []string{"--format", "csv", "--parser", "csv"}, script: "true", code: ExitUsage},
		{name: "a type for a format with no columns", opts: []string{"--format", "jsonl", "--type", "a=int"}, script: "true", code: ExitUsage},
	} {
		for _, stream := range []bool{false, true} {
			want := tc.whole
			if stream {
				want = tc.stream
			}
			if want == "skip" {
				continue
			}
			_ = os.Remove(marker)
			args := append([]string{"run"}, tc.opts...)
			if stream {
				args = append(args, "--stream")
			}
			args = append(args, "--", "sh", "-c", "touch '"+marker+"'; "+tc.script)
			code := h.run(args...)
			_, err := os.Stat(marker)
			if code != tc.code || h.stdout.String() != want || (err == nil) != tc.ran ||
				!strings.Contains(h.stderr.String(), tc.says) ||
				(tc.quiet != "" && strings.Contains(h.stderr.String(), tc.quiet)) {
				t.Errorf("%s, stream %v: code=%d want %d, stdout=%q want %q, ran=%v want %v\n%s",
					tc.name, stream, code, tc.code, h.stdout.String(), want, err == nil, tc.ran, h.stderr.String())
			}
		}
	}
	// A format with no streaming form is known before the command runs.
	_ = os.Remove(marker)
	for _, opts := range [][]string{{"--parser", "one"}, {"--format", "json"}} {
		args := append(append([]string{"run", "--stream"}, opts...), "--", "sh", "-c", "touch '"+marker+"'; echo a=1")
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%v: code=%d %s", opts, code, h.stderr.String())
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("%v: the command ran", opts)
		}
	}
}

// A key the chosen definition cannot have is the caller's mistake whatever
// the input holds, so it is reported before the input is read, whole or
// as a stream.
func TestUnknownKeyIsRefusedBeforeTheInputIsRead(t *testing.T) {
	h := newHarness(t)
	for _, opts := range [][]string{
		{"--parser", "kv", "--variant", "equals"},
		{"--define", "parse: {type: kv}"},
	} {
		for _, stream := range []bool{false, true} {
			args := append(append([]string{}, opts...), "--extract", "nokey")
			if stream {
				args = append(args, "--stream")
			}
			if code := h.pipe("a=1\nnot a pair\n", args...); code != ExitUsage || h.stdout.Len() != 0 {
				t.Errorf("%v: code=%d stdout=%q %s", args, code, h.stdout.String(), h.stderr.String())
			}
		}
	}
}

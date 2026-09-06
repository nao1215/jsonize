package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/registry"
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
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, stdin: &bytes.Buffer{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	h.home = t.TempDir()
	h.env = Env{
		Stdin:           h.stdin,
		Stdout:          h.stdout,
		Stderr:          h.stderr,
		Getenv:          func(string) string { return "" },
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

const (
	gnuDF   = "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs            1000000    5000    995000   1% /run\n/dev/sda1       20000000 5000000  15000000  25% /\n"
	humanDF = "Filesystem      Size  Used Avail Use% Mounted on\ntmpfs           1.0G  5.0M  995M   1% /run\n"
	//nolint:dupword // real uname output repeats the architecture
	unameA = "Linux host1 6.8.0-45-generic #45-Ubuntu SMP PREEMPT_DYNAMIC Fri Aug 30 12:02:04 UTC 2024 x86_64 x86_64 x86_64 GNU/Linux\n"
)

// TestAutoDetectFromStdin is the headline behaviour: no subcommand, no
// parser name, just piped output.
func TestAutoDetectFromStdin(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if len(rows) != 2 || rows[1]["mounted_on"] != "/" || rows[1]["use_percent"] != float64(25) {
		t.Errorf("rows = %v", rows)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr should stay empty: %s", h.stderr.String())
	}

	// A different variant of the same command, and a different command,
	// are told apart from the text alone.
	if code := h.pipe(humanDF, "--meta"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["parser"] != "df" || env["variant"] != "gnu-human" || env["source"] != "embedded" {
		t.Errorf("envelope = %v", env)
	}
	if code := h.pipe(unameA, "--meta"); code != ExitOK {
		t.Fatalf("uname: %d %s", code, h.stderr.String())
	}
	h.json(&env)
	if env["parser"] != "uname" || env["variant"] != "linux" {
		t.Errorf("uname envelope = %v", env)
	}
	data := env["data"].(map[string]any)
	if data["kernel_name"] != "Linux" || data["machine"] != "x86_64" {
		t.Errorf("uname data = %v", data)
	}
}

func TestAutoDetectFlags(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--pretty"); code != ExitOK || !strings.Contains(h.stdout.String(), "\n  {") {
		t.Errorf("pretty: %d\n%s", code, h.stdout.String())
	}
	if code := h.pipe(gnuDF, "-r"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["use_percent"] != "1%" {
		t.Errorf("raw: %v", rows[0])
	}
	// --os narrows the candidates; a contradicting value rejects them.
	if code := h.pipe(gnuDF, "--os", "linux"); code != ExitOK {
		t.Errorf("--os linux: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--os", "darwin"); code != ExitSelect {
		t.Errorf("--os darwin: %d %s", code, h.stderr.String())
	}
	// --file reads the same text from disk.
	p := filepath.Join(h.home, "df.txt")
	if err := os.WriteFile(p, []byte(gnuDF), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--file", p); code != ExitOK {
		t.Errorf("--file: %d %s", code, h.stderr.String())
	}
	if code := h.run("--file", filepath.Join(h.home, "missing.txt")); code != ExitError {
		t.Errorf("missing file: %d", code)
	}
	if code := h.pipe(gnuDF, "--max-input", "10"); code != ExitParse || !strings.Contains(h.stderr.String(), "exceeds 10 bytes") {
		t.Errorf("max-input: %d %s", code, h.stderr.String())
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
			code := h.pipe(tt.input)
			if code != ExitSelect {
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

func TestExplicitParserAndVariant(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--parser", "df", "--meta"); code != ExitOK {
		t.Fatalf("--parser: %d %s", code, h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["variant"] != "gnu" {
		t.Errorf("variant = %v", env["variant"])
	}
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "gnu"); code != ExitOK {
		t.Errorf("--variant: %d %s", code, h.stderr.String())
	}
	// A variant that contradicts the input is refused, and --force
	// overrides that refusal at the user's own risk.
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "bsd"); code != ExitSelect {
		t.Errorf("mismatch: %d %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "df/bsd does not describe this input") || !strings.Contains(h.stderr.String(), "--force") {
		t.Errorf("mismatch message: %s", h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "bsd", "--force"); code != ExitParse {
		t.Errorf("--force should reach the parser and fail there: %d %s", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("failed parse must not print JSON: %q", h.stdout.String())
	}
	// Unknown names fail with a list of what exists.
	if code := h.pipe(gnuDF, "--parser", "nosuch"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser for \"nosuch\"") {
		t.Errorf("unknown parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--parser", "df", "--variant", "nosuch"); code != ExitSelect || !strings.Contains(h.stderr.String(), "available: bsd,") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	// --variant alone cannot identify a definition.
	if code := h.pipe(gnuDF, "--variant", "gnu"); code != ExitUsage || !strings.Contains(h.stderr.String(), "needs --parser") {
		t.Errorf("variant without parser: %d %s", code, h.stderr.String())
	}
}

func TestParseFailureKeepsStdoutEmpty(t *testing.T) {
	h := newHarness(t)
	broken := "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs  abc 0 0 0% /run\n"
	code := h.pipe(broken)
	if code != ExitParse {
		t.Fatalf("code = %d, want %d (%s)", code, ExitParse, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), `df/gnu: line 2: field "1k_blocks": cannot convert "abc" to int`) {
		t.Errorf("stderr = %s", h.stderr.String())
	}
}

func TestUsageAndVersion(t *testing.T) {
	h := newHarness(t)
	// No arguments on a terminal: help instead of blocking on stdin.
	h.terminal = true
	if code := h.run(); code != ExitOK || !strings.Contains(h.stdout.String(), "COMMAND | jz") ||
		!strings.Contains(h.stdout.String(), "Conversion flags") {
		t.Errorf("terminal help: %d\n%s", code, h.stdout.String())
	}
	h.terminal = false
	// The same invocation with piped input converts instead.
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Errorf("piped: %d %s", code, h.stderr.String())
	}
	// Empty piped input is a detection failure, never help.
	if code := h.pipe(""); code != ExitSelect {
		t.Errorf("empty pipe: %d", code)
	}
	if code := h.run("--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Commands:") ||
		!strings.Contains(h.stdout.String(), "Conversion flags") || !strings.Contains(h.stdout.String(), "-parser name") {
		t.Errorf("--help: %d\n%s", code, h.stdout.String())
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
	if code := h.run("version", "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz version") {
		t.Errorf("version help: %d", code)
	}
	// A mistyped subcommand is not silently treated as input.
	if code := h.run("frobnicate"); code != ExitUsage || !strings.Contains(h.stderr.String(), `unknown command "frobnicate"`) {
		t.Errorf("unknown command: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--pretty", "run"); code != ExitUsage || !strings.Contains(h.stderr.String(), "must come before") {
		t.Errorf("subcommand after flags: %d %s", code, h.stderr.String())
	}
	for _, sub := range []string{"run", "list"} {
		if code := h.run(sub, "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz "+sub) {
			t.Errorf("%s --help: %d %q", sub, code, h.stdout.String())
		}
		if code := h.run(sub, "--no-such-flag"); code != ExitUsage {
			t.Errorf("%s bad flag: %d", sub, code)
		}
	}
	if code := h.pipe(gnuDF, "--no-such-flag"); code != ExitUsage {
		t.Errorf("bad flag: %d", code)
	}
	// The removed subcommands are gone for good.
	for _, gone := range []string{"parse", "show", "validate", "registry"} {
		if code := h.pipe(gnuDF, gone); code != ExitUsage {
			t.Errorf("%s should no longer exist: %d", gone, code)
		}
	}
}

func TestList(t *testing.T) {
	h := newHarness(t)
	// Level 1: the commands.
	if code := h.run("list"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out := h.stdout.String()
	if !strings.Contains(out, "COMMAND") || !strings.Contains(out, "bsd, bsd-human, busybox-human, gnu, gnu-human") {
		t.Errorf("list:\n%s", out)
	}
	if !strings.Contains(out, "commands,") {
		t.Errorf("list summary:\n%s", out)
	}
	// Level 2: the variants of one command.
	if code := h.run("list", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out = h.stdout.String()
	if !strings.Contains(out, "VARIANT") || !strings.Contains(out, "gnu-human") || !strings.Contains(out, "embedded") {
		t.Errorf("list df:\n%s", out)
	}
	// Level 3: one definition in full, including what `show` used to print.
	if code := h.run("list", "w", "linux"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	out = h.stdout.String()
	for _, want := range []string{"w/linux", "source:", "format:       1", "detection:", "signature", "composite parts=uptime:regex,users:table", "part uptime:", "load_1m", "float", "part users:", "null_if"} {
		if !strings.Contains(out, want) {
			t.Errorf("list w linux missing %q:\n%s", want, out)
		}
	}
	// JSON at every level.
	if code := h.run("list", "--json"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var commands []map[string]any
	h.json(&commands)
	if len(commands) < 10 || commands[0]["command"] != "df" || len(commands[0]["variants"].([]any)) != 5 {
		t.Errorf("list --json: %v", commands[0])
	}
	if code := h.run("list", "--json", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var variants []map[string]any
	h.json(&variants)
	if len(variants) != 5 || variants[0]["variant"] != "bsd" {
		t.Errorf("list df --json: %v", variants)
	}
	if code := h.run("list", "--json", "id", "posix"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var detail map[string]any
	h.json(&detail)
	if detail["variant"] != "posix" || detail["format"] != float64(1) || !strings.Contains(detail["parse"].(string), "regex each=input") {
		t.Errorf("detail = %v", detail)
	}
	det := detail["detect"].(map[string]any)
	if det["auto_detectable"] != true || len(det["signature_all"].([]any)) == 0 {
		t.Errorf("detect = %v", det)
	}
	fields := detail["fields"].(map[string]any)
	if !strings.Contains(fields["groups"].(string), "array of object") || !strings.Contains(fields["context"].(string), "omit-when-missing") {
		t.Errorf("fields = %v", fields)
	}
	// Errors.
	if code := h.run("list", "nosuch"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser for") {
		t.Errorf("list nosuch: %d %s", code, h.stderr.String())
	}
	if code := h.run("list", "df", "nosuch"); code != ExitSelect || !strings.Contains(h.stderr.String(), "available:") {
		t.Errorf("list df nosuch: %d %s", code, h.stderr.String())
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
	if !strings.Contains(out, "embedded") || !strings.Contains(out, "(built into jz)") || !strings.Contains(out, "user") {
		t.Errorf("sources:\n%s", out)
	}
	if !strings.Contains(out, EnvRegistryPath) {
		t.Errorf("sources hint:\n%s", out)
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

func TestUserRegistryLayering(t *testing.T) {
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
	// A user parser is detected automatically, like an official one.
	if code := h.pipe("hello world\n", "--meta"); code != ExitOK {
		t.Fatalf("user parser: %d %s", code, h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["parser"] != "hello" || env["source"] != "user" {
		t.Errorf("envelope = %v", env)
	}
	// The override shadows the embedded definition.
	if code := h.pipe(gnuDF, "--meta"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&env)
	if env["source"] != "user" || env["data"].([]any)[0].(map[string]any)["blocks"] != "1000000" {
		t.Errorf("override = %v", env)
	}
	if code := h.run("list", "df", "gnu"); code != ExitOK || !strings.Contains(h.stdout.String(), "shadows:      the same definition in embedded") {
		t.Errorf("shadow note: %d\n%s", code, h.stdout.String())
	}
	if code := h.run("list", "--sources"); !strings.Contains(h.stdout.String(), "user") || !strings.Contains(h.stdout.String(), "present") {
		t.Errorf("sources: %d\n%s", code, h.stdout.String())
	}
	// --embedded-only ignores it again.
	if code := h.pipe(gnuDF, "--embedded-only", "--meta"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&env)
	if env["source"] != "embedded" || env["data"].([]any)[0].(map[string]any)["1k_blocks"] != float64(1000000) {
		t.Errorf("embedded-only = %v", env)
	}
	if code := h.pipe("hello world\n", "--embedded-only"); code != ExitSelect {
		t.Errorf("user parser must be gone: %d", code)
	}

	// An extra directory via --registry and via the environment variable.
	extra := filepath.Join(h.home, "extra")
	writeRegistry(t, extra, map[string]string{
		"parsers/bye/default/parser.yaml": strings.ReplaceAll(helloDef, "hello", "bye"),
	})
	if code := h.pipe("bye now\n", "--registry", extra); code != ExitOK {
		t.Errorf("--registry: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("bye now\n", "--registry", filepath.Join(h.home, "nope")); code != ExitRegistry {
		t.Errorf("--registry missing: %d", code)
	}
	h.env.Getenv = func(k string) string {
		if k == EnvRegistryPath {
			return extra + string(os.PathListSeparator) + string(os.PathListSeparator) + filepath.Join(h.home, "absent")
		}
		return ""
	}
	if code := h.pipe("bye now\n"); code != ExitOK {
		t.Errorf("%s: %d %s", EnvRegistryPath, code, h.stderr.String())
	}
	if code := h.run("list", "--sources"); code != ExitOK || !strings.Contains(h.stdout.String(), extra) {
		t.Errorf("sources with env: %d\n%s", code, h.stdout.String())
	}

	// A broken definition is reported but does not take the rest down.
	writeRegistry(t, user, map[string]string{"parsers/broken/x/parser.yaml": "format: 1\n"})
	if code := h.pipe("hello you\n"); code != ExitOK || !strings.Contains(h.stderr.String(), "warning: skipping definition") {
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

func TestRunExecMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	reg := filepath.Join(h.home, "reg")
	writeRegistry(t, reg, map[string]string{
		"parsers/sh/default/parser.yaml": "format: 1\ncommand: sh\nvariant: default\n" +
			"detect: {signature: {all: ['^[A-Za-z_][A-Za-z0-9_]*=']}}\nexec: {env: {JZ_DEF: fromdef}}\n" +
			"parse: {type: kv}\nfields: {n: {type: int}}\n",
		"parsers/sh/alt/parser.yaml": "format: 1\ncommand: sh\nvariant: alt\n" +
			"detect: {signature: {all: ['^ALT']}}\nexec: {env: {JZ_DEF: other}}\n" +
			"parse: {type: regex, pattern: '^(?P<line>.*)$'}\n",
	})
	if code := h.run("run"); code != ExitUsage || !strings.Contains(h.stderr.String(), "no command given") {
		t.Errorf("no command: %d", code)
	}
	if code := h.run("run", "--registry", reg, "--env", "BAD", "sh"); code != ExitUsage {
		t.Errorf("bad env: %d", code)
	}
	// Nothing runs when jz cannot parse the result anyway.
	if code := h.run("run", "--registry", reg, "nosuchcmd"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser for") {
		t.Errorf("unknown parser: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--variant", "zzz", "sh", "-c", "echo x=1"); code != ExitSelect || !strings.Contains(h.stderr.String(), "has no variant") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	// Success: the locale is forced, --env is applied and the conflicting
	// exec.env of the two variants is dropped.
	if code := h.run("run", "--registry", reg, "--env", "JZ_X=1", "sh", "-c", "echo n=$JZ_X; echo lc=$LC_ALL; echo def=${JZ_DEF:-unset}"); code != ExitOK {
		t.Fatalf("run: %d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if len(rows) != 3 || rows[0]["value"] != float64(1) || rows[1]["value"] != "C" || rows[2]["value"] != "unset" {
		t.Errorf("rows = %v", rows)
	}
	if code := h.run("run", "--registry", reg, "--keep-locale", "sh", "-c", "echo lc=${LC_ALL:-inherited}"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"inherited"`) && !strings.Contains(h.stdout.String(), `"value":"`+os.Getenv("LC_ALL")+`"`) {
		t.Errorf("keep-locale: %s", h.stdout.String())
	}
	// The envelope records what was executed.
	if code := h.run("run", "--registry", reg, "--meta", "sh", "-c", "echo a=b"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["exit_status"] != float64(0) || env["variant"] != "default" || len(env["argv"].([]any)) != 3 {
		t.Errorf("meta = %v", env)
	}
	// The output still has to match the variant's signature.
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo no separator here"); code != ExitSelect {
		t.Errorf("signature mismatch in run: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--variant", "default", "sh", "-c", "echo no separator here"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "does not describe this input") {
		t.Errorf("explicit variant mismatch: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--variant", "default", "--force", "sh", "-c", "echo no separator here"); code != ExitParse {
		t.Errorf("--force reaches the parser: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--variant", "alt", "sh", "-c", "echo ALT x"); code != ExitOK ||
		!strings.Contains(h.stdout.String(), `"line":"ALT x"`) {
		t.Errorf("explicit variant: %d %s", code, h.stdout.String())
	}
	// Exit status, stderr passthrough and signals.
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo a=b; echo oops >&2; exit 7"); code != 7 {
		t.Errorf("exit passthrough: %d %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"b"`) || !strings.Contains(h.stderr.String(), "oops") ||
		!strings.Contains(h.stderr.String(), "exited with status 7") {
		t.Errorf("stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "sh", "-c", "exit 3"); code != 3 || h.stdout.Len() != 0 {
		t.Errorf("failure without output: %d %q", code, h.stdout.String())
	}
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo garbage; exit 9"); code != 9 {
		t.Errorf("a failing command keeps its status: %d", code)
	}
	if code := h.run("run", "--registry", reg, "sh", "-c", "kill -TERM $$"); code != 143 || !strings.Contains(h.stderr.String(), "SIGTERM") {
		t.Errorf("signal: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--timeout", "200ms", "sh", "-c", "exec sleep 5"); code != 143 || !strings.Contains(h.stderr.String(), "timeout") {
		t.Errorf("timeout: %d %s", code, h.stderr.String())
	}
	if code := h.run("run", "--registry", reg, "--max-output", "100", "sh", "-c", "yes a=b | head -c 10000"); code != ExitParse ||
		!strings.Contains(h.stderr.String(), "size limit") {
		t.Errorf("max-output: %d %s", code, h.stderr.String())
	}
	writeRegistry(t, reg, map[string]string{
		"parsers/definitely-missing-binary/x/parser.yaml": "format: 1\ncommand: definitely-missing-binary\nvariant: x\nparse: {type: kv}\n",
	})
	if code := h.run("run", "--registry", reg, "definitely-missing-binary"); code != ExitError ||
		!strings.Contains(h.stderr.String(), "cannot run") {
		t.Errorf("missing binary: %d %s", code, h.stderr.String())
	}
}

// TestRunArgumentBoundary pins where jz's own flags stop and the
// command's begin.
func TestRunArgumentBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	reg := filepath.Join(h.home, "reg")
	// The parser echoes the arguments it was given, so the test can see
	// exactly what reached the command.
	writeRegistry(t, reg, map[string]string{
		"parsers/sh/default/parser.yaml": "format: 1\ncommand: sh\nvariant: default\n" +
			"detect: {signature: {all: ['^args=']}}\nparse: {type: kv}\n",
	})
	script := `echo "args=$*"`
	tests := []struct {
		name     string
		args     []string
		wantArgs string
		pretty   bool
	}{
		{"command flags are not jz flags", []string{"run", "--registry", reg, "sh", "-c", script, "sh", "-h"}, "-h", false},
		{"a jz flag before the command is jz's", []string{"run", "--registry", reg, "--pretty", "sh", "-c", script, "sh", "-h"}, "-h", true},
		{"the same flag after the command is the command's", []string{"run", "--registry", reg, "sh", "-c", script, "sh", "--pretty"}, "--pretty", false},
		{"--parser is not a run flag and passes through", []string{"run", "--registry", reg, "sh", "-c", script, "sh", "--parser"}, "--parser", false},
		{"-- stops jz flag parsing", []string{"run", "--registry", reg, "--", "sh", "-c", script, "sh", "--pretty", "--variant"}, "--pretty --variant", false},
		{"-- after the command belongs to the command", []string{"run", "--registry", reg, "sh", "-c", script, "sh", "--", "--literal"}, "-- --literal", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := h.run(tt.args...); code != ExitOK {
				t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
			}
			var rows []map[string]any
			h.json(&rows)
			if rows[0]["value"] != tt.wantArgs {
				t.Errorf("args = %q, want %q", rows[0]["value"], tt.wantArgs)
			}
			if pretty := strings.Contains(h.stdout.String(), "\n  "); pretty != tt.pretty {
				t.Errorf("pretty = %v, want %v: %s", pretty, tt.pretty, h.stdout.String())
			}
		})
	}
}

// TestRunRealCommands checks the detection path against the host's own
// binaries; the fixtures cover the formats jz cannot run here.
func TestRunRealCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX commands")
	}
	h := newHarness(t)
	for _, args := range [][]string{{"uname", "-a"}, {"id"}, {"env"}, {"df"}} {
		if code := h.run(append([]string{"run", "--meta"}, args...)...); code != ExitOK {
			t.Errorf("run %v: %d %s", args, code, h.stderr.String())
			continue
		}
		var env map[string]any
		h.json(&env)
		if env["parser"] != args[0] {
			t.Errorf("run %v selected %v", args, env["parser"])
		}
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
	if got := strings.Join(mergedExecEnv(reg, "c"), ","); got != "TZ=UTC" {
		t.Errorf("mergedExecEnv = %q", got)
	}
}

// TestNoFabricatedUnits pins that a rounded, human-readable size reaches
// JSON exactly as the command printed it. `df -h` counts 1024 per suffix
// step and `df -H` counts 1000, the output does not say which, and both
// are rounded, so any byte count jz produced would be invented.
func TestNoFabricatedUnits(t *testing.T) {
	h := newHarness(t)
	si := "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1       1.1G  100M  1.0G  10% /\n"
	if code := h.pipe(si); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["size"] != "1.1G" || rows[0]["used"] != "100M" {
		t.Errorf("sizes must stay as printed: %v", rows[0])
	}
	if rows[0]["use_percent"] != float64(10) {
		t.Errorf("a percentage is exact and stays typed: %v", rows[0])
	}
	// The exact form of the same command still yields integers.
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["1k_blocks"] != float64(1000000) {
		t.Errorf("1K blocks are exact: %v", rows[0])
	}
	// free and lsblk follow the same rule.
	if code := h.pipe("               total        used        free      shared  buff/cache   available\nMem:            61Gi        34Gi       8.3Gi        17Gi        36Gi        26Gi\n"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["total"] != "61Gi" {
		t.Errorf("free -h: %v", rows[0])
	}
	if code := h.pipe("NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nsda           8:0    0    20G  0 disk \n"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["size"] != "20G" || rows[0]["mountpoint"] != nil {
		t.Errorf("lsblk: %v", rows[0])
	}
}

// TestGenericFormatsNeedAnExplicitParser pins that a format jz cannot
// recognise on its own is refused, with a message naming the parser to
// pass, instead of being claimed because it was the only candidate left.
func TestGenericFormatsNeedAnExplicitParser(t *testing.T) {
	h := newHarness(t)
	numstat := "10\t2\tmain.go\n" // git diff --numstat, not du
	code := h.pipe(numstat)
	if code != ExitSelect {
		t.Fatalf("code = %d, want %d (%s)", code, ExitSelect, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	for _, want := range []string{"could be `du` output", "too generic", "jz --parser du"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, h.stderr.String())
		}
	}
	// Naming the parser is an assertion about the input, and the
	// signature still checks the shape.
	if code := h.pipe("134164\t/usr/bin\n", "--parser", "du"); code != ExitOK {
		t.Fatalf("--parser du: %d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["size"] != float64(134164) || rows[0]["name"] != "/usr/bin" {
		t.Errorf("du rows = %v", rows)
	}
	if code := h.pipe("not du output at all\n", "--parser", "du"); code != ExitSelect {
		t.Errorf("the signature still applies: %d %s", code, h.stderr.String())
	}
	// wc is generic in the same way; three numbers alone say nothing.
	if code := h.pipe("      1       1       4\n"); code != ExitSelect ||
		!strings.Contains(h.stderr.String(), "jz --parser wc") {
		t.Errorf("wc without a parser: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("      1       1       4\n", "--parser", "wc"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	// The third column of the default output counts bytes, not characters.
	if _, ok := rows[0]["characters"]; ok {
		t.Errorf("wc must not report characters: %v", rows[0])
	}
	if rows[0]["bytes"] != float64(4) || rows[0]["lines"] != float64(1) {
		t.Errorf("wc rows = %v", rows)
	}
}

func TestEnvValuesAreKeptVerbatim(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe("A=  padded  \nB=x\nC=\n"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["value"] != "  padded  " || rows[2]["value"] != "" {
		t.Errorf("env values = %v", rows)
	}
	// The NUL form of the same command is the lossless one: a value may
	// contain a newline, which the line form cannot express.
	if code := h.pipe("A=one\nstill A\x00B=two\x00", "--meta"); code != ExitOK {
		t.Fatalf("env -0: %d %s", code, h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["variant"] != "null-separated" {
		t.Errorf("variant = %v", env["variant"])
	}
	data := env["data"].([]any)
	if data[0].(map[string]any)["value"] != "one\nstill A" || len(data) != 2 {
		t.Errorf("NUL data = %v", data)
	}
}

func TestLsListingsAreReadOnce(t *testing.T) {
	h := newHarness(t)
	// A regular file may be called "a -> b"; only a symbolic link has a
	// target.
	listing := "total 8\n" +
		"-rw-r--r-- 1 alice staff   42 Jan  5 10:11 a -> b\n" +
		"lrwxrwxrwx 1 alice staff    7 Jan  5 10:12 bin -> usr/bin\n"
	if code := h.pipe(listing); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["filename"] != "a -> b" {
		t.Errorf("regular file: %v", rows[0])
	}
	if _, ok := rows[0]["link_to"]; ok {
		t.Errorf("a regular file has no target: %v", rows[0])
	}
	if rows[1]["filename"] != "bin" || rows[1]["link_to"] != "usr/bin" {
		t.Errorf("symlink: %v", rows[1])
	}
	// One parser reads both -l and -lh, so neither the sizes in the
	// listing nor their order decide whether it can be read at all.
	small := "-rw-r--r-- 1 u g 3 Jan  5 10:11 small.txt\n"
	mixed := small + strings.Repeat(small, 24) + "-rw-r--r-- 1 u g 2.9K Jan  5 10:11 big.bin\n"
	for name, in := range map[string]string{"small only": small, "suffix past the window": mixed} {
		if code := h.pipe(in, "--meta"); code != ExitOK {
			t.Fatalf("%s: %d %s", name, code, h.stderr.String())
		}
		var env map[string]any
		h.json(&env)
		if env["variant"] != "long" {
			t.Errorf("%s: variant = %v", name, env["variant"])
		}
	}
	h.json(&map[string]any{})
	if code := h.pipe(mixed); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["size"] != "3" || rows[len(rows)-1]["size"] != "2.9K" {
		t.Errorf("sizes stay as printed: %v ... %v", rows[0], rows[len(rows)-1])
	}
}

func TestTruncatedAlignedRowIsAnError(t *testing.T) {
	h := newHarness(t)
	code := h.pipe("NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nsda\n")
	if code != ExitParse {
		t.Fatalf("code = %d, want %d (%s)", code, ExitParse, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), `field "maj_min": required value is missing`) {
		t.Errorf("stderr = %s", h.stderr.String())
	}
	// A column that is legitimately empty is still allowed.
	if code := h.pipe("NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nsda           8:0    0    20G  0 disk \n"); code != ExitOK {
		t.Fatalf("unmounted device: %d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["mountpoint"] != nil {
		t.Errorf("mountpoint = %v", rows[0]["mountpoint"])
	}
}

// TestEmptyInput pins the two ways an empty input can end: unidentifiable
// when nothing was named, and a signature mismatch when a parser was.
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
	// Only --force reaches the parser, which then reports an empty table.
	if code := h.pipe("", "--parser", "df", "--variant", "gnu", "--force"); code != ExitOK ||
		strings.TrimSpace(h.stdout.String()) != "[]" {
		t.Errorf("--force: %d %q", code, h.stdout.String())
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
	var rows []map[string]any
	h.json(&rows)
	if len(rows) != 1 || rows[0]["lines"] != float64(1) || rows[0]["words"] != float64(2) || rows[0]["bytes"] != float64(12) {
		t.Errorf("wc counted %v, want 1 line, 2 words, 12 bytes", rows)
	}
	if _, ok := rows[0]["filename"]; ok {
		t.Errorf("wc reading stdin prints no filename: %v", rows[0])
	}
}

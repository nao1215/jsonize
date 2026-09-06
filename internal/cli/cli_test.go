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
	t      *testing.T
	stdin  *bytes.Buffer
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	env    Env
	home   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, stdin: &bytes.Buffer{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	h.home = t.TempDir()
	h.env = Env{
		Stdin:         h.stdin,
		Stdout:        h.stdout,
		Stderr:        h.stderr,
		Getenv:        func(string) string { return "" },
		UserConfigDir: func() (string, error) { return filepath.Join(h.home, "config"), nil },
		UserCacheDir:  func() (string, error) { return filepath.Join(h.home, "cache"), nil },
		GOOS:          runtime.GOOS,
		Context:       context.Background(),
		Embedded:      registry.Source{Name: SourceEmbedded, FS: official.FS()},
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return Main(args, h.env)
}

func (h *harness) json(v any) {
	h.t.Helper()
	if err := json.Unmarshal(h.stdout.Bytes(), v); err != nil {
		h.t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout.String())
	}
}

const gnuDF = "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs            1000000    5000    995000   1% /run\n/dev/sda1       20000000 5000000  15000000  25% /\n"

func TestUsageAndVersion(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != ExitUsage || !strings.Contains(h.stderr.String(), "Usage:") {
		t.Errorf("no args: code=%d stderr=%s", code, h.stderr.String())
	}
	if code := h.run("--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Subcommands:") {
		t.Errorf("--help: code=%d", code)
	}
	if code := h.run("help", "parse"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz parse") {
		t.Errorf("help parse: code=%d out=%s", code, h.stdout.String())
	}
	if code := h.run("help", "bogus"); code != ExitOK || !strings.Contains(h.stdout.String(), "Subcommands:") {
		t.Errorf("help bogus: code=%d", code)
	}
	if code := h.run("bogus"); code != ExitUsage || !strings.Contains(h.stderr.String(), "unknown command") {
		t.Errorf("bogus: code=%d", code)
	}
	if code := h.run("--version"); code != ExitOK || !strings.HasPrefix(h.stdout.String(), "jz ") {
		t.Errorf("version: %d %s", code, h.stdout.String())
	}
	if code := h.run("version", "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz version") {
		t.Errorf("version help: %d", code)
	}
	for _, sub := range []string{"run", "parse", "list", "show", "validate", "registry"} {
		if code := h.run(sub, "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage:") {
			t.Errorf("%s --help: code=%d out=%q err=%q", sub, code, h.stdout.String(), h.stderr.String())
		}
		if code := h.run(sub, "--no-such-flag"); code != ExitUsage {
			t.Errorf("%s bad flag: code=%d", sub, code)
		}
	}
}

func TestParsePipe(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "df"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if len(rows) != 2 || rows[1]["mounted_on"] != "/" || rows[1]["use_percent"] != float64(25) {
		t.Errorf("rows = %v", rows)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr should be empty: %s", h.stderr.String())
	}

	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "--pretty", "--meta", "--os", "linux", "df"); code != ExitOK {
		t.Fatalf("meta: code=%d %s", code, h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["variant"] != "gnu" || env["source"] != "embedded" || !strings.Contains(h.stdout.String(), "\n  ") {
		t.Errorf("envelope = %v", env)
	}

	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "-r", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["use_percent"] != "1%" {
		t.Errorf("raw = %v", rows[0])
	}

	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "--variant", "bsd", "df"); code != ExitParse {
		t.Errorf("explicit wrong variant should fail to parse: code=%d %s", code, h.stderr.String())
	}
	h.stdin.WriteString("garbage\n")
	if code := h.run("parse", "df"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no variant of \"df\" matches") {
		t.Errorf("no match: code=%d %s", code, h.stderr.String())
	}
	if code := h.run("parse", "df", "extra"); code != ExitUsage {
		t.Errorf("extra arg: %d", code)
	}
	if code := h.run("parse", "nosuchcommand"); code != ExitSelect || !strings.Contains(h.stderr.String(), "no parser definition") {
		t.Errorf("unknown command: %d %s", code, h.stderr.String())
	}
	if code := h.run("parse", "--variant", "zzz", "df"); code != ExitSelect || !strings.Contains(h.stderr.String(), "has no variant") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "--max-input", "10", "df"); code != ExitParse || !strings.Contains(h.stderr.String(), "exceeds 10 bytes") {
		t.Errorf("max input: %d %s", code, h.stderr.String())
	}
	if code := h.run("parse", "--file", filepath.Join(h.home, "missing.txt"), "df"); code != ExitError {
		t.Errorf("missing file: %d", code)
	}
	p := filepath.Join(h.home, "df.txt")
	if err := os.WriteFile(p, []byte(gnuDF), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run("parse", "--file", p, "df"); code != ExitOK {
		t.Errorf("file: %d %s", code, h.stderr.String())
	}
	// stdin with empty input: an empty list.
	h.stdin.Reset()
	if code := h.run("parse", "env"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != "[]" {
		t.Errorf("empty env: %d %q", code, h.stdout.String())
	}
}

func TestListAndShow(t *testing.T) {
	h := newHarness(t)
	if code := h.run("list"); code != ExitOK || !strings.Contains(h.stdout.String(), "df       gnu") {
		t.Errorf("list: %d\n%s", code, h.stdout.String())
	}
	if code := h.run("list", "--json"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var list []map[string]any
	h.json(&list)
	if len(list) < 20 || list[0]["command"] != "df" {
		t.Errorf("list json = %d entries", len(list))
	}
	if code := h.run("show", "df"); code != ExitOK || !strings.Contains(h.stdout.String(), "df/gnu-human") {
		t.Errorf("show df: %d %s", code, h.stdout.String())
	}
	if code := h.run("show", "w", "linux"); code != ExitOK || !strings.Contains(h.stdout.String(), "composite parts=uptime:regex,users:table") {
		t.Errorf("show w linux: %d %s", code, h.stdout.String())
	}
	if code := h.run("show", "--json", "id", "posix"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var shown []map[string]any
	h.json(&shown)
	if len(shown) != 1 || shown[0]["variant"] != "posix" || !strings.Contains(shown[0]["parse"].(string), "regex each=input") {
		t.Errorf("show json = %v", shown)
	}
	fields := shown[0]["fields"].(map[string]any)
	if !strings.Contains(fields["groups"].(string), "array of object") || !strings.Contains(fields["context"].(string), "omit-when-missing") {
		t.Errorf("fields = %v", fields)
	}
	if code := h.run("show", "nosuch"); code != ExitSelect {
		t.Errorf("show unknown: %d", code)
	}
	if code := h.run("show", "df", "nosuch"); code != ExitSelect {
		t.Errorf("show unknown variant: %d", code)
	}
	if code := h.run("show"); code != ExitUsage {
		t.Errorf("show no args: %d", code)
	}
}

func writeRegistry(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const helloDef = `format: 1
command: hello
variant: default
description: greeting lines
parse:
  type: regex
  pattern: '^hello (?P<name>\S+)$'
`

func TestUserRegistryLayeringAndValidate(t *testing.T) {
	h := newHarness(t)
	user := filepath.Join(h.home, "config", "jsonize", "registry")
	writeRegistry(t, user, map[string]string{
		"registry.yaml":                        "format: 1\nname: mine\n",
		"parsers/hello/default/parser.yaml":    helloDef,
		"parsers/hello/default/testdata/a.txt": "hello world\n",
		// Override the official df/gnu with a raw-only version.
		"parsers/df/gnu/parser.yaml":    "format: 1\ncommand: df\nvariant: gnu\ndetect: {signature: {all: ['^Filesystem']}}\nparse: {type: table, header: {columns: [filesystem, 1k_blocks, used, available, use_percent, mounted_on]}}\n",
		"parsers/df/gnu/testdata/a.txt": gnuDF,
	})
	// validate without golden: fails and names the missing file
	if code := h.run("validate", user); code != ExitRegistry || !strings.Contains(h.stdout.String(), "has no a.json") {
		t.Errorf("validate missing golden: %d\n%s%s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.run("validate", "--update", user); code != ExitOK || !strings.Contains(h.stdout.String(), "WROTE") {
		t.Errorf("validate --update: %d\n%s%s", code, h.stdout.String(), h.stderr.String())
	}
	golden, err := os.ReadFile(filepath.Join(user, "parsers/hello/default/testdata/a.json"))
	if err != nil || !strings.Contains(string(golden), `"name": "world"`) {
		t.Errorf("golden = %s %v", golden, err)
	}
	if code := h.run("validate", user); code != ExitOK || !strings.Contains(h.stdout.String(), "ok   hello/default [a]") {
		t.Errorf("validate: %d\n%s%s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.run("validate", "--quiet", user); code != ExitOK || strings.Contains(h.stdout.String(), "ok ") {
		t.Errorf("validate quiet: %d %s", code, h.stdout.String())
	}
	// default directories: user registry exists
	if code := h.run("validate"); code != ExitOK {
		t.Errorf("validate default dirs: %d %s", code, h.stderr.String())
	}
	// user definition shadows embedded
	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if rows[0]["1k_blocks"] != "1000000" {
		t.Errorf("user override should keep strings: %v", rows[0])
	}
	if code := h.run("list"); !strings.Contains(h.stdout.String(), "df       gnu            any                            user") || !strings.Contains(h.stdout.String(), "hello    default") {
		t.Errorf("list with user registry: %d\n%s", code, h.stdout.String())
	}
	if code := h.run("show", "df", "gnu"); !strings.Contains(h.stdout.String(), "shadows:     embedded") {
		t.Errorf("show should mention shadowing: %d\n%s", code, h.stdout.String())
	}
	// --embedded-only ignores the user registry
	h.stdin.WriteString(gnuDF)
	if code := h.run("parse", "--embedded-only", "df"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	h.json(&rows)
	if rows[0]["1k_blocks"] != float64(1000000) {
		t.Errorf("embedded-only should convert: %v", rows[0])
	}
	if code := h.run("parse", "--embedded-only", "hello"); code != ExitSelect {
		t.Errorf("hello must not exist with --embedded-only: %d", code)
	}
	// --registry flag and JSONIZE_REGISTRY_PATH
	extra := filepath.Join(h.home, "extra")
	writeRegistry(t, extra, map[string]string{"parsers/bye/default/parser.yaml": strings.ReplaceAll(strings.ReplaceAll(helloDef, "hello", "bye"), "greeting", "farewell")})
	h.stdin.WriteString("bye now\n")
	if code := h.run("parse", "--registry", extra, "bye"); code != ExitOK {
		t.Errorf("--registry: %d %s", code, h.stderr.String())
	}
	if code := h.run("parse", "--registry", filepath.Join(h.home, "nope"), "bye"); code != ExitRegistry {
		t.Errorf("--registry missing dir: %d", code)
	}
	h.env.Getenv = func(k string) string {
		if k == EnvRegistryPath {
			return extra + string(os.PathListSeparator) + string(os.PathListSeparator) + filepath.Join(h.home, "absent")
		}
		return ""
	}
	h.stdin.WriteString("bye now\n")
	if code := h.run("parse", "bye"); code != ExitOK {
		t.Errorf("env path: %d %s", code, h.stderr.String())
	}
	if code := h.run("registry", "paths"); code != ExitOK || !strings.Contains(h.stdout.String(), extra) || !strings.Contains(h.stdout.String(), "user            present") {
		t.Errorf("registry paths: %d\n%s", code, h.stdout.String())
	}
	// broken definition is reported but does not abort
	writeRegistry(t, user, map[string]string{"parsers/broken/x/parser.yaml": "format: 1\n"})
	h.stdin.WriteString("hello you\n")
	if code := h.run("parse", "hello"); code != ExitOK || !strings.Contains(h.stderr.String(), "warning: skipping definition") {
		t.Errorf("broken def: %d %s", code, h.stderr.String())
	}
	if code := h.run("validate", user); code != ExitRegistry || !strings.Contains(h.stdout.String(), "FAIL") {
		t.Errorf("validate broken: %d\n%s", code, h.stdout.String())
	}
	if code := h.run("validate", h.home); code != ExitRegistry || !strings.Contains(h.stderr.String(), "not a registry directory") {
		t.Errorf("validate non-registry: %d %s", code, h.stderr.String())
	}
}

func TestValidateNothing(t *testing.T) {
	h := newHarness(t)
	if code := h.run("validate"); code != ExitUsage {
		t.Errorf("validate with nothing: %d %s", code, h.stderr.String())
	}
}

func TestRegistryCommandUsage(t *testing.T) {
	h := newHarness(t)
	if code := h.run("registry"); code != ExitUsage || !strings.Contains(h.stderr.String(), "Usage: jz registry") {
		t.Errorf("registry: %d", code)
	}
	if code := h.run("registry", "bogus"); code != ExitUsage {
		t.Errorf("registry bogus: %d", code)
	}
	if code := h.run("registry", "update", "extra"); code != ExitUsage {
		t.Errorf("update positional: %d", code)
	}
	if code := h.run("registry", "update", "--source", "http://example.invalid/r.tar.gz"); code != ExitUsage || !strings.Contains(h.stderr.String(), "https") {
		t.Errorf("insecure source: %d %s", code, h.stderr.String())
	}
	if code := h.run("registry", "update", "--source", "https://127.0.0.1:1/r.tar.gz"); code != ExitRegistry {
		t.Errorf("unreachable: %d %s", code, h.stderr.String())
	}
}

func TestCommandKey(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"df": "df", "/usr/bin/df": "df", `C:\tools\df.EXE`: "df", "tool.cmd": "tool", "x.bat": "x"} {
		if runtime.GOOS != "windows" && strings.Contains(in, `\`) {
			continue
		}
		if got := commandKey(in); got != want {
			t.Errorf("commandKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := commandKey("df.exe"); got != "df" {
		t.Errorf("df.exe -> %q", got)
	}
}

func TestRunExecMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	reg := filepath.Join(h.home, "reg")
	writeRegistry(t, reg, map[string]string{
		"parsers/sh/default/parser.yaml": "format: 1\ncommand: sh\nvariant: default\nexec: {env: {JZ_DEF: fromdef}}\nparse: {type: kv}\nfields: {n: {type: int}}\n",
		"parsers/sh/alt/parser.yaml":     "format: 1\ncommand: sh\nvariant: alt\ndetect: {signature: {all: ['^ALT']}}\nexec: {env: {JZ_DEF: other}}\nparse: {type: regex, pattern: '^(?P<line>.*)$'}\n",
	})
	if code := h.run("run"); code != ExitUsage {
		t.Errorf("run without command: %d", code)
	}
	if code := h.run("run", "--registry", reg, "--env", "BAD", "sh"); code != ExitUsage {
		t.Errorf("bad env: %d", code)
	}
	if code := h.run("run", "--registry", reg, "nosuchcmd"); code != ExitSelect {
		t.Errorf("unknown command must not execute: %d", code)
	}
	if code := h.run("run", "--registry", reg, "--variant", "zzz", "sh", "-c", "echo x=1"); code != ExitSelect {
		t.Errorf("unknown variant must not execute: %d", code)
	}
	// success, env from --env; def env dropped because variants conflict
	if code := h.run("run", "--registry", reg, "--env", "JZ_X=1", "--", "sh", "-c", "echo n=$JZ_X; echo lc=$LC_ALL; echo def=${JZ_DEF:-unset}"); code != ExitOK {
		t.Fatalf("run: %d %s", code, h.stderr.String())
	}
	var rows []map[string]any
	h.json(&rows)
	if len(rows) != 3 || rows[0]["value"] != float64(1) || rows[1]["value"] != "C" || rows[2]["value"] != "unset" {
		t.Errorf("rows = %v", rows)
	}
	// keep-locale: LC_ALL inherited (unset in tests) -> value empty
	if code := h.run("run", "--registry", reg, "--keep-locale", "sh", "-c", "echo lc=${LC_ALL:-inherited}"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"inherited"`) && !strings.Contains(h.stdout.String(), `"value":"`+os.Getenv("LC_ALL")+`"`) {
		t.Errorf("keep-locale: %s", h.stdout.String())
	}
	// meta envelope carries argv and exit status
	if code := h.run("run", "--registry", reg, "--meta", "sh", "-c", "echo a=b"); code != ExitOK {
		t.Fatal(h.stderr.String())
	}
	var env map[string]any
	h.json(&env)
	if env["exit_status"] != float64(0) || env["variant"] != "default" || len(env["argv"].([]any)) != 3 {
		t.Errorf("meta = %v", env)
	}
	// non-zero exit with parseable output: JSON emitted, status mirrored
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo a=b; echo oops >&2; exit 7"); code != 7 {
		t.Errorf("exit passthrough: %d %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), `"value":"b"`) || !strings.Contains(h.stderr.String(), "oops") || !strings.Contains(h.stderr.String(), "exited with status 7") {
		t.Errorf("stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
	}
	// non-zero exit without output: no JSON
	if code := h.run("run", "--registry", reg, "sh", "-c", "exit 3"); code != 3 || h.stdout.Len() != 0 {
		t.Errorf("exit without output: %d %q", code, h.stdout.String())
	}
	// unparseable output from a successful command -> parse error
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo no-separator"); code != ExitParse {
		t.Errorf("parse failure: %d %s", code, h.stderr.String())
	}
	// unparseable output from a failing command -> child's status wins
	if code := h.run("run", "--registry", reg, "sh", "-c", "echo no-separator; exit 9"); code != 9 {
		t.Errorf("parse failure with failing child: %d", code)
	}
	// ambiguous/no-match after a failing command -> child's status wins
	if code := h.run("run", "--registry", reg, "--variant", "alt", "sh", "-c", "echo ALT x"); code != ExitOK || !strings.Contains(h.stdout.String(), `"line":"ALT x"`) {
		t.Errorf("explicit variant: %d %s", code, h.stdout.String())
	}
	// signal termination
	if code := h.run("run", "--registry", reg, "sh", "-c", "kill -TERM $$"); code != 143 || !strings.Contains(h.stderr.String(), "SIGTERM") {
		t.Errorf("signal: %d %s", code, h.stderr.String())
	}
	// timeout
	if code := h.run("run", "--registry", reg, "--timeout", "200ms", "sh", "-c", "exec sleep 5"); code != 143 || !strings.Contains(h.stderr.String(), "timeout") {
		t.Errorf("timeout: %d %s", code, h.stderr.String())
	}
	// output limit
	if code := h.run("run", "--registry", reg, "--max-output", "100", "sh", "-c", "yes a=b | head -c 10000"); code != ExitParse || !strings.Contains(h.stderr.String(), "size limit") {
		t.Errorf("max output: %d %s", code, h.stderr.String())
	}
	// raw + pretty
	if code := h.run("run", "--registry", reg, "-r", "-p", "sh", "-c", "echo n=5"); code != ExitOK || !strings.Contains(h.stdout.String(), `"value": "5"`) {
		t.Errorf("raw pretty: %d %s", code, h.stdout.String())
	}
	// missing binary
	if code := h.run("run", "--registry", reg, "--variant", "default", "sh"); code == ExitOK {
		t.Log("sh with no args reads stdin; skip")
	}
	writeRegistry(t, reg, map[string]string{"parsers/definitely-missing-binary/x/parser.yaml": "format: 1\ncommand: definitely-missing-binary\nvariant: x\nparse: {type: kv}\n"})
	if code := h.run("run", "--registry", reg, "definitely-missing-binary"); code != ExitError || !strings.Contains(h.stderr.String(), "cannot run") {
		t.Errorf("missing binary: %d %s", code, h.stderr.String())
	}
}

func TestRunRealCommandsOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only sanity check against real commands")
	}
	h := newHarness(t)
	for _, args := range [][]string{{"uname", "-a"}, {"id"}, {"env"}} {
		if code := h.run(append([]string{"run"}, args...)...); code != ExitOK {
			t.Errorf("run %v: %d %s", args, code, h.stderr.String())
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

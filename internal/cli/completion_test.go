package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// completion runs `jz __complete` and splits what it wrote into the kind
// of answer and the candidates.
func (h *harness) completion(words ...string) (string, []string) {
	h.t.Helper()
	if code := h.run(append([]string{completeCommand}, words...)...); code != ExitOK {
		h.t.Fatalf("__complete %q: code=%d stderr=%s", words, code, h.stderr.String())
	}
	if h.stderr.Len() != 0 {
		h.t.Errorf("__complete %q wrote to stderr: %s", words, h.stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(h.stdout.String(), "\n"), "\n")
	return lines[0], lines[1:]
}

func TestComplete(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		words []string
		kind  string
		// want are candidates that must be offered, and absent ones that
		// must not.
		want, absent []string
	}{
		{[]string{""}, completeWords, []string{"run", "list", "test", "completion", "version", "help"}, []string{completeCommand}},
		{[]string{"l"}, completeWords, []string{"list"}, []string{"run"}},
		{[]string{"-"}, completeWords, []string{"--parser", "--variant", "--file", "-f", "--format", "--yaml", "--stream", "--columns", "--explain", "--explain=json", "--help", "--version"}, []string{"--env", "--timeout"}},
		{[]string{"--parser", "d"}, completeWords, []string{"df", "dig", "du"}, []string{"ls", "gdf", "--parser=df"}},
		{[]string{"--parser=d"}, completeWords, []string{"--parser=df", "--parser=dig"}, []string{"df"}},
		{[]string{"--parser", "=", "cu"}, completeWords, []string{"curl"}, []string{"--parser=curl"}},
		{[]string{"--parser", "ping", "--variant", ""}, completeWords, []string{"bsd", "linux"}, []string{"gnu"}},
		{[]string{"--parser=csv", "--variant="}, completeWords, []string{"--variant=comma-no-header", "--variant=tab-no-header"}, nil},
		{[]string{"--variant", ""}, completeWords, nil, []string{"gnu"}},
		{[]string{"-f", ""}, completeFiles, nil, nil},
		{[]string{"--file=a"}, completeFiles, nil, nil},
		{[]string{"--explain="}, completeWords, []string{"--explain=json"}, nil},
		{[]string{"--format", "j"}, completeWords, []string{"jsonl", "json"}, []string{"csv", "yaml"}},
		{[]string{"new", "-"}, completeWords, []string{"--array", "--pretty", "--yaml"}, []string{"--file", "--stream"}},
		{[]string{"new", "a=1", ""}, completeNone, nil, nil},
		{[]string{"--format="}, completeWords, []string{"--format=csv", "--format=yaml"}, nil},
		{[]string{"--define", ""}, completeNone, nil, nil},
		{[]string{"--yaml", "--pa"}, completeWords, []string{"--parser"}, nil},
		{[]string{"run", ""}, completeWords, []string{"df", "ls", "ping", "ping6", "curl"}, []string{"run"}},
		{[]string{"run", "-"}, completeWords, []string{"--env", "--timeout", "--keep-locale", "--yaml"}, nil},
		{[]string{"run", "--parser", "csv", "--variant", "t"}, completeWords, []string{"tab", "tab-no-header"}, []string{"comma"}},
		{[]string{"run", "--timeout", ""}, completeNone, nil, nil},
		{[]string{"run", "df", ""}, completeFiles, nil, nil},
		// What follows the command is the command's, options included.
		{[]string{"run", "df", "-"}, completeFiles, nil, nil},
		{[]string{"run", "--", "--odd-name", ""}, completeFiles, nil, nil},
		{[]string{"list", ""}, completeWords, []string{"df", "ping"}, nil},
		{[]string{"list", "stat", ""}, completeWords, []string{"bsd", "bsd-verbose", "gnu"}, nil},
		{[]string{"list", "stat", "bsd", ""}, completeNone, nil, nil},
		{[]string{"list", "--j"}, completeWords, []string{"--json"}, nil},
		{[]string{"list", "--", "j"}, completeWords, []string{"journalctl"}, []string{"--json"}},
		{[]string{"list", "df", "--"}, completeWords, nil, nil},
		{[]string{"test", ""}, completeDirs, nil, nil},
		{[]string{"test", "--decoys", ""}, completeDirs, nil, nil},
		{[]string{"completion", ""}, completeWords, []string{"bash", "zsh"}, nil},
		{[]string{"completion", "bash", ""}, completeNone, nil, nil},
		{[]string{"help", "r"}, completeWords, []string{"run"}, nil},
		{[]string{"version", ""}, completeNone, nil, nil},
		{nil, completeWords, []string{"run"}, nil},
	}
	for _, tt := range tests {
		kind, got := h.completion(tt.words...)
		if kind != tt.kind {
			t.Errorf("%q: kind %s, want %s", tt.words, kind, tt.kind)
		}
		for _, w := range tt.want {
			if !slices.Contains(got, w) {
				t.Errorf("%q: %q not offered in %q", tt.words, w, got)
			}
		}
		for _, w := range tt.absent {
			if slices.Contains(got, w) {
				t.Errorf("%q: %q offered", tt.words, w)
			}
		}
		if kind != completeWords && len(got) != 0 {
			t.Errorf("%q: %s with candidates %q", tt.words, kind, got)
		}
		sorted := slices.Sorted(slices.Values(got))
		if len(slices.Compact(sorted)) != len(got) {
			t.Errorf("%q: a candidate is offered twice: %q", tt.words, got)
		}
	}
}

// The parsers of every registry jz reads are offered, and a definition
// that does not load is left out without a word on standard error, where
// it would land in the middle of the line being typed.
func TestCompleteReadsEveryRegistry(t *testing.T) {
	h := newHarness(t)
	extra := t.TempDir()
	writeDef(t, extra, "mytool", "plain", "format: 1\ncommand: mytool\nvariant: plain\naliases: [{name: mytool2}]\ndetect: {signature: {all: ['\\Amytool ']}}\nparse: {type: regex, pattern: '^mytool (?P<v>.+)$'}\n")
	writeDef(t, extra, "mytool", "broken", "format: 1\ncommand: mytool\nvariant: broken\nparse: {type: nope}\n")
	user := filepath.Join(h.home, "config", "jsonize", "registry")
	writeDef(t, user, "df", "mine", "format: 1\ncommand: df\nvariant: mine\ndetect: {auto_detect: false}\nparse: {type: csv}\n")
	h.registryPath = extra
	_, got := h.completion("--parser", "myt")
	if !slices.Equal(got, []string{"mytool", "mytool2"}) {
		t.Errorf("parsers: %q", got)
	}
	_, got = h.completion("--parser", "mytool", "--variant", "")
	if !slices.Equal(got, []string{"plain"}) {
		t.Errorf("variants: %q", got)
	}
	_, got = h.completion("list", "df", "m")
	if !slices.Equal(got, []string{"mine"}) {
		t.Errorf("a user registry's variant: %q", got)
	}
	if kind, got := h.completion("run", "mytool2", ""); kind != completeFiles || len(got) != 0 {
		t.Errorf("after the command: %s %q", kind, got)
	}
}

func writeDef(t *testing.T, root, cmd, variant, body string) {
	t.Helper()
	dir := filepath.Join(root, "parsers", cmd, variant)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parser.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionScripts(t *testing.T) {
	h := newHarness(t)
	for shell, want := range map[string]string{"bash": "complete -F _jz jz", "zsh": "compdef _jz jz"} {
		if code := h.run("completion", shell); code != ExitOK || !strings.Contains(h.stdout.String(), want) || !strings.Contains(h.stdout.String(), completeCommand) {
			t.Errorf("%s: code=%d\n%s", shell, code, h.stdout.String())
		}
	}
	// Built from a checkout with CRLF endings, the scripts still reach the
	// shell with LF.
	if strings.Contains(bashCompletion+zshCompletion, "\r") {
		for _, shell := range []string{"bash", "zsh"} {
			h.run("completion", shell)
			if strings.Contains(h.stdout.String(), "\r") {
				t.Errorf("%s: the script carries a carriage return", shell)
			}
		}
	}
	for _, args := range [][]string{{"completion"}, {"completion", "fish"}, {"completion", "bash", "zsh"}} {
		if code := h.run(args...); code != ExitUsage || h.stdout.Len() != 0 {
			t.Errorf("%q: code=%d stdout=%q", args, code, h.stdout.String())
		}
	}
	if code := h.run("completion", "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "source <(jz completion zsh)") {
		t.Errorf("help: %d %s", code, h.stdout.String())
	}
	// The entry the scripts call is not offered to people.
	if code := h.run("--help"); code != ExitOK || strings.Contains(h.stdout.String(), completeCommand) || !strings.Contains(h.stdout.String(), "completion") {
		t.Errorf("--help: %s", h.stdout.String())
	}
}

package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	h := newHarness(t)
	version := h.writeFile("VERSION", []byte("1.4.0\n"))
	deploy := h.writeFile("deploy.yaml", []byte("replicas: 3\n"))
	for _, tt := range []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"an object", "", []string{"new", "name=api", "replicas:=3", "debug:=false", "tags[]=web", "tags[]=prod"}, `{"name":"api","replicas":3,"debug":false,"tags":["web","prod"]}` + "\n"},
		{"no arguments", "", []string{"new"}, "{}\n"},
		{"an array", "", []string{"new", "--array", "a", ":=1", ":=null"}, `["a",1,null]` + "\n"},
		{"files", "", []string{"new", "version=@" + version, "spec:=@" + deploy}, `{"version":"1.4.0","spec":{"replicas":3}}` + "\n"},
		{"standard input", `{"a":[1]}`, []string{"new", "pod:=@-"}, `{"pod":{"a":[1]}}` + "\n"},
		{"pretty", "", []string{"new", "-p", "a:=[1]"}, "{\n  \"a\": [\n    1\n  ]\n}\n"},
		{"yaml", "", []string{"new", "--yaml", "a=1", "b:=1"}, "a: \"1\"\nb: 1\n"},
		{"after --", "", []string{"new", "--", "-x=1"}, `{"-x":"1"}` + "\n"},
	} {
		if code := h.pipe(tt.stdin, tt.args...); code != ExitOK || h.stdout.String() != tt.want {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, want %q", tt.name, code, h.stdout.String(), h.stderr.String(), tt.want)
		}
	}
}

func TestNewFailures(t *testing.T) {
	h := newHarness(t)
	bad := h.writeFile("bad.yaml", []byte("a: .nan\n"))
	latin := h.writeFile("latin.txt", []byte("caf\xe9"))
	broken := h.writeFile("broken.json.gz", []byte("nope"))
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no operator", []string{"new", "name"}, ExitUsage, `jz: new: "name": an argument is KEY=VALUE`},
		{"a key given twice", []string{"new", "a=1", "a=2"}, ExitUsage, `the key "a" is given twice`},
		{"not JSON", []string{"new", "a:=yes"}, ExitUsage, "the value after := is not JSON"},
		{"an option after the arguments", []string{"new", "a=1", "-p"}, ExitUsage, "options come before the arguments"},
		{"pretty and yaml", []string{"new", "-p", "--yaml"}, ExitUsage, "--pretty and --yaml cannot be used together"},
		{"an unknown option", []string{"new", "--stream"}, ExitUsage, "flag provided but not defined"},
		{"a data file that is not its format", []string{"new", "a:=@" + bad}, ExitParse, "yaml: line 1: .nan is not a number JSON can hold"},
		{"text that is not UTF-8", []string{"new", "a=@" + latin}, ExitParse, "not valid UTF-8"},
		{"damaged compression", []string{"new", "a:=@" + broken}, ExitParse, "cannot be decompressed"},
	} {
		code := h.run(tt.args...)
		if code != tt.code || !strings.Contains(h.stderr.String(), tt.want) {
			t.Errorf("%s: exit %d, stderr %q, want %d %q", tt.name, code, h.stderr.String(), tt.code, tt.want)
		}
		if h.stdout.Len() > 0 {
			t.Errorf("%s: wrote %q", tt.name, h.stdout.String())
		}
	}
	if code := h.run("new", "--help"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz new") || !strings.Contains(h.stdout.String(), "--array") {
		t.Errorf("--help: %d %s", code, h.stdout.String())
	}
	if code := h.run("help", "new"); code != ExitOK || !strings.Contains(h.stdout.String(), "Usage: jz new") {
		t.Errorf("help new: %d %s", code, h.stdout.String())
	}
}

// A missing file's message is the system's, which differs by platform;
// what is fixed is that it names the argument.
func TestNewNamesTheArgument(t *testing.T) {
	h := newHarness(t)
	arg := "a=@" + filepath.Join(h.home, "nope")
	if code := h.run("new", arg); code != ExitError || !strings.Contains(h.stderr.String(), "jz: new: "+arg+": ") {
		t.Errorf("%d %s", code, h.stderr.String())
	}
}

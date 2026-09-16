package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/jsonbuild"
	"github.com/nao1215/jsonize/pkg/engine"
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
		{"after --", "", []string{"new", "--", "-x=1"}, `{"-x":"1"}` + "\n"},
		{"literal strings", "", []string{"new", "--string", "m=@VERSION", "--string=flag=--pretty", "--string", "e="}, `{"m":"@VERSION","flag":"--pretty","e":""}` + "\n"},
		{"a text file keeps its line endings", "one\r\n\n", []string{"new", "--text-file", "body=-", "--text-file", "v=" + version, "less=@" + version}, `{"body":"one\r\n\n","v":"1.4.0\n","less":"1.4.0"}` + "\n"},
		{"pointers", "", []string{"new", "--path", "/metadata/name=api", "--path", "/spec/replicas:=3", "--string", "/metadata/labels/app=@web", "kind=Deployment"}, `{"metadata":{"name":"api","labels":{"app":"@web"}},"spec":{"replicas":3},"kind":"Deployment"}` + "\n"},
		{"an array of objects", "", []string{"new", "--array", "--path", "/-/name=a", "--path", "/0/port:=80", "--string", "=@b"}, `[{"name":"a","port":80},"@b"]` + "\n"},
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
		{"yaml output", []string{"new", "-p", "--yaml"}, ExitUsage, "--yaml was removed"},
		{"an unknown option", []string{"new", "--stream"}, ExitUsage, "flag provided but not defined"},
		{"--string split in two", []string{"new", "--string", "message", "hello"}, ExitUsage, `jz: new: "--string message": --string takes KEY=TEXT or POINTER=TEXT as one argument`},
		{"a location given twice", []string{"new", "--path", "/a/b=1", "--path", "/a/b=2"}, ExitUsage, `/a/b is given twice, first by "--path /a/b=1"`},
		{"a pointer into a value", []string{"new", "--path", "/spec/x=1", "spec:=3"}, ExitUsage, `"spec:=3": /spec is given twice, first by "--path /spec/x=1"`},
		{"an index past the end", []string{"new", "--path", "/a/1=x"}, ExitUsage, "/a does not exist yet, so 1 cannot be an index in it"},
		{"a text file that is not UTF-8", []string{"new", "--text-file", "a=" + latin}, ExitParse, "not valid UTF-8"},
		{"a missing text file", []string{"new", "--text-file", "a=" + latin + ".missing"}, ExitError, "--text-file a="},
		{"an option after a plain argument", []string{"new", "a=1", "--string", "b=2"}, ExitUsage, "options come before the arguments"},
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

// --each makes one document per line of standard input: the line read as
// JSON for KEY:=@-, as a string for KEY=@-. The files the arguments name
// are read once, and a line that cannot be read ends the documents
// there with the status 3, keeping the ones already written.
func TestNewEach(t *testing.T) {
	h := newHarness(t)
	version := h.writeFile("VERSION", []byte("1.4.0\n"))
	for _, tt := range []struct {
		name  string
		stdin string
		args  []string
		code  int
		want  string
		err   string
	}{
		{"JSON records", "{\"used\":42}\n\n[1,2]\n7\n", []string{"new", "--each", "host=a", "sample:=@-", "v=@" + version}, ExitOK,
			`{"host":"a","sample":{"used":42},"v":"1.4.0"}` + "\n" + `{"host":"a","sample":[1,2],"v":"1.4.0"}` + "\n" + `{"host":"a","sample":7,"v":"1.4.0"}` + "\n", ""},
		{"line records keep empty lines", "a\r\n\n b ", []string{"new", "--each", "--path", "/log/line=@-", "--string", "tag=@x"}, ExitOK,
			`{"log":{"line":"a"},"tag":"@x"}` + "\n" + `{"log":{"line":""},"tag":"@x"}` + "\n" + `{"log":{"line":" b "},"tag":"@x"}` + "\n", ""},
		{"an array per record", "1\n2\n", []string{"new", "--each", "--array", ":=@-", "x"}, ExitOK, "[1,\"x\"]\n[2,\"x\"]\n", ""},
		{"no records", "", []string{"new", "--each", "a:=@-"}, ExitOK, "", ""},
		{"a later record that is not JSON", "1\nnope\n2\n", []string{"new", "--each", "n:=@-"}, ExitParse, "{\"n\":1}\n", "jz: new: n:=@-: jsonl: line 2: the line is not one JSON value"},
		{"a line that is not UTF-8", "a\n\xff\nb\n", []string{"new", "--each", "s=@-"}, ExitParse, "{\"s\":\"a\"}\n", "lines: line 2: the text is not valid UTF-8"},
	} {
		code := h.pipe(tt.stdin, tt.args...)
		if code != tt.code || h.stdout.String() != tt.want || !strings.Contains(h.stderr.String(), tt.err) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.name, code, h.stdout.String(), h.stderr.String())
		}
		if tt.code != ExitOK && strings.Count(h.stderr.String(), "\n") != 1 {
			t.Errorf("%s: one line on stderr, got %q", tt.name, h.stderr.String())
		}
	}
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no argument reads standard input", []string{"new", "--each", "a=1"}, ExitUsage, "no argument reads it"},
		{"--text-file reads it whole", []string{"new", "--each", "--text-file", "b=-"}, ExitUsage, `"--text-file b=-" reads standard input whole`},
		{"--pretty", []string{"new", "--each", "-p", "a:=@-"}, ExitUsage, "--pretty and --each cannot be used together"},
		{"a missing file before any record", []string{"new", "--each", "a:=@-", "v=@" + version + ".missing"}, ExitError, "VERSION.missing"},
	} {
		h.env.Stdin = untouchedInput{t}
		code := h.run(tt.args...)
		if code != tt.code || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.name, code, h.stdout.String(), h.stderr.String())
		}
	}
}

// Input past a limit is a parse failure for jz new the way it is for the
// default mode: a file past the byte limit, and a document past the value
// limit, are exit 3 with nothing written.
func TestNewLimitsAreParseFailures(t *testing.T) {
	h := newHarness(t)
	big := h.writeFile("big.json", []byte(`"0123456789"`))
	if _, err := readBounded(big, 12); err != nil {
		t.Errorf("a file at the byte limit: %v", err)
	}
	_, err := readBounded(big, 11)
	a := &app{env: h.env}
	if err == nil || !strings.Contains(err.Error(), "exceeds the 11 byte limit") {
		t.Fatalf("a file past the byte limit: %v", err)
	}
	if code := a.newFailed(&jsonbuild.InputError{Arg: "a:=@" + big, Err: err}); code != ExitParse {
		t.Errorf("a file past the byte limit: exit %d", code)
	}
	plan, err := jsonbuild.Parse([]jsonbuild.Arg{{Form: jsonbuild.Plain, Text: "a:=@" + big}, {Form: jsonbuild.Plain, Text: "b:=[1,2]"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan.Build(jsonbuild.Sources{ReadFile: readLimited, MaxValues: 4})
	if code := a.newFailed(err); code != ExitParse || !strings.Contains(h.stderr.String(), "more than 4 values") || h.stdout.Len() != 0 {
		t.Errorf("a document past the value limit: exit %d, %v, stderr %q", code, err, h.stderr.String())
	}
	// With --each a document past the limit is a record that cannot be
	// made into one, which ends the documents there.
	plan, err = jsonbuild.Parse([]jsonbuild.Arg{{Form: jsonbuild.Plain, Text: "a:=[1,2]"}, {Form: jsonbuild.Plain, Text: "r:=@-"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := plan.Fixed(jsonbuild.Sources{MaxValues: 5})
	if err != nil {
		t.Fatal(err)
	}
	h.stdout.Reset()
	h.stderr.Reset()
	if err := a.eachDocument(fixed, []any{int64(1)}); !errors.Is(err, engine.ErrTooManyValues) || h.stdout.Len() != 0 {
		t.Errorf("a document past the limit ends the documents: %v, stdout %q", err, h.stdout.String())
	}
	if err := a.eachDocument(fixed, int64(1)); err != nil || h.stdout.String() != `{"a":[1,2],"r":1}`+"\n" {
		t.Errorf("a document at the limit: %v, stdout %q", err, h.stdout.String())
	}
}

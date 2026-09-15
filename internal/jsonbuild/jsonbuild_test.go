package jsonbuild

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// files is a set of files by path, read the way Sources.ReadFile reads.
func files(t *testing.T, stdin string) Sources {
	t.Helper()
	m := map[string][]byte{
		"VERSION":        []byte("1.2.3\n"),
		"crlf.txt":       []byte("line\r\n"),
		"two.txt":        []byte("a\n\n"),
		"deploy.yaml":    []byte("replicas: 3\nname: api\n"),
		"users.csv":      []byte("id,name\n1,alice\n"),
		"doc.json":       []byte(`{"b":1,"a":2}`),
		"doc.json.gz":    gz(t, `[1,2]`),
		"notes.unknown":  []byte(`{"k":true}`),
		"bad.yaml":       []byte("a: .inf\n"),
		"latin1.txt":     []byte("caf\xe9\n"),
		"broken.json.gz": []byte("not gzip"),
		"big.json.gz":    gz(t, `"`+strings.Repeat("x", 200)+`"`),
	}
	return Sources{
		ReadFile: func(path string) ([]byte, error) {
			data, ok := m[path]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return data, nil
		},
		Stdin:   strings.NewReader(stdin),
		MaxSize: 100,
	}
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonutil.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestObject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"nothing", nil, "", `{}`},
		{"strings stay strings", []string{"a=007", "b=true", "c=", "d=x=y", "e=:=z"}, "", `{"a":"007","b":"true","c":"","d":"x=y","e":":=z"}`},
		{"json values", []string{"n:=3", "f:=2.50", "t:=true", "z:=null", "s:=\"@here\"", "o:={\"k\":[1]}", "big:=12345678901234567890"}, "",
			`{"n":3,"f":2.50,"t":true,"z":null,"s":"@here","o":{"k":[1]},"big":12345678901234567890}`},
		{"arrays keep their order", []string{"tags[]=web", "n=1", "tags[]:=2", "tags[]=@VERSION"}, "", `{"tags":["web",2,"1.2.3"],"n":"1"}`},
		{"a file's text loses one line ending", []string{"v=@VERSION", "c=@crlf.txt", "two=@two.txt"}, "", `{"v":"1.2.3","c":"line","two":"a\n"}`},
		{"a data file by its extension", []string{"spec:=@deploy.yaml", "users:=@users.csv", "doc:=@doc.json"}, "",
			`{"spec":{"replicas":3,"name":"api"},"users":[{"id":"1","name":"alice"}],"doc":{"b":1,"a":2}}`},
		{"a compressed data file", []string{"d:=@doc.json.gz"}, "", `{"d":[1,2]}`},
		{"an unknown extension is JSON", []string{"n:=@notes.unknown"}, "", `{"n":{"k":true}}`},
		{"standard input as JSON", []string{"pod:=@-"}, `{"a":1}`, `{"pod":{"a":1}}`},
		{"standard input as text", []string{"body=@-"}, "hello\n", `{"body":"hello"}`},
		{"keys with odd characters", []string{"app.kubernetes.io/name=x", "a b=c", "κ=λ"}, "", `{"app.kubernetes.io/name":"x","a b":"c","κ":"λ"}`},
		{"a colon before the equals sign is :=", []string{"a::=1"}, "", `{"a:":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obj, err := Object(tt.args, files(t, tt.stdin))
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, obj); got != tt.want {
				t.Errorf("got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestArray(t *testing.T) {
	t.Parallel()
	arr, err := Array([]string{"a", "=b", "k=v", ":=1", ":=[true]", "=@VERSION", ":=@deploy.yaml", "", "@plain"}, files(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := `["a","b","k=v",1,[true],"1.2.3",{"replicas":3,"name":"api"},"","@plain"]`
	if got := encode(t, arr); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	empty, err := Array(nil, files(t, ""))
	if err != nil || encode(t, empty) != "[]" {
		t.Errorf("no values: %v %v", empty, err)
	}
	stdin, err := Array([]string{":=@-"}, files(t, "[1]"))
	if err != nil || encode(t, stdin) != "[[1]]" {
		t.Errorf("standard input: %v %v", stdin, err)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		array bool
		args  []string
		want  string
	}{
		{"no operator", false, []string{"name"}, "an argument is KEY=VALUE"},
		{"an option after the arguments", false, []string{"a=1", "--pretty"}, "options come before the arguments"},
		{"an empty key", false, []string{"=x"}, "the key is empty"},
		{"an empty key before :=", false, []string{":=1"}, "the key is empty"},
		{"an empty array key", false, []string{"[]=x"}, "the key is empty"},
		{"a key given twice", false, []string{"a=1", "a:=2"}, `the key "a" is given twice`},
		{"a key as a value and an array", false, []string{"a[]=1", "a=2"}, "both as a value and as an array"},
		{"an array key then a value", false, []string{"a=1", "a[]=2"}, "both as a value and as an array"},
		{"json that is not json", false, []string{"a:=nope"}, "the value after := is not JSON"},
		{"two json values", false, []string{"a:=1 2"}, "the value after := is not JSON"},
		{"json with a key given twice", false, []string{`a:={"k":1,"k":2}`}, `the value after := cannot be written: the key "k" is given twice in one object`},
		{"json nested too deeply", true, []string{":=" + strings.Repeat("[", 1001) + strings.Repeat("]", 1001)}, "the value after := cannot be written: arrays and objects nest deeper than 1000"},
		{"@ with no path", false, []string{"a=@"}, "@ names no file"},
		{"standard input twice", false, []string{"a=@-", "b:=@-"}, `standard input is already read by "a=@-"`},
		{"an array element that is not json", true, []string{":=nope"}, "not JSON"},
		{"an array element with @ and no path", true, []string{"=@"}, "@ names no file"},
		{"standard input twice in an array", true, []string{"=@-", ":=@-"}, "already read"},
		{"an argument that is not UTF-8", false, []string{"a=\x80"}, "not valid UTF-8"},
		{"an element that is not UTF-8", true, []string{"\xff"}, "not valid UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := files(t, "")
			var err error
			if tt.array {
				_, err = Array(tt.args, src)
			} else {
				_, err = Object(tt.args, src)
			}
			var ue *UsageError
			if !errors.As(err, &ue) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want a usage error holding %q", err, tt.want)
			}
		})
	}
}

// A long argument is cut short where an error quotes it.
func TestUsageErrorQuotesALongArgumentShort(t *testing.T) {
	t.Parallel()
	arg := "a:=" + strings.Repeat("[", 500) + "é"
	_, err := Object([]string{arg}, files(t, ""))
	if err == nil {
		t.Fatal("accepted")
	}
	msg := err.Error()
	if len(msg) > 200 || !strings.Contains(msg, `"a:=[[[`) || !strings.Contains(msg, `..."`) || !utf8.ValidString(msg) {
		t.Errorf("message: %s", msg)
	}
}

// A usage error is found before any file or standard input is read.
func TestUsageErrorsComeFirst(t *testing.T) {
	t.Parallel()
	read := false
	src := Sources{ReadFile: func(string) ([]byte, error) { read = true; return []byte("x"), nil }, Stdin: iotest.ErrReader(errors.New("read")), MaxSize: 10}
	if _, err := Object([]string{"a=@file", "b=@-", "a=2"}, src); err == nil || read {
		t.Errorf("err %v, read %v", err, read)
	}
}

func TestInputErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		args  []string
		stdin string
		check func(error) bool
		want  string
	}{
		{"a missing file", []string{"a=@missing"}, "", func(err error) bool { return errors.Is(err, fs.ErrNotExist) }, "a=@missing: file does not exist"},
		{"text that is not UTF-8", []string{"a=@latin1.txt"}, "", func(err error) bool { return errors.Is(err, ErrNotUTF8) }, "not valid UTF-8"},
		{"a data file that is not its format", []string{"a:=@bad.yaml"}, "", func(err error) bool {
			var pe *engine.ParseError
			return errors.As(err, &pe) && pe.Line == 1
		}, "a:=@bad.yaml: yaml: line 1: .inf"},
		{"damaged compression", []string{"a:=@broken.json.gz"}, "", func(err error) bool {
			var ce *datafile.CompressionError
			return errors.As(err, &ce)
		}, "cannot be decompressed"},
		{"a compressed file past the limit", []string{"a:=@big.json.gz"}, "", func(err error) bool { return true }, "decompressed text exceeds the 100 byte limit"},
		{"standard input past the limit", []string{"a=@-"}, strings.Repeat("x", 101), func(err error) bool { return true }, "standard input exceeds the 100 byte limit"},
		{"standard input that is not json", []string{"a:=@-"}, "{", func(err error) bool {
			var pe *engine.ParseError
			return errors.As(err, &pe)
		}, "json: line 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Object(tt.args, files(t, tt.stdin))
			var ie *InputError
			if !errors.As(err, &ie) || !tt.check(err) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an input error holding %q", err, tt.want)
			}
		})
	}
	failing := Sources{Stdin: iotest.ErrReader(errors.New("broken pipe")), MaxSize: 10}
	if _, err := Object([]string{"a=@-"}, failing); err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("a failing standard input: %v", err)
	}
	if _, err := Object([]string{"a=@-"}, Sources{MaxSize: 10}); err == nil || !strings.Contains(err.Error(), "no standard input") {
		t.Errorf("no standard input: %v", err)
	}
}

// Whatever the arguments, the builder either says why or makes a value
// that is JSON: it encodes, and reads back as the same document.
func FuzzObject(f *testing.F) {
	for _, s := range []string{"a=1", "a:=1", "a[]=x", "b:={\"c\":[1]}", "=x", "a", "a=@-", "k:=\"s\""} {
		f.Add(s, "b=2", false)
		f.Add(s, s, true)
	}
	f.Fuzz(func(t *testing.T, one, two string, array bool) {
		src := Sources{ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist }, Stdin: strings.NewReader(`{"in":1}`), MaxSize: 1 << 16}
		var (
			v   any
			err error
		)
		if array {
			v, err = Array([]string{one, two}, src)
		} else {
			v, err = Object([]string{one, two}, src)
		}
		if err != nil {
			var (
				ue *UsageError
				ie *InputError
			)
			if !errors.As(err, &ue) && !errors.As(err, &ie) {
				t.Fatalf("an error of neither kind: %v", err)
			}
			return
		}
		out := encode(t, v)
		again, err := datafile.Read(datafile.JSON, []byte(out))
		if err != nil {
			t.Fatalf("the JSON made does not read back: %v\n%s", err, out)
		}
		if encode(t, again) != out {
			t.Fatalf("a second reading differs:\n%s\n%s", out, encode(t, again))
		}
	})
}

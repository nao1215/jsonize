package jsonbuild

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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
		"empty.txt":      {},
		"bare.txt":       []byte("no line ending"),
		"bom.txt":        []byte("\xEF\xBB\xBFx\n"),
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

func plain(args []string) []Arg {
	out := make([]Arg, len(args))
	for i, a := range args {
		out[i] = Arg{Form: Plain, Text: a}
	}
	return out
}

func build(args []Arg, array bool, src Sources) (any, error) {
	p, err := Parse(args, array)
	if err != nil {
		return nil, err
	}
	return p.Build(src)
}

func buildObject(args []string, src Sources) (any, error) { return build(plain(args), false, src) }

func buildArray(args []string, src Sources) (any, error) { return build(plain(args), true, src) }

func with(t *testing.T, f *Fixed, v any) any {
	t.Helper()
	doc, err := f.With(v)
	if err != nil {
		t.Fatal(err)
	}
	return doc
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
			obj, err := buildObject(tt.args, files(t, tt.stdin))
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
	arr, err := buildArray([]string{"a", "=b", "k=v", ":=1", ":=[true]", "=@VERSION", ":=@deploy.yaml", "", "@plain"}, files(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := `["a","b","k=v",1,[true],"1.2.3",{"replicas":3,"name":"api"},"","@plain"]`
	if got := encode(t, arr); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	empty, err := buildArray(nil, files(t, ""))
	if err != nil || encode(t, empty) != "[]" {
		t.Errorf("no values: %v %v", empty, err)
	}
	stdin, err := buildArray([]string{":=@-"}, files(t, "[1]"))
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
		{"a key that still ends in [] after the one that appends", false, []string{"a[][]=x"}, "a key cannot end in [], which appends"},
		{"a key given twice", false, []string{"a=1", "a:=2"}, `the key "a" is given twice`},
		{"a key as a value and an array", false, []string{"a[]=1", "a=2"}, "both as a value and as an array"},
		{"an array key then a value", false, []string{"a=1", "a[]=2"}, "both as a value and as an array"},
		{"json that is not json", false, []string{"a:=nope"}, "the value after := is not JSON"},
		{"two json values", false, []string{"a:=1 2"}, "the value after := is not JSON"},
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
				_, err = buildArray(tt.args, src)
			} else {
				_, err = buildObject(tt.args, src)
			}
			var ue *UsageError
			if !errors.As(err, &ue) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want a usage error holding %q", err, tt.want)
			}
		})
	}
}

// An argument that is JSON, refused for what it holds rather than for
// not being JSON, is the input failing and not the command line. It
// keeps the parse failure it is, so that jz returns the status the same
// text read from a file returns.
func TestInlineJSONRefusedIsAParseFailure(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		array bool
		args  []string
		want  string
	}{
		{"a key given twice", false, []string{`a:={"k":1,"k":2}`}, `the value after := cannot be written: the key "k" is given twice in one object`},
		{"nested too deeply", true, []string{":=" + strings.Repeat("[", 1001) + strings.Repeat("]", 1001)}, "the value after := cannot be written: arrays and objects nest deeper than 1000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := files(t, "")
			var err error
			if tt.array {
				_, err = buildArray(tt.args, src)
			} else {
				_, err = buildObject(tt.args, src)
			}
			var (
				pe *engine.ParseError
				ue *UsageError
			)
			if !errors.As(err, &pe) || errors.As(err, &ue) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want a parse failure holding %q", err, tt.want)
			}
		})
	}
}

func str(t string) Arg  { return Arg{Form: String, Text: t} }
func text(t string) Arg { return Arg{Form: TextFile, Text: t} }
func ptr(t string) Arg  { return Arg{Form: Path, Text: t} }
func arg(t string) Arg  { return Arg{Form: Plain, Text: t} }

// --string writes the text after the first = as it is: @, =, :=, quotes,
// spaces, line breaks and what looks like an option are all text.
func TestStringIsLiteral(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		array bool
		args  []Arg
		want  string
	}{
		{"an @ is not a file", false, []Arg{str("message=@here"), str("m2=@-")}, `{"message":"@here","m2":"@-"}`},
		{"the text after the first =", false, []Arg{str("a=b=c"), str("d=:=1"), str("e=")}, `{"a":"b=c","d":":=1","e":""}`},
		{"spaces, quotes and line breaks", false, []Arg{str("s= lead and trail "), str(`q="quoted" 'single'`), str("n=one\ntwo\r\n")}, `{"s":" lead and trail ","q":"\"quoted\" 'single'","n":"one\ntwo\r\n"}`},
		{"an option-like value", false, []Arg{str("flag=--pretty"), str("dash=-")}, `{"flag":"--pretty","dash":"-"}`},
		{"unicode", false, []Arg{str("κλειδί=日本語 🎌")}, `{"κλειδί":"日本語 🎌"}`},
		{"a key with [] appends", false, []Arg{str("tags[]=@a"), arg("tags[]:=1"), str("tags[]=b")}, `{"tags":["@a",1,"b"]}`},
		{"a pointer", false, []Arg{str("/meta/message=@x"), str("/~1a~0b=y")}, `{"meta":{"message":"@x"},"/a~b":"y"}`},
		{"a dot is part of the key", false, []Arg{str("spec.replicas=3")}, `{"spec.replicas":"3"}`},
		{"an array appends with an empty key or /-", true, []Arg{str("=@x"), arg("plain"), str("/-=:=1")}, `["@x","plain",":=1"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := build(tt.args, tt.array, files(t, ""))
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, v); got != tt.want {
				t.Errorf("got %s\nwant %s", got, tt.want)
			}
		})
	}
}

// --text-file keeps a file's text whole, every line ending included,
// where KEY=@FILE drops the last one.
func TestTextFileKeepsLineEndings(t *testing.T) {
	t.Parallel()
	src := files(t, "from stdin\r\n\n")
	v, err := build([]Arg{
		text("two=two.txt"), arg("two_less=@two.txt"),
		text("crlf=crlf.txt"), arg("crlf_less=@crlf.txt"),
		text("/body/stdin=-"),
		text("empty=empty.txt"), text("bare=bare.txt"), text("bom=bom.txt"), arg("bom_less=@bom.txt"),
		text("list[]=VERSION"),
	}, false, src)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"two":"a\n\n","two_less":"a\n","crlf":"line\r\n","crlf_less":"line","body":{"stdin":"from stdin\r\n\n"},"empty":"","bare":"no line ending","bom":"x\n","bom_less":"x","list":["1.2.3\n"]}`
	if got := encode(t, v); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	arr, err := build([]Arg{text("=VERSION")}, true, files(t, ""))
	if err != nil || encode(t, arr) != `["1.2.3\n"]` {
		t.Errorf("an array: %v %v", arr, err)
	}
	for _, tt := range []struct {
		args  []Arg
		stdin string
		want  string
	}{
		{[]Arg{text("a=latin1.txt")}, "", "not valid UTF-8"},
		{[]Arg{text("a=missing")}, "", "--text-file a=missing: file does not exist"},
		{[]Arg{text("a=-")}, strings.Repeat("x", 101), "standard input exceeds the 100 byte limit"},
	} {
		_, err := build(tt.args, false, files(t, tt.stdin))
		var ie *InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: got %v, want an input error holding %q", tt.args, err, tt.want)
		}
	}
}

// --path places a value at a JSON Pointer, making the objects and arrays
// on the way.
func TestPathPlacesValues(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		array bool
		args  []Arg
		want  string
	}{
		{"objects on the way", false, []Arg{ptr("/metadata/name=api"), ptr("/spec/replicas:=3"), ptr("/metadata/labels/app=web")}, `{"metadata":{"name":"api","labels":{"app":"web"}},"spec":{"replicas":3}}`},
		{"every operator", false, []Arg{ptr("/v=@VERSION"), ptr("/spec:=@deploy.yaml"), ptr("/j:=[1]"), ptr("/s=007")}, `{"v":"1.2.3","spec":{"replicas":3,"name":"api"},"j":[1],"s":"007"}`},
		{"- appends", false, []Arg{ptr("/a/-=x"), ptr("/a/-:=1"), arg("a[]=y")}, `{"a":["x",1,"y"]}`},
		{"an index names an element already given", false, []Arg{ptr("/items/-/name=a"), ptr("/items/0/age:=3"), ptr("/items/-/name=b")}, `{"items":[{"name":"a","age":3},{"name":"b"}]}`},
		{"keys keep the order they were first given", false, []Arg{arg("name=api"), ptr("/labels/app=web"), arg("tags[]=x"), ptr("/tags/-=y"), ptr("/labels/tier=db")}, `{"name":"api","labels":{"app":"web","tier":"db"},"tags":["x","y"]}`},
		{"a number is a key in an object", false, []Arg{ptr("/0=x"), ptr("/m/k=1"), ptr("/m/1=2")}, `{"0":"x","m":{"k":"1","1":"2"}}`},
		{"a dot is part of the key", false, []Arg{ptr("/spec.replicas:=3"), arg("spec.name=x")}, `{"spec.replicas":3,"spec.name":"x"}`},
		{"escapes", false, []Arg{ptr("/a~1b/c~0d=1"), ptr("/~01=2")}, `{"a/b":{"c~d":"1"},"~1":"2"}`},
		{"an array of objects", true, []Arg{ptr("/-/name=a"), ptr("/0/port:=80"), arg(":=null")}, `[{"name":"a","port":80},null]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := build(tt.args, tt.array, files(t, ""))
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, v); got != tt.want {
				t.Errorf("got %s\nwant %s", got, tt.want)
			}
		})
	}
}

// A location that cannot hold the value is refused, naming what is in the
// way, and never by overwriting, changing a type or filling a hole.
func TestLocationErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		array bool
		args  []Arg
		want  string
	}{
		{"a pointer given twice", false, []Arg{ptr("/a/b=1"), ptr("/a/b=2")}, `"--path /a/b=2": /a/b is given twice, first by "--path /a/b=1"`},
		{"a key and a pointer to it", false, []Arg{ptr("/a/b=1"), arg("a=2")}, `"a=2": /a is given twice, first by "--path /a/b=1"`},
		{"an element given twice", false, []Arg{ptr("/a/-=1"), ptr("/a/0=2")}, `/a/0 is given twice, first by "--path /a/-=1"`},
		{"into a value given whole", false, []Arg{arg(`spec:={"a":1}`), ptr("/spec/b=2")}, `/spec is a value given whole by "spec:={\"a\":1}", and jz does not add to a value it was given`},
		{"into a string", false, []Arg{str("name=x"), ptr("/name/first=y")}, `/name is a value given whole by "--string name=x"`},
		{"a key in an array", false, []Arg{arg("tags[]=a"), ptr("/tags/name=x")}, `/tags is an array, made by "tags[]=a", and "name" is not an index in it; append with /tags/-`},
		{"an append to an object", false, []Arg{ptr("/m/k=1"), ptr("/m/-=2")}, `/m is an object, made by "--path /m/k=1", and - appends to an array`},
		{"an index past the end", false, []Arg{ptr("/a/-=1"), ptr("/a/3/x=1")}, "/a has 1 elements, so 3 names none; append with /a/-"},
		{"an index with a leading zero", false, []Arg{ptr("/a/-=1"), ptr("/a/00=1")}, `"00" is not an index in it`},
		{"an index in nothing yet", false, []Arg{ptr("/items/0/name=x")}, "/items does not exist yet, so 0 cannot be an index in it; append with /items/-"},
		{"an append to the document", false, []Arg{ptr("/-=1")}, "the document is an object: - appends to an array; name a key"},
		{"a key in an array document", true, []Arg{ptr("/name=1")}, "the document is an array (--array): name an element with an index or append with -"},
		{"an index in an empty array document", true, []Arg{ptr("/0=1")}, "the array has 0 elements, so 0 names none; append with /-"},
		{"a pointer without /", false, []Arg{ptr("a=1")}, "a JSON Pointer starts with /"},
		{"an empty pointer token", false, []Arg{ptr("/a//b=1")}, "the pointer /a//b holds an empty key"},
		{"the root pointer", false, []Arg{ptr("/=1")}, "the pointer / holds an empty key"},
		{"an escape that is not one", false, []Arg{ptr("/a~2=1")}, `~ in a pointer is ~0 (a ~) or ~1 (a /), and "a~2" holds another`},
		{"a trailing ~", false, []Arg{str("/a~=1")}, `"a~" holds another`},
		{"[] in a pointer", false, []Arg{ptr("/tags[]=1")}, "[] appends after a plain key; in the pointer /tags[] append with /-"},
		{"no operator after a pointer", false, []Arg{ptr("/a")}, "--path takes POINTER=VALUE"},
		{"JSON that is not JSON", false, []Arg{ptr("/a:=nope")}, "the value after := is not JSON"},
		{"@ with no path", false, []Arg{ptr("/a=@")}, "@ names no file"},
		{"--string without =", false, []Arg{str("message")}, "--string takes KEY=TEXT or POINTER=TEXT"},
		{"--string with an empty key", false, []Arg{str("=x")}, "the key is empty"},
		{"--string with []  and no key", false, []Arg{str("[]=x")}, "the key is empty"},
		{"--string with [] twice", false, []Arg{str("a[][]=x")}, "a key cannot end in [], which appends"},
		{"--string that looks like :=", false, []Arg{str("n:=3")}, "a key ending in : looks like :=, which reads JSON; use --path for JSON"},
		{"--string with a pointer that looks like :=", false, []Arg{str("/spec/replicas:=3")}, "a key ending in : looks like :="},
		{"--text-file with a pointer that looks like :=", false, []Arg{text("/body:=notes.txt")}, "--text-file writes a file's text as it is"},
		{"--string with a key in an array", true, []Arg{str("k=x")}, "an array has no keys"},
		{"--text-file without =", false, []Arg{text("body")}, "--text-file takes KEY=PATH or POINTER=PATH"},
		{"--text-file with no path", false, []Arg{text("body=")}, "the path is empty; - is standard input"},
		{"--text-file that looks like :=", false, []Arg{text("b:=x")}, "--text-file writes a file's text as it is"},
		{"standard input twice across forms", false, []Arg{text("a=-"), arg("b:=@-")}, `standard input is already read by "--text-file a=-"`},
		{"an option argument that is not UTF-8", false, []Arg{str("a=\xff")}, "not valid UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read := false
			src := Sources{ReadFile: func(string) ([]byte, error) { read = true; return nil, fs.ErrNotExist }, Stdin: iotest.ErrReader(errors.New("read")), MaxSize: 10}
			_, err := build(tt.args, tt.array, src)
			var ue *UsageError
			if !errors.As(err, &ue) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want a usage error holding %q", err, tt.want)
			}
			if read {
				t.Error("a file was read before the refusal")
			}
		})
	}
}

// A long argument is cut short where an error quotes it.
func TestUsageErrorQuotesALongArgumentShort(t *testing.T) {
	t.Parallel()
	arg := "a:=" + strings.Repeat("[", 500) + "é"
	_, err := buildObject([]string{arg}, files(t, ""))
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
	if _, err := buildObject([]string{"a=@file", "b=@-", "a=2"}, src); err == nil || read {
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
			_, err := buildObject(tt.args, files(t, tt.stdin))
			var ie *InputError
			if !errors.As(err, &ie) || !tt.check(err) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an input error holding %q", err, tt.want)
			}
		})
	}
	failing := Sources{Stdin: iotest.ErrReader(errors.New("broken pipe")), MaxSize: 10}
	if _, err := buildObject([]string{"a=@-"}, failing); err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("a failing standard input: %v", err)
	}
	if _, err := buildObject([]string{"a=@-"}, Sources{MaxSize: 10}); err == nil || !strings.Contains(err.Error(), "no standard input") {
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
			v, err = buildArray([]string{one, two}, src)
		} else {
			v, err = buildObject([]string{one, two}, src)
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

// Arguments in any of the forms, in any order, either earn a usage or an
// input error, or make a document that encodes and reads back the same.
func FuzzPlan(f *testing.F) {
	for _, s := range []string{"/a/b=1", "/a/-:=2", "/a/0/c=x", "k=@-", "/x~1y=z", "tags[]=a", "=v", "/-=1", "a:=[1]"} {
		f.Add(uint8(0), s, uint8(3), "/a/-=2", false)
		f.Add(uint8(1), s, uint8(2), "b=-", true)
	}
	f.Fuzz(func(t *testing.T, form1 uint8, one string, form2 uint8, two string, array bool) {
		args := []Arg{{Form: Form(form1 % 4), Text: one}, {Form: Form(form2 % 4), Text: two}}
		src := Sources{ReadFile: func(string) ([]byte, error) { return []byte("x\n"), nil }, Stdin: strings.NewReader(`{"in":1}`), MaxSize: 1 << 16}
		v, err := build(args, array, src)
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

// A plan made once per record reads its files once, and each document
// holds the record it was made with where standard input is named.
func TestFixedWithARecordPerDocument(t *testing.T) {
	t.Parallel()
	reads := 0
	src := files(t, "")
	readFile := src.ReadFile
	src.ReadFile = func(path string) ([]byte, error) {
		reads++
		return readFile(path)
	}
	for _, tt := range []struct {
		args   []Arg
		array  bool
		format string
		want   string
	}{
		{[]Arg{arg("host=a"), arg("sample:=@-"), arg("v=@VERSION")}, false, datafile.JSONL, `{"host":"a","sample":[1,{"k":null}],"v":"1.2.3"}`},
		{[]Arg{ptr("/meta/line=@-"), str("tag=@x")}, false, datafile.LINES, `{"meta":{"line":[1,{"k":null}]},"tag":"@x"}`},
		{[]Arg{arg(":=@-"), arg("end")}, true, datafile.JSONL, `[[1,{"k":null}],"end"]`},
	} {
		p, err := Parse(tt.args, tt.array)
		if err != nil {
			t.Fatal(err)
		}
		if format, _ := p.StdinFormat(); format != tt.format {
			t.Errorf("%v: format %q, want %q", tt.args, format, tt.format)
		}
		f, err := p.Fixed(src)
		if err != nil {
			t.Fatal(err)
		}
		before := reads
		for range 3 {
			if got := encode(t, with(t, f, []any{int64(1), jsonutil.NewObject()})); got == "" {
				t.Fatal("nothing made")
			}
		}
		obj := jsonutil.NewObject()
		obj.Set("k", nil)
		if got := encode(t, with(t, f, []any{json.Number("1"), obj})); got != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
		if reads != before {
			t.Errorf("files read again per record: %d", reads-before)
		}
	}
	p, err := Parse([]Arg{text("body=-"), arg("a=1")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if format, which := p.StdinFormat(); format != datafile.TEXT || which != "--text-file body=-" {
		t.Errorf("--text-file: %q %q", format, which)
	}
	p, err = Parse([]Arg{arg("a=1")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if format, _ := p.StdinFormat(); format != "" {
		t.Errorf("no standard input: %q", format)
	}
	f, err := p.Fixed(src)
	if err != nil || encode(t, with(t, f, "ignored")) != `{"a":"1"}` {
		t.Errorf("no slot: %v", err)
	}
	if _, err := p.Fixed(Sources{ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist }}); err != nil {
		t.Errorf("nothing to read: %v", err)
	}
	p, err = Parse([]Arg{arg("a=@missing"), arg("b:=@-")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fixed(src); err == nil {
		t.Error("a missing file was not reported")
	}
}

// A document is bounded as a whole, the way a data file is: values read
// from several files, each under the limit, can still make a document
// over it, and --each bounds each document it makes.
func TestDocumentValueLimit(t *testing.T) {
	t.Parallel()
	src := files(t, "")
	// {"a":{"b":1,"a":2},"c":{"b":1,"a":2}} holds seven values.
	args := []string{"a:=@doc.json", "c:=@doc.json"}
	src.MaxValues = 7
	if v, err := buildObject(args, src); err != nil || encode(t, v) != `{"a":{"b":1,"a":2},"c":{"b":1,"a":2}}` {
		t.Errorf("at the limit: %v %v", v, err)
	}
	src.MaxValues = 6
	_, err := buildObject(args, src)
	var pe *engine.ParseError
	if !errors.As(err, &pe) || !errors.Is(err, engine.ErrTooManyValues) || !strings.Contains(err.Error(), "more than 6 values") {
		t.Errorf("past the limit: %v", err)
	}
	// With --each the fixed values count in every document, and the record
	// that takes one past the limit is refused.
	p, err := Parse([]Arg{arg("a:=@doc.json"), arg("r:=@-")}, false)
	if err != nil {
		t.Fatal(err)
	}
	src.MaxValues = 6
	f, err := p.Fixed(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.With([]any{int64(1)}); err != nil {
		t.Errorf("a record at the limit: %v", err)
	}
	if _, err := f.With([]any{int64(1), int64(2)}); !errors.Is(err, engine.ErrTooManyValues) {
		t.Errorf("a record past the limit: %v", err)
	}
}

// A document made by locations has the same nesting limit as a document
// read from JSON or YAML. The containers a location makes and the value it
// places count together, including the record placed by --each.
func TestDocumentDepthLimit(t *testing.T) {
	t.Parallel()
	src := files(t, "")
	src.MaxDepth = 3
	if v, err := build([]Arg{ptr("/a/b/c=x")}, false, src); err != nil || encode(t, v) != `{"a":{"b":{"c":"x"}}}` {
		t.Errorf("at the limit: %v %v", v, err)
	}
	if _, err := build([]Arg{ptr("/a/b/c/d=x")}, false, src); err == nil || !strings.Contains(err.Error(), "nests deeper than 3") {
		t.Errorf("past the limit: %v", err)
	}
	if _, err := buildObject([]string{"a:=[[[1]]]"}, src); err == nil || !strings.Contains(err.Error(), "nests deeper than 3") {
		t.Errorf("a nested value past the completed document limit: %v", err)
	}

	p, err := Parse([]Arg{ptr("/a/b:=@-")}, false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := p.Fixed(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.With([]any{int64(1)}); err != nil {
		t.Errorf("a record at the limit: %v", err)
	}
	if _, err := f.With([]any{[]any{int64(1)}}); err == nil || !strings.Contains(err.Error(), "nests deeper than 3") {
		t.Errorf("a record past the limit: %v", err)
	}
}

// Input past the byte limit is a parse failure on every path, the way it
// is when jz reads its standard input or a file with --file.
func TestByteLimitIsAParseFailure(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"a=@-"}, {"a:=@big.json.gz"}} {
		_, err := buildObject(args, files(t, strings.Repeat("x", 101)))
		var pe *engine.ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%v: %v is not a parse failure", args, err)
		}
	}
}

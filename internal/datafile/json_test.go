package datafile

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonutil.Marshal(v)
	if err != nil {
		t.Fatalf("encoding %v: %v", v, err)
	}
	return string(b)
}

// parseError asserts err is a parse failure on line and that its message
// holds want.
func parseError(t *testing.T, err error, line int, want string) {
	t.Helper()
	var pe *engine.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("want a parse error, got %v", err)
	}
	if pe.Line != line || !strings.Contains(pe.Msg, want) {
		t.Errorf("got line %d %q, want line %d holding %q", pe.Line, pe.Msg, line, want)
	}
}

func TestReadJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"keys keep their order", `{"z":1,"a":2,"m":3}`, `{"z":1,"a":2,"m":3}`},
		{"numbers keep their digits", `[2.50, 1e3, -0, 12345678901234567890123]`, `[2.50,1e3,-0,12345678901234567890123]`},
		{"nested values", `{"a":[{"b":null},true,false,"x"]}`, `{"a":[{"b":null},true,false,"x"]}`},
		{"whitespace and a byte order mark", "\xEF\xBB\xBF \n {\"a\" : 1 } \n", `{"a":1}`},
		{"a scalar document", `"text"`, `"text"`},
		{"escapes", `"aé\n\"b"`, `"aé\n\"b"`},
		{"empty collections", `{"a":{},"b":[]}`, `{"a":{},"b":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := Read(JSON, []byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, v); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestReadJSONRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in string
		line     int
		want     string
	}{
		{"nothing", "", 0, "there is no JSON value"},
		{"only blanks", " \n\t", 0, "there is no JSON value"},
		{"a key given twice", "{\n\"a\":1,\n\"a\":2}", 3, `the key "a" is given twice`},
		{"a key given twice deeper", `[{"b":{"c":1,"c":1}}]`, 1, `the key "c" is given twice`},
		{"text after the value", "{\"a\":1}\n{\"a\":2}", 2, "text after the JSON value"},
		{"a syntax error", "{\n\"a\":,\n\"b\":1}", 2, "not valid JSON"},
		{"a document cut short", "{\"a\":[1,", 1, "ends before it is complete"},
		{"a closing bracket first", "]", 1, "not valid JSON"},
		{"not UTF-8", "{\"a\":\n\"\xff\"}", 2, "not valid UTF-8"},
		{"a bare word", "nope", 1, "not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := Read(JSON, []byte(tt.in))
			if err == nil {
				t.Fatalf("read %s", encode(t, v))
			}
			if tt.line == 0 {
				var pe *engine.ParseError
				if !errors.As(err, &pe) || !strings.Contains(pe.Msg, tt.want) {
					t.Errorf("got %v, want %q", err, tt.want)
				}
				return
			}
			parseError(t, err, tt.line, tt.want)
		})
	}
}

// Nesting is bounded, so no document can run the reader out of stack.
func TestReadJSONDepth(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Read(JSON, []byte(deep)); err == nil || !strings.Contains(err.Error(), "nest deeper") {
		t.Errorf("a document nested past the limit: %v", err)
	}
	ok := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	if _, err := Read(JSON, []byte(ok)); err != nil {
		t.Errorf("a document nested to the limit: %v", err)
	}
}

func TestJSONLines(t *testing.T) {
	t.Parallel()
	in := "\xEF\xBB\xBF{\"a\":1}\r\n\n   \n[1,2]\n\"s\"\nnull"
	v, err := Read(JSONL, []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := encode(t, v), `[{"a":1},[1,2],"s",null]`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	empty, err := Read(JSONL, nil)
	if err != nil || encode(t, empty) != "[]" {
		t.Errorf("an empty file is an empty list: %v %v", empty, err)
	}
	for _, tt := range []struct {
		in   string
		line int
		want string
	}{
		{"{\"a\":1}\n{\"a\":", 2, "not one JSON value"},
		{"1 2\n", 1, "text after the JSON value on the line"},
		{"{\"a\":1,\"a\":1}\n", 1, `the key "a" is given twice`},
		{"ok\n", 1, "not one JSON value"},
		// Only the white space JSON allows between tokens makes a line blank.
		{"1\n\u00a0\n2\n", 2, "not one JSON value"},
		{"1\n\f\n2\n", 2, "not one JSON value"},
		{"1\n\u3000\n", 2, "not one JSON value"},
		{"[1]\n\xff\n", 2, "not valid UTF-8"},
		{strings.Repeat("[", MaxDepth+1) + "\n", 1, "nest deeper"},
	} {
		_, err := Read(JSONL, []byte(tt.in))
		parseError(t, err, tt.line, tt.want)
	}
}

// Every document encoding/json accepts is read, and what is read writes
// back as the same JSON: the same keys in the same order and the same
// number literals.
//
// Four kinds of document it accepts are refused instead, and each is a
// statement this package makes rather than a gap. A key given twice and
// a document nested past the limit are the two the reader declares; text
// that is not UTF-8 is not text. The fourth is an escape naming half of
// a surrogate pair: json.Valid says yes and json.Unmarshal then hands
// back U+FFFD, which is a character the input may hold for itself, so
// reading it that way would change what it was given.
// TestJSONRefusesHalfOfASurrogatePair pins that.
func FuzzReadJSON(f *testing.F) {
	for _, s := range []string{`{"a":1}`, `[1,2.5e3,"x",null,true]`, `{"a":{"a":{}}}`, `{"a":1,"a":2}`, ``, `"é"`, `[`, "{\"a\"\n:1}"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Read(JSON, data)
		trimmed := bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
		if err != nil {
			var pe *engine.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("a failure that is not a parse error: %v", err)
			}
			if json.Valid(trimmed) && !strings.Contains(pe.Msg, "given twice") && !strings.Contains(pe.Msg, "nest deeper") && !strings.Contains(pe.Msg, "UTF-8") && !strings.Contains(pe.Msg, "surrogate pair") {
				t.Fatalf("valid JSON refused: %v", err)
			}
			return
		}
		if !json.Valid(trimmed) {
			t.Fatalf("invalid JSON read as %s", encode(t, v))
		}
		out := encode(t, v)
		again, err := Read(JSON, []byte(out))
		if err != nil {
			t.Fatalf("what was written does not read back: %v\n%s", err, out)
		}
		if encode(t, again) != out {
			t.Fatalf("a second reading differs:\n%s\n%s", out, encode(t, again))
		}
	})
}

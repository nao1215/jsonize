package yaml

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestParseValues pins what each shape of the subset reads as, through
// the generic decoding.
func TestParseValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want any
	}{
		{"empty", "", nil},
		{"only comments", "# a\n\n  # b\n", nil},
		{"scalar", "hello", "hello"},
		{"document marker", "---\na: 1\n", map[string]any{"a": int64(1)}},
		{"marker with value", "--- 5\n", int64(5)},
		{"end marker", "a: 1\n...\n", map[string]any{"a": int64(1)}},
		{"mapping", "a: 1\nb: two\nc: true\nd: ~\ne:\n", map[string]any{"a": int64(1), "b": "two", "c": true, "d": nil, "e": nil}},
		{"nested mapping", "a:\n  b:\n    c: 1\n  d: 2\n", map[string]any{"a": map[string]any{"b": map[string]any{"c": int64(1)}, "d": int64(2)}}},
		{"sequence", "- 1\n- b\n-\n- - x\n  - y\n", []any{int64(1), "b", nil, []any{"x", "y"}}},
		{"sequence at key level", "a:\n- 1\n- 2\nb: 3\n", map[string]any{"a": []any{int64(1), int64(2)}, "b": int64(3)}},
		{"mapping in sequence", "- a: 1\n  b: 2\n- a: 3\n", []any{map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{"a": int64(3)}}},
		{"sequence in mapping in sequence", "- a:\n    - 1\n  b: x\n", []any{map[string]any{"a": []any{int64(1)}, "b": "x"}}},
		{"flow sequence", "a: [1, two, 'three', \"four\", [5]]\n", map[string]any{"a": []any{int64(1), "two", "three", "four", []any{int64(5)}}}},
		{"flow mapping", "a: {b: 1, c: [x], d: {e: f}, g}\n", map[string]any{"a": map[string]any{"b": int64(1), "c": []any{"x"}, "d": map[string]any{"e": "f"}, "g": nil}}},
		{"flow over lines", "a: [\n  1, # one\n  2,\n]\n", map[string]any{"a": []any{int64(1), int64(2)}}},
		{"flow plain with colon", "[http://x, a:b, -v, --x]", []any{"http://x", "a:b", "-v", "--x"}},
		{"flow plain folded", "[a\n  b, c]", []any{"a b", "c"}},
		{"empty flow", "a: []\nb: {}\n", map[string]any{"a": []any{}, "b": map[string]any{}}},
		{"comments", "a: 1 # one\n# two\nb: x#y\n", map[string]any{"a": int64(1), "b": "x#y"}},
		{"single quoted", "a: 'it''s'\n", map[string]any{"a": "it's"}},
		{"double quoted escapes", `a: "t\tn\nq\"s\\x\x41u\u00e9U\U0001F600z\0"`, map[string]any{"a": "t\tn\nq\"s\\xAu\xc3\xa9U\xf0\x9f\x98\x80z\x00"}},
		{"quoted over lines", "a: \"one\n  two\n\n  three\"\n", map[string]any{"a": "one two\nthree"}},
		{"a tab inside a scalar", "a: \"one\ttwo\"\n", map[string]any{"a": "one\ttwo"}},
		{"characters the text may hold", "a: \"\u0085\u00a0\ufffd\U0001F600\"\n", map[string]any{"a": "\u0085\u00a0\ufffd\U0001F600"}},
		{"escaped break", "a: \"one\\\n  two\"\n", map[string]any{"a": "onetwo"}},
		{"quoted keys", "\"a b\": 1\n'c': 2\n", map[string]any{"a b": int64(1), "c": int64(2)}},
		{"explicit keys", "? a b\n: 1\n? 'c'\n:\n  d: 2\ne: 3\n", map[string]any{"a b": int64(1), "c": map[string]any{"d": int64(2)}, "e": int64(3)}},
		{"explicit key in a sequence", "- ? a\n  : 1\n", []any{map[string]any{"a": int64(1)}}},
		{"plain over lines", "a: one\n  two\n\n  three\nb: 2\n", map[string]any{"a": "one two\nthree", "b": int64(2)}},
		{"plain in sequence over lines", "- one\n  two\n- three\n", []any{"one two", "three"}},
		{"dash continues a plain scalar", "- a\n  - b\n", []any{"a - b"}},
		{"comment ends a plain scalar", "a: one\n  # c\nb: 2\n", map[string]any{"a": "one", "b": int64(2)}},
		{"space inside a flow scalar", "a: [1 2]\n", map[string]any{"a": []any{"1 2"}}},
		{"literal", "a: |\n  one\n   two\n\n  three\nb: 1\n", map[string]any{"a": "one\n two\n\nthree\n", "b": int64(1)}},
		{"literal strip", "a: |-\n  one\n\n", map[string]any{"a": "one"}},
		{"literal keep", "a: |+\n  one\n\nb: 1\n", map[string]any{"a": "one\n\n", "b": int64(1)}},
		{"folded", "a: >\n  one\n  two\n\n  three\n\n\n  four\nb: 1\n", map[string]any{"a": "one two\nthree\n\nfour\n", "b": int64(1)}},
		{"folded strip", "a: >-\n  one\n  two\n", map[string]any{"a": "one two"}},
		{"folded more indented", "a: >\n  one\n    two\n  three\n", map[string]any{"a": "one\n  two\nthree\n"}},
		{"block scalar indicator digit", "a: |2\n   one\n", map[string]any{"a": " one\n"}},
		{"block scalar at top", "|\n  x\n", "x\n"},
		{"block scalar in sequence", "- |\n  x\n- y\n", []any{"x\n", "y"}},
		{"empty block scalar", "a: |\nb: 1\n", map[string]any{"a": "", "b": int64(1)}},
		{"numbers", "a: -3\nb: 0x1f\nc: 1.5\nd: 1e3\ne: '7'\nf: 1_0\ng: .inf\nh: +2\n", map[string]any{"a": int64(-3), "b": "0x1f", "c": 1.5, "d": 1000.0, "e": "7", "f": "1_0", "g": ".inf", "h": int64(2)}},
		{"bools and nulls", "a: True\nb: FALSE\nc: null\nd: Null\ne: 'null'\nf: yes\n", map[string]any{"a": true, "b": false, "c": nil, "d": nil, "e": "null", "f": "yes"}},
		{"crlf and bom", "\xEF\xBB\xBFa: 1\r\nb: |\r\n  x\r\n", map[string]any{"a": int64(1), "b": "x\n"}},
		{"trailing spaces", "a: 1   \nb: x  # c\n", map[string]any{"a": int64(1), "b": "x"}},
		{"dash in value", "a: - b\n", map[string]any{"a": "- b"}},
		{"colon in value", "a: b:c\nurl: http://x/y\n", map[string]any{"a": "b:c", "url": "http://x/y"}},
		{"tab inside a value", "a: b\tc\n", map[string]any{"a": "b\tc"}},
		{"deep flow", strings.Repeat("[", 50) + strings.Repeat("]", 50), nest(50)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got any
			if err := Unmarshal([]byte(tt.in), &got, true); err != nil {
				t.Fatalf("Unmarshal(%q): %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unmarshal(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func nest(n int) any {
	var v any = []any{}
	for range n - 1 {
		v = []any{v}
	}
	return v
}

// TestParseErrors pins that what the subset does not read is refused,
// on the line it stands.
func TestParseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		line int
		msg  string
	}{
		{"tab indentation", "a:\n\tb: 1\n", 2, "tab is not indentation"},
		{"a NUL in a plain scalar", "key: ab\x00cd\n", 1, "the character U+0000 is not allowed in YAML text"},
		{"a control character in a quoted scalar", "key: \"ab\x01cd\"\n", 1, "the character U+0001 is not allowed in YAML text"},
		{"a control character on a later line", "a: 1\nb: 2\nc: \x1f\n", 3, "the character U+001F is not allowed in YAML text"},
		{"delete", "a: \x7f\n", 1, "the character U+007F is not allowed in YAML text"},
		{"a C1 control character", "a: \u0091\n", 1, "the character U+0091 is not allowed in YAML text"},
		{"a non-character at the end of the plane", "a: \ufffe\n", 1, "the character U+FFFE is not allowed in YAML text"},
		{"a control character in a comment", "a: 1 # \x02\n", 1, "the character U+0002 is not allowed in YAML text"},
		{"tab indentation first line", "\ta: 1\n", 1, "tab is not indentation"},
		{"anchor", "a: &x 1\n", 1, "anchors, aliases and tags"},
		{"alias", "a: *x\n", 1, "anchors, aliases and tags"},
		{"tag", "a: !!str 1\n", 1, "anchors, aliases and tags"},
		{"tag at key", "!!map {a: 1}\n", 1, "anchors, aliases and tags"},
		{"tag in flow", "[!x 1]\n", 1, "anchors, aliases and tags"},
		{"explicit key holding a list", "? [a]\n: 1\n", 1, `after "? " is one scalar`},
		{"explicit key without a colon", "? a\nb: 1\n", 1, `needs its ":"`},
		{"explicit key without a value line", "- ? a\n", 1, `needs its ":"`},
		{"explicit key with nothing", "?\n: 1\n", 1, `after "? " is one scalar`},
		{"explicit quoted key with junk", "? 'a' b\n: 1\n", 1, "unexpected text after the quoted key"},
		{"two documents", "a: 1\n---\nb: 2\n", 2, "one document"},
		{"text after document", "a: 1\n...\nb: 2\n", 3, "unexpected text"},
		{"duplicate key", "a: 1\nb: 2\na: 3\n", 3, `duplicate key "a"`},
		{"duplicate flow key", "{a: 1, a: 2}\n", 1, `duplicate key "a"`},
		{"unexpected indentation", "a: [1]\n  b: 2\n", 2, "unexpected indentation"},
		{"unexpected indentation after sequence", "- [a]\n  - b\n", 2, "unexpected indentation"},
		{"deeper line after a scalar", "a: 1\n  b: 2\n", 2, "mapping value is not allowed"},
		{"sequence where key", "a: 1\n- b\n", 2, "sequence item where a key"},
		{"no key", "a: 1\nb\n", 2, "expected a key"},
		{"mapping value in scalar", "a: b\n  c: d\n", 2, "mapping value is not allowed"},
		{"mapping value in scalar first line", "a: b: c\n", 1, "mapping value is not allowed"},
		{"unterminated quote", "a: 'b\n", 1, "unterminated quoted value"},
		{"unterminated double quote", "a: \"b\n", 1, "unterminated quoted value"},
		{"text after quote", "a: 'b' c\n", 1, "unexpected text after the quoted value"},
		{"unknown escape", `a: "\q"`, 1, "unknown escape"},
		{"bad hex escape", `a: "\xzz"`, 1, "invalid escape"},
		{"short escape", `a: "\u12`, 1, "unterminated escape"},
		{"unterminated flow", "a: [1, 2\n", 1, "unterminated flow"},
		{"unterminated flow mapping", "a: {b: 1\n", 1, "unterminated flow"},
		{"flow mapping separator", "a: {b: 1 c: 2}\n", 1, `expected "," or "}"`},
		{"empty flow value", "a: [1, , 2]\n", 1, "expected a value"},
		{"block scalar in flow", "a: [|\n  x]\n", 1, "block scalar cannot"},
		{"block scalar junk", "a: | x\n  y\n", 1, "unexpected text after the block scalar"},
		{"too deep", strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1), 1, "nested deeper"},
		{"too deep block", strings.Repeat("- ", MaxDepth+1) + "x\n", 1, "nested deeper"},
		{"empty key", ": 1\n", 1, "empty key"},
		{"reserved start", "a: @x\n", 1, "cannot start a value"},
		{"quoted key without colon", "'a' 1\n", 1, "unexpected text after the quoted value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tt.in))
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("Parse(%q) = %v, want a syntax error", tt.in, err)
			}
			if se.Line != tt.line || !strings.Contains(se.Msg, tt.msg) {
				t.Errorf("Parse(%q) = %q, want line %d holding %q", tt.in, err, tt.line, tt.msg)
			}
		})
	}
}

// TestNodePositions pins where nodes are placed, which is what every
// error about a value is reported with.
func TestNodePositions(t *testing.T) {
	t.Parallel()
	n, err := Parse([]byte("# c\na: 1\nb:\n  - x\n  - {c: 2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n.Line != 2 || n.Col != 1 {
		t.Errorf("root at %d:%d", n.Line, n.Col)
	}
	b := n.Pairs[1]
	if b.Key.Line != 3 || b.Value.Line != 4 || b.Value.Col != 3 {
		t.Errorf("b at %d, its value at %d:%d", b.Key.Line, b.Value.Line, b.Value.Col)
	}
	c := b.Value.Items[1].Pairs[0]
	if c.Key.Line != 5 || c.Key.Col != 6 || c.Value.Col != 9 {
		t.Errorf("c at %d:%d, its value at column %d", c.Key.Line, c.Key.Col, c.Value.Col)
	}
	if !n.Pairs[0].Value.Plain || n.Pairs[0].Value.IsNull() {
		t.Errorf("a's value: %+v", n.Pairs[0].Value)
	}
}

type target struct {
	Name     string            `yaml:"name"`
	Count    int               `yaml:"count,omitempty"`
	On       *bool             `yaml:"on,omitempty"`
	Tags     []string          `yaml:"tags,omitempty"`
	Rename   map[string]string `yaml:"rename,omitempty"`
	Child    *target           `yaml:"child,omitempty"`
	Parts    []part            `yaml:"parts,omitempty"`
	Shape    shape             `yaml:"shape,omitempty"`
	Ratio    float64           `yaml:"ratio,omitempty"`
	Any      any               `yaml:"any,omitempty"`
	Hidden   string            `yaml:"-"`
	Untagged string
	hidden   string
}

type part struct {
	Name string `yaml:"name"`
}

// shape is written as one string or as a list of them.
type shape []string

func (s *shape) UnmarshalYAML(n *Node) error {
	var one string
	if err := Decode(n, &one, true); err == nil {
		*s = shape{one}
		return nil
	}
	var many []string
	if err := Decode(n, &many, true); err != nil {
		return errors.New("a shape is a string or a list of strings")
	}
	*s = many
	return nil
}

// TestDecodeStruct exercises decoding into every kind of field a
// definition has.
func TestDecodeStruct(t *testing.T) {
	t.Parallel()
	src := `
name: x
count: 0x10
on: false
tags: [a, "b"]
rename: {a: b}
child:
  name: y
  count: 2.0
  shape: [p, q]
parts:
  - name: p1
  - {name: p2}
shape: one
ratio: 0.5
any: {k: [1]}
untagged: u
`
	var got target
	if err := Unmarshal([]byte(src), &got, true); err != nil {
		t.Fatal(err)
	}
	off := false
	want := target{
		Name: "x", Count: 16, On: &off, Tags: []string{"a", "b"}, Rename: map[string]string{"a": "b"},
		Child: &target{Name: "y", Count: 2, Shape: shape{"p", "q"}},
		Parts: []part{{"p1"}, {"p2"}}, Shape: shape{"one"}, Ratio: 0.5,
		Any: map[string]any{"k": []any{int64(1)}}, Untagged: "u",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got.hidden != "" || got.Hidden != "" {
		t.Error("hidden fields were set")
	}
}

// TestDecodeErrors pins the refusals of a value that does not fit.
func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     string
		strict bool
		path   string
		msg    string
		line   int
	}{
		{"unknown key", "name: x\nbogus: 1\n", true, "", `unknown field "bogus"`, 2},
		{"nested unknown key", "child: {name: y, bogus: 1}\n", true, "", `unknown field "bogus"`, 1},
		{"list for string", "name: [x]\n", true, "name", "expected a string, not a list", 1},
		{"mapping for list", "tags: {a: 1}\n", true, "tags", "expected a list, not a mapping", 1},
		{"string for mapping", "rename: x\n", true, "rename", "expected a mapping", 1},
		{"word for number", "count: abc\n", true, "count", `cannot read "abc" as a whole number`, 1},
		{"fraction", "child:\n  count: 1.5\n", true, "child.count", "must be written as a whole number, not 1.5", 2},
		{"overflow", "count: 99999999999999999999\n", true, "count", "too large", 1},
		{"word for bool", "on: yes\n", true, "on", "expected true or false", 1},
		{"list for bool", "on: [x]\n", true, "on", "expected true or false", 1},
		{"word for float", "ratio: x\n", true, "ratio", "as a number", 1},
		{"list element", "parts: [{name: [x]}]\n", true, "parts[0].name", "expected a string", 1},
		{"custom", "shape: {a: 1}\n", true, "", "a shape is a string", 0},
		{"non-strict keeps going", "bogus: 1\nname: x\n", false, "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got target
			err := Unmarshal([]byte(tt.in), &got, tt.strict)
			if tt.msg == "" {
				if err != nil || got.Name != "x" {
					t.Fatalf("Unmarshal(%q) = %v, %+v", tt.in, err, got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("Unmarshal(%q) = %v, want %q", tt.in, err, tt.msg)
			}
			var de *DecodeError
			var ue *UnknownFieldError
			switch {
			case errors.As(err, &ue):
				if ue.Line != tt.line {
					t.Errorf("line %d, want %d", ue.Line, tt.line)
				}
			case errors.As(err, &de):
				if de.Path != tt.path || de.Line != tt.line {
					t.Errorf("path %q line %d, want %q line %d", de.Path, de.Line, tt.path, tt.line)
				}
			case tt.line != 0:
				t.Errorf("error %T carries no position", err)
			}
		})
	}
	if err := Unmarshal([]byte("a: 1"), target{}, true); err == nil || !strings.Contains(err.Error(), "non-nil pointer") {
		t.Errorf("decoding into a value: %v", err)
	}
	var ch chan int
	if err := Unmarshal([]byte("1"), &ch, true); err == nil || !strings.Contains(err.Error(), "cannot decode into") {
		t.Errorf("decoding into a channel: %v", err)
	}
	var m map[int]string
	if err := Unmarshal([]byte("{a: b}"), &m, true); err == nil || !strings.Contains(err.Error(), "keyed by") {
		t.Errorf("decoding into a map keyed by int: %v", err)
	}
}

// TestDecodeNulls pins that a null leaves a value empty whatever its
// type, and that a null document changes nothing.
func TestDecodeNulls(t *testing.T) {
	t.Parallel()
	on := true
	got := target{Name: "keep", Count: 3, On: &on, Tags: []string{"x"}, Rename: map[string]string{"a": "b"}, Child: &target{}, Any: 1, Ratio: 2}
	if err := Unmarshal([]byte("# nothing\n"), &got, true); err != nil || got.Name != "keep" {
		t.Fatalf("null document: %v %+v", err, got)
	}
	if err := Unmarshal([]byte("name:\ncount: ~\non: null\ntags:\nrename:\nchild:\nany:\nratio:\nshape:\n"), &got, true); err != nil {
		t.Fatal(err)
	}
	if got.Name != "" || got.Count != 0 || got.On != nil || got.Tags != nil || got.Rename != nil || got.Child != nil || got.Any != nil || got.Ratio != 0 || got.Shape != nil {
		t.Errorf("nulls left %+v", got)
	}
}

// FuzzParseYAML holds the parser to never panicking, on any text, and
// to a tree the generic decoding can walk.
func FuzzParseYAML(f *testing.F) {
	for _, s := range []string{
		"a: 1\nb: [1, {c: d}]\n", "- a\n- - b\n", "a: |\n  x\n", "a: >-\n  x\n\n  y\n", `a: "\u00e9\n"`,
		"a: 'x''y'\n", "--- a\n...\n", "a:\n- b\nc: d\n", "[", "{a: [}", "\ta", "a: &x", "a: !t", "? a",
		strings.Repeat("- ", 200), "a: b\n  c: d\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		n, err := Parse([]byte(in))
		if err != nil {
			var se *SyntaxError
			if !errors.As(err, &se) || se.Line < 1 {
				t.Fatalf("%q: error without a line: %v", in, err)
			}
			return
		}
		var v any
		if err := Decode(n, &v, true); err != nil {
			t.Fatalf("%q: a tree that decodes generically with an error: %v", in, err)
		}
		var tgt target
		_ = Unmarshal([]byte(in), &tgt, false)
	})
}

// TestParseCorners reaches the branches the shapes above pass by:
// markers next to comments, tabs on blank lines, comments that end a
// value, and the folds of flow scalars and folded scalars at their
// edges.
func TestParseCorners(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want any
	}{
		{"--- # c\n\n", nil},
		{"---\n...\n", nil},
		{"a: 1\n...\n# c\n", map[string]any{"a": int64(1)}},
		{" a: 1\n", map[string]any{"a": int64(1)}},
		{"\t\n a: 1\n", map[string]any{"a": int64(1)}},
		{"a: 1\n\t# c\nb: 2\n", map[string]any{"a": int64(1), "b": int64(2)}},
		{"a #b: 1\n", "a"},
		{"a: ?x\n", map[string]any{"a": "?x"}},
		{"?a: 1\n", map[string]any{"?a": int64(1)}},
		{"a: b\n\t\n  c\n", map[string]any{"a": "b\nc"}},
		{"a: b\n\t# x\nc: 1\n", map[string]any{"a": "b", "c": int64(1)}},
		{"a: |", map[string]any{"a": ""}},
		{"a: >\n\n  x\n", map[string]any{"a": "\nx\n"}},
		{"a: >\n  x\n\n    y\n  z\n", map[string]any{"a": "x\n\n  y\nz\n"}},
		{"{a:, b: 1}", map[string]any{"a": nil, "b": int64(1)}},
		{"[a # c\n, b]", []any{"a", "b"}},
		{"[a\n\n  b]", []any{"a\nb"}},
		{"-", []any{nil}},
		{"a: \"x\\\n\"", map[string]any{"a": "x"}},
		{"%YAML 1.2\n---\na: 1\n", map[string]any{"a": int64(1)}},
		{"# c\n%YAML 1.2\n\n%FOO bar\n--- # c\na: 1\n", map[string]any{"a": int64(1)}},
		{"%YAML 1.1\n--- [1]\n", []any{int64(1)}},
	}
	for _, tt := range tests {
		var got any
		if err := Unmarshal([]byte(tt.in), &got, true); err != nil {
			t.Errorf("Unmarshal(%q): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Unmarshal(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
	for _, tt := range []struct {
		in, msg string
	}{
		{"'a: 1\n", "unterminated quoted value"},
		{"@x: 1\n", "cannot start a value"},
		{"a: b\n\tc\n", "tab is not indentation"},
		{"a: \"\\", "unterminated escape"},
		{"[a # c", "unterminated flow"},
		{"[a: b]", `expected "," or "]"`},
		{"{", "unterminated flow"},
		{"{'a: 1}", "unterminated quoted value"},
		{"{a", "unterminated flow"},
		{"{a:", "unterminated flow"},
		{"{a: [b]", "unterminated flow"},
		{"a:\n  b: 1\n\tc: 2\n", "tab is not indentation"},
		{"- a\n\t- b\n", "tab is not indentation"},
		{"a: |\n  x\n\ty\n", "tab is not indentation"},
		{"%TAG ! tag:example.com,2000:\n---\na: 1\n", "anchors, aliases and tags"},
		{"%YAML 1.2\na: 1\n", `a directive is followed by "---"`},
		{"%YAML 1.2\n", `a directive is followed by "---"`},
		{"%YAML 2.0\n---\na: 1\n", "YAML 2.0"},
	} {
		var got any
		err := Unmarshal([]byte(tt.in), &got, true)
		if err == nil || !strings.Contains(err.Error(), tt.msg) || !strings.HasPrefix(err.Error(), "line ") {
			t.Errorf("Unmarshal(%q) = %v, want %q with its line", tt.in, err, tt.msg)
		}
	}
	if !(*Node)(nil).IsNull() {
		t.Error("a nil node is null")
	}
}

type numbers struct {
	Small int8  `yaml:"small"`
	Big   int64 `yaml:"big"`
	Part  part  `yaml:"part"`
}

// TestDecodeNumbers pins the spellings of a whole number and their
// refusals.
func TestDecodeNumbers(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int64{"0o17": 15, "-0x10": -16, "+5": 5, "2.0": 2, "1e2": 100, "'3'": 3} {
		var got numbers
		if err := Unmarshal([]byte("big: "+in), &got, true); err != nil || got.Big != want {
			t.Errorf("big: %s = %d, %v; want %d", in, got.Big, err, want)
		}
	}
	for in, msg := range map[string]string{
		"small: 300": "does not fit in int8", "big: 0xzz": "cannot read", "big: 1e400": "cannot read",
		"big: 1e19": "too large", "big: [1]": "expected a number, not a list", "big: {a: 1}": "expected a number, not a mapping",
	} {
		var got numbers
		if err := Unmarshal([]byte(in), &got, true); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", in, err, msg)
		}
	}
	got := numbers{Part: part{Name: "keep"}}
	if err := Unmarshal([]byte("part: ~"), &got, true); err != nil || got.Part.Name != "keep" {
		t.Errorf("null into a struct: %v %+v", err, got)
	}
	var m map[string]string
	if err := Unmarshal([]byte("{a: [x]}"), &m, true); err == nil || !strings.Contains(err.Error(), "a: expected a string") {
		t.Errorf("map element: %v", err)
	}
	var v any
	if err := Unmarshal([]byte("["), &v, true); err == nil {
		t.Error("Unmarshal of broken text succeeded")
	}
}

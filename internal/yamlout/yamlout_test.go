package yamlout

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/yaml"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// obj builds an ordered object from alternating keys and values.
func obj(kv ...any) *jsonutil.Object {
	o := jsonutil.NewObject()
	for i := 0; i < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// pair and pairs are a mapping in the order its keys stand, which is
// what jz wrote and what a reader hands back.
type pair struct {
	key   string
	value any
}

type pairs []pair

// decodeAll reads every document of a YAML stream, keeping the order of
// mapping keys. A stream is documents between "---" and "..." lines; a
// text that opens with neither is one document.
func decodeAll(t *testing.T, text string) []any {
	t.Helper()
	docs := []string{text}
	if strings.HasPrefix(text, "---\n") {
		docs = nil
		var cur strings.Builder
		open := false
		for _, line := range strings.SplitAfter(text, "\n") {
			switch {
			case line == "---\n":
				open = true
				cur.Reset()
			case line == "...\n":
				docs = append(docs, cur.String())
				open = false
			case open:
				cur.WriteString(line)
			case line != "":
				t.Fatalf("text outside a document in %q: %q", text, line)
			}
		}
		if open {
			t.Fatalf("unclosed document in %q", text)
		}
	}
	out := make([]any, 0, len(docs))
	for _, doc := range docs {
		n, err := yaml.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("decoding %q: %v", doc, err)
		}
		out = append(out, fromNode(t, n))
	}
	return out
}

// plainGo turns a value jz produces into what a YAML reader hands back
// for it: an ordered mapping, a list, and numbers as int64 or float64.
func plainGo(v any) any {
	switch t := v.(type) {
	case *jsonutil.Object:
		if t == nil {
			return nil
		}
		out := pairs{}
		for _, m := range t.Members() {
			out = append(out, pair{key: m.Key, value: plainGo(m.Value)})
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plainGo(e)
		}
		return out
	case int:
		return int64(t)
	default:
		return v
	}
}

// fromNode brings what the reader decoded to the same form.
func fromNode(t *testing.T, n *yaml.Node) any {
	t.Helper()
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		out := make(pairs, 0, len(n.Pairs))
		for _, p := range n.Pairs {
			out = append(out, pair{key: p.Key.Value, value: fromNode(t, p.Value)})
		}
		return out
	case yaml.SequenceNode:
		out := make([]any, len(n.Items))
		for i, e := range n.Items {
			out[i] = fromNode(t, e)
		}
		return out
	case yaml.ScalarNode:
	}
	var v any
	if err := yaml.Decode(n, &v, true); err != nil {
		t.Fatal(err)
	}
	return v
}

// same reports how read differs from what was written, or nothing.
func same(wrote, read any) string {
	if reflect.DeepEqual(wrote, read) {
		return ""
	}
	return "wrote " + describe(wrote) + "\nread  " + describe(read)
}

// describe renders a value with its mappings in order.
func describe(v any) string {
	switch t := v.(type) {
	case pairs:
		parts := make([]string, len(t))
		for i, p := range t {
			parts[i] = strconvQuote(p.key) + ": " + describe(p.value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = describe(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return strconvQuote(t)
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtV(v), "\n", " "), "  ", " "))
	}
}

func roundTrip(t *testing.T, v any) string {
	t.Helper()
	var b bytes.Buffer
	if err := Encode(&b, v); err != nil {
		t.Fatalf("Encode(%#v): %v", v, err)
	}
	docs := decodeAll(t, b.String())
	if len(docs) != 1 {
		t.Fatalf("%d documents in %q", len(docs), b.String())
	}
	if diff := same(plainGo(v), docs[0]); diff != "" {
		t.Errorf("read back differently:\n%s\nYAML:\n%s", diff, b.String())
	}
	return b.String()
}

func TestEncodeLayout(t *testing.T) {
	t.Parallel()
	v := []any{
		obj("filesystem", "/dev/sda1", "size", int64(1024), "use_percent", 3.5, "mounted_on", "/"),
		obj("filesystem", "tmpfs", "size", int64(0), "use_percent", nil, "mounted_on", "/run/user/1000"),
	}
	want := `- filesystem: /dev/sda1
  size: 1024
  use_percent: 3.5
  mounted_on: /
- filesystem: tmpfs
  size: 0
  use_percent: null
  mounted_on: /run/user/1000
`
	if got := roundTrip(t, v); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestEncodeNesting(t *testing.T) {
	t.Parallel()
	v := obj(
		"destination", obj("name", "localhost", "data_bytes", int64(56)),
		"replies", []any{obj("seq", int64(1), "flags", []any{"a", "b"}), obj("seq", int64(2), "flags", []any{})},
		"matrix", []any{[]any{int64(1), int64(2)}, []any{}, []any{obj()}},
		"empty", obj(),
		"none", []any{},
	)
	want := `destination:
  name: localhost
  data_bytes: 56
replies:
  - seq: 1
    flags:
      - a
      - b
  - seq: 2
    flags: []
matrix:
  - - 1
    - 2
  - []
  - - {}
empty: {}
none: []
`
	if got := roundTrip(t, v); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A decimal is written with a point, so that a reader gets back the
// decimal it was and not an integer.
func TestEncodeDecimals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "0.0"}, {3, "3.0"}, {-0.5, "-0.5"}, {0.04, "0.04"},
		{1e21, "1.0e+21"}, {1e-7, "1.0e-7"}, {123456789, "123456789.0"}, {2.5e-10, "2.5e-10"},
	} {
		got := roundTrip(t, c.in)
		if got != c.want+"\n" {
			t.Errorf("%v: got %q want %q", c.in, got, c.want)
		}
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		var b bytes.Buffer
		if err := Encode(&b, []any{f}); !errors.Is(err, ErrNonFiniteNumber) || b.Len() != 0 {
			t.Errorf("%v: err=%v wrote %q", f, err, b.String())
		}
	}
}

// Strings that a reader could take for something else are quoted, and
// every one of them reads back as the string it was.
func TestEncodeStringsKeepTheirMeaning(t *testing.T) {
	t.Parallel()
	quoted := []string{
		"", " ", "  lead", "trail ", "yes", "No", "ON", "off", "y", "N", "true", "False", "null", "NULL", "~",
		"0", "1", "-1", "+1", "1.0", "1e3", ".5", "0x1F", "0o17", "017", "1_000", "1:20", "12:30:45",
		"2026-09-11", "2026-09-11T12:30:45Z", ".inf", "-.inf", ".nan", ".NaN",
		"- item", "-", "? q", ":", "a: b", "a:", "key #comment", "#x", "@at", "`tick", "!tag", "&anchor",
		"*alias", "|block", ">fold", "%dir", "[x]", "{x}", "'single'", `"double"`, "back\\slash",
		"line\nbreak", "tab\there", "cr\rhere", "nul\x00byte", "bell\a", "del\x7f", "nel\u0085", "c1\u0090",
		"ls\u2028ps\u2029", "\ufeffbom", "\ufffe", "1.8T", "955M", "-rw-r--r--", "a  b", "日本語 ",
		"ゆ\u3000き", "=", "<<", "---", "...", "--- x", "... x", "a\\b",
	}
	for _, s := range quoted {
		got := roundTrip(t, []any{s})
		if !strings.HasPrefix(got, `- "`) {
			t.Errorf("%q was written unquoted: %q", s, got)
		}
		roundTrip(t, obj(s, s))
	}
	plainOK := []string{"a", "localhost", "/dev/sda1", "/", "_x", "Filesystem", "Moved Permanently", "text/html; charset=UTF-8"[:9],
		"https://example.com/a", "a:b", "user@host", "a-b.c+d(e)=f%g,h", "日本語", "Destination Host Unreachable", "é"}
	for _, s := range plainOK {
		got := roundTrip(t, []any{s})
		if got != "- "+s+"\n" {
			t.Errorf("%q: got %q, want it unquoted", s, got)
		}
	}
	// A byte that is not UTF-8 is written as the replacement character,
	// as the JSON output writes it.
	var b bytes.Buffer
	if err := Encode(&b, []any{"a\xffb"}); err != nil || b.String() != "- \"a\ufffdb\"\n" {
		t.Errorf("got %q, %v", b.String(), err)
	}
}

// A key is a string like any other, and one no longer than YAML allows
// before its colon is written after "? ".
func TestEncodeKeys(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("k", maxImplicitKey+1)
	v := obj("yes", "y", "1", int64(1), "Sector size (logical/physical)", "512 bytes", "a: b", true, "", false, long, obj("in", "x"))
	got := roundTrip(t, v)
	if !strings.Contains(got, "\"yes\": \"y\"\n") || !strings.Contains(got, "? "+long+"\n:\n  in: x\n") {
		t.Errorf("got\n%s", got)
	}
}

func TestEncodeDocumentMarksEachRecord(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	records := []any{obj("seq", int64(1), "time_ms", 0.5), obj("seq", int64(2), "time_ms", nil), obj()}
	for _, r := range records {
		if err := EncodeDocument(&b, r); err != nil {
			t.Fatal(err)
		}
	}
	want := "---\nseq: 1\ntime_ms: 0.5\n...\n---\nseq: 2\ntime_ms: null\n...\n---\n{}\n...\n"
	if b.String() != want {
		t.Fatalf("got\n%s", b.String())
	}
	docs := decodeAll(t, b.String())
	if len(docs) != len(records) {
		t.Fatalf("%d documents", len(docs))
	}
	for i, r := range records {
		if diff := same(plainGo(r), docs[i]); diff != "" {
			t.Errorf("document %d: %s", i, diff)
		}
	}
}

func TestEncodeRefusesWhatJSONRefuses(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := Encode(&b, obj("a", struct{}{})); err == nil || b.Len() != 0 {
		t.Errorf("err=%v wrote %q", err, b.String())
	}
	if err := EncodeDocument(&b, []any{math.NaN()}); err == nil || b.Len() != 0 {
		t.Errorf("err=%v wrote %q", err, b.String())
	}
	for _, v := range []any{nil, true, int64(-3), 2, "x", (*jsonutil.Object)(nil)} {
		b.Reset()
		if err := Encode(&b, v); err != nil {
			t.Errorf("%#v: %v", v, err)
		}
	}
	if got := b.String(); got != "null\n" {
		t.Errorf("nil object: %q", got)
	}
}

// FuzzEncodeString checks that any string reads back as itself, as a
// value and as a key.
func FuzzEncodeString(f *testing.F) {
	for _, s := range []string{"", "yes", "a: b", "1.0", "\n", "\u2028", "- x", "日本語", "\x00", "a\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		want := strings.Map(func(r rune) rune { return r }, s)
		var b bytes.Buffer
		if err := Encode(&b, obj("k", s, want+"!", int64(1))); err != nil {
			t.Fatal(err)
		}
		docs := decodeAll(t, b.String())
		if len(docs) != 1 {
			t.Fatalf("%d documents in %q", len(docs), b.String())
		}
		m, ok := docs[0].(pairs)
		if !ok || len(m) != 2 || m[0].value != want || m[1].key != want+"!" {
			t.Fatalf("%q read back as %#v from %q", s, docs[0], b.String())
		}
	})
}

func strconvQuote(s string) string { return strconv.Quote(s) }

func fmtV(v any) string { return fmt.Sprintf("%#v", v) }

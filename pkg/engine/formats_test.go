package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// A csv value may contain the delimiter, a quote written twice, or a
// line break, which is what separates this from a table split on a
// literal separator.
func TestParseCSV(t *testing.T) {
	t.Parallel()
	const src = `format: 1
command: t
variant: v
parse: {type: csv}
fields: {count: {type: int}}
`
	input := "name,note,count\n" +
		"web,\"a, b\",3\n" +
		"db,\"he said \"\"hi\"\"\",4\n" +
		"log,\"first\nsecond\",5\n"
	v, err := Parse(load(t, src), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"name":"web","note":"a, b","count":3},` +
		`{"name":"db","note":"he said \"hi\"","count":4},` +
		`{"name":"log","note":"first\nsecond","count":5}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestParseCSVDelimiterAndHeader(t *testing.T) {
	t.Parallel()
	tab := `format: 1
command: t
variant: v
parse: {type: csv, delimiter: "\t"}
`
	v, err := Parse(load(t, tab), []byte("a\tb\n1\t2\n"), Options{})
	if err != nil || mustJSON(t, v) != `[{"a":"1","b":"2"}]` {
		t.Errorf("tab: %v %v", mustJSON(t, v), err)
	}

	// Declared columns mean the file has no header line of its own.
	named := `format: 1
command: t
variant: v
parse: {type: csv, header: {none: true, columns: [x, y]}}
`
	v, err = Parse(load(t, named), []byte("1,2\n3,4\n"), Options{})
	if err != nil || mustJSON(t, v) != `[{"x":"1","y":"2"},{"x":"3","y":"4"}]` {
		t.Errorf("declared columns: %v %v", mustJSON(t, v), err)
	}

	// A short row leaves the remaining keys null, so every object carries
	// the same keys; a long one has a value with nowhere to go.
	plain := `format: 1
command: t
variant: v
parse: {type: csv}
`
	v, err = Parse(load(t, plain), []byte("a,b,c\n1,2\n"), Options{})
	if err != nil || mustJSON(t, v) != `[{"a":"1","b":"2","c":null}]` {
		t.Errorf("short row: %v %v", mustJSON(t, v), err)
	}
	if _, err := Parse(load(t, plain), []byte("a,b\n1,2,3\n"), Options{}); err == nil {
		t.Error("a row wider than the header was accepted")
	}
	// A quote that never closes is a record the input did not finish.
	if _, err := Parse(load(t, plain), []byte("a,b\n1,\"x\n"), Options{}); err == nil {
		t.Error("an unterminated quote was accepted")
	}
	// A header that names one column twice keeps both values reachable.
	v, err = Parse(load(t, plain), []byte("a,a\n1,2\n"), Options{})
	if err != nil || mustJSON(t, v) != `[{"a":"1","a_2":"2"}]` {
		t.Errorf("repeated header: %v %v", mustJSON(t, v), err)
	}
}

func TestParseINI(t *testing.T) {
	t.Parallel()
	const src = `format: 1
command: t
variant: v
parse: {type: ini}
`
	input := "# a comment\ntop = 1\n\n[core]\nbare = false\n; another\nrepo = 0\n\n[user]\nname = Alice\n"
	v, err := Parse(load(t, src), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `{"":{"top":"1"},"core":{"bare":"false","repo":"0"},"user":{"name":"Alice"}}`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	// Nothing before the first heading means no nameless section at all,
	// rather than an empty one every consumer has to skip.
	v, err = Parse(load(t, src), []byte("[a]\nk = 1\n"), Options{})
	if err != nil || mustJSON(t, v) != `{"a":{"k":"1"}}` {
		t.Errorf("no preamble: %v %v", mustJSON(t, v), err)
	}
	// A section written twice continues the first one, and a key written
	// twice takes the last value.
	v, err = Parse(load(t, src), []byte("[a]\nk = 1\n[b]\nj = 2\n[a]\nk = 3\n"), Options{})
	if err != nil || mustJSON(t, v) != `{"a":{"k":"3"},"b":{"j":"2"}}` {
		t.Errorf("repeats: %v %v", mustJSON(t, v), err)
	}
	// A comment marker inside a value is part of the value: a password or
	// a path may contain either character.
	v, err = Parse(load(t, src), []byte("[a]\nk = x#y;z\n"), Options{})
	if err != nil || mustJSON(t, v) != `{"a":{"k":"x#y;z"}}` {
		t.Errorf("markers in a value: %v %v", mustJSON(t, v), err)
	}
	// A line that is neither a heading nor a pair is a failure rather
	// than something quietly dropped.
	if _, err := Parse(load(t, src), []byte("[a]\nnot a pair\n"), Options{}); err == nil {
		t.Error("a line with no separator was accepted")
	}
	// An ini result is one object, so it has no streaming form.
	if _, err := streamAll(t, src, "[a]\nk = 1\n"); err == nil {
		t.Error("an ini parser offered a streaming form")
	}
}

const boxDef = `format: 1
command: t
variant: v
parse: {type: table, split: box}
`

func TestParseBox(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"+------+-------------+-------+",
		"| name | image       | ports |",
		"+------+-------------+-------+",
		"| web  | nginx:1.27  | 80    |",
		"|      | (alpine)    |       |",
		"+------+-------------+-------+",
		"| db   | postgres:16 |       |",
		"+------+-------------+-------+",
		"",
	}, "\n")
	v, err := Parse(load(t, boxDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"name":"web","image":"nginx:1.27\n(alpine)","ports":"80"},` +
		`{"name":"db","image":"postgres:16","ports":null}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// The same records, one at a time.
	got, err := streamAll(t, boxDef, input)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if lines := strings.Count(got, "\n"); lines != 2 {
		t.Errorf("streamed %d records:\n%s", lines, got)
	}
}

// The Unicode rules are the same rules, and a header broken over two
// lines is one name.
func TestParseBoxUnicodeAndWrappedHeader(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"┌────────┬─────────┐",
		"│ device │ mount   │",
		"│ name   │ point   │",
		"╞════════╪═════════╡",
		"│ sda1   │ /       │",
		"├────────┼─────────┤",
		"│ sda2   │ /home   │",
		"└────────┴─────────┘",
		"",
	}, "\n")
	v, err := Parse(load(t, boxDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"device_name":"sda1","mount_point":"/"},{"device_name":"sda2","mount_point":"/home"}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A rule separates the header from the body and nothing else in the
// common case: MySQL, psql and sqlite3 in box mode draw one around the
// table and one under the header. Every line of the body is a row.
func TestParseBoxOneRowPerLine(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"+----+-------+",
		"| id | name  |",
		"+----+-------+",
		"|  1 | alpha |",
		"|  2 | beta  |",
		"|  3 | gamma |",
		"+----+-------+",
		"",
	}, "\n")
	v, err := Parse(load(t, boxDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"id":"1","name":"alpha"},{"id":"2","name":"beta"},{"id":"3","name":"gamma"}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got, err := streamAll(t, boxDef, input); err != nil || strings.Count(got, "\n") != 3 {
		t.Errorf("streamed %q, %v", got, err)
	}
}

func TestParseBoxRefuses(t *testing.T) {
	t.Parallel()
	// A header with an unnamed column has nowhere to put its values.
	if _, err := Parse(load(t, boxDef), []byte("+--+--+\n| a |  |\n+--+--+\n| 1 | 2 |\n"), Options{}); err == nil {
		t.Error("a header with a nameless column was accepted")
	}
	// The same name twice would hide one of the two columns.
	if _, err := Parse(load(t, boxDef), []byte("+--+--+\n| a | a |\n+--+--+\n| 1 | 2 |\n"), Options{}); err == nil {
		t.Error("a header naming one column twice was accepted")
	}
	// Text with no bars in it is not a drawn table.
	if _, err := Parse(load(t, boxDef), []byte("a b\n1 2\n"), Options{}); err == nil {
		t.Error("text with no cells was accepted")
	}
}

// Each new format gets the same treatment as the others: whatever the
// text is, the result is either an error or a value that encodes to
// JSON, and reading it whole and reading it as a stream agree.
func FuzzFormats(f *testing.F) {
	defs := []string{
		"format: 1\ncommand: t\nvariant: c\nparse: {type: csv}\nfields: {n: {type: int}}\n",
		"format: 1\ncommand: t\nvariant: d\nparse: {type: csv, delimiter: \"\\t\"}\n",
		"format: 1\ncommand: t\nvariant: i\nparse: {type: ini}\n",
		"format: 1\ncommand: t\nvariant: b\nparse: {type: table, split: box}\n",
		"format: 1\ncommand: t\nvariant: t\nparse: {type: tree, indent: \"  \", node: {parse: {type: regex, patterns: ['^(?P<k>\\S+): (?P<v>.*)$', '^(?P<x>.+)$']}}}\n",
		"format: 1\ncommand: t\nvariant: w\nparse: {type: tree, indent: \"\\t\", node: {parse: {type: kv, separator: \": \"}}}\n",
	}
	for _, seed := range []string{
		"a,b\n1,2\n",
		"a,b\n1,\"x,y\"\n",
		"a,b\n1,\"x\n",
		"a\tb\n1\t2\n",
		"[s]\nk = 1\n",
		"# c\n; c\n[a]\n[a]\nk=1\nk=2\n",
		"+--+--+\n| a | b |\n+--+--+\n| 1 | 2 |\n+--+--+\n",
		"┌─┬─┐\n│a│b│\n╞═╪═╡\n│1│2│\n└─┴─┘\n",
		"a: 1\n  b: 2\n    c: 3\nd: 4\n",
		"a: 1\n    b: 2\n",
		"   a\n",
		"\ta: 1\n",
		"|", "+", "\"", "\n\n\n", "",
	} {
		for i := range defs {
			f.Add(i, []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, which int, input []byte) {
		if which < 0 {
			which = -which
		}
		src := defs[which%len(defs)]
		d, err := definition.Load([]byte(src), "fuzz")
		if err != nil {
			t.Fatal(err)
		}
		v, err := Parse(d, input, Options{MaxInputSize: 1 << 20})
		if err != nil {
			return
		}
		if _, err := jsonutil.Marshal(v); err != nil {
			t.Fatalf("result not encodable: %v", err)
		}
		if !d.Parse.YieldsArray() {
			return
		}
		var batch bytes.Buffer
		for _, rec := range v.([]any) {
			if err := jsonutil.Encode(&batch, rec, false); err != nil {
				t.Fatal(err)
			}
		}
		var streamed bytes.Buffer
		err = Stream(d, bytes.NewReader(input), Options{}, func(rec any) error {
			return jsonutil.Encode(&streamed, rec, false)
		}, nil)
		if err != nil {
			t.Fatalf("read whole but not as a stream: %v", err)
		}
		if streamed.String() != batch.String() {
			t.Fatalf("the two readings differ:\nwhole  %q\nstream %q", batch.String(), streamed.String())
		}
	})
}

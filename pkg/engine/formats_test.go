package engine

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

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

// A column a caller named (WithTypes) that the csv does not have is
// refused where the columns become known: at the header line, or at the
// first record of a csv that numbers its columns. A stream ends there
// rather than leaving the header out as a bad record and writing rows the
// conversion never touched. A definition's own field rule for a column
// the file lacks is not refused: it does nothing, as it always has.
func TestCSVColumnNamedByTheCallerIsMissing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, def, input, want string
		types                  map[string]string
	}{
		{"a header without the column", "parse: {type: csv}\n", "name,size\nweb,3\n", `t/v: line 1: no column "count"; the columns are "name", "size"`, map[string]string{"count": "int"}},
		{"a numbered column past the first record", "parse: {type: csv, header: {none: true}}\n", "a,1\nb,2\n", `t/v: line 1: no column "column_3"; the columns are "column_1", "column_2"`, map[string]string{"column_3": "int"}},
		{"a column the definition does not name", "parse: {type: csv, header: {none: true, columns: [id]}}\n", "1\n2\n", `t/v: line 1: no column "count"; the columns are "id"`, map[string]string{"count": "int"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			def, err := load(t, "format: 1\ncommand: t\nvariant: v\n"+tt.def).WithTypes(tt.types)
			if err != nil {
				t.Fatal(err)
			}
			var mc *MissingColumnError
			if _, err := Parse(def, []byte(tt.input), Options{}); !errors.As(err, &mc) || err.Error() != tt.want {
				t.Errorf("Parse: %v, want %s", err, tt.want)
			}
			wrote := 0
			err = Stream(def, strings.NewReader(tt.input), Options{}, func(any) error {
				wrote++
				return nil
			})
			if !errors.As(err, &mc) || err.Error() != tt.want || wrote != 0 {
				t.Errorf("Stream: %v, %d written", err, wrote)
			}
		})
	}
	typed, err := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: csv}\n").WithTypes(map[string]string{"count": "int"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"count\n7\n", ""} {
		if _, err := Parse(typed, []byte(input), Options{}); err != nil {
			t.Errorf("%q: %v", input, err)
		}
	}
	own := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: csv}\nfields: {count: {type: int}, notes: {when_missing: omit}}\n")
	if v, err := Parse(own, []byte("id,count\n1,2\n"), Options{}); err != nil || mustJSON(t, v) != `[{"id":"1","count":2}]` {
		t.Errorf("a definition's field for a column the file lacks: %v %v", v, err)
	}
}

// A csv with no header line and no names numbers its columns from its
// first record, which is data like the rest. The whole document and the
// stream say the same records, for the comma and the tab, through
// quotes, line breaks inside a value and empty values; a row wider than
// the first record is refused in both, and a narrower one leaves nulls.
func TestParseCSVWithoutAHeaderLine(t *testing.T) {
	t.Parallel()
	const comma = "format: 1\ncommand: t\nvariant: v\nparse: {type: csv, header: {none: true}}\n"
	const tab = "format: 1\ncommand: t\nvariant: v\nparse: {type: csv, delimiter: \"\\t\", header: {none: true}}\n"
	const named = "format: 1\ncommand: t\nvariant: v\nparse: {type: csv, header: {none: true, columns: [id, note]}}\n"
	tests := []struct {
		name, src, input, want, err string
	}{
		{"the first record is a record", comma, "name,value\nid,3\n", `{"column_1":"name","column_2":"value"}` + "\n" + `{"column_1":"id","column_2":"3"}` + "\n", ""},
		{"quotes and a line break", comma, "1,\"a, b\"\n2,\"say \"\"hi\"\"\"\n3,\"first\nsecond\"\n", `{"column_1":"1","column_2":"a, b"}` + "\n" + `{"column_1":"2","column_2":"say \"hi\""}` + "\n" + `{"column_1":"3","column_2":"first\nsecond"}` + "\n", ""},
		{"empty values stay empty", comma, "a,,\n,,\n\"\",x,\n", `{"column_1":"a","column_2":"","column_3":""}` + "\n" + `{"column_1":"","column_2":"","column_3":""}` + "\n" + `{"column_1":"","column_2":"x","column_3":""}` + "\n", ""},
		{"a narrower row leaves nulls", comma, "1,2,3\n4\n", `{"column_1":"1","column_2":"2","column_3":"3"}` + "\n" + `{"column_1":"4","column_2":null,"column_3":null}` + "\n", ""},
		{"a wider row is refused", comma, "1,2\n3,4,5\n", "", "row has 3 fields but the first record, which numbers the columns, has 2"},
		{"tabs", tab, "a b\tc,d\n\"x\ty\"\t\n", `{"column_1":"a b","column_2":"c,d"}` + "\n" + `{"column_1":"x\ty","column_2":""}` + "\n", ""},
		{"named columns", named, "1,one\n2\n", `{"id":"1","note":"one"}` + "\n" + `{"id":"2","note":null}` + "\n", ""},
		{"more fields than names", named, "1,one,extra\n", "", "row has 3 fields but 2 columns are named"},
		{"CRLF", comma, "1,2\r\n3,4\r\n", `{"column_1":"1","column_2":"2"}` + "\n" + `{"column_1":"3","column_2":"4"}` + "\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			streamed, serr := streamAll(t, tt.src, tt.input)
			v, berr := Parse(load(t, tt.src), []byte(tt.input), Options{})
			if tt.err != "" {
				if berr == nil || !strings.Contains(berr.Error(), tt.err) {
					t.Errorf("whole: %v, want %q", berr, tt.err)
				}
				if serr == nil || !strings.Contains(serr.Error(), tt.err) {
					t.Errorf("stream: %v, want %q", serr, tt.err)
				}
				return
			}
			if berr != nil || serr != nil {
				t.Fatalf("whole: %v, stream: %v", berr, serr)
			}
			var whole bytes.Buffer
			for _, rec := range v.([]any) {
				if err := jsonutil.Encode(&whole, rec, false); err != nil {
					t.Fatal(err)
				}
			}
			if whole.String() != tt.want || streamed != tt.want {
				t.Errorf("whole  %s\nstream %s\nwant   %s", whole.String(), streamed, tt.want)
			}
		})
	}
	// Nothing at all is no records, not a header waiting for rows.
	if v, err := Parse(load(t, comma), nil, Options{}); err != nil || mustJSON(t, v) != "[]" {
		t.Errorf("empty: %v %v", mustJSON(t, v), err)
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

// A carriage return outside a quoted value ends no csv line and belongs
// to no value, so a file whose lines end with CR alone is refused where
// it was read as one header line and no rows. Inside quotes it is a
// value's own, and the CR of a CRLF ending is the line ending.
func TestParseCSVRefusesACarriageReturnOutsideQuotes(t *testing.T) {
	t.Parallel()
	plain := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: csv}\n")
	for _, input := range []string{
		"a,b\r1,2\r3,4\r",
		"a,b\n1\r,2\n",
		"a\r1\r\n2\n",
		"a,b\n\"x\"\r,2\n",
	} {
		_, err := Parse(plain, []byte(input), Options{})
		if err == nil || !strings.Contains(err.Error(), "carriage return outside a quoted value") {
			t.Errorf("%q: %v", input, err)
		}
	}
	for input, want := range map[string]string{
		"a,b\r\n1,2\r\n":      `[{"a":"1","b":"2"}]`,
		"a,b\n\"x\ry\",2\n":   `[{"a":"x\ry","b":"2"}]`,
		"a,b\n\"x\r\ny\",2\n": `[{"a":"x\ny","b":"2"}]`,
	} {
		v, err := Parse(plain, []byte(input), Options{})
		if err != nil || mustJSON(t, v) != want {
			t.Errorf("%q: %v %v", input, mustJSON(t, v), err)
		}
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
	// A section written twice continues the first one.
	v, err = Parse(load(t, src), []byte("[a]\nk = 1\n[b]\nj = 2\n[a]\nl = 3\n"), Options{})
	if err != nil || mustJSON(t, v) != `{"a":{"k":"1","l":"3"},"b":{"j":"2"}}` {
		t.Errorf("repeats: %v %v", mustJSON(t, v), err)
	}
	// A key written twice under one section, even in its second half,
	// would lose a value whatever the two are.
	for _, input := range []string{"[a]\nk = 1\n[b]\nj = 2\n[a]\nk = 3\n", "[a]\nk = 1\nk = 1\n"} {
		if _, err = Parse(load(t, src), []byte(input), Options{}); err == nil || !strings.Contains(err.Error(), "already set on line 2") {
			t.Errorf("a repeated key in %q: %v", input, err)
		}
	}
	// The same key in two sections is two keys.
	if v, err = Parse(load(t, src), []byte("[a]\nk = 1\n[b]\nk = 2\n"), Options{}); err != nil || mustJSON(t, v) != `{"a":{"k":"1"},"b":{"k":"2"}}` {
		t.Errorf("one key in two sections: %v %v", mustJSON(t, v), err)
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
		v, acct, err := ParseAccounted(d, input, Options{MaxInputSize: 1 << 20})
		if err != nil {
			return
		}
		if _, err := jsonutil.Marshal(v); err != nil {
			t.Fatalf("result not encodable: %v", err)
		}
		// A reading that succeeded has put every line somewhere.
		accounted := acct.Read + acct.Folded + acct.Blank
		for _, ig := range acct.Ignored {
			accounted += ig.Lines
		}
		if accounted != acct.Lines {
			t.Fatalf("the account covers %d of %d lines: %+v", accounted, acct.Lines, acct)
		}
		sameAsStream(t, d, input, v)
	})
}

// sameAsStream checks that a text read whole is read the same way one
// record at a time, for a definition that has a streaming form. The
// stream is given the same limits, and is also fed the text one byte at
// a time, since the two readings must not depend on how the bytes come.
func sameAsStream(t *testing.T, d *definition.Definition, input []byte, whole any) {
	t.Helper()
	if !d.Parse.YieldsArray() {
		return
	}
	var batch bytes.Buffer
	for _, rec := range whole.([]any) {
		if err := jsonutil.Encode(&batch, rec, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []io.Reader{bytes.NewReader(input), iotest.OneByteReader(bytes.NewReader(input))} {
		var streamed bytes.Buffer
		err := Stream(d, r, Options{MaxInputSize: 1 << 20}, func(rec any) error {
			return jsonutil.Encode(&streamed, rec, false)
		})
		if err != nil {
			t.Fatalf("read whole but not as a stream: %v", err)
		}
		if streamed.String() != batch.String() {
			t.Fatalf("the two readings differ:\nwhole  %q\nstream %q", batch.String(), streamed.String())
		}
	}
}

// Numbering a repeated heading must not land on a heading the file
// already has: "x, x, x_2" used to put the second x under x_2, over the
// value of the third column.
func TestCSVColumnNamesAreUnique(t *testing.T) {
	t.Parallel()
	cases := []struct{ header, want string }{
		{"x,x,x_2", "x,x_3,x_2"},
		{"x_2,x,x", "x_2,x,x_3"},
		{"a,a,a,a_2,a_3", "a,a_4,a_5,a_2,a_3"},
		{",,", "column,column_2,column_3"},
		{"a b,a-b,a_b", "a_b,a_b_2,a_b_3"},
	}
	for _, tc := range cases {
		def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: csv}\n")
		got, err := Parse(def, []byte(tc.header+"\n1,2,3,4,5\n"[:2*len(strings.Split(tc.header, ","))]+"\n"), Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.header, err)
		}
		obj := got.([]any)[0].(*jsonutil.Object)
		var names []string
		for _, m := range obj.Members() {
			names = append(names, m.Key)
		}
		if strings.Join(names, ",") != tc.want {
			t.Errorf("%q: columns %v, want %s", tc.header, names, tc.want)
		}
		if len(obj.Members()) != len(strings.Split(tc.header, ",")) {
			t.Errorf("%q: %d values for %d columns", tc.header, len(obj.Members()), len(strings.Split(tc.header, ",")))
		}
	}
	// A rename that lands on another heading is numbered the same way.
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: csv, header: {rename: {b: a}}}\n")
	got, err := Parse(def, []byte("a,b\n1,2\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s := mustJSON(t, got); s != `[{"a":"1","a_2":"2"}]` {
		t.Error(s)
	}
}

// A quoted value keeps every line it holds, a blank one included, and
// the lines a quoted value holds are not lines input.ignore or
// input.select look at: the record is made before they do. The stream
// reads it the same way.
func TestCSVQuotedValueKeepsBlankLines(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, def, in, want string }{
		{"blank line inside a value", "parse: {type: csv}\n", "x\n\"a\n\nb\"\n", `[{"x":"a\n\nb"}]`},
		{"blank lines kept and a value over several", "input: {skip_blank: false}\nparse: {type: csv}\n", "x\n\"a\n\n\nb\"\n1\n", `[{"x":"a\n\n\nb"},{"x":"1"}]`},
		{"ignore does not see inside a value", "input: {ignore: ['^#']}\nparse: {type: csv}\n", "# comment\nx\n\"#not\na comment\"\n# also\n", `[{"x":"#not\na comment"}]`},
		{"fold runs on records", "input: {fold: '^ '}\nparse: {type: csv}\n", "x\n\"a\nb\"\n1\n 2\n", `[{"x":"a\nb"},{"x":"1 2"}]`},
		{"a value over lines with a delimiter and a quote inside", "parse: {type: csv}\n", "a,b\n\"1,\"\"\n\",2\n", `[{"a":"1,\"\n","b":"2"}]`},
	}
	for _, tc := range cases {
		def := load(t, "format: 1\ncommand: t\nvariant: v\n"+tc.def)
		got, err := Parse(def, []byte(tc.in), Options{})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if s := mustJSON(t, got); s != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, s, tc.want)
		}
		var streamed []any
		if err := Stream(def, strings.NewReader(tc.in), Options{}, func(v any) error {
			streamed = append(streamed, v)
			return nil
		}); err != nil {
			t.Errorf("%s: stream: %v", tc.name, err)
			continue
		}
		if s := mustJSON(t, streamed); s != tc.want {
			t.Errorf("%s: stream got %s, want %s", tc.name, s, tc.want)
		}
	}
	// select counts records: a record select.limit leaves out is one
	// unread record, however many lines its value holds.
	def := load(t, "format: 1\ncommand: t\nvariant: v\ninput: {select: {limit: 1}}\nparse: {type: csv}\n")
	_, err := Parse(def, []byte("x\n\"a\nb\"\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "line 2:") || strings.Contains(err.Error(), "line 3") {
		t.Errorf("a record select left out: %v", err)
	}
}

// The line a csv error names is the line of the input the record is on,
// whatever came before it: a header, a value over several lines, a line
// input.ignore dropped.
func TestCSVErrorsNameTheirLine(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, def, in, want string }{
		{"a row after the header", "parse: {type: csv}\nfields: {x: {type: int}}\n", "x\nabc\n", "line 2:"},
		{"a row after a value over two lines", "parse: {type: csv}\nfields: {x: {type: int}}\n", "x,y\n1,\"a\nb\"\n4,5\nabc,6\n", "line 5:"},
		{"a row after an ignored line", "input: {ignore: ['^#']}\nparse: {type: csv}\nfields: {x: {type: int}}\n", "x\n# note\n# note\nabc\n", "line 4:"},
		{"a bare quote in a value over two lines", "parse: {type: csv}\n", "x,y\n\"a\nb\",c\"d\n", "line 3:"},
		{"too many fields", "parse: {type: csv}\n", "x\n1\n2,3\n", "line 3:"},
		// A quote that never closes is found where the input ends.
		{"a quote that never closes", "parse: {type: csv}\n", "x\n1\n\"open\n2\n", "line 4: column 2: extraneous or missing \" in quoted-field"},
	}
	for _, tc := range cases {
		def := load(t, "format: 1\ncommand: t\nvariant: v\n"+tc.def)
		_, err := Parse(def, []byte(tc.in), Options{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: whole: %v, want %s", tc.name, err, tc.want)
		}
		err = Stream(def, strings.NewReader(tc.in), Options{}, func(any) error { return nil })
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: stream: %v, want %s", tc.name, err, tc.want)
		}
	}
}

// csvQuote follows the quoting rules one line at a time and says the
// same thing about every prefix that reading the text at once would.
func TestCSVQuoteFollowsLines(t *testing.T) {
	t.Parallel()
	text := "a,\"b\nc\"\"d\",e\n\"f\n\ng\",h\ni\"j,k\n\"\"\"\nl\"\n"
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	q := newCSVQuote(',')
	for i, l := range lines {
		got := q.feed(l)
		// The reference reading: the whole text so far, one scan.
		want := csvOpenAtOnce(strings.Join(lines[:i+1], "\n"), ',')
		if got != want {
			t.Errorf("after line %d %q: open=%v, at once %v", i+1, l, got, want)
		}
	}
}

// csvOpenAtOnce is the one-pass reading csvQuote replaced, kept as the
// reference for the test above.
func csvOpenAtOnce(text string, delim rune) bool {
	quoted, start := false, true
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quoted && r == '"' && i+1 < len(runes) && runes[i+1] == '"':
			i++
		case quoted && r == '"':
			quoted = false
		case quoted:
		case start && r == '"':
			quoted, start = true, false
		case r == delim || r == '\n':
			start = true
		default:
			start = false
		}
	}
	return quoted
}

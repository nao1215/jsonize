package engine

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// streamAll collects the records Stream emits as one JSON document per
// line, which is what the CLI writes.
func streamAll(t *testing.T, src, input string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Stream(load(t, src), strings.NewReader(input), Options{}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	})
	return out.String(), err
}

// batchAll reads the same input whole, so the two readings can be
// compared. --stream changes when a record is written, never what it
// says.
func batchAll(t *testing.T, src, input string) string {
	t.Helper()
	v, err := Parse(load(t, src), []byte(input), Options{})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	var out bytes.Buffer
	for _, rec := range v.([]any) {
		if err := jsonutil.Encode(&out, rec, false); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}

func TestStreamMatchesWholeDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		def   string
		input string
	}{
		{
			name: "table with a derived header",
			def: `format: 1
command: t
variant: v
parse: {type: table}
fields: {n: {type: int}}
`,
			input: "NAME  N\na  1\nbb  2\n",
		},
		{
			name: "table without a header line",
			def: `format: 1
command: t
variant: v
parse:
  type: table
  split: delimiter
  delimiter: "\t"
  header: {none: true, columns: [size, name]}
fields: {size: {type: int}}
`,
			input: "4\t./a\n8\t./b\n",
		},
		{
			name: "regex per line, several alternatives",
			def: `format: 1
command: t
variant: v
parse:
  type: regex
  patterns: ['^a(?P<a>\d+)$', '^b(?P<b>\d+)$']
`,
			input: "a1\nb2\na3\n",
		},
		{
			name: "kv as a list",
			def: `format: 1
command: t
variant: v
parse: {type: kv, separator: ':'}
`,
			input: "one: 1\ntwo: 2\n",
		},
		{
			name: "records closed by the next start line and by the end",
			def: `format: 1
command: t
variant: v
parse:
  type: records
  start: '^# '
  parts:
    - name: head
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^# (?P<name>\S+)$'}
    - name: values
      select: {skip: 1}
      parse: {type: kv, separator: '='}
`,
			input: "# one\na=1\nb=2\n# two\nc=3\n",
		},
		{
			// The stage that needs a lookahead: a line is only complete
			// once the next one turns out not to continue it.
			name: "folded continuation lines",
			def: `format: 1
command: t
variant: v
input: {fold: '^[ \t]+\S'}
parse: {type: regex, pattern: '^(?P<k>\S+)=(?P<v>.+)$'}
`,
			input: "a=one\n   and more\nb=two\n",
		},
		{
			name: "ignore, blank lines and a heading together",
			def: `format: 1
command: t
variant: v
input:
  ignore: ['^#']
  select: {after: '^BEGIN$'}
parse: {type: regex, pattern: '^(?P<v>\S+)$'}
`,
			input: "# noise\nBEGIN\n# comment\n\nkept1\nkept2\n\n",
		},
		{
			name: "a record read in full by its parts",
			def: `format: 1
command: t
variant: v
parse:
  type: records
  start: '^# '
  parts:
    - name: head
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^# (?P<name>\S+)$'}
    - name: values
      select: {after: '^values:$', until: '^end$'}
      parse: {type: kv, separator: '='}
    - name: trailer
      select: {after: '^end$'}
      parse: {type: regex, pattern: '^(?P<note>.+)$'}
`,
			input: "# one\nvalues:\na=1\nend\nnote one\n# two\nvalues:\nb=2\nend\n",
		},
		{
			name: "NUL separated records",
			def: `format: 1
command: t
variant: v
input: {record_separator: nul}
parse: {type: kv, separator: '='}
`,
			input: "a=one\ntwo\x00b=three\x00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := streamAll(t, tt.def, tt.input)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			if want := batchAll(t, tt.def, tt.input); got != want {
				t.Errorf("stream:\n%s\nwhole document:\n%s", got, want)
			}
		})
	}
}

const streamTable = `format: 1
command: t
variant: v
parse: {type: table, header: {none: true, columns: [n]}}
fields: {n: {type: int}}
`

// The records already written stay written: a stream is a sequence of
// complete documents, not one document that can be abandoned half way.
func TestStreamKeepsWhatItAlreadyWrote(t *testing.T) {
	t.Parallel()
	got, err := streamAll(t, streamTable, "1\n2\nnot-a-number\n4\n")
	if err == nil {
		t.Fatal("expected a parse failure")
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line != 3 {
		t.Errorf("error = %v", err)
	}
	if got != "{\"n\":1}\n{\"n\":2}\n" {
		t.Errorf("records before the failure = %q", got)
	}
}

func TestStreamRefusesFormatsWithoutRecords(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"format: 1\ncommand: t\nvariant: v\nparse: {type: kv, as: map}\n",
		"format: 1\ncommand: t\nvariant: v\nparse: {type: regex, each: input, pattern: '(?P<a>.+)'}\n",
		"format: 1\ncommand: t\nvariant: v\nparse: {type: ini}\n",
	} {
		_, err := streamAll(t, src, "a=1\n")
		var ns *NoStreamError
		if !errors.As(err, &ns) {
			t.Errorf("%s: got %v", src, err)
		}
	}
}

// A producer with no separators in its output must not be able to make jz
// hold an unbounded record.
func TestStreamBoundsOneRecord(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 300)
	var out bytes.Buffer
	err := Stream(load(t, streamTable), strings.NewReader("1\n"+long+"\n"), Options{MaxLineLength: 100}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	})
	if !errors.Is(err, ErrLineTooLong) {
		t.Errorf("err = %v", err)
	}
	if out.String() != "{\"n\":1}\n" {
		t.Errorf("records before the limit = %q", out.String())
	}
}

// An emit that fails stops the read rather than being ignored: the CLI
// uses it to report a key the format does not produce.
func TestStreamStopsWhenEmitFails(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	seen := 0
	err := Stream(load(t, streamTable), strings.NewReader("1\n2\n3\n"), Options{}, func(any) error {
		seen++
		return boom
	})
	if !errors.Is(err, boom) || seen != 1 {
		t.Errorf("err=%v seen=%d", err, seen)
	}
}

func TestStreamEmptyInput(t *testing.T) {
	t.Parallel()
	got, err := streamAll(t, streamTable, "")
	if err != nil || got != "" {
		t.Errorf("got %q, %v", got, err)
	}
	// A reader that fails part way through is reported, not silently
	// treated as the end of the input.
	err = Stream(load(t, streamTable), io.MultiReader(strings.NewReader("1\n"), errReader{}), Options{}, func(any) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("err = %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A read that fails part way through a record leaves that record out:
// what arrived of it is where the input stopped, not where the record
// ends. That holds for the line the failure fell in and for a record of
// several lines the failure left open.
func TestStreamLeavesOutTheRecordAFailedReadCut(t *testing.T) {
	t.Parallel()
	blocks := `format: 1
command: t
variant: v
parse:
  type: records
  start: '^# '
  parts:
    - name: head
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^# (?P<name>\S+)$'}
    - name: values
      select: {skip: 1}
      parse: {type: kv, separator: '='}
`
	tests := []struct {
		name, def, input, want string
	}{
		{"a line", streamTable, "1\n2\n3", "{\"n\":1}\n{\"n\":2}\n"},
		{"a line cut after its separator", streamTable, "1\n2\n", "{\"n\":1}\n{\"n\":2}\n"},
		{"a record of several lines", blocks, "# one\na=1\n# two\nb=2\n", "{\"head\":{\"name\":\"one\"},\"values\":[{\"name\":\"a\",\"value\":\"1\"}]}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := Stream(load(t, tt.def), io.MultiReader(strings.NewReader(tt.input), errReader{}), Options{}, func(v any) error {
				return jsonutil.Encode(&out, v, false)
			})
			if err == nil || !strings.Contains(err.Error(), "broken") {
				t.Errorf("err = %v", err)
			}
			if out.String() != tt.want {
				t.Errorf("records = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

// A record jz cannot read ends the read with that failure. The records
// handed over before it stand, since a stream is written as it is read,
// and the ones after it are not read: a stream with a gap in it is an
// answer nobody can check against the input.
func TestStreamEndsAtAnUnreadableRecord(t *testing.T) {
	t.Parallel()
	out, err := streamAll(t, streamTable, "1\nnot-a-number\n3\n")
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line != 2 {
		t.Fatalf("err = %v", err)
	}
	if out != "{\"n\":1}\n" {
		t.Errorf("records = %q", out)
	}
}

// A csv line with a quote that cannot open a quoted value (one in the
// middle of a value) is a bad record of its own. Counting quotes took it
// for a value that goes on to the next line and swallowed every line
// after it, so the failure has to be reported against the line it is on
// and the records before it have to stand.
func TestStreamCSVBadQuoteIsItsOwnRecord(t *testing.T) {
	t.Parallel()
	const def = "format: 1\ncommand: t\nvariant: v\nparse: {type: csv}\n"
	tests := []struct {
		name, input, want string
		bad               int
	}{
		{"a bare quote", "a,b\n1,x\"y\n2,z\n3,w\n", "", 2},
		{"text after a closing quote", "a,b\n1,\"x\"y\n2,z\n", "", 2},
		// A quote that opens a value does hold the next line.
		{"a value over two lines", "a,b\n1,\"x\ny\"\n2,z\n", "{\"a\":\"1\",\"b\":\"x\\ny\"}\n{\"a\":\"2\",\"b\":\"z\"}\n", 0},
		{"a doubled quote inside a value", "a,b\n1,\"say \"\"hi\n\"\"\"\n2,z\n", "{\"a\":\"1\",\"b\":\"say \\\"hi\\n\\\"\"}\n{\"a\":\"2\",\"b\":\"z\"}\n", 0},
		{"an empty quoted value", "a,b\n1,\"\"\n2,z\n", "{\"a\":\"1\",\"b\":\"\"}\n{\"a\":\"2\",\"b\":\"z\"}\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := streamAll(t, def, tt.input)
			var pe *ParseError
			switch {
			case tt.bad == 0 && err != nil:
				t.Fatalf("err = %v", err)
			case tt.bad > 0 && (!errors.As(err, &pe) || pe.Line != tt.bad):
				t.Fatalf("err = %v, want a failure on line %d", err, tt.bad)
			}
			if out != tt.want {
				t.Errorf("records = %q, want %q", out, tt.want)
			}
		})
	}
}

// A stream ends with the failure it has, and each kind keeps its own: a
// format with no streaming form, an emit that fails, and a record over
// the length limit are not records that could not be read.
func TestStreamKeepsTheKindOfFailure(t *testing.T) {
	t.Parallel()
	_, err := streamAll(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: kv, as: map}\n", "a=1\n")
	var ns *NoStreamError
	if !errors.As(err, &ns) {
		t.Errorf("no streaming form: got %v", err)
	}

	boom := errors.New("boom")
	err = Stream(load(t, streamTable), strings.NewReader("1\n2\n"), Options{}, func(any) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("emit failure: got %v", err)
	}

	long := strings.Repeat("x", 300)
	err = Stream(load(t, streamTable), strings.NewReader("1\n"+long+"\n"), Options{MaxLineLength: 100}, func(any) error { return nil })
	if !errors.Is(err, ErrLineTooLong) {
		t.Errorf("record over the limit: got %v", err)
	}
}

// A header the definition cannot read ends the stream. No row after it
// can be cut without knowing where the columns are, and the reader used
// to take each of those rows for another header: the same failure was
// reported once per line, naming a row's number and quoting the row as
// though it were the header.
func TestStreamStopsAtAHeaderItCannotRead(t *testing.T) {
	t.Parallel()
	src := "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {columns: [a, b, c]}}\n"
	out, err := streamAll(t, src, "NAME AGE\nbob 30\nann 40\n")
	if err == nil || !strings.Contains(err.Error(), `line 1: header has 2 columns but the definition declares 3`) {
		t.Fatalf("err = %v", err)
	}
	if out != "" {
		t.Errorf("records written: %q", out)
	}
}

// The length limit counts the bytes between two separators as they were
// read, and the last record of the input is held to it whether or not a
// separator follows it, in a stream as in a whole document.
func TestLineLimitIsTheSameBothWays(t *testing.T) {
	t.Parallel()
	for _, sep := range []string{"newline", "nul"} {
		def := load(t, "format: 1\ncommand: t\nvariant: v\ninput: {record_separator: "+sep+", skip_blank: false}\nparse: {type: regex, pattern: '(?s)(?P<x>.*)'}\n")
		end := "\n"
		if sep == "nul" {
			end = "\x00"
		}
		for _, tc := range []struct {
			text string
			ok   bool
		}{
			{"abcd" + end, true},
			{"abcd", true},
			{"abcde" + end, false},
			{"abcde", false},
			{"ab" + end + "abcde", false},
			{"ab" + end + "abcde" + end, false},
			// The carriage return of a CRLF ending and an escape sequence
			// are bytes of the record as it was read.
			{"abc\r" + end, true},
			{"abcd\r" + end, false},
			{"a\x1b[0m" + end, false},
		} {
			_, werr := Parse(def, []byte(tc.text), Options{MaxLineLength: 4})
			serr := Stream(def, strings.NewReader(tc.text), Options{MaxLineLength: 4}, func(any) error { return nil })
			if (werr == nil) != tc.ok || (serr == nil) != tc.ok {
				t.Errorf("%s %q: whole %v, stream %v, want ok=%v", sep, tc.text, werr, serr, tc.ok)
			}
			if werr != nil && !errors.Is(werr, ErrLineTooLong) || serr != nil && !errors.Is(serr, ErrLineTooLong) {
				t.Errorf("%s %q: whole %v, stream %v: not the line limit", sep, tc.text, werr, serr)
			}
		}
	}
}

// A blank line a definition keeps is still a line input.ignore may name,
// in a stream as in a whole document.
func TestStreamHeldBlankLinesPassThroughIgnore(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\ninput: {skip_blank: false, ignore: ['^$']}\nparse: {type: regex, pattern: '(?P<x>.+)'}\n")
	const in = "a\n\nb\n\n\nc\n"
	whole, err := Parse(def, []byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var streamed []any
	if err := Stream(def, strings.NewReader(in), Options{}, func(v any) error {
		streamed = append(streamed, v)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if a, b := mustJSON(t, whole), mustJSON(t, streamed); a != b || a != `[{"x":"a"},{"x":"b"},{"x":"c"}]` {
		t.Errorf("whole %s, stream %s", a, b)
	}
}

// What a stream holds back while it waits for a record to end is bounded
// the way a whole document is: a fold that never stops, a block that
// never closes, a node with endless children, a quote that never closes,
// a composite part whose region never ends. Each stops at the bound,
// and the error is not one a record could be skipped over.
func TestStreamBoundsWhatItHolds(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, def, in string }{
		{"fold", "input: {fold: '^ '}\nparse: {type: regex, pattern: '(?P<x>.*)'}\n", "abc\n" + strings.Repeat(" def\n", 100)},
		{"records", "parse: {type: records, start: '^start', parts: [{name: p, parse: {type: kv}}]}\n", "start=1\n" + strings.Repeat("k=v\n", 100)},
		{"tree", "parse: {type: tree, indent: ' ', node: {parse: {type: regex, pattern: '(?P<x>.*)'}}}\n", "root\n" + strings.Repeat(" leaf\n", 100)},
		{"csv", "parse: {type: csv}\n", "x\n\"open\n" + strings.Repeat("more\n", 100)},
		{"csv part", "parse: {type: composite, parts: [{name: p, parse: {type: csv}}]}\n", "x\n\"open\n" + strings.Repeat("more\n", 100)},
		{"single part", "parse: {type: composite, parts: [{name: p, parse: {type: kv, as: map}}]}\n", strings.Repeat("k=v\n", 100)},
		{"box", "parse: {type: table, split: box}\n", "+---+\n" + strings.Repeat("| a |\n", 100)},
		{"blank lines kept", "input: {skip_blank: false}\nparse: {type: regex, pattern: '(?P<x>.*)'}\n", "a\n" + strings.Repeat("\n", 300)},
	}
	for _, tc := range cases {
		def := load(t, "format: 1\ncommand: t\nvariant: v\n"+tc.def)
		var got int
		err := Stream(def, strings.NewReader(tc.in), Options{MaxInputSize: 200}, func(any) error {
			got++
			return nil
		})
		if err == nil || !errors.Is(err, ErrInputTooLarge) || !strings.Contains(err.Error(), "exceeds 200 bytes") {
			t.Errorf("%s: %v", tc.name, err)
		}
		// The same text is within a larger bound: whatever else it is,
		// it is not the hold that refuses it.
		if err := Stream(def, strings.NewReader(tc.in), Options{MaxInputSize: 100000}, func(any) error { return nil }); errors.Is(err, ErrInputTooLarge) {
			t.Errorf("%s within the bound: %v", tc.name, err)
		}
	}
	// What is released is given back: many small records in a row do not
	// add up to the bound.
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: records, start: '^start', parts: [{name: p, parse: {type: kv}}]}\n")
	n := 0
	if err := Stream(def, strings.NewReader(strings.Repeat("start=1\nk=v\n", 100)), Options{MaxInputSize: 40}, func(any) error {
		n++
		return nil
	}); err != nil || n != 100 {
		t.Errorf("released records: %d, %v", n, err)
	}
}

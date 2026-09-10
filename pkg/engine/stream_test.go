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
	}, nil)
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
		"format: 1\ncommand: t\nvariant: v\nparse: {type: composite, parts: [{name: p, parse: {type: kv}}]}\n",
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
	}, nil)
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
	}, nil)
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
	err = Stream(load(t, streamTable), io.MultiReader(strings.NewReader("1\n"), errReader{}), Options{}, func(any) error { return nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("err = %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

// With an onError that returns nil, a record jz cannot read is reported
// and the ones after it are still read. It is what --stream needs from a
// command that keeps printing: one unreadable line must not end the
// stream.
func TestStreamContinuesPastAnUnreadableRecord(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	var reported []*ParseError
	err := Stream(load(t, streamTable), strings.NewReader("1\nnot-a-number\n3\n"), Options{}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	}, func(pe *ParseError) error {
		reported = append(reported, pe)
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.String() != "{\"n\":1}\n{\"n\":3}\n" {
		t.Errorf("records = %q", out.String())
	}
	if len(reported) != 1 || reported[0].Line != 2 {
		t.Fatalf("reported = %v", reported)
	}
}

// An onError that returns an error stops the read there, which is how a
// caller asks for the batch behaviour while still seeing the record that
// failed.
func TestStreamStopsWhenOnErrorRefuses(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	stop := errors.New("stop")
	calls := 0
	err := Stream(load(t, streamTable), strings.NewReader("1\nx\ny\n3\n"), Options{}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	}, func(*ParseError) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("err=%v calls=%d", err, calls)
	}
	if out.String() != "{\"n\":1}\n" {
		t.Errorf("records = %q", out.String())
	}
}

// onError sees only records jz could not read. A format with no
// streaming form, an emit that fails and a record over the length limit
// are not records to skip, so they end the stream whatever onError says.
func TestStreamOnErrorDoesNotSwallowTheOtherFailures(t *testing.T) {
	t.Parallel()
	keepGoing := func(*ParseError) error { return nil }

	_, err := StreamCollect(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: kv, as: map}\n", "a=1\n", keepGoing)
	var ns *NoStreamError
	if !errors.As(err, &ns) {
		t.Errorf("no streaming form: got %v", err)
	}

	boom := errors.New("boom")
	err = Stream(load(t, streamTable), strings.NewReader("1\n2\n"), Options{}, func(any) error { return boom }, keepGoing)
	if !errors.Is(err, boom) {
		t.Errorf("emit failure: got %v", err)
	}

	long := strings.Repeat("x", 300)
	err = Stream(load(t, streamTable), strings.NewReader("1\n"+long+"\n"), Options{MaxLineLength: 100}, func(any) error { return nil }, keepGoing)
	if !errors.Is(err, ErrLineTooLong) {
		t.Errorf("record over the limit: got %v", err)
	}
}

// StreamCollect runs Stream with an onError of the caller's choosing.
func StreamCollect(t *testing.T, src, input string, onError func(*ParseError) error) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Stream(load(t, src), strings.NewReader(input), Options{}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	}, onError)
	return out.String(), err
}

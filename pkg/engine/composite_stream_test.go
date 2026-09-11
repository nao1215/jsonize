package engine

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// A header, a line per event and a summary: the shape ping prints, which
// is what a composite stream is for.
const pingLike = `format: 1
command: t
variant: v
parse:
  type: composite
  parts:
    - name: destination
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '\APING (?P<name>\S+)\z'}
    - name: replies
      select: {after: '^PING ', until: '^--- '}
      parse:
        type: regex
        patterns:
          - '^reply seq=(?P<seq>\d+) time=(?P<time_ms>[\d.]+)$'
          - '^From (?P<from>\S+) seq=(?P<seq>\d+) (?P<error>.+)$'
      fields:
        seq: {type: int}
        time_ms: {type: float}
    - name: statistics
      select: {after: '^--- \S+ statistics ---$'}
      parse: {type: regex, each: input, pattern: '\A(?P<sent>\d+) sent, (?P<received>\d+) received\z'}
      fields:
        sent: {type: int}
        received: {type: int}
`

// collect streams input and returns the documents one per line, and what
// onError was handed.
func collect(t *testing.T, src string, r io.Reader) (string, []*ParseError, error) {
	t.Helper()
	var out bytes.Buffer
	var bad []*ParseError
	err := Stream(load(t, src), r, Options{}, func(v any) error {
		return jsonutil.Encode(&out, v, false)
	}, func(pe *ParseError) error {
		bad = append(bad, pe)
		return nil
	})
	return out.String(), bad, err
}

func TestCompositeStreamsPartByPart(t *testing.T) {
	t.Parallel()
	input := "PING host\nreply seq=1 time=0.5\nFrom gw seq=2 Destination Host Unreachable\nreply seq=3 time=1.25\n--- host statistics ---\n3 sent, 2 received\n"
	got, bad, err := collect(t, pingLike, strings.NewReader(input))
	if err != nil || len(bad) > 0 {
		t.Fatalf("err=%v bad=%v", err, bad)
	}
	want := `{"part":"destination","value":{"name":"host"}}
{"part":"replies","value":{"seq":1,"time_ms":0.5}}
{"part":"replies","value":{"from":"gw","seq":2,"error":"Destination Host Unreachable"}}
{"part":"replies","value":{"seq":3,"time_ms":1.25}}
{"part":"statistics","value":{"sent":3,"received":2}}
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// The whole document is the same values, one key per part.
	whole, err := Parse(load(t, pingLike), []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s := mustJSON(t, whole); s != `{"destination":{"name":"host"},"replies":[{"seq":1,"time_ms":0.5},{"from":"gw","seq":2,"error":"Destination Host Unreachable"},{"seq":3,"time_ms":1.25}],"statistics":{"sent":3,"received":2}}` {
		t.Errorf("whole: %s", s)
	}
}

// The documents of a stream are written when their lines have come, not
// when the input ends: the reader here hands over one line and then
// blocks until the test has seen the record it should have produced.
func TestCompositeStreamWritesBeforeTheInputEnds(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	docs := make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		done <- Stream(load(t, pingLike), pr, Options{}, func(v any) error {
			var b bytes.Buffer
			if err := jsonutil.Encode(&b, v, false); err != nil {
				return err
			}
			docs <- b.String()
			return nil
		}, nil)
	}()
	steps := []struct{ send, want string }{
		{"PING host\n", `{"part":"destination","value":{"name":"host"}}` + "\n"},
		{"reply seq=1 time=0.5\n", `{"part":"replies","value":{"seq":1,"time_ms":0.5}}` + "\n"},
		{"reply seq=2 time=0.7\n", `{"part":"replies","value":{"seq":2,"time_ms":0.7}}` + "\n"},
	}
	for _, st := range steps {
		if _, err := io.WriteString(pw, st.send); err != nil {
			t.Fatal(err)
		}
		if got := <-docs; got != st.want {
			t.Fatalf("after %q got %s want %s", st.send, got, st.want)
		}
	}
	// The summary is one value, so it waits for the end of its region.
	if _, err := io.WriteString(pw, "--- host statistics ---\n2 sent, 2 received\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-docs:
		t.Fatalf("the summary was written before its region ended: %s", got)
	default:
	}
	pw.Close()
	if got := <-docs; got != `{"part":"statistics","value":{"sent":2,"received":2}}`+"\n" {
		t.Errorf("summary: %s", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A line no part reads is reported where it is and the stream goes on; a
// line inside a single part's region that its pattern does not reach is
// reported once that part has been read.
func TestCompositeStreamReportsWhatNoPartReads(t *testing.T) {
	t.Parallel()
	input := "PING host\nreply seq=1 time=0.5\ngarbage\nreply seq=2 time=0.7\n--- host statistics ---\n2 sent, 2 received\n"
	got, bad, err := collect(t, pingLike, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, `"part":"replies"`) != 2 || !strings.Contains(got, `"part":"statistics"`) {
		t.Errorf("records around the bad line were lost:\n%s", got)
	}
	if len(bad) != 1 || bad[0].Line != 3 {
		t.Fatalf("reported %v, want line 3", bad)
	}
	// A line inside the summary's region that its pattern does not
	// reach fails the part, once, the way it fails the whole document.
	_, bad, err = collect(t, pingLike, strings.NewReader("PING host\n--- host statistics ---\n1 sent, 0 received\ntrailer\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || !strings.Contains(bad[0].Error(), `part "statistics"`) {
		t.Fatalf("reported %v, want the summary part", bad)
	}
	// A line that is in no part's region is unread wherever it is.
	const twoParts = "format: 1\ncommand: t\nvariant: v\nparse:\n  type: composite\n  parts:\n    - name: head\n      select: {limit: 1}\n      parse: {type: regex, each: input, pattern: '\\A# (?P<t>.+)\\z'}\n    - name: rows\n      select: {after: '^---$'}\n      parse: {type: regex, pattern: '^(?P<n>\\d+)$'}\n"
	got, bad, err = collect(t, twoParts, strings.NewReader("# title\nstray\n---\n1\n2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || !errors.Is(bad[0], ErrUnread) || bad[0].Line != 2 || strings.Count(got, `"part":"rows"`) != 2 {
		t.Fatalf("got %s, reported %v, want line 2 unread", got, bad)
	}
}

// A summary that never comes is what the whole document refuses too: at
// the end the part is read with the lines it got, none, and fails.
func TestCompositeStreamReadsAnEmptySinglePartAtTheEnd(t *testing.T) {
	t.Parallel()
	got, bad, err := collect(t, pingLike, strings.NewReader("PING host\nreply seq=1 time=0.5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "\n") != 2 {
		t.Errorf("records: %s", got)
	}
	if len(bad) != 1 || !strings.Contains(bad[0].Error(), `part "statistics"`) {
		t.Errorf("reported %v", bad)
	}
	if _, err := Parse(load(t, pingLike), []byte("PING host\nreply seq=1 time=0.5\n"), Options{}); err == nil {
		t.Error("the whole document read a text with no summary")
	}
}

// A read that fails ends the stream with the records before it, and the
// single parts still open are not read: their lines never all came.
func TestCompositeStreamCutShort(t *testing.T) {
	t.Parallel()
	got, bad, err := collect(t, pingLike, io.MultiReader(strings.NewReader("PING host\nreply seq=1 time=0.5\n--- host statistics ---\n1 sent"), errReader{}))
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(got, "statistics") || strings.Count(got, "\n") != 2 || len(bad) > 0 {
		t.Errorf("got %s, bad %v", got, bad)
	}
}

// A list part whose records close at the next start line hands over its
// last record when its region ends, not when the input does: here the
// blocks end at "--- end", and the summary after them is still coming.
func TestCompositeStreamClosesAListPartAtTheEndOfItsRegion(t *testing.T) {
	t.Parallel()
	const def = `format: 1
command: t
variant: v
parse:
  type: composite
  parts:
    - name: blocks
      select: {until: '^--- end$'}
      parse:
        type: records
        start: '^# '
        parts:
          - name: head
            select: {limit: 1}
            parse: {type: regex, each: input, pattern: '\A# (?P<name>\S+)\z'}
          - name: values
            select: {skip: 1}
            parse: {type: kv}
    - name: summary
      select: {after: '^--- end$'}
      parse: {type: regex, pattern: '^total (?P<n>\d+)$'}
`
	pr, pw := io.Pipe()
	docs := make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		done <- Stream(load(t, def), pr, Options{}, func(v any) error {
			docs <- mustJSON(t, v)
			return nil
		}, nil)
	}()
	if _, err := io.WriteString(pw, "# one\na=1\n# two\nb=2\n"); err != nil {
		t.Fatal(err)
	}
	if got := <-docs; got != `{"part":"blocks","value":{"head":{"name":"one"},"values":[{"name":"a","value":"1"}]}}` {
		t.Fatalf("first block: %s", got)
	}
	if _, err := io.WriteString(pw, "--- end\n"); err != nil {
		t.Fatal(err)
	}
	if got := <-docs; got != `{"part":"blocks","value":{"head":{"name":"two"},"values":[{"name":"b","value":"2"}]}}` {
		t.Fatalf("the last block waited for the end of the input: %s", got)
	}
	if _, err := io.WriteString(pw, "total 2\n"); err != nil {
		t.Fatal(err)
	}
	if got := <-docs; got != `{"part":"summary","value":{"n":"2"}}` {
		t.Fatalf("summary: %s", got)
	}
	pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

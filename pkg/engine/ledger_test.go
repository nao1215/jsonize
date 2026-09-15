package engine

import (
	"errors"
	"strings"
	"testing"
)

// unread parses input and returns the unread report, failing the test
// when the reading succeeded or failed for another reason.
func unread(t *testing.T, src, input string) (*UnreadError, *ParseError) {
	t.Helper()
	v, err := Parse(load(t, src), []byte(input), Options{})
	if err == nil {
		t.Fatalf("the reading succeeded with text left unread: %s", mustJSON(t, v))
	}
	var ue *UnreadError
	var pe *ParseError
	if !errors.As(err, &ue) || !errors.As(err, &pe) || !errors.Is(err, ErrUnread) {
		t.Fatalf("want an unread report, got %v", err)
	}
	return ue, pe
}

// A composite reads what its parts read. Two documents one after the
// other are the case this was built for: every part finds what it needs
// in the first, and without the accounting the second vanished and the
// answer was the first on its own at exit 0.
const reply = `format: 1
command: t
variant: v
parse:
  type: composite
  parts:
    - name: header
      parse: {type: regex, each: input, pattern: 'status: (?P<status>\w+)'}
    - name: answers
      select: {after: '^ANSWER:$', until: '^END$'}
      parse: {type: table, header: {none: true, columns: [name, value]}}
    - name: footer
      select: {after: '^END$'}
      parse: {type: regex, pattern: '^took (?P<ms>\d+) ms$'}
`

const oneReply = "status: ok\nANSWER:\na 1\nb 2\nEND\ntook 3 ms\n"

func TestCompositeReadsEverything(t *testing.T) {
	t.Parallel()
	got, err := Parse(load(t, reply), []byte(oneReply), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"header":{"status":"ok"},"answers":[{"name":"a","value":"1"},{"name":"b","value":"2"}],"footer":[{"ms":"3"}]}`
	if mustJSON(t, got) != want {
		t.Errorf("got %s", mustJSON(t, got))
	}

	// The second reply starts on line 7. The header part matched the
	// first status line, the answers stopped at the first END, and the
	// footer part is where it all lands: its region runs to the end, so
	// the second reply's lines are read there and fail its pattern.
	_, err = Parse(load(t, reply), []byte(oneReply+oneReply), Options{})
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line != 7 {
		t.Fatalf("two replies: %v", err)
	}

	// With the footer's region closed, the second reply falls between
	// the parts, and that is named line by line.
	closed := strings.Replace(reply, "select: {after: '^END$'}", "select: {after: '^END$', limit: 1}", 1)
	ue, pe := unread(t, closed, oneReply+oneReply)
	if pe.Line != 7 || ue.Total != 6 || ue.Spans[0].Text != "status: ok" || len(ue.Spans) != maxUnreadReported {
		t.Errorf("two replies, closed footer: line %d, %+v", pe.Line, ue)
	}
	for _, want := range []string{"6 lines", `line 7 "status: ok"`, `line 8 "ANSWER:"`, "and 3 more"} {
		if !strings.Contains(pe.Error(), want) {
			t.Errorf("the message %q does not say %q", pe.Error(), want)
		}
	}

	// A section this definition has never seen, in the middle.
	ue, pe = unread(t, reply, "status: ok\nWARNING: odd\nANSWER:\na 1\nEND\ntook 3 ms\n")
	if pe.Line != 2 || ue.Total != 1 || !strings.Contains(pe.Error(), `a line no part of the definition read: "WARNING: odd"`) {
		t.Errorf("a line between parts: %v", pe)
	}
}

// A part's ignore list says which lines of its region belong to a
// sibling. It is a hand-over, not a way to drop a line: a line no
// sibling reads is still unread.
func TestPartIgnoreHandsOverRatherThanDrops(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
parse:
  type: composite
  parts:
    - name: pairs
      ignore: ['^#']
      parse: {type: kv, as: map}
    - name: notes
      ignore: ['=']
      parse: {type: regex, pattern: '^# (?P<note>.+)$'}
`
	got, err := Parse(load(t, src), []byte("a=1\n# first\nb=2\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"pairs":{"a":"1","b":"2"},"notes":[{"note":"first"}]}` {
		t.Errorf("a hand-over: %v %v", mustJSON(t, got), err)
	}
	// With the notes part gone, the comment is still handed over by the
	// pairs part, and now there is nobody to take it.
	onlyPairs := strings.SplitN(src, "    - name: notes", 2)[0]
	ue, pe := unread(t, onlyPairs, "a=1\n# first\nb=2\n")
	if pe.Line != 2 || ue.Total != 1 {
		t.Errorf("ignored by a part and read by nobody: %v", pe)
	}
}

// input.ignore and blank lines are the rules a definition states for
// leaving text out, and the account says which rule took which line.
func TestAccountSaysWhereEveryLineWent(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
input:
  fold: '^[ \t]+\S'
  ignore: ['^#', '^total ']
parse: {type: kv}
`
	input := "# comment\na=1\n   and more\n\ntotal 2\n# again\nb=2\n\n"
	v, acct, err := ParseAccounted(load(t, src), []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, v) != `[{"name":"a","value":"1 and more"},{"name":"b","value":"2"}]` {
		t.Errorf("got %s", mustJSON(t, v))
	}
	if acct.Lines != 8 || acct.Read != 2 || acct.Folded != 1 || acct.Blank != 2 {
		t.Errorf("account %+v", acct)
	}
	if len(acct.Ignored) != 2 || acct.Ignored[0] != (Ignored{Index: 0, Expr: "^#", Lines: 2}) || acct.Ignored[1] != (Ignored{Index: 1, Expr: "^total ", Lines: 1}) {
		t.Errorf("ignored %+v", acct.Ignored)
	}
	if n := acct.Read + acct.Folded + acct.Blank + acct.Ignored[0].Lines + acct.Ignored[1].Lines; n != acct.Lines {
		t.Errorf("the account covers %d of %d lines", n, acct.Lines)
	}
}

// A definition that keeps blank lines (skip_blank: false) hands them to
// the parser. One nobody asked for is still not text anyone could miss.
func TestBlankLinesAreNeverUnread(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
input:
  skip_blank: false
parse:
  type: composite
  parts:
    - name: first
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^(?P<v>\S+)$'}
`
	v, acct, err := ParseAccounted(load(t, src), []byte("x\n\n  \n\t\n"), Options{})
	if err != nil || mustJSON(t, v) != `{"first":{"v":"x"}}` || acct.Blank != 3 || acct.Read != 1 {
		t.Errorf("%v %+v %v", mustJSON(t, v), acct, err)
	}
	_, pe := unread(t, src, "x\n\ny\n")
	if pe.Line != 3 {
		t.Errorf("want line 3, got %v", pe)
	}
}

// skip_blank: false keeps the blank lines that separate things, and
// nothing is separated from what comes before the first line or after
// the last: an empty line there is not part of the text, whole or
// streamed. A line of spaces is, since where blank lines are kept it can
// be a value.
func TestBlankLinesAroundTheTextAreSkipped(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
input:
  skip_blank: false
parse:
  type: records
  start: '^commit '
  parts:
    - name: head
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^commit (?P<id>\S+)$'}
    - name: body
      select: {skip: 1}
      parse: {type: regex, pattern: '^(?P<line>.*)$'}
`
	const input = "\n\ncommit a\n\nmsg\n\n  \n\n\n"
	want := `[{"head":{"id":"a"},"body":[{"line":""},{"line":"msg"},{"line":""},{"line":"  "}]}]`
	v, acct, err := ParseAccounted(load(t, src), []byte(input), Options{})
	if err != nil || mustJSON(t, v) != want || acct.Blank != 4 {
		t.Errorf("whole: %v %+v %v", mustJSON(t, v), acct, err)
	}
	got, err := streamAll(t, src, input)
	if err != nil || got != want[1:len(want)-1]+"\n" {
		t.Errorf("stream: %q %v", got, err)
	}
	// A line of spaces before the text is text, and here it comes before
	// the first record, which is refused the same way in both readings.
	for _, read := range []func() error{
		func() error { _, err := Parse(load(t, src), []byte("  \ncommit a\n"), Options{}); return err },
		func() error { _, err := streamAll(t, src, "  \ncommit a\n"); return err },
	} {
		if err := read(); err == nil || !strings.Contains(err.Error(), "line 1") {
			t.Errorf("a line of spaces before the first record: %v", err)
		}
	}
}

// A heading select.after names counts as read only when the expression
// states the whole line. One that states how the line opens leaves the
// rest of it, which is where a value such as an interface name sits.
func TestHeadingIsReadOnlyWhenStatedWhole(t *testing.T) {
	t.Parallel()
	const tmpl = `format: 1
command: t
variant: v
input:
  select: {after: 'AFTER'}
parse: {type: regex, pattern: '^(?P<k>\w+): (?P<v>\w+)$'}
`
	whole := strings.Replace(tmpl, "AFTER", `^Features for \S+:$`, 1)
	got, err := Parse(load(t, whole), []byte("Features for lo:\na: on\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"k":"a","v":"on"}]` {
		t.Errorf("a heading stated whole: %v %v", mustJSON(t, got), err)
	}
	opening := strings.Replace(tmpl, "AFTER", `^Features for `, 1)
	ue, pe := unread(t, opening, "Features for lo:\na: on\n")
	if pe.Line != 1 || ue.Spans[0].Text != "Features for lo:" {
		t.Errorf("a heading stated by how it opens: %v", pe)
	}
	// Surrounding whitespace is not part of what has to be stated.
	unanchored := strings.Replace(tmpl, "AFTER", `Features for \S+:`, 1)
	got, err = Parse(load(t, unanchored), []byte("  Features for lo:  \na: on\n"), Options{})
	if err != nil {
		t.Errorf("a heading with surrounding whitespace: %v %v", mustJSON(t, got), err)
	}
}

// The line top-level select.until matches closes the input, and counts
// as read when the expression states the whole of it, as a heading does.
// A line after it is another command's and is unread, in a whole
// document and in a stream alike.
func TestEndIsReadOnlyWhenStatedWhole(t *testing.T) {
	t.Parallel()
	const tmpl = `format: 1
command: t
variant: v
input:
  select: {until: 'UNTIL'}
parse: {type: regex, pattern: '^(?P<k>\w+): (?P<v>\w+)$'}
`
	whole := strings.Replace(tmpl, "UNTIL", `^The command completed successfully\.$`, 1)
	const done = "a: on\nThe command completed successfully.\n\n"
	got, err := Parse(load(t, whole), []byte(done), Options{})
	if err != nil || mustJSON(t, got) != `[{"k":"a","v":"on"}]` {
		t.Errorf("a closing line stated whole: %v %v", mustJSON(t, got), err)
	}
	if got, err := streamAll(t, whole, done); err != nil || got != `{"k":"a","v":"on"}`+"\n" {
		t.Errorf("a closing line stated whole, streamed: %q %v", got, err)
	}
	const after = "a: on\nThe command completed successfully.\nb: off\n"
	ue, pe := unread(t, whole, after)
	if pe.Line != 3 || ue.Spans[0].Text != "b: off" {
		t.Errorf("a line after the closing line: %v", pe)
	}
	if _, err := streamAll(t, whole, after); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("a line after the closing line, streamed: %v", err)
	}
	// A limit reached before the closing line leaves it to until, in a
	// stream as in a whole document.
	limited := strings.Replace(whole, "select: {until:", "select: {limit: 1, until:", 1)
	const pastLimit = "a: on\n\nThe command completed successfully.\n"
	if got, err := Parse(load(t, limited), []byte(pastLimit), Options{}); err != nil || mustJSON(t, got) != `[{"k":"a","v":"on"}]` {
		t.Errorf("a closing line past the limit: %v %v", mustJSON(t, got), err)
	}
	if got, err := streamAll(t, limited, pastLimit); err != nil || got != `{"k":"a","v":"on"}`+"\n" {
		t.Errorf("a closing line past the limit, streamed: %q %v", got, err)
	}
	opening := strings.Replace(tmpl, "UNTIL", `^The command`, 1)
	ue, pe = unread(t, opening, done)
	if pe.Line != 2 || ue.Spans[0].Text != "The command completed successfully." {
		t.Errorf("a closing line stated by how it opens: %v", pe)
	}
}

// A pattern matched against one line has to reach both ends of it.
func TestPatternReadsTheWholeLine(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
parse: {type: regex, pattern: '(?P<n>\d+) packets'}
`
	got, err := Parse(load(t, src), []byte("12 packets\n  7 packets  \n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"n":"12"},{"n":"7"}]` {
		t.Errorf("whole lines: %v %v", mustJSON(t, got), err)
	}
	ue, pe := unread(t, src, "12 packets\n7 packets, 2 lost\n")
	if pe.Line != 2 || ue.Spans[0].Column != 10 || ue.Spans[0].Text != ", 2 lost" {
		t.Errorf("text after the match: %+v %v", ue.Spans, pe)
	}
	if !strings.Contains(pe.Error(), `line 2: text the pattern did not read at column 10: ", 2 lost"`) {
		t.Errorf("message: %v", pe)
	}
	ue, _ = unread(t, src, "sent 12 packets\n")
	if ue.Spans[0].Column != 1 || ue.Spans[0].Text != "sent" {
		t.Errorf("text before the match: %+v", ue.Spans)
	}
	// A tree node is one line read the same way, and so is a record the
	// streaming reader reads.
	tree := `format: 1
command: t
variant: v
parse:
  type: tree
  indent: "  "
  node: {parse: {type: regex, pattern: '^(?P<name>\w+)'}}
`
	ue, pe = unread(t, tree, "root\n  child extra\n")
	if pe.Line != 2 || ue.Spans[0].Text != "extra" {
		t.Errorf("a tree node: %v", pe)
	}
	_, err = streamAll(t, src, "12 packets\n7 packets, 2 lost\n")
	if !errors.Is(err, ErrUnread) {
		t.Errorf("stream: %v", err)
	}
}

// A pattern matched against the whole input reads the lines its match
// covers. A line it does not reach is unread, and so is the part of a
// line it starts or ends inside.
func TestPatternAgainstTheInputReadsWhatItCovers(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
parse:
  type: regex
  each: input
  pattern: 'sent (?P<sent>\d+)\nlost (?P<lost>\d+)'
`
	got, err := Parse(load(t, src), []byte("sent 3\nlost 1\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"sent":"3","lost":"1"}` {
		t.Errorf("covered: %v %v", mustJSON(t, got), err)
	}
	ue, pe := unread(t, src, "sent 3\nlost 1\nsent 4\nlost 2\n")
	if pe.Line != 3 || ue.Total != 2 {
		t.Errorf("a second copy below: %v", pe)
	}
	ue, pe = unread(t, src, "banner\nsent 3\nlost 1\n")
	if pe.Line != 1 || ue.Total != 1 {
		t.Errorf("a line above: %v", pe)
	}
	ue, pe = unread(t, src, "sent 3\nlost 1 of 4\n")
	if pe.Line != 2 || ue.Spans[0].Text != "of 4" {
		t.Errorf("the end of the last line: %v", pe)
	}
	ue, pe = unread(t, src, "we sent 3\nlost 1\n")
	if pe.Line != 1 || ue.Spans[0].Text != "we" {
		t.Errorf("the start of the first line: %v", pe)
	}
}

// A field of type object splits a value that was read whole, so what its
// pattern matches around would vanish between the value and the object.
func TestObjectFieldReadsTheWholeValue(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
parse: {type: regex, pattern: '^(?P<size>.+)$'}
fields:
  size:
    type: object
    regex: '(?P<n>\d+) (?P<unit>kB)'
    fields: {n: {type: int}}
`
	got, err := Parse(load(t, src), []byte("12 kB\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"size":{"n":12,"unit":"kB"}}]` {
		t.Errorf("%v %v", mustJSON(t, got), err)
	}
	_, err = Parse(load(t, src), []byte("12 kB (cached)\n"), Options{})
	var pe *ParseError
	if !errors.As(err, &pe) || !errors.Is(err, ErrUnread) || pe.Field != "size" || !strings.Contains(err.Error(), `"(cached)"`) {
		t.Errorf("text around the match: %v", err)
	}
}

// A drawn table takes its cells from the bars, so a line with no bar and
// no rule on it has no cells, and a cell past the header's last column
// has no name. Either would be text missing from the rows.
func TestBoxTableReadsEveryCell(t *testing.T) {
	t.Parallel()
	src := `format: 1
command: t
variant: v
parse: {type: table, split: box}
`
	table := "+---+---+\n| a | b |\n+---+---+\n| 1 | 2 |\n+---+---+\n"
	got, err := Parse(load(t, src), []byte(table), Options{})
	if err != nil || mustJSON(t, got) != `[{"a":"1","b":"2"}]` {
		t.Errorf("%v %v", mustJSON(t, got), err)
	}
	for name, input := range map[string]string{
		"a line with no cell":          "+---+---+\n| a | b |\n+---+---+\n| 1 | 2 |\nstray words\n+---+---+\n",
		"a line after the frame":       table + "stray words\n",
		"a cell past the last column":  "+---+---+\n| a | b |\n+---+---+\n| 1 | 2 | 3 |\n+---+---+\n",
		"an extra cell in a wrap line": "+---+---+\n| a | b |\n+---+---+\n| 1 | 2 |\n|   |   | 3 |\n+---+---+\n",
	} {
		if _, err := Parse(load(t, src), []byte(input), Options{}); err == nil {
			t.Errorf("%s: the table was read", name)
		}
		if _, err := streamAll(t, src, input); err == nil {
			t.Errorf("%s: the stream was read", name)
		}
	}
	// An empty cell past the last column is only the frame.
	got, err = Parse(load(t, src), []byte("+---+---+\n| a | b |\n+---+---+\n| 1 | 2 |  |\n+---+---+\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"a":"1","b":"2"}]` {
		t.Errorf("an empty extra cell: %v %v", mustJSON(t, got), err)
	}
}

// The streaming reader holds a record to the rule a whole document is
// held to, one record at a time, and a line input.select leaves out is
// unread there too.
func TestStreamAccountsForEveryLine(t *testing.T) {
	t.Parallel()
	records := `format: 1
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
      select: {skip: 1, limit: 1}
      parse: {type: kv}
`
	input := "# one\na=1\n# two\nb=2\nc=3\n# three\nd=4\n"
	var skipped []int
	var out strings.Builder
	err := Stream(load(t, records), strings.NewReader(input), Options{}, func(v any) error {
		out.WriteString(mustJSON(t, v) + "\n")
		return nil
	}, func(pe *ParseError) error {
		if !errors.Is(pe, ErrUnread) {
			return pe
		}
		skipped = append(skipped, pe.Line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The second record is refused for its third line; the ones around it
	// are written.
	if len(skipped) != 1 || skipped[0] != 5 || strings.Count(out.String(), "\n") != 2 || strings.Contains(out.String(), "two") {
		t.Errorf("skipped %v, wrote:\n%s", skipped, out.String())
	}
	if _, err := Parse(load(t, records), []byte(input), Options{}); !errors.Is(err, ErrUnread) {
		t.Errorf("the whole document: %v", err)
	}

	// A selection that closes before the end: the stream reads on and
	// reports what it left out, the way the whole document does. The
	// closing line the expression states whole is read, and the first
	// line left out is the one after it.
	sel := `format: 1
command: t
variant: v
input:
  select: {until: '^END$'}
parse: {type: regex, pattern: '^(?P<v>\S+)$'}
`
	_, err = streamAll(t, sel, "a\nb\nEND\nlater\n")
	var pe *ParseError
	if !errors.As(err, &pe) || !errors.Is(err, ErrUnread) || pe.Line != 4 {
		t.Errorf("stream past the selection: %v", err)
	}
	_, pe = unread(t, sel, "a\nb\nEND\nlater\n")
	if pe.Line != 4 {
		t.Errorf("whole document past the selection: %v", pe)
	}
	// A blank line past it is nothing.
	if got, err := streamAll(t, sel, "a\nb\n\n"); err != nil || got != "{\"v\":\"a\"}\n{\"v\":\"b\"}\n" {
		t.Errorf("blank tail: %q %v", got, err)
	}
}

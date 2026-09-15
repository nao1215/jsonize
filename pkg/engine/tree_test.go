package engine

import (
	"strings"
	"testing"
)

const treeDef = `format: 1
command: t
variant: v
parse:
  type: tree
  indent: "  "
  node:
    parse:
      type: regex
      patterns:
        - '^(?P<name>\S+): (?P<value>.*)$'
        - '^(?P<text>.+)$'
`

// The depth comes from the input and the shape from the definition: a
// node is what the node expression reads plus its children, however deep
// the input goes.
func TestParseTree(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"a: 1",
		"  b: 2",
		"    c: 3",
		"  d: 4",
		"e: 5",
		"",
	}, "\n")
	v, err := Parse(load(t, treeDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"name":"a","value":"1","children":[` +
		`{"name":"b","value":"2","children":[{"name":"c","value":"3","children":[]}]},` +
		`{"name":"d","value":"4","children":[]}]},` +
		`{"name":"e","value":"5","children":[]}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// A stream hands over one top-level node at a time, which is what a
	// node being complete means here: nothing deeper follows it.
	got, err := streamAll(t, treeDef, input)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if n := strings.Count(got, "\n"); n != 2 {
		t.Errorf("streamed %d records:\n%s", n, got)
	}
}

// A node with nothing under it has an empty children array rather than
// no key, so a consumer walks every node the same way.
func TestParseTreeEmptyChildren(t *testing.T) {
	t.Parallel()
	v, err := Parse(load(t, treeDef), []byte("a: 1\nb: 2\n"), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := mustJSON(t, v); got != `[{"name":"a","value":"1","children":[]},{"name":"b","value":"2","children":[]}]` {
		t.Errorf("got %s", got)
	}
	// Nothing at all is an empty forest, not a failure.
	v, err = Parse(load(t, treeDef), []byte(""), Options{})
	if err != nil || mustJSON(t, v) != `[]` {
		t.Errorf("empty input: %v %v", mustJSON(t, v), err)
	}
}

func TestParseTreeRefuses(t *testing.T) {
	t.Parallel()
	// A line two levels below the one above it has no parent, and
	// rounding it down would put it under the wrong one.
	for _, input := range []string{
		"a: 1\n    b: 2\n",
		"    a: 1\n",
		"a: 1\n  b: 2\n      c: 3\n",
	} {
		if v, err := Parse(load(t, treeDef), []byte(input), Options{}); err == nil {
			t.Errorf("%q was read as %s", input, mustJSON(t, v))
		}
	}
	// Indentation that is not a whole number of levels means the unit the
	// definition states is not the one the text uses.
	if _, err := Parse(load(t, treeDef), []byte("a: 1\n   b: 2\n"), Options{}); err == nil {
		t.Error("a three-space indent was read as a two-space unit")
	}
	// A stream refuses the same shapes.
	if _, err := streamAll(t, treeDef, "a: 1\n    b: 2\n"); err == nil {
		t.Error("a stream accepted a line with no parent")
	}
	if _, err := streamAll(t, treeDef, "  a: 1\n"); err == nil {
		t.Error("a stream accepted an indented first line")
	}
}

const treeRootDef = `format: 1
command: t
variant: v
parse:
  type: tree
  indent: "  "
  root: '^[0-9a-f]{2}:[0-9a-f]{2}\.[0-9] '
  node:
    parse:
      type: regex
      patterns:
        - '^(?P<name>\S+): (?P<value>.*)$'
        - '^(?P<text>.+)$'
`

// A definition that states what a top-level line looks like has a line
// of any other shape at depth zero refused, rather than read as a root
// by the node pattern that reads any text. A line printed after the
// report, or another command's output piped in behind it, is what such
// a line is.
func TestParseTreeRoot(t *testing.T) {
	t.Parallel()
	input := "00:1f.0 ISA bridge\n  Flags: bus master\n00:1f.3 Audio device\n  Flags: fast devsel\n"
	v, err := Parse(load(t, treeRootDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"text":"00:1f.0 ISA bridge","children":[{"name":"Flags","value":"bus master","children":[]}]},` +
		`{"text":"00:1f.3 Audio device","children":[{"name":"Flags","value":"fast devsel","children":[]}]}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	for _, in := range []string{
		input + "done\n",
		"done\n" + input,
		input + "Flags: bus master\n",
	} {
		if v, err := Parse(load(t, treeRootDef), []byte(in), Options{}); err == nil {
			t.Errorf("%q was read as %s", in, mustJSON(t, v))
		} else if !strings.Contains(err.Error(), "does not match root") {
			t.Errorf("%q: %v", in, err)
		}
	}
	// A child line is not held to the root expression.
	if _, err := Parse(load(t, treeRootDef), []byte("00:1f.0 ISA bridge\n  done\n"), Options{}); err != nil {
		t.Errorf("a child of any shape was refused: %v", err)
	}
	// A stream refuses the same line, and what came before it is not
	// written as if the report had ended there.
	if _, err := streamAll(t, treeRootDef, input+"done\n"); err == nil {
		t.Error("a stream accepted a top-level line that does not match root")
	}
	// Without root, the same line is a root of its own, which is what
	// the node pattern says.
	if _, err := Parse(load(t, treeDef), []byte("a: 1\ndone\n"), Options{}); err != nil {
		t.Errorf("a tree without root refused a top-level line: %v", err)
	}
}

const treeBranchDef = `format: 1
command: t
variant: v
parse:
  type: tree
  indent: ["| ", "|-", "  ", "` + "`" + `-"]
  node:
    parse:
      type: regex
      patterns:
        - '^ *(?P<pid>[0-9]+) (?P<command>.+)$'
        - '^(?P<name>\S+)$'
`

// A branch drawn to a node ends the indentation: nothing deeper is drawn
// after it on the same line, so the spaces a report pads a right-aligned
// number with after the branch are the node's text, not a level. Units
// that are only blank still count, and a blank that is not a whole level
// before any branch is still refused.
func TestParseTreeBranchEndsIndentation(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"-.slice",
		"|-user.slice",
		"| |-  987 /usr/bin/a",
		"| `-12345 /usr/bin/b --flag",
		"`-system.slice",
		"  `-cron.service",
		"    `-   42 /usr/sbin/cron -f",
		"",
	}, "\n")
	v, err := Parse(load(t, treeBranchDef), []byte(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"name":"-.slice","children":[` +
		`{"name":"user.slice","children":[` +
		`{"pid":"987","command":"/usr/bin/a","children":[]},` +
		`{"pid":"12345","command":"/usr/bin/b --flag","children":[]}]},` +
		`{"name":"system.slice","children":[` +
		`{"name":"cron.service","children":[` +
		`{"pid":"42","command":"/usr/sbin/cron -f","children":[]}]}]}]}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// Two spaces after a branch are not a second level: the node stays
	// where the branch put it.
	if _, err := Parse(load(t, treeBranchDef), []byte("a\n|-  b\n"), Options{}); err == nil {
		t.Error("a node pattern that takes no leading blank read one")
	}
	// A blank that is not a whole number of levels, with no branch before
	// it, is still refused.
	if _, err := Parse(load(t, treeBranchDef), []byte("a\n   b\n"), Options{}); err == nil {
		t.Error("three spaces were read as a level of two")
	}
	if _, err := streamAll(t, treeBranchDef, input); err != nil {
		t.Errorf("stream: %v", err)
	}
}

// A producer cannot make jz build an unbounded stack of objects.
func TestParseTreeBoundsDepth(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range 40 {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("a: 1\n")
	}
	_, err := Parse(load(t, treeDef), []byte(b.String()), Options{})
	if err == nil || !strings.Contains(err.Error(), "nested deeper than 32") {
		t.Errorf("err = %v", err)
	}
	// Exactly at the limit is still read.
	b.Reset()
	for i := range 33 {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("a: 1\n")
	}
	if _, err := Parse(load(t, treeDef), []byte(b.String()), Options{}); err != nil {
		t.Errorf("32 levels below the top: %v", err)
	}
}

// A node may also be read as a key and a value, which is the shape of a
// report that indents its settings.
func TestParseTreeKVNode(t *testing.T) {
	t.Parallel()
	const src = `format: 1
command: t
variant: v
parse:
  type: tree
  indent: "\t"
  node:
    parse: {type: kv, separator: ": "}
`
	v, err := Parse(load(t, src), []byte("Section: one\n\tKey: two\n"), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := `[{"name":"Section","value":"one","children":[{"name":"Key","value":"two","children":[]}]}]`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A tree may be a part of a composite, which is what a report with a
// banner above the tree needs. It may not be a part of records: a record
// is a block that repeats at one level and a tree is what the input
// decides the depth of, and one definition cannot say both.
func TestTreeNesting(t *testing.T) {
	t.Parallel()
	const composite = `format: 1
command: t
variant: v
parse:
  type: composite
  parts:
    - name: header
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '^(?P<title>.+)$'}
    - name: items
      select: {skip: 1}
      parse:
        type: tree
        indent: "  "
        node:
          parse: {type: regex, pattern: '^(?P<name>.+)$'}
`
	v, err := Parse(load(t, composite), []byte("Report\na\n  b\n"), Options{})
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	want := `{"header":{"title":"Report"},"items":[{"name":"a","children":[{"name":"b","children":[]}]}]}`
	if got := mustJSON(t, v); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

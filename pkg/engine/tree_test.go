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

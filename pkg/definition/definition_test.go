package definition

import (
	"errors"
	"strings"
	"testing"
)

const validTable = `
format: 1
command: df
variant: gnu
description: GNU df
detect:
  os: [linux]
  args: {any: ["-k"], none: ["-h"]}
  signature:
    all: ['^Filesystem\s+1K-blocks']
    window: 5
  priority: 1
exec:
  env: {LC_ALL: C}
input:
  ignore: ['^df: ']
  select: {skip: 0}
parse:
  type: table
  header:
    columns: [filesystem, blocks, used, available, use_percent, mounted_on]
  max_fields: 6
  min_fields: 6
fields:
  blocks: {type: int}
  use_percent: {type: int, trim_suffix: "%", null_if: ["-"]}
  mounted_on: {required: true}
`

func TestLoadValid(t *testing.T) {
	t.Parallel()
	d, err := Load([]byte(validTable), "df.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.ID() != "df/gnu" {
		t.Errorf("ID = %s", d.ID())
	}
	if d.Source != "df.yaml" {
		t.Errorf("Source = %s", d.Source)
	}
	all, _, _ := d.Detect.Signature.Compiled()
	if len(all) != 1 {
		t.Errorf("signature not compiled: %v", all)
	}
	if len(d.Input.IgnorePatterns()) != 1 {
		t.Error("ignore not compiled")
	}
	if !d.Input.SkipBlankLines() {
		t.Error("skip_blank default should be true")
	}
	if d.Detect.Args.IsZero() || d.Detect.Signature.IsZero() || !d.Input.Select.IsZero() {
		t.Error("IsZero helpers")
	}
	if d.Fields["blocks"].EffectiveType() != FieldInt || d.Fields["mounted_on"].EffectiveType() != FieldString {
		t.Error("EffectiveType")
	}
	var nilField *Field
	if nilField.EffectiveType() != FieldString {
		t.Error("nil field type")
	}
}

func TestLoadRegexKVComposite(t *testing.T) {
	t.Parallel()
	src := `
format: 1
command: w
variant: linux
parse:
  type: composite
  parts:
    - name: summary
      select: {limit: 1}
      parse:
        type: regex
        each: input
        pattern: '^(?P<time>\S+) up (?P<uptime>.+)$'
    - name: users
      select: {skip: 1, after: '^never', until: '^never'}
      parse:
        type: table
        split: aligned
      fields:
        idle: {null_if: ["-"]}
    - name: env
      ignore: ['^\\S']
      parse:
        type: kv
        separator: "="
        as: list
`
	d, err := Load([]byte(src), "w.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := d.Parse.Parts[0].Parse
	if len(p.CompiledPatterns()) != 1 || strings.Join(p.Groups(), ",") != "time,uptime" {
		t.Errorf("regex part not compiled: %v", p.Groups())
	}
	sel := d.Parse.Parts[1].Select
	if sel.CompiledAfter() == nil || sel.CompiledUntil() == nil {
		t.Error("select regexes not compiled")
	}
}

func TestLoadPatternsAndOptions(t *testing.T) {
	t.Parallel()
	d, err := Load([]byte(`
format: 1
command: ls
variant: long
detect:
  auto_detect: false
input:
  record_separator: nul
parse:
  type: regex
  patterns:
    - '^l(?P<flags>\S+) (?P<filename>.+?) -> (?P<link_to>.+)$'
    - '^(?P<flags>\S+) (?P<filename>.+)$'
fields:
  link_to: {when_missing: omit}
`), "ls.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(d.Parse.CompiledPatterns()) != 2 {
		t.Errorf("patterns = %d", len(d.Parse.CompiledPatterns()))
	}
	if got := strings.Join(d.Parse.Groups(), ","); got != "flags,filename,link_to" {
		t.Errorf("groups = %s", got)
	}
	if len(d.Parse.PatternSources()) != 2 {
		t.Errorf("sources = %v", d.Parse.PatternSources())
	}
	if d.Detect.AutoDetectable() {
		t.Error("auto_detect: false was not read")
	}
	if d.Input.Separator() != 0 {
		t.Errorf("separator = %q", d.Input.Separator())
	}

	// An alternative may state values of its own, kept in the order of
	// their names; a plain string is an alternative with none.
	d, err = Load([]byte(`
format: 1
command: c
variant: v
parse:
  type: regex
  patterns:
    - pattern: '^(?P<name>\w+)=(?P<value>.*)$'
      values: {kind: environment, a: first}
    - '^(?P<command>.+)$'
`), "c.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := d.Parse.PatternValues(0); len(got) != 2 || got[0] != (Value{Name: "a", Value: "first"}) || got[1] != (Value{Name: "kind", Value: "environment"}) {
		t.Errorf("values of the first alternative = %v", got)
	}
	if got := d.Parse.PatternValues(1); got != nil {
		t.Errorf("a plain pattern has no values: %v", got)
	}
	if got := d.Parse.PatternValues(5); got != nil {
		t.Errorf("an alternative that is not there has no values: %v", got)
	}

	// Defaults.
	d, err = Load([]byte("format: 1\ncommand: c\nvariant: v\nparse: {type: kv}\n"), "d.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Detect.AutoDetectable() || !d.Parse.TrimCells() || d.Input.Separator() != '\n' {
		t.Error("defaults")
	}
	d, err = Load([]byte("format: 1\ncommand: c\nvariant: v\nparse: {type: kv, trim: false}\n"), "d.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Parse.TrimCells() {
		t.Error("trim: false was not read")
	}
	if got := d.Parse.PatternSources(); got != nil {
		t.Errorf("kv has no patterns: %v", got)
	}
}

func TestLoadNestedFields(t *testing.T) {
	t.Parallel()
	src := `
format: 1
command: id
variant: posix
parse:
  type: regex
  each: input
  pattern: '^uid=(?P<uid>\S+) groups=(?P<groups>\S+)(?: context=(?P<context>\S+))?$'
fields:
  uid:
    type: object
    regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$'
    fields:
      id: {type: int}
  groups:
    type: array
    split: ","
    items:
      type: object
      regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$'
      fields:
        id: {type: int}
  context:
    type: object
    when_missing: omit
    regex: '^(?P<user>[^:]+):(?P<role>[^:]+)$'
`
	d, err := Load([]byte(src), "id.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.Fields["uid"].CompiledRegex() == nil || len(d.Fields["uid"].Groups()) != 2 {
		t.Error("object regex not compiled")
	}
	if d.Fields["groups"].Items.CompiledRegex() == nil {
		t.Error("items regex not compiled")
	}
	arr := `
format: 1
command: x
variant: y
parse: {type: kv}
fields:
  a: {type: array, split_regex: '\s*,\s*'}
`
	d, err = Load([]byte(arr), "x.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.Fields["a"].CompiledSplit() == nil {
		t.Error("split_regex not compiled")
	}
}

// This test is deliberately not parallel, and neither are its subtests.
// Decoding this many documents at once has crashed the YAML library on
// Windows with Go 1.26 (a fault inside its decodeMap, which recover
// cannot catch). jz reads a registry one definition at a time, so the
// concurrency here bought nothing but a flaky build.
func TestLoadErrors(t *testing.T) {
	base := "format: 1\ncommand: c\nvariant: v\nparse: {type: kv}\n"
	tests := []struct {
		name string
		src  string
		want string // substring expected in the error
	}{
		{"not yaml", "format: [", "invalid YAML"},
		{"empty file", "", "is empty"},
		{"only a comment", "# nothing here\n", "holds no definition"},
		{"unknown key", base + "bogus: 1\n", `unknown key "bogus"; this definition may need a newer jz`},
		{"format 2", "format: 2\ncommand: c\nvariant: v\nparse: {type: kv}\n", "format 2 is not supported"},
		{"missing command", "format: 1\nvariant: v\nparse: {type: kv}\n", "command: is required"},
		{"bad command", "format: 1\ncommand: 'A B'\nvariant: v\nparse: {type: kv}\n", "command"},
		{"missing variant", "format: 1\ncommand: c\nparse: {type: kv}\n", "variant: is required"},
		{"bad variant", "format: 1\ncommand: c\nvariant: 'Bad_'\nparse: {type: kv}\n", "variant"},
		{"reserved variant", "format: 1\ncommand: c\nvariant: aux\nparse: {type: kv}\n", "reserved file name"},
		{"reserved command", "format: 1\ncommand: nul\nvariant: v\nparse: {type: kv}\n", "reserved file name"},
		{"key removed from the format", base + "min_jsonize: abc\n", `unknown key "min_jsonize"`},
		{"nested unknown key", base + "input: {fold: 'a', bogus: 'b'}\n", `unknown key "bogus"`},
		{"bad os", base + "detect: {os: [plan9]}\n", "unknown operating system"},
		{"empty arg", base + "detect: {args: {any: ['']}}\n", "empty argument"},
		{"bad window", base + "detect: {signature: {window: 999}}\n", "window"},
		{"bad signature regex", base + "detect: {signature: {all: ['(']}}\n", "invalid regular expression"},
		{"long regex", base + "detect: {signature: {any: ['" + strings.Repeat("a", MaxRegexLength+1) + "']}}\n", "longer than"},
		{"bad env", base + "exec: {env: {'A B': x}}\n", "invalid variable name"},
		{"bad ignore", base + "input: {ignore: ['[']}\n", "input.ignore[0]"},
		{"negative skip", base + "input: {select: {skip: -1}}\n", "skip"},
		{"negative limit", base + "input: {select: {limit: -1}}\n", "limit"},
		{"bad after", base + "input: {select: {after: '('}}\n", "after"},
		{"bad until", base + "input: {select: {until: '('}}\n", "until"},
		{"missing type", "format: 1\ncommand: c\nvariant: v\nparse: {}\n", "parse.type: is required"},
		{"unknown type", "format: 1\ncommand: c\nvariant: v\nparse: {type: magic}\n", "unknown parse type"},
		{"table bad split", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, split: comma}\n", "unknown split mode"},
		{"table delimiter without mode", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, delimiter: ','}\n", "only valid with split: delimiter"},
		{"table delimiter missing", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, split: delimiter}\n", "delimiter: is required"},
		{"table negative fields", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, max_fields: -1}\n", "must not be negative"},
		{"table min > max", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, max_fields: 2, min_fields: 3}\n", "exceeds max_fields"},
		{"aligned with max", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, split: aligned, max_fields: 2}\n", "do not apply"},
		{"header none no columns", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {none: true}}\n", "columns: is required"},
		{"header none leading", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {none: true, columns: [a], leading_label: b}}\n", "cannot be combined"},
		{"aligned no header", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, split: aligned, header: {none: true, columns: [a]}}\n", "requires a header"},
		{"bad column", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {columns: ['a b']}}\n", "invalid column name"},
		{"dup column", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {columns: [a, a]}}\n", "duplicate column"},
		{"bad leading", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {leading_label: 'x y'}}\n", "leading_label"},
		{"bad rename", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {rename: {a: 'x y'}}}\n", "rename"},
		{"max > columns", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {columns: [a]}, max_fields: 2}\n", "exceeds the number of columns"},
		{"field not column", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {columns: [a]}}\nfields: {b: {type: int}}\n", "not one of the declared columns"},
		{"table with regex keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, pattern: x}\n", "only valid for type regex"},
		{"table with kv keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, separator: x}\n", "only valid for type kv"},
		{"table with parts", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, parts: [{name: a, parse: {type: kv}}]}\n", "only valid for type composite"},
		{"on_mismatch is gone", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', on_mismatch: skip}\n", `unknown key "on_mismatch"`},
		{"regex missing pattern", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex}\n", "pattern: is required"},
		{"regex both pattern forms", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', patterns: ['(?P<b>.)']}\n", "mutually exclusive"},
		{"regex too many patterns", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: [" + strings.Repeat("'(?P<a>.)',", MaxPatterns+1) + "]}\n", "more than 16 alternatives"},
		{"regex patterns invalid", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: ['(?P<a>.)', '(']}\n", "patterns[1]"},
		{"regex patterns no groups", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: ['abc']}\n", "at least one named group"},
		{"field not in any pattern", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: ['(?P<a>.)', '(?P<b>.)']}\nfields: {c: {}}\n", "not a named group of any pattern"},
		{"trim outside kv", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', trim: false}\n", "only valid for type kv"},
		{"patterns outside regex", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, patterns: ['(?P<a>.)']}\n", "only valid for type regex"},
		{"bad record separator", "format: 1\ncommand: c\nvariant: v\ninput: {record_separator: comma}\nparse: {type: kv}\n", "must be newline or nul"},
		{"regex invalid", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '('}\n", "invalid regular expression"},
		{"regex no groups", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: 'abc'}\n", "at least one named group"},
		{"regex bad each", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', each: all}\n", "must be line or input"},
		{"time without a layout", base + "fields: {t: {type: time}}\n", "layout: is required"},
		{"time layout without a year", base + "fields: {t: {type: time, layout: '15:04:05'}}\n", "states no year"},
		{"unknown location", base + "fields: {t: {type: time, layout: '2006', location: 'Asia/Tokyo'}}\n", "must be utc or local"},
		{"layout on another type", base + "fields: {t: {type: int, layout: '2006'}}\n", "layout/location are only valid for type time and type duration"},
		{"year assumed alongside a year in the layout", base + "fields: {t: {type: time, layout: '2006-01-02', year: assumed}}\n", "contradicts a layout"},
		{"year on another type", base + "fields: {t: {type: int, year: assumed}}\n", "is only valid for type time"},
		{"year with a spelling of its own", base + "fields: {t: {type: time, layout: '15:04', year: guessed}}\n", `must be "assumed"`},
		{"duration without a layout", base + "fields: {t: {type: duration}}\n", "is required for type duration"},
		{"tree without an indent", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, node: {parse: {type: regex, pattern: '(?P<a>.)'}}}\n", "indent: is required"},
		{"tree with an empty indent", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"\", node: {parse: {type: regex, pattern: '(?P<a>.)'}}}\n", "indent[0]: is empty"},
		{"tree with an empty form among the indents", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: [\"  \", \"\"], node: {parse: {type: regex, pattern: '(?P<a>.)'}}}\n", "indent[1]: is empty"},
		{"tree indent that is neither a string nor a list", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: 2, node: {parse: {type: regex, pattern: '(?P<a>.)'}}}\n", "not how many characters it is"},
		{"tree without a node", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \"}\n", "node: is required"},
		{"tree node that is not a line", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \", node: {parse: {type: table}}}\n", "read with regex or kv"},
		{"tree node matched against the whole input", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \", node: {parse: {type: regex, each: input, pattern: '(?P<a>.)'}}}\n", "each: input has nothing to match"},
		{"tree node with a group the children overwrite", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \", node: {parse: {type: regex, pattern: '(?P<name>\\w+) (?P<children>\\d+)'}}}\n", `names a key "children"`},
		{"tree node with a fixed value the children overwrite", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \", node: {parse: {type: regex, patterns: [{pattern: '(?P<name>\\w+)', values: {children: lost}}]}}}\n", `names a key "children"`},
		{"one pattern naming a group twice", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<x>a)(?P<x>b)'}\n", `names the group "x" twice`},
		{"one alternative naming a group twice", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: ['(?P<y>.)', '(?P<x>a)|(?P<x>b)']}\n", `patterns[1]: names the group "x" twice`},
		{"fields beside a composite", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: p, parse: {type: kv}}]}\nfields: {x: {type: int}}\n", "fields: does not apply to type composite"},
		{"fields beside a records parser", "format: 1\ncommand: c\nvariant: v\nparse: {type: records, start: '^a', parts: [{name: p, parse: {type: kv}}]}\nfields: {x: {required: true}}\n", "fields: does not apply to type records"},
		{"fields beside a tree", "format: 1\ncommand: c\nvariant: v\nparse: {type: tree, indent: \"  \", node: {parse: {type: regex, pattern: '(?P<a>.)'}}}\nfields: {a: {type: int}}\n", "fields: does not apply to type tree"},
		{"fields beside a records part", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: p, parse: {type: records, start: '^a', parts: [{name: q, parse: {type: kv}}]}, fields: {x: {type: int}}}]}\n", "parse.parts[0].fields: does not apply to type records"},
		{"fields beside a tree part", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: p, parse: {type: tree, indent: \"  \", node: {parse: {type: regex, pattern: '(?P<a>.)'}}}, fields: {a: {type: int}}}]}\n", "parse.parts[0].fields: does not apply to type tree"},
		{"csv NUL delimiter", "format: 1\ncommand: c\nvariant: v\nparse: {type: csv, delimiter: \"\\0\"}\n", "delimiter"},
		{"csv quote delimiter", "format: 1\ncommand: c\nvariant: v\nparse: {type: csv, delimiter: '\"'}\n", "delimiter"},
		{"csv line break delimiter", "format: 1\ncommand: c\nvariant: v\nparse: {type: csv, delimiter: \"\\n\"}\n", "delimiter"},
		{"header repeated with no header line", "format: 1\ncommand: c\nvariant: v\nparse: {type: csv, header: {none: true, repeated: true}}\n", "nothing to repeat"},
		{"header repeated outside table and csv", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, header: {repeated: false}}\n", "only valid for type table and type csv"},

		{"indent on another parse type", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, indent: \"  \"}\n", "indent/node are only valid for type tree"},
		{"duration with a time layout", base + "fields: {t: {type: duration, layout: '2006-01-02'}}\n", "must be h:mm or mm:ss"},
		{"location on a duration", base + "fields: {t: {type: duration, layout: 'mm:ss', location: utc}}\n", "only valid for type time"},
		{"bad part ignore", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: p, ignore: ['('], parse: {type: kv}}]}\n", "parts[0].ignore[0]"},
		{"regex field not group", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)'}\nfields: {b: {}}\n", "not a named group"},
		{"regex with table keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', header: {columns: [a]}}\n", "only valid for type table"},
		{"regex with split", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', split: aligned}\n", "only valid for type table"},
		{"kv bad as", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, as: tuple}\n", "must be list or map"},
		{"kv bad value name", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, value_name: 'a b'}\n", "value_name"},
		{"kv with pattern", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, pattern: x}\n", "only valid for type regex"},
		{"alias without a name", "format: 1\ncommand: c\nvariant: v\naliases: [{args: {all: [x]}}]\nparse: {type: kv}\n", "aliases[0].name: is required"},
		{"alias is the command", "format: 1\ncommand: c\nvariant: v\naliases: [{name: c}]\nparse: {type: kv}\n", "is the command itself"},
		{"duplicate alias", "format: 1\ncommand: c\nvariant: v\naliases: [{name: d}, {name: d}]\nparse: {type: kv}\n", "duplicate alias"},
		{"alias bad name", "format: 1\ncommand: c\nvariant: v\naliases: [{name: 'A B'}]\nparse: {type: kv}\n", "must match"},
		{"bad fold", "format: 1\ncommand: c\nvariant: v\ninput: {fold: '('}\nparse: {type: kv}\n", "invalid regular expression"},
		{"composite no parts", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite}\n", "parts: is required"},
		{"composite nested", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: a, parse: {type: composite, parts: [{name: b, parse: {type: kv}}]}}]}\n", "cannot be composite"},
		{"records with no parts", "format: 1\ncommand: c\nvariant: v\nparse: {type: records, start: x}\n", "is required for type records"},
		{"records nested in records", "format: 1\ncommand: c\nvariant: v\nparse: {type: records, start: x, parts: [{name: a, parse: {type: records, start: y, parts: [{name: b, parse: {type: kv}}]}}]}\n", "cannot be records"},
		{"records part cannot be composite", "format: 1\ncommand: c\nvariant: v\nparse: {type: records, start: x, parts: [{name: a, parse: {type: composite, parts: [{name: b, parse: {type: kv}}]}}]}\n", "cannot be composite"},
		{"composite no name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{parse: {type: kv}}]}\n", "name: is required"},
		{"composite bad name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: 'a b', parse: {type: kv}}]}\n", "invalid name"},
		{"composite dup name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: a, parse: {type: kv}}, {name: a, parse: {type: kv}}]}\n", "duplicate part name"},
		{"composite with kv keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, separator: x, parts: [{name: a, parse: {type: kv}}]}\n", "only valid for type kv"},
		{"field bad name", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, header: {columns: [a]}}\nfields: {'a b': {}}\n", "invalid field name"},
		{"kv field empty key", base + "fields: {'': {}}\n", "invalid key"},
		{"field unknown type", base + "fields: {a: {type: date}}\n", "unknown type"},
		{"bool overlap", base + "fields: {a: {type: bool, true_values: [x], false_values: [X]}}\n", "also listed in false_values"},
		// A size with a unit is printed rounded, so there is no type that
		// turns it into bytes.
		{"size is not a type", base + "fields: {a: {type: size}}\n", `unknown type "size"`},
		{"array no split", base + "fields: {a: {type: array}}\n", "need split"},
		{"array both split", base + "fields: {a: {type: array, split: ',', split_regex: ','}}\n", "mutually exclusive"},
		{"array bad regex", base + "fields: {a: {type: array, split_regex: '('}}\n", "split_regex"},
		// A separator that can be nothing splits "abc def" into single
		// characters, which reads as a list of the value's letters.
		{"array regex matches nothing", base + "fields: {a: {type: array, split_regex: '[ \\t]*'}}\n", "split_regex: matches the empty string"},
		{"array of array", base + "fields: {a: {type: array, split: ',', items: {type: array, split: ';'}}}\n", "nested arrays"},
		{"object no regex", base + "fields: {a: {type: object}}\n", "regex: is required"},
		{"object bad regex", base + "fields: {a: {type: object, regex: '('}}\n", "invalid regular expression"},
		{"object no groups", base + "fields: {a: {type: object, regex: 'x'}}\n", "at least one named group"},
		{"object field not group", base + "fields: {a: {type: object, regex: '(?P<x>.)', fields: {y: {}}}}\n", "not a named group"},
		{"string with split", base + "fields: {a: {split: ','}}\n", "only valid for type array"},
		{"int with regex", base + "fields: {a: {type: int, regex: '(?P<a>x)'}}\n", "only valid for type object and type string"},
		{"string regex without a group", base + "fields: {a: {regex: 'x'}}\n", "exactly one named group"},
		{"string regex with two groups", base + "fields: {a: {regex: '(?P<a>x)(?P<b>y)'}}\n", "exactly one named group"},
		{"string with fields", base + "fields: {a: {regex: '(?P<a>x)', fields: {a: {}}}}\n", "fields is only valid for type object"},
		{"unescape without sequences", base + "fields: {a: {unescape: {}}}\n", "unescape.sequences: is required"},
		{"unescape of one character", base + "fields: {a: {unescape: {sequences: {'n': 'x'}}}}\n", "is not an escape"},
		{"unescape of two escape characters", base + "fields: {a: {unescape: {sequences: {'\\n': 'x', '%n': 'y'}}}}\n", "every escape begins with the same character"},
		{"unescape on an int", base + "fields: {a: {type: int, unescape: {sequences: {'\\n': 'x'}}}}\n", "only valid for type string"},
		{"unescape when outside a regex", base + "fields: {a: {unescape: {when: b, sequences: {'\\n': 'x'}}}}\n", "only a regex parser has groups"},
		{"unescape when not a group", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.*)'}\nfields: {a: {unescape: {when: nope, sequences: {'\\n': 'x'}}}}\n", `"nope" is not a named group`},
		{"pattern without its expression", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: [{values: {kind: x}}]}\n", "pattern: is required"},
		{"pattern value named like a group", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: [{pattern: '(?P<kind>.)', values: {kind: x}}]}\n", "is also a named group of the pattern"},
		{"pattern value bad name", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: [{pattern: '(?P<a>.)', values: {'a b': x}}]}\n", "not a valid field name"},
		{"pattern with an unknown key", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, patterns: [{pattern: '(?P<a>.)', value: {k: x}}]}\n", `unknown key "value"`},
		{"string with true", base + "fields: {a: {true_values: [x]}}\n", "only valid for type bool"},
		{"bad when_missing", base + "fields: {a: {when_missing: skip}}\n", "must be null or omit"},
		{"required omit", base + "fields: {a: {required: true, when_missing: omit}}\n", "contradictory"},
		{"too large", "format: 1\n# " + strings.Repeat("x", MaxDefinitionSize) + "\n", "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.src), tt.name+".yaml")
			if err == nil {
				t.Fatalf("expected error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoadRecoversFromDecoderPanic(t *testing.T) {
	t.Parallel()
	// A tagged scalar where a sequence is expected makes the YAML library
	// panic; Load must report an error instead.
	src := "format: 1\ncommand: c\nvariant: v\nparse:\n  type: table\n  header:\n   columns: !x\n0000"
	_, err := Load([]byte(src), "panic.yaml")
	if err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("expected invalid YAML error, got %v", err)
	}
}

func TestLoadCollectsMultipleErrors(t *testing.T) {
	t.Parallel()
	_, err := Load([]byte("format: 1\nparse: {type: nope}\nfields: {a: {type: bad}}\n"), "multi.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"command: is required", "variant: is required", "unknown parse type", "unknown type"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("errors.As ValidationError failed: %T", err)
	}
}

func TestErrorTypes(t *testing.T) {
	t.Parallel()
	fe := &FormatError{Source: "s", Got: 9}
	if !strings.Contains(fe.Error(), "format 9") {
		t.Error(fe.Error())
	}
	e := &ValidationError{Msg: "m"}
	if e.Error() != "definition: m" {
		t.Error(e.Error())
	}
	_, err := Load([]byte("format: 3\n"), "x")
	var got *FormatError
	if !errors.As(err, &got) || got.Got != 3 {
		t.Errorf("expected FormatError, got %v", err)
	}
}

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Filesystem":  "filesystem",
		"1K-blocks":   "1k_blocks",
		"512-blocks":  "512_blocks",
		"Use%":        "use_percent",
		"%CPU":        "cpu_percent",
		"%iused":      "iused_percent",
		"Mounted on":  "mounted_on",
		"MAJ:MIN":     "maj_min",
		"buff/cache":  "buff_cache",
		"LOGIN@":      "login",
		"  Weird__x ": "weird_x",
		"%":           "percent",
		"a%b":         "a_percentb",
	}
	for in, want := range tests {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Decode undoes exactly the escapes it is given and refuses an escape
// character that begins none of them, rather than keeping it.
func TestUnescapeDecode(t *testing.T) {
	t.Parallel()
	gnu := &Unescape{Sequences: map[string]string{`\\`: `\`, `\n`: "\n", `\r`: "\r"}}
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"plain name", "plain name", true},
		{`nl\nname`, "nl\nname", true},
		{`back\\slash`, `back\slash`, true},
		{`lit\\nname`, `lit\nname`, true},
		{`cr\rx`, "cr\rx", true},
		{`a\\\\b`, `a\\b`, true},
		{`tab\tname`, "", false},
		{`ends with\`, "", false},
	} {
		got, ok := gnu.Decode(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("Decode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func FuzzLoad(f *testing.F) {
	f.Add([]byte(validTable))
	f.Add([]byte("format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.+)'}\n"))
	f.Add([]byte("format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: a, parse: {type: kv}}]}\n"))
	f.Add([]byte("{"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Load(data, "fuzz")
		if err == nil && d == nil {
			t.Fatal("nil definition without error")
		}
	})
}

// A csv with no header line may leave its columns to be numbered, and
// WithColumns names them instead: a copy is returned, the names follow
// the rules header.columns follows, and a definition that has nothing to
// name, or converts a column the names leave out, is refused.
func TestWithColumns(t *testing.T) {
	t.Parallel()
	const numbered = "format: 1\ncommand: csv\nvariant: v\ndetect: {auto_detect: false}\nparse: {type: csv, header: {none: true}}\n"
	d, err := Load([]byte(numbered), "v.yaml")
	if err != nil {
		t.Fatalf("a headerless csv needs no column names: %v", err)
	}
	named, err := d.WithColumns([]string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(named.Parse.Header.Columns, ",") != "id,name" || len(d.Parse.Header.Columns) != 0 || named.ID() != d.ID() {
		t.Errorf("named %v, original %v", named.Parse.Header.Columns, d.Parse.Header.Columns)
	}
	for _, tt := range []struct {
		src   string
		names []string
		want  string
	}{
		{numbered, []string{"a b"}, "invalid column name"},
		{numbered, []string{"a", "a"}, "given twice"},
		{numbered, nil, "no column names"},
		{numbered, make([]string, MaxColumns+1), "more than"},
		{"format: 1\ncommand: csv\nvariant: v\ndetect: {auto_detect: false}\nparse: {type: csv}\n", []string{"a"}, "does not read a csv without a header line"},
		{"format: 1\ncommand: csv\nvariant: v\ndetect: {auto_detect: false}\nparse: {type: csv, header: {none: true, columns: [x]}}\n", []string{"a"}, "names its columns already"},
		{"format: 1\ncommand: t\nvariant: v\nparse: {type: table, header: {none: true, columns: [x]}}\n", []string{"a"}, "does not read a csv"},
		{"format: 1\ncommand: csv\nvariant: v\ndetect: {auto_detect: false}\nparse: {type: csv, header: {none: true}}\nfields: {column_2: {type: int}}\n", []string{"a", "b"}, `converts the column "column_2"`},
	} {
		d, err := Load([]byte(tt.src), "v.yaml")
		if err != nil {
			t.Fatalf("%s: %v", tt.src, err)
		}
		if _, err := d.WithColumns(tt.names); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v on %s: %v, want %q", tt.names, tt.src, err, tt.want)
		}
	}
}

// A definition given on the command line is the schema of a file without
// the keys that place a definition in a registry, in block style or as
// one flow mapping, and it is bounded and checked the way a file is.
func TestLoadInline(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"parse: {type: csv}\nfields: {a: {type: int}}\n",
		"{parse: {type: csv}, fields: {a: {type: int}}}",
		"  {parse: {type: csv}, fields: {a: {type: int}}}\n",
	} {
		d, err := LoadInline([]byte(body), "--define")
		if err != nil {
			t.Errorf("%q: %v", body, err)
			continue
		}
		if d.ID() != "inline/inline" || d.Format != CurrentFormat || d.Parse.Type != TypeCSV || d.Fields["a"].Type != FieldInt {
			t.Errorf("%q: loaded %+v", body, d)
		}
	}
	for _, tc := range []struct{ body, want string }{
		{"format: 1\nparse: {type: kv}\n", "states no format"},
		{"command: x\nparse: {type: kv}\n", "states no command"},
		{"{variant: x, parse: {type: kv}}", "states no variant"},
		{"detect: {signature: {all: ['x']}}\nparse: {type: kv}\n", "states no detect"},
		{"parse: {type: nope}\n", "unknown parse type"},
		{"parse: {type: kv, bogus: 1}\n", `unknown key "bogus"`},
		{"[1, 2]", "invalid YAML"},
		{"parse: {type: kv}\n" + strings.Repeat("#", MaxDefinitionSize), "exceeds"},
	} {
		if _, err := LoadInline([]byte(tc.body), "--define"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%.40q: %v, want %q", tc.body, err, tc.want)
		}
	}
}

// FuzzLoadInline drives the inline loader, whose probe reads the body
// loosely before the strict pass: neither may panic, and a body that
// loads is a definition the engine may be given.
func FuzzLoadInline(f *testing.F) {
	f.Add("parse: {type: csv}\n")
	f.Add("{parse: {type: kv}, fields: {a: {type: int}}}")
	f.Add("format: 1\nparse: {type: kv}\n")
	f.Add("!!binary x\n")
	f.Add("- a\n- b\n")
	f.Add("parse: !!map {type: regex, pattern: '(?P<a>.)'}\n")
	f.Fuzz(func(t *testing.T, body string) {
		d, err := LoadInline([]byte(body), "fuzz")
		if err != nil {
			return
		}
		if d.ID() != "inline/inline" || d.Format != CurrentFormat {
			t.Errorf("loaded %q as %s format %d", body, d.ID(), d.Format)
		}
		if d.Parse.Type == "" {
			t.Errorf("loaded %q with no parse type", body)
		}
	})
}

// A records parser says how a record is read once: by named regions, or
// by one parser over the whole block. Saying it twice, saying it not at
// all, or naming a parser whose result is a list are all refused when the
// definition is loaded.
func TestRecordsRecordForm(t *testing.T) {
	t.Parallel()
	base := "format: 1\ncommand: x\nvariant: y\nparse:\n  type: records\n  start: '^a'\n"
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "record and parts together",
			yaml: base + "  record:\n    parse: {type: kv}\n  parts:\n    - name: p\n      parse: {type: kv}\n",
			want: "write one of them",
		},
		{
			name: "neither",
			yaml: base,
			want: "parts",
		},
		{
			name: "a record that is a list",
			yaml: base + "  record:\n    parse: {type: regex, pattern: '^(?P<a>.+)$'}\n",
			want: "yields a list",
		},
		{
			name: "fields beside the records parser",
			yaml: base + "  record:\n    parse: {type: kv, as: map}\nfields:\n  a: {type: int}\n",
			want: "record.fields",
		},
		{
			name: "record on a composite",
			yaml: "format: 1\ncommand: x\nvariant: y\nparse:\n  type: composite\n  record:\n    parse: {type: kv, as: map}\n  parts:\n    - name: p\n      parse: {type: kv}\n",
			want: "only valid for type records",
		},
		{
			name: "record on a table",
			yaml: "format: 1\ncommand: x\nvariant: y\nparse:\n  type: table\n  record:\n    parse: {type: kv, as: map}\n",
			want: "only valid for type records",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load([]byte(tt.yaml), "x/y")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want it to mention %q", err, tt.want)
			}
		})
	}

	// The accepted form loads and keeps the fields under record.
	d, err := Load([]byte(base+"  record:\n    parse: {type: kv, as: map}\n    fields:\n      n: {type: int}\n"), "x/y")
	if err != nil {
		t.Fatal(err)
	}
	if d.Parse.Record == nil || d.Parse.Record.Fields["n"].Type != "int" {
		t.Errorf("record not loaded: %+v", d.Parse.Record)
	}
}

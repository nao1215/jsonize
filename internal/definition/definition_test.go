package definition

import (
	"errors"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/buildinfo"
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
      parse:
        type: kv
        separator: "="
        as: list
        key_name: k
        value_name: v
        on_mismatch: skip
`
	d, err := Load([]byte(src), "w.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := d.Parse.Parts[0].Parse
	if p.CompiledPattern() == nil || strings.Join(p.Groups(), ",") != "time,uptime" {
		t.Errorf("regex part not compiled: %v", p.Groups())
	}
	sel := d.Parse.Parts[1].Select
	if sel.CompiledAfter() == nil || sel.CompiledUntil() == nil {
		t.Error("select regexes not compiled")
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

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	base := "format: 1\ncommand: c\nvariant: v\nparse: {type: kv}\n"
	tests := []struct {
		name string
		src  string
		want string // substring expected in the error
	}{
		{"not yaml", "format: [", "invalid YAML"},
		{"unknown key", base + "bogus: 1\n", "invalid YAML"},
		{"format 2", "format: 2\ncommand: c\nvariant: v\nparse: {type: kv}\n", "format 2 is not supported"},
		{"missing command", "format: 1\nvariant: v\nparse: {type: kv}\n", "command: is required"},
		{"bad command", "format: 1\ncommand: 'A B'\nvariant: v\nparse: {type: kv}\n", "command"},
		{"missing variant", "format: 1\ncommand: c\nparse: {type: kv}\n", "variant: is required"},
		{"bad variant", "format: 1\ncommand: c\nvariant: 'Bad_'\nparse: {type: kv}\n", "variant"},
		{"reserved variant", "format: 1\ncommand: c\nvariant: aux\nparse: {type: kv}\n", "reserved file name"},
		{"reserved command", "format: 1\ncommand: nul\nvariant: v\nparse: {type: kv}\n", "reserved file name"},
		{"bad min_jsonize", base + "min_jsonize: abc\n", "min_jsonize"},
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
		{"table on_mismatch", "format: 1\ncommand: c\nvariant: v\nparse: {type: table, on_mismatch: skip}\n", "on_mismatch"},
		{"regex missing pattern", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex}\n", "pattern: is required"},
		{"regex invalid", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '('}\n", "invalid regular expression"},
		{"regex no groups", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: 'abc'}\n", "at least one named group"},
		{"regex bad each", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', each: all}\n", "must be line or input"},
		{"regex bad mismatch", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', on_mismatch: ignore}\n", "must be error or skip"},
		{"regex field not group", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)'}\nfields: {b: {}}\n", "not a named group"},
		{"regex with table keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', header: {columns: [a]}}\n", "only valid for type table"},
		{"regex with split", "format: 1\ncommand: c\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.)', split: aligned}\n", "only valid for type table"},
		{"kv bad as", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, as: tuple}\n", "must be list or map"},
		{"kv map with names", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, as: map, key_name: k}\n", "do not apply to as: map"},
		{"kv bad key name", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, key_name: 'a b'}\n", "key_name"},
		{"kv bad value name", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, value_name: 'a b'}\n", "value_name"},
		{"kv same names", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, key_name: a, value_name: a}\n", "must differ"},
		{"kv with pattern", "format: 1\ncommand: c\nvariant: v\nparse: {type: kv, pattern: x}\n", "only valid for type regex"},
		{"composite no parts", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite}\n", "parts: is required"},
		{"composite nested", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: a, parse: {type: composite, parts: [{name: b, parse: {type: kv}}]}}]}\n", "cannot be composite"},
		{"composite no name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{parse: {type: kv}}]}\n", "name: is required"},
		{"composite bad name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: 'a b', parse: {type: kv}}]}\n", "invalid name"},
		{"composite dup name", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, parts: [{name: a, parse: {type: kv}}, {name: a, parse: {type: kv}}]}\n", "duplicate part name"},
		{"composite with kv keys", "format: 1\ncommand: c\nvariant: v\nparse: {type: composite, separator: x, parts: [{name: a, parse: {type: kv}}]}\n", "only valid for type kv"},
		{"field bad name", base + "fields: {'a b': {}}\n", "invalid field name"},
		{"field unknown type", base + "fields: {a: {type: date}}\n", "unknown type"},
		{"bool overlap", base + "fields: {a: {type: bool, true_values: [x], false_values: [X]}}\n", "also listed in false_values"},
		{"size bad unit", base + "fields: {a: {type: size, unit: hex}}\n", "must be binary or decimal"},
		{"array no split", base + "fields: {a: {type: array}}\n", "need split"},
		{"array both split", base + "fields: {a: {type: array, split: ',', split_regex: ','}}\n", "mutually exclusive"},
		{"array bad regex", base + "fields: {a: {type: array, split_regex: '('}}\n", "split_regex"},
		{"array of array", base + "fields: {a: {type: array, split: ',', items: {type: array, split: ';'}}}\n", "nested arrays"},
		{"object no regex", base + "fields: {a: {type: object}}\n", "regex: is required"},
		{"object bad regex", base + "fields: {a: {type: object, regex: '('}}\n", "invalid regular expression"},
		{"object no groups", base + "fields: {a: {type: object, regex: 'x'}}\n", "at least one named group"},
		{"object field not group", base + "fields: {a: {type: object, regex: '(?P<x>.)', fields: {y: {}}}}\n", "not a named group"},
		{"string with split", base + "fields: {a: {split: ','}}\n", "only valid for type array"},
		{"string with regex", base + "fields: {a: {regex: 'x'}}\n", "only valid for type object"},
		{"string with true", base + "fields: {a: {true_values: [x]}}\n", "only valid for type bool"},
		{"string with unit", base + "fields: {a: {unit: binary}}\n", "only valid for type size"},
		{"bad when_missing", base + "fields: {a: {when_missing: skip}}\n", "must be null or omit"},
		{"required omit", base + "fields: {a: {required: true, when_missing: omit}}\n", "contradictory"},
		{"too large", "format: 1\n# " + strings.Repeat("x", MaxDefinitionSize) + "\n", "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
	ve := &VersionError{Source: "s", Required: "2.0.0", Running: "1.0.0"}
	if !strings.Contains(ve.Error(), ">= 2.0.0") {
		t.Error(ve.Error())
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

func TestMinJsonize(t *testing.T) {
	old := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = old })
	buildinfo.Version = "v1.0.0"
	src := "format: 1\ncommand: c\nvariant: v\nmin_jsonize: 2.0.0\nparse: {type: kv}\n"
	_, err := Load([]byte(src), "x.yaml")
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("expected VersionError, got %v", err)
	}
	buildinfo.Version = "v2.1.0"
	if _, err := Load([]byte(src), "x.yaml"); err != nil {
		t.Errorf("newer jsonize should accept: %v", err)
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

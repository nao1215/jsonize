package schema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

func load(t *testing.T, body string) *definition.Definition {
	t.Helper()
	d, err := definition.Load([]byte("format: 1\ncommand: t\nvariant: v\n"+body), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// compact renders a schema without the annotations, which is the part
// these tests are about.
func compact(t *testing.T, s *Schema) string {
	t.Helper()
	c := *s
	c.Schema, c.ID, c.Title, c.Description, c.Contract = "", "", "", "", nil
	b, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Each case is a definition and the schema it implies, written out, with
// the reason in a comment above it where it is not obvious.
func TestGenerate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, def, want string
	}{
		{
			// Every row has every column: min_fields defaults to all of
			// them. null_if is the one way a cell becomes null.
			name: "whitespace table with named columns",
			def:  "parse: {type: table, header: {columns: [name, size, use]}}\nfields: {size: {type: int}, use: {type: int, null_if: ['-']}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"size":{"type":"integer"},"use":{"type":["integer","null"]}},"required":["name","size","use"]}}`,
		},
		{
			// Past min_fields a cell may be missing.
			name: "a row allowed to stop short",
			def:  "parse: {type: table, min_fields: 1, header: {none: true, columns: [a, b]}}\nfields: {b: {when_missing: omit}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a"]}}`,
		},
		{
			// An aligned cell with nothing under its heading is null, in
			// any column; required says it never is.
			name: "aligned table",
			def:  "parse: {type: table, split: aligned, header: {columns: [a, b]}}\nfields: {a: {required: true}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"a":{"type":"string"},"b":{"type":["string","null"]}},"required":["a","b"]}}`,
		},
		{
			// The names come from the input, so only the converted ones
			// are named and the rest are text or null.
			name: "a header that names the columns",
			def:  "parse: {type: table}\nfields: {size: {type: size}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"size":{"type":["integer","null"]}},"additionalProperties":{"type":["string","null"]}}}`,
		},
		{
			// A group only one alternative has is not required; an
			// optional group is null when it did not take part; a group
			// that is a list of literals is an enum.
			name: "regex alternatives",
			def:  "parse: {type: regex, patterns: ['^(?P<kind>up|down) (?P<name>\\S+)(?: (?P<note>.+))?$', '^(?P<kind>gone) (?P<name>\\S+)$']}\nfields: {note: {when_missing: omit}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string","enum":["down","gone","up"]},"name":{"type":"string"},"note":{"type":"string"}},"required":["kind","name"]}}`,
		},
		{
			name: "regex against the whole input",
			def:  "parse: {type: regex, each: input, pattern: 'sent (?P<sent>\\d+)(?:, lost (?P<lost>\\d+))?'}\nfields: {sent: {type: int}, lost: {type: float}}\n",
			want: `{"type":"object","properties":{"sent":{"type":"integer"},"lost":{"type":["number","null"]}},"required":["sent","lost"]}`,
		},
		{
			// An array's items are converted one by one; an item null_if
			// names is null. An object field is its regex's groups.
			name: "array and object fields",
			def: "parse: {type: regex, pattern: '^(?P<ids>.*) (?P<size>.*)$'}\nfields:\n" +
				"  ids: {type: array, split: ',', items: {type: int, null_if: ['?']}}\n" +
				"  size: {type: object, regex: '^(?P<n>\\d+)(?P<unit>[kMG])?$', fields: {n: {type: int}}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"ids":{"type":"array","items":{"type":["integer","null"]}},"size":{"type":"object","properties":{"n":{"type":"integer"},"unit":{"type":["string","null"]}},"required":["n","unit"]}},"required":["ids","size"]}}`,
		},
		{
			// Which conversion a value went through depends on its key, so
			// the value's type is every one it can have.
			name: "kv list",
			def:  "parse: {type: kv}\nfields: {n: {type: int}, on: {type: bool}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"value":{"type":["string","integer","boolean"]}},"required":["name","value"]}}`,
		},
		{
			name: "kv map",
			def:  "parse: {type: kv, as: map}\nfields: {n: {type: int}}\n",
			want: `{"type":"object","properties":{"n":{"type":"integer"}},"additionalProperties":{"type":"string"}}`,
		},
		{
			name: "ini",
			def:  "parse: {type: ini}\n",
			want: `{"type":"object","additionalProperties":{"type":"object","properties":{},"additionalProperties":{"type":"string"}}}`,
		},
		{
			// Every part is always there, and a record is a composite.
			name: "records of parts",
			def: "parse:\n  type: records\n  start: '^# '\n  parts:\n" +
				"    - {name: head, select: {limit: 1}, parse: {type: regex, each: input, pattern: '^# (?P<name>\\S+)$'}}\n" +
				"    - {name: rows, select: {skip: 1}, parse: {type: kv}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"head":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]},"rows":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}}},"required":["head","rows"]}}`,
		},
		{
			// A node is described once and refers to itself, and a tree
			// inside a part has its description at the top, where a $ref
			// is resolved.
			name: "a tree inside a part",
			def: "parse:\n  type: composite\n  parts:\n" +
				"    - {name: title, select: {limit: 1}, parse: {type: regex, each: input, pattern: '^(?P<title>.+)$'}}\n" +
				"    - {name: items, select: {skip: 1}, parse: {type: tree, indent: '  ', node: {parse: {type: regex, pattern: '^(?P<name>.+)$'}}}}\n",
			want: `{"type":"object","properties":{"title":{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]},"items":{"type":"array","items":{"$ref":"#/$defs/node_items"}}},"required":["title","items"],"$defs":{"node_items":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/$defs/node_items"}}},"required":["name","children"]}}}`,
		},
		{
			// A CSV row may stop after its first field.
			name: "csv",
			def:  "parse: {type: csv, header: {columns: [a, b]}}\n",
			want: `{"type":"array","items":{"type":"object","properties":{"a":{"type":"string"},"b":{"type":["string","null"]}},"required":["a","b"]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := compact(t, Generate(load(t, tt.def), 1)); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// What the engine produces fits what the generator says it produces, for
// the shapes above that have an input to try.
func TestGeneratedSchemaFitsTheEngine(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ def, input string }{
		{"parse: {type: table, split: aligned, header: {columns: [a, b]}}\n", "A  B\nx\ny  z\n"},
		{"parse: {type: regex, patterns: ['^(?P<kind>up|down) (?P<name>\\S+)(?: (?P<note>.+))?$', '^(?P<kind>gone) (?P<name>\\S+)$']}\nfields: {note: {when_missing: omit}}\n", "up a\ndown b why\ngone c\n"},
		{"parse: {type: tree, indent: '  ', node: {parse: {type: regex, pattern: '^(?P<name>.+)$'}}}\n", "a\n  b\n    c\nd\n"},
		{"parse: {type: kv}\nfields: {n: {type: int}, on: {type: bool}}\n", "n=1\non=yes\nx=y\n"},
	} {
		def := load(t, tt.def)
		v, err := engine.Parse(def, []byte(tt.input), engine.Options{})
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonutil.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if errs := Validate(Generate(def, 1), doc); len(errs) > 0 {
			t.Errorf("%s: %v", doc, errs)
		}
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	s := &Schema{Type: []string{TypeArray}, Items: &Schema{Ref: "#/$defs/n"}, Defs: []Property{{Name: "n", Schema: &Schema{
		Type: []string{TypeObject},
		Properties: []Property{
			{Name: "id", Schema: &Schema{Type: []string{TypeInteger}}},
			{Name: "ratio", Schema: &Schema{Type: []string{TypeNumber, TypeNull}}},
			{Name: "state", Schema: &Schema{Type: []string{TypeString}, Enum: []string{"up", "down"}}},
			{Name: "kids", Schema: &Schema{Type: []string{TypeArray}, Items: &Schema{Ref: "#/$defs/n"}}},
		},
		Required: []string{"id", "kids"},
	}}}}
	ok := `[{"id":1,"ratio":0.5,"state":"up","kids":[{"id":2.0,"ratio":null,"kids":[]}]}]`
	if errs := Validate(s, []byte(ok)); len(errs) > 0 {
		t.Errorf("valid document: %v", errs)
	}
	for doc, want := range map[string]string{
		`[{"id":1.5,"kids":[]}]`:                   `$[0].id: number where the schema allows integer`,
		`[{"kids":[]}]`:                            `$[0]: required key "id" is missing`,
		`[{"id":1,"kids":[],"extra":true}]`:        `$[0]: key "extra" is not in the schema`,
		`[{"id":1,"state":"sideways","kids":[]}]`:  `$[0].state: "sideways" is not one of ["up" "down"]`,
		`[{"id":1,"kids":[{"id":"x","kids":[]}]}]`: `$[0].kids[0].id: string where the schema allows integer`,
		`{"id":1}`:                                   `$: object where the schema allows array`,
		`[{"id":1,"ratio":"high","kids":[]}]`:        `$[0].ratio: string where the schema allows number or null`,
		`[{"id":1,"kids":null}]`:                     `$[0].kids: null where the schema allows array`,
		`[{"id":1,"kids":[],"state":null}]`:          `$[0].state: null where the schema allows string`,
		`[{"id":1,"kids":[{"id":1,"kids":[1]}]}]`:    `$[0].kids[0].kids[0]: integer where the schema allows object`,
		`[{"id":9007199254740993,"kids":[],"x":{}}]`: `$[0]: key "x" is not in the schema`,
	} {
		errs := Validate(s, []byte(doc))
		if len(errs) == 0 || errs[0].Error() != want {
			t.Errorf("%s: got %v, want %s", doc, errs, want)
		}
	}
	// A dynamic key goes through additionalProperties.
	m := &Schema{Type: []string{TypeObject}, Properties: []Property{}, AdditionalProperties: &Schema{Type: []string{TypeString}}}
	if errs := Validate(m, []byte(`{"a":"x","b":1}`)); len(errs) != 1 || !strings.Contains(errs[0].Error(), "$.b: integer") {
		t.Errorf("additional: %v", errs)
	}
}

func obj(props ...Property) *Schema {
	s := &Schema{Type: []string{TypeObject}, Properties: props}
	for _, p := range props {
		if !strings.HasSuffix(p.Name, "?") {
			s.Required = append(s.Required, p.Name)
		}
	}
	for i := range s.Properties {
		s.Properties[i].Name = strings.TrimSuffix(s.Properties[i].Name, "?")
	}
	return s
}

func prop(name string, s *Schema) Property { return Property{Name: name, Schema: s} }

func typ(types ...string) *Schema { return &Schema{Type: types} }

// Every rule of the comparison, each with the smallest schema that shows
// it. The kinds the task names are all here: a key removed, a type
// changed, an object become an array, optional become required, an enum
// value removed and an element type changed; and so are their
// counterparts, since a schema is read by programs and by documents
// kept from before.
func TestCompare(t *testing.T) {
	t.Parallel()
	base := func() *Schema {
		return &Schema{Type: []string{TypeArray}, Items: obj(
			prop("name", typ(TypeString)),
			prop("size", typ(TypeInteger)),
			prop("note?", typ(TypeString)),
			prop("state", &Schema{Type: []string{TypeString}, Enum: []string{"down", "up"}}),
			prop("tags", &Schema{Type: []string{TypeArray}, Items: typ(TypeString)}),
			prop("meta", obj(prop("a", typ(TypeString)))),
		)}
	}
	tests := []struct {
		name   string
		change func(s *Schema)
		want   string // the one change reported, as Path: Kind
		breaks bool
	}{
		{"a key removed", func(s *Schema) { s.Items.Properties = s.Items.Properties[1:]; s.Items.Required = s.Items.Required[1:] }, "$[].name: removed", true},
		{"a type changed", func(s *Schema) { s.Items.Prop("size").Type = []string{TypeString} }, "$[].size: type", true},
		{"an integer widened to a number", func(s *Schema) { s.Items.Prop("size").Type = []string{TypeNumber} }, "$[].size: type", true},
		{"a value became nullable", func(s *Schema) { s.Items.Prop("size").Type = []string{TypeInteger, TypeNull} }, "$[].size: type", true},
		{"an object became an array", func(s *Schema) {
			s.Items.Properties[5].Schema = &Schema{Type: []string{TypeArray}, Items: typ(TypeString)}
		}, "$[].meta: type", true},
		{"an optional key became required", func(s *Schema) { s.Items.Required = append(s.Items.Required, "note") }, "$[].note: required", true},
		{"a required key became optional", func(s *Schema) { s.Items.Required = s.Items.Required[1:] }, "$[].name: optional", true},
		{"an enum value removed", func(s *Schema) { s.Items.Prop("state").Enum = []string{"up"} }, "$[].state: enum", true},
		{"an enum value added", func(s *Schema) { s.Items.Prop("state").Enum = []string{"down", "gone", "up"} }, "$[].state: enum", true},
		{"an enum dropped", func(s *Schema) { s.Items.Prop("state").Enum = nil }, "$[].state: enum", true},
		{"an element type changed", func(s *Schema) { s.Items.Prop("tags").Items = typ(TypeInteger) }, "$[].tags[]: type", true},
		{"a nested key removed", func(s *Schema) { s.Items.Prop("meta").Properties = []Property{}; s.Items.Prop("meta").Required = nil }, "$[].meta.a: removed", true},
		{"a new key that is always there", func(s *Schema) {
			s.Items.Properties = append(s.Items.Properties, prop("more", typ(TypeString)))
			s.Items.Required = append(s.Items.Required, "more")
		}, "$[].more: required", true},
		{"keys the input names", func(s *Schema) { s.Items.AdditionalProperties = typ(TypeString) }, "$[]: keys", true},
		{"a new key that may appear", func(s *Schema) { s.Items.Properties = append(s.Items.Properties, prop("more", typ(TypeString))) }, "$[].more: added", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := base()
			tt.change(next)
			changes := Compare(base(), next)
			if len(changes) != 1 || changes[0].Path+": "+changes[0].Kind != tt.want || changes[0].Breaking != tt.breaks {
				t.Errorf("got %v, want one change %s (breaking %v)", changes, tt.want, tt.breaks)
			}
		})
	}
	if changes := Compare(base(), base()); len(changes) != 0 {
		t.Errorf("no change reported %v", changes)
	}
	// An array becoming an object is named as such, and the comparison
	// stops there instead of listing every key as removed.
	arr := &Schema{Type: []string{TypeArray}, Items: typ(TypeString)}
	o := obj(prop("a", typ(TypeString)))
	if c := Compare(arr, o); len(c) != 1 || c[0].Detail != "an array became an object" {
		t.Errorf("array to object: %v", c)
	}
	if c := Compare(o, arr); len(c) != 1 || c[0].Detail != "an object became an array" {
		t.Errorf("object to array: %v", c)
	}
}

// A tree refers to itself; comparing two of them ends, and a change
// inside the node is found once.
func TestCompareTree(t *testing.T) {
	t.Parallel()
	gen := func(pattern string) *Schema {
		return Generate(load(t, "parse: {type: tree, indent: '  ', node: {parse: {type: regex, pattern: '"+pattern+"'}}}\n"), 1)
	}
	changes := Compare(gen(`^(?P<name>\S+)$`), gen(`^(?P<label>\S+)$`))
	kinds := make([]string, 0, len(changes))
	for _, c := range changes {
		kinds = append(kinds, c.Path+": "+c.Kind)
	}
	if got := strings.Join(kinds, ", "); got != "$[].label: required, $[].name: removed" {
		t.Errorf("got %s", got)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	def := load(t, "parse:\n  type: composite\n  parts:\n    - {name: a, parse: {type: tree, indent: '  ', node: {parse: {type: kv}}}}\n    - {name: b, parse: {type: kv, as: map}}\n")
	s := Generate(def, 3)
	data, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(back)
	if err != nil || string(again) != string(data) {
		t.Errorf("round trip differs:\n%s\n%s", data, again)
	}
	if Version(back) != 3 || back.Contract.Definition != "t/v" || len(Compare(s, back)) != 0 {
		t.Errorf("decoded %+v", back.Contract)
	}
	// A keyword the comparison does not know is refused rather than
	// skipped, so a change to it cannot pass unseen.
	if _, err := Decode([]byte(`{"type":"object","minProperties":1}`)); err == nil || !strings.Contains(err.Error(), `unsupported keyword "minProperties"`) {
		t.Errorf("unknown keyword: %v", err)
	}
}

// Next is how a new schema is published: a compatible change keeps the
// version, a breaking one is refused, and one that is meant goes up by
// one.
func TestNext(t *testing.T) {
	t.Parallel()
	old := Generate(load(t, "parse: {type: regex, pattern: '^(?P<n>\\d+)$'}\nfields: {n: {type: int}}\n"), 2)
	compatible := load(t, "parse: {type: regex, pattern: '^(?P<n>\\d+)(?: (?P<m>\\S+))?$'}\nfields: {n: {type: int}, m: {when_missing: omit}}\n")
	s, changes, err := Next(compatible, old, false)
	if err != nil || Version(s) != 2 || len(changes) != 1 || changes[0].Kind != KindAdded {
		t.Errorf("compatible: %v %v %v", s, changes, err)
	}
	breaking := load(t, "parse: {type: regex, pattern: '^(?P<n>\\d+)$'}\n")
	_, _, err = Next(breaking, old, false)
	var be *BreakError
	if !errors.As(err, &be) || be.Version != 2 || !strings.Contains(err.Error(), "$[].n: type: was integer, is string") ||
		!strings.Contains(err.Error(), "publish it as version 3 (make registry-update-schema BREAKING=t/v)") {
		t.Errorf("breaking: %v", err)
	}
	if s, _, err := Next(breaking, old, true); err != nil || Version(s) != 3 {
		t.Errorf("meant: %v %v", s, err)
	}
	if s, _, err := Next(breaking, nil, false); err != nil || Version(s) != 1 {
		t.Errorf("first publication: %v %v", s, err)
	}
}

// The check against the base branch is what refuses a breaking change
// published without a new version, which is how one edited in by hand is
// caught.
func TestAgainstBase(t *testing.T) {
	t.Parallel()
	file := func(version int, pattern string) *fstest.MapFile {
		data, err := Encode(Generate(load(t, "parse: {type: regex, pattern: '"+pattern+"'}\n"), version))
		if err != nil {
			t.Fatal(err)
		}
		return &fstest.MapFile{Data: data}
	}
	// A key that may appear is the one addition nobody is broken by.
	data, err := Encode(Generate(load(t, "parse: {type: regex, pattern: '^(?P<n>\\d+)(?P<m>x)?$'}\nfields: {m: {when_missing: omit}}\n"), 1))
	if err != nil {
		t.Fatal(err)
	}
	omitted := &fstest.MapFile{Data: data}
	base := fstest.MapFS{
		"schemas/a/v.json": file(1, `^(?P<n>\d+)$`),
		"schemas/b/v.json": file(1, `^(?P<n>\d+)$`),
		"schemas/c/v.json": file(4, `^(?P<n>\d+)$`),
	}
	tests := []struct {
		name string
		head fstest.MapFS
		want []string
	}{
		{"nothing changed", fstest.MapFS{"schemas/a/v.json": base["schemas/a/v.json"], "schemas/b/v.json": base["schemas/b/v.json"], "schemas/c/v.json": base["schemas/c/v.json"]}, nil},
		{"a compatible change and a new definition", fstest.MapFS{
			"schemas/a/v.json": omitted, "schemas/b/v.json": base["schemas/b/v.json"], "schemas/c/v.json": base["schemas/c/v.json"],
			"schemas/d/v.json": file(1, `^(?P<n>\d+)$`),
		}, nil},
		{"a breaking change without a new version", fstest.MapFS{
			"schemas/a/v.json": file(1, `^(?P<m>\d+)$`), "schemas/b/v.json": base["schemas/b/v.json"], "schemas/c/v.json": base["schemas/c/v.json"],
		}, []string{"a/v: a breaking change published without a new contract version (still 1):\n  $[].m: required: a new key that is always there, which a document of the previous version lacks\n  $[].n: removed: the key is no longer produced"}},
		{"a breaking change with a new version", fstest.MapFS{
			"schemas/a/v.json": file(2, `^(?P<m>\d+)$`), "schemas/b/v.json": base["schemas/b/v.json"], "schemas/c/v.json": base["schemas/c/v.json"],
		}, nil},
		{"a definition gone", fstest.MapFS{"schemas/a/v.json": base["schemas/a/v.json"], "schemas/c/v.json": base["schemas/c/v.json"]},
			[]string{"b/v: the schema was published and is gone; list b/v in schemas/retired if the definition was removed on purpose"}},
		{"a definition retired", fstest.MapFS{"schemas/a/v.json": base["schemas/a/v.json"], "schemas/c/v.json": base["schemas/c/v.json"], "schemas/retired": {Data: []byte("# removed with the old tool\nb/v\n")}}, nil},
		{"a version that went down", fstest.MapFS{"schemas/a/v.json": base["schemas/a/v.json"], "schemas/b/v.json": base["schemas/b/v.json"], "schemas/c/v.json": file(3, `^(?P<n>\d+)$`)},
			[]string{"c/v: the contract version went down from 4 to 3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := AgainstBase(base, tt.head)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n---\n") != strings.Join(tt.want, "\n---\n") {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n---\n"), strings.Join(tt.want, "\n---\n"))
			}
		})
	}
	// A base that published nothing has nothing to break.
	if got, err := AgainstBase(fstest.MapFS{}, base); err != nil || len(got) != 0 {
		t.Errorf("empty base: %v %v", got, err)
	}
}

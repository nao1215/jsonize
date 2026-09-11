package schema

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"

	"github.com/nao1215/jsonize/pkg/definition"
)

// childrenKey is where a tree node's children go; the engine uses the
// same name.
const childrenKey = "children"

// nodeDef is the $defs entry a tree's nodes refer to.
const nodeDef = "node"

// Generate derives the schema of what def produces. version is the
// output contract version to state; it comes from the schema published
// before, and Generate never decides it.
//
// What is derived is what the engine guarantees, and no more. A key is
// required when every object of its kind carries it. A value may be null
// when the engine can leave it empty: an aligned cell with nothing under
// it, a regex group that did not take part, a value null_if names. The
// types are the ones the field rules convert to; a value that keeps its
// text (a time the format dates without a year) is a string either way.
// Where the keys come from the input (a table that names its columns
// from its header, a key/value map), the schema says so and gives the
// type of those values instead of naming them.
func Generate(def *definition.Definition, version int) *Schema {
	g := &generator{taken: map[string]bool{}}
	s := g.parse(&def.Parse, def.Fields, nodeDef)
	// A $ref is resolved against the whole document, so a tree described
	// inside a part keeps its node description at the top.
	s.Defs = hoist(s)
	s.Schema = Draft
	s.ID = BaseID + def.Command + "/" + def.Variant + ".json"
	s.Title = def.ID()
	s.Description = def.Description
	s.Contract = &Contract{Definition: def.ID(), Version: version}
	return s
}

// generator carries what one Generate call shares between the parsers of
// a definition.
type generator struct {
	// taken holds the $defs names given out, so that two trees never
	// describe their nodes under one name.
	taken map[string]bool
}

// defName returns want, or want with a number after it when want is
// taken. Part names may themselves contain "_", so the part "a_b" and the
// part "b" inside the part "a" both ask for "node_a_b".
func (g *generator) defName(want string) string {
	name := want
	for i := 2; g.taken[name]; i++ {
		name = fmt.Sprintf("%s_%d", want, i)
	}
	g.taken[name] = true
	return name
}

// parse returns the schema of one parser's result. node is the $defs
// name a tree here asks to describe its nodes under.
func (g *generator) parse(p *definition.Parse, fields map[string]*definition.Field, node string) *Schema {
	switch p.Type {
	case definition.TypeTable, definition.TypeCSV:
		return array(table(p, fields))
	case definition.TypeRegex:
		obj := regexObject(p.CompiledPatterns(), fields)
		if p.Each == definition.EachInput {
			return obj
		}
		return array(obj)
	case definition.TypeKV:
		return kv(p, fields)
	case definition.TypeINI:
		section := mapOf(fields, &Schema{Type: []string{TypeString}})
		return &Schema{Type: []string{TypeObject}, Properties: nil, AdditionalProperties: section}
	case definition.TypeComposite:
		return g.composite(p, node)
	case definition.TypeRecords:
		return array(g.composite(p, node))
	case definition.TypeTree:
		return tree(p, g.defName(node))
	}
	return &Schema{}
}

func array(items *Schema) *Schema {
	return &Schema{Type: []string{TypeArray}, Items: items}
}

func (g *generator) composite(p *definition.Parse, node string) *Schema {
	obj := &Schema{Type: []string{TypeObject}, Properties: []Property{}}
	for i := range p.Parts {
		part := &p.Parts[i]
		obj.Properties = append(obj.Properties, Property{Name: part.Name, Schema: g.parse(&part.Parse, part.Fields, node+"_"+part.Name)})
		obj.Required = append(obj.Required, part.Name)
	}
	return obj
}

// tree describes a node once, in $defs, and refers to it from the list
// of top-level nodes and from every node's children.
func tree(p *definition.Parse, name string) *Schema {
	var node *Schema
	if p.Node.Parse.Type == definition.TypeKV {
		node = kvEntry(&p.Node.Parse, p.Node.Fields)
	} else {
		node = regexObject(p.Node.Parse.CompiledPatterns(), p.Node.Fields)
	}
	ref := &Schema{Ref: "#/$defs/" + name}
	node.Properties = append(node.Properties, Property{Name: childrenKey, Schema: array(ref)})
	node.Required = append(node.Required, childrenKey)
	return &Schema{Type: []string{TypeArray}, Items: &Schema{Ref: "#/$defs/" + name}, Defs: []Property{{Name: name, Schema: node}}}
}

// hoist takes every $defs entry out of s and the schemas under it and
// returns them, in the order they were found.
func hoist(s *Schema) []Property {
	if s == nil {
		return nil
	}
	defs := s.Defs
	s.Defs = nil
	for _, p := range s.Properties {
		defs = append(defs, hoist(p.Schema)...)
	}
	defs = append(defs, hoist(s.Items)...)
	defs = append(defs, hoist(s.AdditionalProperties)...)
	return defs
}

// table describes one row of a table or a CSV.
func table(p *definition.Parse, fields map[string]*definition.Field) *Schema {
	row := &Schema{Type: []string{TypeObject}, Properties: []Property{}}
	cols := p.Header.Columns
	if len(cols) == 0 {
		// The names come from the header line the input prints, so the
		// schema can say what the fields a definition converts look like
		// and that every other column is the text it was cut from.
		for _, name := range sortedFields(fields) {
			v, _ := value(fields[name], true)
			row.Properties = append(row.Properties, Property{Name: name, Schema: v})
		}
		if lead := p.Header.LeadingLabel; lead != "" && row.Prop(lead) == nil {
			// The label is the first column, so it can be empty where any
			// first cell can: under an aligned or drawn heading.
			v, omittable := value(fields[lead], cellMayBeEmpty(p, 0, 1))
			row.Properties = append([]Property{{Name: lead, Schema: v}}, row.Properties...)
			if !omittable {
				row.Required = append(row.Required, lead)
			}
		}
		row.AdditionalProperties = &Schema{Type: []string{TypeString, TypeNull}}
		return row
	}
	for i, name := range cols {
		v, omittable := value(fields[name], cellMayBeEmpty(p, i, len(cols)))
		row.Properties = append(row.Properties, Property{Name: name, Schema: v})
		if !omittable {
			row.Required = append(row.Required, name)
		}
	}
	return row
}

// cellMayBeEmpty reports whether column i can come out with nothing in
// it: a cell past the minimum a whitespace or delimited row must have, an
// aligned or drawn cell with nothing under its heading, a CSV row cut
// short.
func cellMayBeEmpty(p *definition.Parse, i, ncols int) bool {
	if p.Type == definition.TypeCSV {
		return i > 0
	}
	switch p.Split {
	case definition.SplitAligned, definition.SplitBox:
		return true
	}
	least := p.MinFields
	if least == 0 {
		least = ncols
	}
	return i >= least
}

// kv describes a key/value parser: a list of name/value entries, or one
// object keyed by what the command printed.
func kv(p *definition.Parse, fields map[string]*definition.Field) *Schema {
	if p.As == definition.AsMap {
		return mapOf(fields, &Schema{Type: []string{TypeString}})
	}
	return array(kvEntry(p, fields))
}

// kvEntry is one {"name": ..., "value": ...} entry. Which conversion the
// value went through depends on the key, so its type is every type a
// value can have.
func kvEntry(_ *definition.Parse, fields map[string]*definition.Field) *Schema {
	types := []string{TypeString}
	omittable := false
	for _, name := range sortedFields(fields) {
		v, omit := value(fields[name], false)
		types = append(types, v.Type...)
		omittable = omittable || omit
	}
	entry := &Schema{Type: []string{TypeObject}, Properties: []Property{
		{Name: "name", Schema: &Schema{Type: []string{TypeString}}},
		{Name: "value", Schema: &Schema{Type: normalizeTypes(types)}},
	}, Required: []string{"name"}}
	if !omittable {
		entry.Required = append(entry.Required, "value")
	}
	return entry
}

// mapOf is an object keyed by what the input printed. The keys a
// definition converts are named, and none of them is required: a map has
// whatever keys the text had.
func mapOf(fields map[string]*definition.Field, rest *Schema) *Schema {
	obj := &Schema{Type: []string{TypeObject}, Properties: []Property{}, AdditionalProperties: rest}
	for _, name := range sortedFields(fields) {
		v, _ := value(fields[name], false)
		obj.Properties = append(obj.Properties, Property{Name: name, Schema: v})
	}
	return obj
}

// regexObject describes the object a line matched by one of patterns
// becomes. A group every pattern has is required unless the definition
// omits it; one that only some patterns have is not, since the objects
// the others produce lack it.
func regexObject(patterns []*regexp.Regexp, fields map[string]*definition.Field) *Schema {
	type group struct {
		name     string
		in       int  // patterns that have it
		optional bool // some pattern may leave it out of its match
		enum     []string
		enumOK   bool
	}
	var order []string
	groups := map[string]*group{}
	for _, re := range patterns {
		for _, g := range captures(re) {
			cur, ok := groups[g.name]
			if !ok {
				cur = &group{name: g.name, enum: g.values, enumOK: g.finite}
				groups[g.name] = cur
				order = append(order, g.name)
			} else {
				cur.enum, cur.enumOK = unionEnum(cur.enum, cur.enumOK, g.values, g.finite)
			}
			cur.in++
			cur.optional = cur.optional || g.optional
		}
	}
	obj := &Schema{Type: []string{TypeObject}, Properties: []Property{}}
	for _, name := range order {
		g := groups[name]
		f := fields[name]
		v, omittable := value(f, g.optional)
		// A value that can also be null keeps its type list and no enum:
		// the list of values would have to name null as one of them.
		if g.enumOK && plainString(f) && !v.Has(TypeNull) {
			v.Enum = g.enum
		}
		obj.Properties = append(obj.Properties, Property{Name: name, Schema: v})
		if g.in == len(patterns) && !omittable {
			obj.Required = append(obj.Required, name)
		}
	}
	return obj
}

// value describes the value one field rule produces. missing says the
// raw value can be absent (an empty cell, a group that did not take
// part). The second result reports that the key itself may be left out,
// which is when_missing: omit meeting a value that is missing or named
// by null_if.
func value(f *definition.Field, missing bool) (*Schema, bool) {
	if f == nil {
		if missing {
			return &Schema{Type: []string{TypeString, TypeNull}}, false
		}
		return &Schema{Type: []string{TypeString}}, false
	}
	empty := missing || len(f.NullIf) > 0
	omit := f.WhenMissing == definition.MissingOmit
	s := converted(f)
	if empty && !f.Required && !omit {
		s.Type = normalizeTypes(append(s.Type, TypeNull))
	}
	return s, empty && omit
}

// converted is the schema of a value after its field rule, before any
// question of it being missing.
func converted(f *definition.Field) *Schema {
	switch f.EffectiveType() {
	case definition.FieldInt:
		return &Schema{Type: []string{TypeInteger}}
	case definition.FieldFloat, definition.FieldDuration:
		return &Schema{Type: []string{TypeNumber}}
	case definition.FieldBool:
		return &Schema{Type: []string{TypeBoolean}}
	case definition.FieldArray:
		items := &Schema{Type: []string{TypeString}}
		if f.Items != nil {
			items = converted(f.Items)
			// An item null_if names becomes null, unless the item is
			// omitted, in which case it is left out of the array.
			if len(f.Items.NullIf) > 0 && !f.Items.Required && f.Items.WhenMissing != definition.MissingOmit {
				items.Type = normalizeTypes(append(items.Type, TypeNull))
			}
		}
		return array(items)
	case definition.FieldObject:
		return regexObject([]*regexp.Regexp{f.CompiledRegex()}, f.Fields)
	default:
		// string and time: a time is written as RFC 3339 when the text
		// says what it means and kept as printed when it does not, and
		// both are strings.
		return &Schema{Type: []string{TypeString}}
	}
}

// plainString reports a value that reaches the output as the text a
// group matched, so the values that group can match are the values the
// key can have.
func plainString(f *definition.Field) bool {
	return f == nil || (f.EffectiveType() == definition.FieldString && f.TrimPrefix == "" && f.TrimSuffix == "" && len(f.NullIf) == 0)
}

func sortedFields(fields map[string]*definition.Field) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// capture is what the syntax tree of a pattern says about one named
// group.
type capture struct {
	name     string
	optional bool     // a match may leave the group out
	values   []string // every text the group can match, when finite
	finite   bool
}

// maxEnum bounds how many values a group may match before its values
// stop being worth listing.
const maxEnum = 32

// captures reads the named groups of a pattern from its syntax tree. It
// parses with the flags regexp.Compile uses, so the tree is the one the
// engine matches with.
func captures(re *regexp.Regexp) []capture {
	tree, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return nil
	}
	var out []capture
	var walk func(n *syntax.Regexp, optional bool)
	walk = func(n *syntax.Regexp, optional bool) {
		switch {
		case n.Op == syntax.OpCapture && n.Name != "":
			values, finite := language(n.Sub[0])
			out = append(out, capture{name: n.Name, optional: optional, values: values, finite: finite})
		case n.Op == syntax.OpAlternate:
			optional = optional || len(n.Sub) > 1
		case n.Op == syntax.OpQuest, n.Op == syntax.OpStar:
			optional = true
		case n.Op == syntax.OpRepeat:
			optional = optional || n.Min == 0
		}
		for _, sub := range n.Sub {
			walk(sub, optional)
		}
	}
	walk(tree, false)
	// A name used twice in one pattern is optional unless every use is
	// required, and its values are the union of both.
	merged := out[:0:0]
	index := map[string]int{}
	for _, c := range out {
		if i, ok := index[c.name]; ok {
			merged[i].optional = merged[i].optional || c.optional
			merged[i].values, merged[i].finite = unionEnum(merged[i].values, merged[i].finite, c.values, c.finite)
			continue
		}
		index[c.name] = len(merged)
		merged = append(merged, c)
	}
	return merged
}

func unionEnum(a []string, aok bool, b []string, bok bool) ([]string, bool) {
	if !aok || !bok {
		return nil, false
	}
	set := map[string]bool{}
	for _, v := range append(append([]string{}, a...), b...) {
		set[v] = true
	}
	if len(set) > maxEnum {
		return nil, false
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, true
}

// language returns every string n can match, when there are few of
// them: literals, alternatives of literals, an optional literal, a small
// character class. Anything open-ended (a repetition, \S+) is not
// finite.
func language(n *syntax.Regexp) ([]string, bool) {
	switch {
	case n.Op == syntax.OpLiteral:
		if n.Flags&syntax.FoldCase != 0 {
			return nil, false
		}
		return []string{string(n.Rune)}, true
	case n.Op == syntax.OpCharClass:
		return charClass(n.Rune)
	case n.Op == syntax.OpCapture:
		return language(n.Sub[0])
	case n.Op == syntax.OpQuest:
		sub, ok := language(n.Sub[0])
		if !ok {
			return nil, false
		}
		return unionEnum(sub, true, []string{""}, true)
	case n.Op == syntax.OpAlternate:
		return alternatives(n.Sub)
	case n.Op == syntax.OpConcat:
		return concatenation(n.Sub)
	case zeroWidth[n.Op]:
		return []string{""}, true
	}
	return nil, false
}

// zeroWidth are the operators that match without consuming text.
var zeroWidth = map[syntax.Op]bool{
	syntax.OpEmptyMatch: true, syntax.OpBeginLine: true, syntax.OpEndLine: true,
	syntax.OpBeginText: true, syntax.OpEndText: true, syntax.OpWordBoundary: true, syntax.OpNoWordBoundary: true,
}

// charClass lists the characters of a class given as ranges.
func charClass(ranges []rune) ([]string, bool) {
	var out []string
	for i := 0; i+1 < len(ranges); i += 2 {
		for r := ranges[i]; r <= ranges[i+1]; r++ {
			if len(out) >= maxEnum {
				return nil, false
			}
			out = append(out, string(r))
		}
	}
	return out, true
}

// alternatives is the union of what each alternative matches.
func alternatives(subs []*syntax.Regexp) ([]string, bool) {
	var out []string
	for _, s := range subs {
		sub, ok := language(s)
		if !ok {
			return nil, false
		}
		if out, ok = unionEnum(out, true, sub, true); !ok {
			return nil, false
		}
	}
	return out, true
}

// concatenation is every way of matching the parts one after the other.
func concatenation(subs []*syntax.Regexp) ([]string, bool) {
	out := []string{""}
	for _, s := range subs {
		sub, ok := language(s)
		if !ok {
			return nil, false
		}
		next := make([]string, 0, len(out)*len(sub))
		for _, a := range out {
			for _, b := range sub {
				next = append(next, a+b)
			}
		}
		if len(next) > maxEnum {
			return nil, false
		}
		out = next
	}
	return unionEnum(out, true, nil, true)
}

// Package schema derives the JSON Schema of what a parser definition
// produces, checks a document against it, and compares two versions of
// it for changes that would break the programs reading the output.
//
// The schema is derived, not written. A definition already states every
// key it produces (a column, a named group, a part) and every conversion
// (int, bool, duration), so the contract a consumer relies on can be read
// off the definition the way the engine reads the input. Writing it by
// hand would be a second statement of the same thing, free to drift from
// the first.
//
// Only the keywords the generator emits are modelled: type, enum,
// properties, required, additionalProperties, items, $ref and $defs,
// plus the annotations that name a schema. That is a small, exact subset
// of JSON Schema draft 2020-12, and the validator in this package covers
// exactly that subset.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Draft is the JSON Schema dialect the schemas declare.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// BaseID is where the published schemas live. A schema's $id is this,
// then command/variant.json.
const BaseID = "https://nao1215.github.io/jsonize/schemas/"

// JSON types, in the order a type list is written in.
const (
	TypeObject  = "object"
	TypeArray   = "array"
	TypeString  = "string"
	TypeInteger = "integer"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeNull    = "null"
)

var typeOrder = map[string]int{TypeObject: 0, TypeArray: 1, TypeString: 2, TypeInteger: 3, TypeNumber: 4, TypeBoolean: 5, TypeNull: 6}

// Schema is one JSON Schema, or one subschema of it.
type Schema struct {
	Schema      string
	ID          string
	Title       string
	Description string
	// Contract names the definition the schema describes and the version
	// of the output contract. The version is not the definition format:
	// format says how a definition is written, this says what its output
	// looks like, and the two change for unrelated reasons.
	Contract *Contract

	Ref  string
	Type []string
	Enum []string
	// Properties are the keys an object may have, in the order the
	// definition produces them.
	Properties []Property
	Required   []string
	// AdditionalProperties describes the keys a definition cannot name
	// in advance: the columns of a table that takes its names from the
	// header, the keys of a key/value map. Nil means the object has no
	// such keys.
	AdditionalProperties *Schema
	Items                *Schema
	Defs                 []Property
}

// Contract is the x-jsonize annotation: which definition a schema is
// for, and which version of its output contract it states.
type Contract struct {
	Definition string `json:"definition"`
	Version    int    `json:"version"`
}

// Property is a named subschema.
type Property struct {
	Name   string
	Schema *Schema
}

// Prop returns the subschema of a property, or nil.
func (s *Schema) Prop(name string) *Schema {
	for _, p := range s.Properties {
		if p.Name == name {
			return p.Schema
		}
	}
	return nil
}

// Def returns a definition from $defs, or nil.
func (s *Schema) Def(name string) *Schema {
	for _, p := range s.Defs {
		if p.Name == name {
			return p.Schema
		}
	}
	return nil
}

// IsRequired reports whether name is in required.
func (s *Schema) IsRequired(name string) bool {
	for _, r := range s.Required {
		if r == name {
			return true
		}
	}
	return false
}

// Has reports whether the type list names t.
func (s *Schema) Has(t string) bool {
	for _, x := range s.Type {
		if x == t {
			return true
		}
	}
	return false
}

// normalizeTypes sorts a type list into its written order and drops
// duplicates, so that two schemas that allow the same types compare
// equal.
func normalizeTypes(types []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(types))
	for _, t := range types {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return typeOrder[out[i]] < typeOrder[out[j]] })
	return out
}

// MarshalJSON writes the schema with its keys in a fixed order and its
// properties in the order the definition produces them, so a generated
// file only changes when the contract does.
func (s *Schema) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	field := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key) //nolint:errchkjson // a string always marshals
		b.Write(k)
		b.WriteByte(':')
		b.Write(raw)
		return nil
	}
	var errs []error
	add := func(key string, v any, present bool) {
		if present {
			errs = append(errs, field(key, v))
		}
	}
	add("$schema", s.Schema, s.Schema != "")
	add("$id", s.ID, s.ID != "")
	add("title", s.Title, s.Title != "")
	add("description", s.Description, s.Description != "")
	add("x-jsonize", s.Contract, s.Contract != nil)
	add("$ref", s.Ref, s.Ref != "")
	switch len(s.Type) {
	case 0:
	case 1:
		add("type", s.Type[0], true)
	default:
		add("type", s.Type, true)
	}
	add("enum", s.Enum, len(s.Enum) > 0)
	add("properties", orderedProps(s.Properties), s.Properties != nil)
	add("required", s.Required, len(s.Required) > 0)
	add("additionalProperties", s.AdditionalProperties, s.AdditionalProperties != nil)
	add("items", s.Items, s.Items != nil)
	add("$defs", orderedProps(s.Defs), len(s.Defs) > 0)
	b.WriteByte('}')
	return b.Bytes(), errors.Join(errs...)
}

// orderedProps writes properties as an object whose keys keep their
// order.
type orderedProps []Property

// MarshalJSON writes the properties as one object in their order.
func (p orderedProps) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, prop := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(prop.Name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(prop.Schema)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads a schema this package wrote, keeping the order of
// its properties. A keyword it does not model is an error rather than
// something silently ignored: a compatibility check that skipped a
// keyword would pass changes to it unseen.
func (s *Schema) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return s.decode(dec)
}

func (s *Schema) decode(dec *json.Decoder) error {
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if err := s.decodeKey(dec, key); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return expectDelim(dec, '}')
}

func (s *Schema) decodeKey(dec *json.Decoder, key string) error {
	switch key {
	case "$schema":
		return dec.Decode(&s.Schema)
	case "$id":
		return dec.Decode(&s.ID)
	case "title":
		return dec.Decode(&s.Title)
	case "description":
		return dec.Decode(&s.Description)
	case "x-jsonize":
		s.Contract = &Contract{}
		return dec.Decode(s.Contract)
	case "$ref":
		return dec.Decode(&s.Ref)
	case "type":
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		var one string
		if err := json.Unmarshal(raw, &one); err == nil {
			s.Type = []string{one}
			return nil
		}
		return json.Unmarshal(raw, &s.Type)
	case "enum":
		return dec.Decode(&s.Enum)
	case "properties":
		props, err := decodeProps(dec)
		if props == nil {
			props = []Property{}
		}
		s.Properties = props
		return err
	case "required":
		return dec.Decode(&s.Required)
	case "additionalProperties":
		s.AdditionalProperties = &Schema{}
		return s.AdditionalProperties.decode(dec)
	case "items":
		s.Items = &Schema{}
		return s.Items.decode(dec)
	case "$defs":
		props, err := decodeProps(dec)
		s.Defs = props
		return err
	default:
		return fmt.Errorf("unsupported keyword %q", key)
	}
}

func decodeProps(dec *json.Decoder) ([]Property, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	var out []Property
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		sub := &Schema{}
		if err := sub.decode(dec); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, Property{Name: name, Schema: sub})
	}
	return out, expectDelim(dec, '}')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, found %v", want, tok)
	}
	return nil
}

// Encode renders a schema as indented JSON with a trailing newline, the
// form the files in the repository are written in.
func Encode(s *Schema) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// Decode reads a schema file.
func Decode(data []byte) (*Schema, error) {
	s := &Schema{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

// typeList renders a type list for a message.
func typeList(types []string) string {
	if len(types) == 0 {
		return "any type"
	}
	return strings.Join(types, " or ")
}

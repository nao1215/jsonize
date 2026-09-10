package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Validate checks a JSON document against a schema this package
// generated and returns every place it does not fit.
//
// It is strict about keys where JSON Schema is lenient: an object whose
// schema names its properties and states no additionalProperties may
// carry no other key. A generated schema names every key a definition can
// produce, so a key it does not name is a disagreement between the
// generator and the engine, which is what checking fixtures against
// their schema is for.
func Validate(root *Schema, doc []byte) []error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return []error{fmt.Errorf("not JSON: %w", err)}
	}
	var errs []error
	validate(root, root, v, "$", &errs)
	return errs
}

func validate(root, s *Schema, v any, path string, errs *[]error) {
	if s.Ref != "" {
		name, ok := strings.CutPrefix(s.Ref, "#/$defs/")
		target := root.Def(name)
		if !ok || target == nil {
			*errs = append(*errs, fmt.Errorf("%s: unresolved $ref %q", path, s.Ref))
			return
		}
		s = target
	}
	if len(s.Type) > 0 && !s.Has(kind(v)) && (kind(v) != TypeInteger || !s.Has(TypeNumber)) {
		*errs = append(*errs, fmt.Errorf("%s: %s where the schema allows %s", path, kind(v), typeList(s.Type)))
		return
	}
	if len(s.Enum) > 0 {
		str, _ := v.(string)
		found := false
		for _, e := range s.Enum {
			if e == str {
				found = true
			}
		}
		if !found {
			*errs = append(*errs, fmt.Errorf("%s: %q is not one of %q", path, str, s.Enum))
		}
	}
	switch t := v.(type) {
	case map[string]any:
		validateObject(root, s, t, path, errs)
	case []any:
		if s.Items != nil {
			for i, e := range t {
				validate(root, s.Items, e, fmt.Sprintf("%s[%d]", path, i), errs)
			}
		}
	}
}

func validateObject(root, s *Schema, obj map[string]any, path string, errs *[]error) {
	for _, r := range s.Required {
		if _, ok := obj[r]; !ok {
			*errs = append(*errs, fmt.Errorf("%s: required key %q is missing", path, r))
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sub := s.Prop(k)
		switch {
		case sub != nil:
		case s.AdditionalProperties != nil:
			sub = s.AdditionalProperties
		case s.Properties != nil:
			*errs = append(*errs, fmt.Errorf("%s: key %q is not in the schema", path, k))
			continue
		default:
			continue
		}
		validate(root, sub, obj[k], path+"."+k, errs)
	}
}

// kind names the JSON type of a decoded value. A number with no
// fractional part is an integer, which is how JSON Schema counts 1.0.
func kind(v any) string {
	switch t := v.(type) {
	case nil:
		return TypeNull
	case bool:
		return TypeBoolean
	case string:
		return TypeString
	case json.Number:
		if r, ok := new(big.Rat).SetString(t.String()); ok && r.IsInt() {
			return TypeInteger
		}
		return TypeNumber
	case []any:
		return TypeArray
	case map[string]any:
		return TypeObject
	}
	return fmt.Sprintf("%T", v)
}

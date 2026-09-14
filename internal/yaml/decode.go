package yaml

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Unmarshaler is a type that reads itself from a node, for a value that
// may be written in more than one shape.
type Unmarshaler interface {
	UnmarshalYAML(n *Node) error
}

// DecodeError is a document that does not fit the value it is decoded
// into: a mapping where a string was expected, a word where a number
// was, a key the value has no field for.
type DecodeError struct {
	Line int
	// Path names the value in the document, "parse.header.columns[2]".
	Path string
	Msg  string
}

func (e *DecodeError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return fmt.Sprintf("line %d: %s: %s", e.Line, e.Path, e.Msg)
}

// UnknownFieldError is a key the value being decoded into has no field
// for, which a strict decode refuses.
type UnknownFieldError struct {
	Line int
	Key  string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("line %d: unknown field %q", e.Line, e.Key)
}

// Unmarshal parses data and decodes it into the value v points to. With
// strict set, a mapping key that names no field of a struct is an
// error; otherwise it is left out. A null document leaves v as it is.
func Unmarshal(data []byte, v any, strict bool) error {
	root, err := Parse(data)
	if err != nil {
		return err
	}
	return Decode(root, v, strict)
}

// Decode decodes a node into the value v points to.
func Decode(n *Node, v any, strict bool) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("yaml: cannot decode into %T; a non-nil pointer is needed", v)
	}
	if n == nil {
		return nil
	}
	d := &decoder{strict: strict}
	return d.decode(n, rv.Elem(), "")
}

type decoder struct {
	strict bool
}

var unmarshalerType = reflect.TypeFor[Unmarshaler]()

func (d *decoder) fail(n *Node, path, format string, args ...any) error {
	return &DecodeError{Line: n.Line, Path: path, Msg: fmt.Sprintf(format, args...)}
}

// decode reads n into rv, which is settable. path names rv in the
// document, for the errors.
func (d *decoder) decode(n *Node, rv reflect.Value, path string) error {
	if n == nil {
		// A key with nothing after it holds null.
		n = &Node{Kind: ScalarNode, Plain: true}
	}
	if rv.CanAddr() && rv.Addr().Type().Implements(unmarshalerType) {
		if n.IsNull() {
			return nil
		}
		u, _ := rv.Addr().Interface().(Unmarshaler)
		return u.UnmarshalYAML(n)
	}
	switch rv.Kind() { //nolint:exhaustive // every other kind is refused below
	case reflect.Pointer:
		if n.IsNull() {
			rv.Set(reflect.Zero(rv.Type()))
			return nil
		}
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return d.decode(n, rv.Elem(), path)
	case reflect.Interface:
		v := generic(n)
		if v == nil {
			rv.Set(reflect.Zero(rv.Type()))
			return nil
		}
		rv.Set(reflect.ValueOf(v))
		return nil
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		return d.scalar(n, rv, path)
	case reflect.Slice:
		return d.slice(n, rv, path)
	case reflect.Map:
		return d.mapping(n, rv, path)
	case reflect.Struct:
		return d.structure(n, rv, path)
	default:
		return d.fail(n, path, "cannot decode into %s", rv.Type())
	}
}

// scalar reads n into a string, bool or number. Null is the zero value.
func (d *decoder) scalar(n *Node, rv reflect.Value, path string) error {
	if n.IsNull() {
		rv.Set(reflect.Zero(rv.Type()))
		return nil
	}
	if n.Kind != ScalarNode {
		return d.fail(n, path, "expected %s, not a %s", scalarName(rv.Kind()), kindName(n))
	}
	switch rv.Kind() { //nolint:exhaustive // decode passes only these kinds
	case reflect.String:
		rv.SetString(n.Value)
	case reflect.Bool:
		b, ok := parseBool(n.Value)
		if !ok {
			return d.fail(n, path, "expected true or false, not %q", n.Value)
		}
		rv.SetBool(b)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(n.Value), 64)
		if err != nil {
			return d.fail(n, path, "cannot read %q as a number", n.Value)
		}
		rv.SetFloat(f)
	default:
		i, err := wholeNumber(n.Value)
		if err != nil {
			return d.fail(n, path, "%s", err)
		}
		if rv.OverflowInt(i) {
			return d.fail(n, path, "%s does not fit in %s", n.Value, rv.Type())
		}
		rv.SetInt(i)
	}
	return nil
}

// scalarName says what a scalar kind is called in an error.
func scalarName(k reflect.Kind) string {
	switch k { //nolint:exhaustive // the callers pass scalar kinds
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	default:
		return "a number"
	}
}

// slice reads a sequence into a slice.
func (d *decoder) slice(n *Node, rv reflect.Value, path string) error {
	if n.IsNull() {
		rv.Set(reflect.Zero(rv.Type()))
		return nil
	}
	if n.Kind != SequenceNode {
		return d.fail(n, path, "expected a list, not a %s", kindName(n))
	}
	out := reflect.MakeSlice(rv.Type(), len(n.Items), len(n.Items))
	for i, item := range n.Items {
		if err := d.decode(item, out.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	rv.Set(out)
	return nil
}

// mapping reads a mapping into a map keyed by strings.
func (d *decoder) mapping(n *Node, rv reflect.Value, path string) error {
	if n.IsNull() {
		rv.Set(reflect.Zero(rv.Type()))
		return nil
	}
	if n.Kind != MappingNode {
		return d.fail(n, path, "expected a mapping, not a %s", kindName(n))
	}
	if rv.Type().Key().Kind() != reflect.String {
		return d.fail(n, path, "cannot decode into a map keyed by %s", rv.Type().Key())
	}
	out := reflect.MakeMapWithSize(rv.Type(), len(n.Pairs))
	for _, pair := range n.Pairs {
		elem := reflect.New(rv.Type().Elem()).Elem()
		if err := d.decode(pair.Value, elem, join(path, pair.Key.Value)); err != nil {
			return err
		}
		out.SetMapIndex(reflect.ValueOf(pair.Key.Value).Convert(rv.Type().Key()), elem)
	}
	rv.Set(out)
	return nil
}

// structure reads a mapping into a struct by its yaml tags. A key with
// no field is an error when strict, and left out otherwise.
func (d *decoder) structure(n *Node, rv reflect.Value, path string) error {
	if n.IsNull() {
		return nil
	}
	if n.Kind != MappingNode {
		return d.fail(n, path, "expected a mapping, not a %s", kindName(n))
	}
	fields := structFields(rv.Type())
	for _, pair := range n.Pairs {
		i, ok := fields[pair.Key.Value]
		if !ok {
			if d.strict {
				return &UnknownFieldError{Line: pair.Key.Line, Key: pair.Key.Value}
			}
			continue
		}
		if err := d.decode(pair.Value, rv.Field(i), join(path, pair.Key.Value)); err != nil {
			return err
		}
	}
	return nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func kindName(n *Node) string {
	switch n.Kind {
	case SequenceNode:
		return "list"
	case MappingNode:
		return "mapping"
	case ScalarNode:
		return "scalar"
	}
	return "value"
}

// parseBool reads the spellings of a bool.
func parseBool(s string) (bool, bool) {
	switch s {
	case "true", "True", "TRUE":
		return true, true
	case "false", "False", "FALSE":
		return false, true
	}
	return false, false
}

// wholeNumber reads an integer. A hexadecimal or octal spelling is the
// number it says, and so is a decimal with nothing after the point
// (2.0); a decimal with a fraction is refused, since reading 1.5 as 1
// would count a number nobody wrote.
func wholeNumber(s string) (int64, error) {
	t := strings.TrimSpace(s)
	if i, err := strconv.ParseInt(t, 10, 64); err == nil {
		return i, nil
	}
	body, neg := strings.TrimPrefix(t, "-"), strings.HasPrefix(t, "-")
	body = strings.TrimPrefix(body, "+")
	for prefix, base := range map[string]int{"0x": 16, "0o": 8} {
		if strings.HasPrefix(body, prefix) {
			i, err := strconv.ParseInt(body[2:], base, 64)
			if err != nil {
				return 0, fmt.Errorf("cannot read %q as a whole number", s)
			}
			if neg {
				i = -i
			}
			return i, nil
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("cannot read %q as a whole number", s)
	}
	if f != math.Trunc(f) {
		return 0, fmt.Errorf("must be written as a whole number, not %s", t)
	}
	if f < math.MinInt64 || f > math.MaxInt64 {
		return 0, fmt.Errorf("%s is too large", t)
	}
	return int64(f), nil
}

// generic turns a node into the plain Go value it holds: nil, bool,
// int64, float64 or string for a scalar, []any for a sequence and
// map[string]any for a mapping.
func generic(n *Node) any {
	switch n.Kind { //nolint:exhaustive // a scalar is read below
	case SequenceNode:
		out := make([]any, len(n.Items))
		for i, item := range n.Items {
			if item != nil {
				out[i] = generic(item)
			}
		}
		return out
	case MappingNode:
		out := make(map[string]any, len(n.Pairs))
		for _, pair := range n.Pairs {
			if pair.Value != nil {
				out[pair.Key.Value] = generic(pair.Value)
			} else {
				out[pair.Key.Value] = nil
			}
		}
		return out
	}
	if !n.Plain {
		return n.Value
	}
	if n.IsNull() {
		return nil
	}
	if b, ok := parseBool(n.Value); ok {
		return b
	}
	if i, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
		return i
	}
	if looksNumeric(n.Value) {
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return f
		}
	}
	return n.Value
}

// looksNumeric reports a plain scalar that is a decimal number as YAML
// spells one, so that a word strconv would also read (Inf, 1_000) stays
// a string.
func looksNumeric(s string) bool {
	seenDigit := false
	for i, c := range s {
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
		case c == '.' || c == 'e' || c == 'E':
		case (c == '+' || c == '-') && (i == 0 || s[i-1] == 'e' || s[i-1] == 'E'):
		default:
			return false
		}
	}
	return seenDigit
}

var fieldCache sync.Map // reflect.Type -> map[string]int

// structFields maps the yaml key of every field of t to its index. A
// field's key is its `yaml` tag, or its name in lower case; a tag of "-"
// leaves the field out.
func structFields(t reflect.Type) map[string]int {
	if m, ok := fieldCache.Load(t); ok {
		fields, _ := m.(map[string]int)
		return fields
	}
	m := map[string]int{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		m[name] = i
	}
	fieldCache.Store(t, m)
	return m
}

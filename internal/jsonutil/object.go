// Package jsonutil provides an insertion-ordered JSON object and encoding
// helpers. Parsed command output keeps the column order of the original
// text, which is friendlier for humans reading the JSON and makes golden
// files stable.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// Member is one key/value pair of an Object.
type Member struct {
	Key   string
	Value any
}

// Object is a JSON object that preserves insertion order.
//
// Values may be: nil, string, bool, int64, float64, []any, *Object.
// Other Go values are encoded by encoding/json as-is.
type Object struct {
	members []Member
	index   map[string]int
}

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{index: map[string]int{}}
}

// Set inserts or replaces a key. Replacing keeps the original position.
func (o *Object) Set(key string, value any) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if i, ok := o.index[key]; ok {
		o.members[i].Value = value
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, Member{Key: key, Value: value})
}

// Get returns the value for key and whether it exists.
func (o *Object) Get(key string) (any, bool) {
	if o == nil || o.index == nil {
		return nil, false
	}
	i, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.members[i].Value, true
}

// Delete removes a key if present.
func (o *Object) Delete(key string) {
	if o == nil {
		return
	}
	i, ok := o.index[key]
	if !ok {
		return
	}
	o.members = append(o.members[:i], o.members[i+1:]...)
	delete(o.index, key)
	for j := i; j < len(o.members); j++ {
		o.index[o.members[j].Key] = j
	}
}

// Len returns the number of members.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.members)
}

// Members returns the members in insertion order. The slice must not be
// modified.
func (o *Object) Members() []Member {
	if o == nil {
		return nil
	}
	return o.members
}

// MarshalJSON implements json.Marshaler.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ErrNonFiniteNumber is returned when a float is NaN or infinite. JSON has
// no representation for those values.
var ErrNonFiniteNumber = errors.New("non-finite number cannot be encoded as JSON")

func writeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case *Object:
		if t == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		for i, m := range t.members {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeString(buf, m.Key); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeValue(buf, m.Value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeValue(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case string:
		return writeString(buf, t)
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int64:
		fmt.Fprintf(buf, "%d", t)
	case int:
		fmt.Fprintf(buf, "%d", t)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return ErrNonFiniteNumber
		}
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func writeString(buf *bytes.Buffer, s string) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	// Encoder appends a newline; drop it.
	buf.Truncate(buf.Len() - 1)
	return nil
}

// Encode writes v as JSON to w. When pretty is true the output is indented
// with two spaces. A trailing newline is always written.
func Encode(w io.Writer, v any, pretty bool) error {
	var buf bytes.Buffer
	if err := writeValue(&buf, v); err != nil {
		return err
	}
	if pretty {
		var out bytes.Buffer
		if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
			return err
		}
		buf = out
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// Marshal returns v encoded as compact JSON without a trailing newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToPlain converts ordered objects into map[string]any recursively so the
// value can be compared with values produced by encoding/json.Unmarshal.
func ToPlain(v any) any {
	switch t := v.(type) {
	case *Object:
		if t == nil {
			return nil
		}
		m := make(map[string]any, len(t.members))
		for _, mem := range t.members {
			m[mem.Key] = ToPlain(mem.Value)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = ToPlain(e)
		}
		return out
	case int64:
		return float64(t)
	case int:
		return float64(t)
	default:
		return v
	}
}

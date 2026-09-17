// Package jsonutil provides an insertion-ordered JSON object and encoding
// helpers. Parsed command output keeps the column order of the original
// text, which is friendlier for humans reading the JSON and makes golden
// files stable.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
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
	// index finds a key in an object that holds more members than a scan
	// reads quickly. A record of command output holds a handful, and a
	// map for each of a hundred thousand records would cost more than
	// the records do, so it is made when an object grows past indexAt.
	index map[string]int
}

// indexAt is how many members an object holds before it keeps an index
// of them. A row of ps aux or top has eleven or twelve columns, and a
// map for each of those rows cost more memory than the row's values and
// more time than scanning a few dozen short keys does.
const indexAt = 32

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{}
}

// find returns the position of key.
func (o *Object) find(key string) (int, bool) {
	if o.index != nil {
		i, ok := o.index[key]
		return i, ok
	}
	for i := range o.members {
		if o.members[i].Key == key {
			return i, true
		}
	}
	return 0, false
}

// reindex builds the index over every member.
func (o *Object) reindex() {
	o.index = make(map[string]int, 2*len(o.members))
	for i, m := range o.members {
		o.index[m.Key] = i
	}
}

// Set inserts or replaces a key. Replacing keeps the original position.
func (o *Object) Set(key string, value any) {
	if i, ok := o.find(key); ok {
		o.members[i].Value = value
		return
	}
	if o.members == nil {
		// Room for the few members a record holds, in one allocation
		// rather than one per doubling.
		o.members = make([]Member, 0, 4)
	}
	o.members = append(o.members, Member{Key: key, Value: value})
	switch {
	case o.index != nil:
		o.index[key] = len(o.members) - 1
	case len(o.members) > indexAt:
		o.reindex()
	}
}

// Get returns the value for key and whether it exists.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	i, ok := o.find(key)
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
	i, ok := o.find(key)
	if !ok {
		return
	}
	o.members = append(o.members[:i], o.members[i+1:]...)
	if o.index != nil {
		o.reindex()
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
	if err := writeValue(&buf, o, "", ""); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ErrNonFiniteNumber is returned when a float is NaN or infinite. JSON has
// no representation for those values.
var ErrNonFiniteNumber = errors.New("non-finite number cannot be encoded as JSON")

// writeValue writes v to buf. indent is one level of indentation, empty
// for compact output, and prefix the indentation of the line v starts
// on, which is what the members of a container are written under. The
// shape is the one json.Indent gives: an empty container stays on its
// line, and every member of one that is not empty gets a line of its
// own.
func writeValue(buf *bytes.Buffer, v any, indent, prefix string) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case *Object:
		if t == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		inner := prefix + indent
		for i, m := range t.members {
			if i > 0 {
				buf.WriteByte(',')
			}
			newline(buf, indent, inner)
			writeString(buf, m.Key)
			buf.WriteByte(':')
			if indent != "" {
				buf.WriteByte(' ')
			}
			if err := writeValue(buf, m.Value, indent, inner); err != nil {
				return err
			}
		}
		if len(t.members) > 0 {
			newline(buf, indent, prefix)
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		inner := prefix + indent
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			newline(buf, indent, inner)
			if err := writeValue(buf, e, indent, inner); err != nil {
				return err
			}
		}
		if len(t) > 0 {
			newline(buf, indent, prefix)
		}
		buf.WriteByte(']')
	case string:
		writeString(buf, t)
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int64:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), t, 10))
	case int:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(t), 10))
	case float64:
		return writeFloat(buf, t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if indent == "" {
			buf.Write(b)
			return nil
		}
		return json.Indent(buf, b, prefix, indent)
	}
	return nil
}

// newline starts a line at the given indentation, or does nothing in
// compact output.
func newline(buf *bytes.Buffer, indent, prefix string) {
	if indent == "" {
		return
	}
	buf.WriteByte('\n')
	buf.WriteString(prefix)
}

// writeFloat writes f the way encoding/json does: plainly, and in
// exponent form only where the plain form would run to many digits.
func writeFloat(buf *bytes.Buffer, f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ErrNonFiniteNumber
	}
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b := strconv.AppendFloat(buf.AvailableBuffer(), f, format, -1, 64)
	if format == 'e' {
		// e-09 is written e-9.
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	buf.Write(b)
	return nil
}

const hexDigits = "0123456789abcdef"

// writeEscape writes r as a \u escape of four hex digits, which is how
// the control characters, the replacement character and the two line
// separators are written.
func writeEscape(buf *bytes.Buffer, r rune) {
	buf.WriteString(`\u`)
	buf.WriteByte(hexDigits[r>>12&0xF])
	buf.WriteByte(hexDigits[r>>8&0xF])
	buf.WriteByte(hexDigits[r>>4&0xF])
	buf.WriteByte(hexDigits[r&0xF])
}

// writeString writes s as a JSON string the way encoding/json does with
// HTML escaping off: the quote, the backslash and the control characters
// are escaped, a byte that is not UTF-8 becomes U+FFFD, and the two line
// separators are escaped so that the output is a JavaScript string too.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				i++
				continue
			}
			buf.WriteString(s[start:i])
			switch b {
			case '\\', '"':
				buf.WriteByte('\\')
				buf.WriteByte(b)
			case '\b':
				buf.WriteString(`\b`)
			case '\f':
				buf.WriteString(`\f`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				writeEscape(buf, rune(b))
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case c == utf8.RuneError && size == 1:
			buf.WriteString(s[start:i])
			writeEscape(buf, utf8.RuneError)
		case c == 0x2028 || c == 0x2029:
			buf.WriteString(s[start:i])
			writeEscape(buf, c)
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	buf.WriteString(s[start:])
	buf.WriteByte('"')
}

// Encode writes v as JSON to w. When pretty is true the output is indented
// with two spaces. A trailing newline is always written.
func Encode(w io.Writer, v any, pretty bool) error {
	indent := ""
	if pretty {
		indent = "  "
	}
	var buf bytes.Buffer
	if err := writeValue(&buf, v, indent, ""); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// Marshal returns v encoded as compact JSON without a trailing newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v, "", ""); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

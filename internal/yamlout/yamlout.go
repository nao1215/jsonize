// Package yamlout writes the values jz converts as YAML.
//
// It writes the same values the JSON output carries and nothing else:
// an ordered object is a mapping in the order its keys were set, a list
// is a sequence, and a scalar keeps the type it has in the JSON. The one
// thing YAML adds is ambiguity, since an unquoted scalar is typed by how
// it looks, and the rules for that differ between YAML 1.1 and 1.2
// readers. A string is therefore written unquoted only when it cannot be
// read as anything else by either, and in double quotes otherwise; a
// number that is a decimal always carries a decimal point, so that it is
// read back as the decimal it is.
package yamlout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// ErrNonFiniteNumber is returned for a NaN or an infinity, which the JSON
// output cannot carry either, so the two outputs refuse the same values.
var ErrNonFiniteNumber = errors.New("non-finite number cannot be written")

// Encode writes v as one YAML document with no document markers,
// followed by a newline. Nothing is written when v holds a value that
// cannot be written.
func Encode(w io.Writer, v any) error {
	var b bytes.Buffer
	if err := top(&b, v); err != nil {
		return err
	}
	_, err := w.Write(b.Bytes())
	return err
}

// EncodeDocument writes v as one document of a stream: a "---" line, the
// document and a "..." line. The closing marker is what lets a reader
// act on the document before the next one begins.
func EncodeDocument(w io.Writer, v any) error {
	var b bytes.Buffer
	b.WriteString("---\n")
	if err := top(&b, v); err != nil {
		return err
	}
	b.WriteString("...\n")
	_, err := w.Write(b.Bytes())
	return err
}

func top(b *bytes.Buffer, v any) error {
	if s, ok, err := scalar(v); ok || err != nil {
		if err != nil {
			return err
		}
		b.WriteString(s)
		b.WriteByte('\n')
		return nil
	}
	return block(b, v, 0)
}

// block writes a mapping or a sequence whose first line continues the
// current one, which is at column col, and whose other lines start at
// col. It is how an element of a sequence shares the line of its dash.
func block(b *bytes.Buffer, v any, col int) error {
	switch t := v.(type) {
	case *jsonutil.Object:
		for i, m := range t.Members() {
			if i > 0 {
				indent(b, col)
			}
			key(b, m.Key, col)
			b.WriteByte(':')
			if err := afterKey(b, m.Value, col); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range t {
			if i > 0 {
				indent(b, col)
			}
			b.WriteString("- ")
			if s, ok, err := scalar(e); ok || err != nil {
				if err != nil {
					return err
				}
				b.WriteString(s)
				b.WriteByte('\n')
				continue
			}
			if err := block(b, e, col+2); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("cannot write %T as YAML", v)
	}
	return nil
}

// afterKey writes the value of a mapping key, which starts at column
// col: a scalar on the key's line, a collection on the lines under it.
func afterKey(b *bytes.Buffer, v any, col int) error {
	if s, ok, err := scalar(v); ok || err != nil {
		if err != nil {
			return err
		}
		b.WriteByte(' ')
		b.WriteString(s)
		b.WriteByte('\n')
		return nil
	}
	b.WriteByte('\n')
	indent(b, col+2)
	return block(b, v, col+2)
}

func indent(b *bytes.Buffer, n int) {
	for range n {
		b.WriteByte(' ')
	}
}

// maxImplicitKey is the longest key YAML lets stand before its colon
// without saying it is a key; a longer one is written after "? ".
const maxImplicitKey = 1024

func key(b *bytes.Buffer, k string, col int) {
	s := str(k)
	if utf8.RuneCountInString(s) <= maxImplicitKey {
		b.WriteString(s)
		return
	}
	b.WriteString("? ")
	b.WriteString(s)
	b.WriteByte('\n')
	indent(b, col)
}

// scalar renders v when it is written on one line: a null, a boolean, a
// number, a string, or an empty collection. The bool result is false for
// a collection with something in it.
func scalar(v any) (string, bool, error) {
	switch t := v.(type) {
	case nil:
		return "null", true, nil
	case bool:
		return strconv.FormatBool(t), true, nil
	case int64:
		return strconv.FormatInt(t, 10), true, nil
	case int:
		return strconv.Itoa(t), true, nil
	case float64:
		s, err := decimal(t)
		return s, true, err
	case json.Number:
		s, err := number(t)
		return s, true, err
	case string:
		return str(t), true, nil
	case *jsonutil.Object:
		if t == nil {
			return "null", true, nil
		}
		if t.Len() == 0 {
			return "{}", true, nil
		}
	case []any:
		if len(t) == 0 {
			return "[]", true, nil
		}
	default:
		return "", false, fmt.Errorf("cannot write %T as YAML", v)
	}
	return "", false, nil
}

// number writes a JSON number read from a document with the digits it
// was written with, which is what keeps an integer longer than 64 bits
// whole. A literal with an exponent and no decimal point gets one, so a
// YAML reader takes it for the decimal it is: 1e3 is written 1.0e3.
func number(n json.Number) (string, error) {
	s := string(n)
	if !jsonNumber.MatchString(s) {
		return "", fmt.Errorf("%q is not a JSON number", s)
	}
	if strings.ContainsRune(s, '.') {
		return s, nil
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		return s[:i] + ".0" + s[i:], nil
	}
	return s, nil
}

var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][-+]?[0-9]+)?$`)

// decimal writes a float the way the JSON output does, with a decimal
// point added where that spelling has none: 3 and 1e+21 are integers to
// a YAML reader, and 3.0 and 1.0e+21 are the decimals they came from.
func decimal(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", ErrNonFiniteNumber
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	s := string(raw)
	if strings.ContainsRune(s, '.') {
		return s, nil
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		return s[:i] + ".0" + s[i:], nil
	}
	return s + ".0", nil
}

// reserved are the words a YAML 1.1 or 1.2 reader takes for a boolean or
// a null, compared without regard to case.
var reserved = map[string]bool{
	"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true,
	"true": true, "false": true, "null": true,
}

// str writes a string plain when no YAML reader can take it for anything
// but that string, and double-quoted otherwise.
func str(s string) string {
	if plain(s) {
		return s
	}
	return quote(s)
}

// plain reports whether s can be written without quotes. The test is
// deliberately narrower than YAML's own: a string that begins with a
// letter, an underscore or a slash, ends without a space and holds only
// letters, digits, single spaces and a few punctuation marks that mean
// nothing inside a plain scalar. That excludes every indicator, every
// number, date and time, and every word a reader takes for a boolean or
// a null.
func plain(s string) bool {
	if s == "" || reserved[strings.ToLower(s)] {
		return false
	}
	prev := ' '
	for i, r := range s {
		switch {
		case i == 0:
			if !unicode.IsLetter(r) && r != '_' && r != '/' {
				return false
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
		case r == ' ':
			if prev == ' ' || prev == ':' {
				return false
			}
		case r == ':':
		case strings.ContainsRune("_-./@+()=%,", r):
		default:
			return false
		}
		prev = r
	}
	// A colon is a key indicator when a space or the end follows it.
	return prev != ' ' && prev != ':'
}

// quote writes s as a double-quoted scalar. Every character YAML does
// not print as itself, and every line or paragraph separator a YAML 1.1
// reader would take for a line break, is escaped; a byte that is not
// UTF-8 is written as U+FFFD, as the JSON output writes it.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			fmt.Fprintf(&b, `\x%02X`, r)
		case r == 0x2028 || r == 0x2029 || r == 0xfeff || r == 0xfffe || r == 0xffff:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

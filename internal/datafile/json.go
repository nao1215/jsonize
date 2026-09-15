package datafile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/limits"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// MaxDepth bounds how deeply arrays and objects may nest in a JSON value.
// A document nested deeper is refused rather than read, so no input can
// exhaust the stack. It is the depth YAML is held to as well.
const MaxDepth = limits.MaxDepth

// readJSON reads one JSON document. The keys keep the order they were
// written in, a number keeps the digits it was written with, and the
// document has to be the whole of the text.
func readJSON(data []byte, c *counter) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(data) {
		return nil, docError(JSON, data, invalidUTF8Offset(data), "the text is not valid UTF-8")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, docError(JSON, data, 0, "there is no JSON value")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec, 0, c)
	if err != nil {
		return nil, jsonError(JSON, data, dec, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, docError(JSON, data, dec.InputOffset(), "text after the JSON value")
	}
	if off, ok := loneSurrogate(data); !ok {
		return nil, docError(JSON, data, off, surrogateMsg(data, off))
	}
	return v, nil
}

// jsonLine reads the JSON value one line of JSON Lines holds.
func jsonLine(line []byte, num int, c *counter) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	v, err := decodeValue(dec, 0, c)
	if err != nil {
		var (
			se *json.SyntaxError
			pe *engine.ParseError
		)
		if errors.As(err, &pe) {
			pe.Line = num
			return nil, pe
		}
		if errors.As(err, &se) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return nil, lineError(JSONL, num, "the line is not one JSON value")
		}
		return nil, lineError(JSONL, num, err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, lineError(JSONL, num, "text after the JSON value on the line")
	}
	if off, ok := loneSurrogate(line); !ok {
		return nil, lineError(JSONL, num, surrogateMsg(line, off))
	}
	return v, nil
}

// loneSurrogate finds a \u escape naming half of a surrogate pair that
// the other half does not follow or precede. Go's decoder puts U+FFFD in
// its place, which is a character the input may also hold for itself, so
// a reading that kept it would hand on text jz was not given and say
// nothing about it. The escape is refused instead, with the place it is
// in.
//
// The text is walked for string literals rather than decoded again: in
// JSON a quote outside a string is nothing but the start of one, and
// inside a string a backslash takes the byte after it. The document has
// already been decoded when this runs, so its quotes balance.
func loneSurrogate(data []byte) (int64, bool) {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; i < len(data) && data[i] != '"'; {
			if data[i] != '\\' {
				i++
				continue
			}
			r, ok := unicodeEscape(data[i:])
			if !ok {
				// An escape of one character, which may be a quote or a
				// backslash and is stepped over whole.
				i += 2
				continue
			}
			switch {
			case r >= 0xDC00 && r <= 0xDFFF:
				return int64(i), false
			case r >= 0xD800 && r <= 0xDBFF:
				lo, ok := unicodeEscape(data[i+6:])
				if !ok || lo < 0xDC00 || lo > 0xDFFF {
					return int64(i), false
				}
				i += 12
			default:
				i += 6
			}
		}
	}
	return 0, true
}

// unicodeEscape reads a \uXXXX at the start of b.
func unicodeEscape(b []byte) (rune, bool) {
	if len(b) < 6 || b[0] != '\\' || b[1] != 'u' {
		return 0, false
	}
	var r rune
	for _, c := range b[2:6] {
		var d rune
		switch {
		case c >= '0' && c <= '9':
			d = rune(c - '0')
		case c >= 'a' && c <= 'f':
			d = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = rune(c-'A') + 10
		default:
			return 0, false
		}
		r = r<<4 | d
	}
	return r, true
}

// surrogateMsg names the escape that was refused.
func surrogateMsg(data []byte, off int64) string {
	end := min(off+6, int64(len(data)))
	return fmt.Sprintf("the escape %s is half of a surrogate pair and names no character", data[off:end])
}

type duplicateKeyError struct {
	key    string
	offset int64
}

func (e *duplicateKeyError) Error() string {
	return fmt.Sprintf("the key %q is given twice in one object", e.key)
}

var errTooDeep = fmt.Errorf("arrays and objects nest deeper than %d", MaxDepth)

// decodeValue reads the next value from dec. An object becomes a
// jsonutil.Object, so its keys stay in order, and a key given twice is
// refused: which of the two values a reader keeps is not something JSON
// says. Every value is counted by c, and the one past its limit is
// refused without a line, which the caller gives it.
func decodeValue(dec *json.Decoder, depth int, c *counter) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if err := c.add(0); err != nil {
		return nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if depth >= MaxDepth {
		return nil, errTooDeep
	}
	switch d {
	case '{':
		obj := jsonutil.NewObject()
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			// The offset is where the key ends, which is on its line.
			at := dec.InputOffset()
			key, _ := kt.(string)
			if _, dup := obj.Get(key); dup {
				return nil, &duplicateKeyError{key: key, offset: at}
			}
			v, err := decodeValue(dec, depth+1, c)
			if err != nil {
				return nil, err
			}
			obj.Set(key, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec, depth+1, c)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	// A closing delimiter where a value belongs.
	return nil, &json.SyntaxError{Offset: dec.InputOffset()}
}

// jsonError places a decoding error in the document by line.
func jsonError(format string, data []byte, dec *json.Decoder, err error) error {
	var (
		se  *json.SyntaxError
		dup *duplicateKeyError
		pe  *engine.ParseError
	)
	switch {
	case errors.As(err, &pe):
		// A value past the limit, placed by where the decoder stopped.
		pe.Line = 1 + bytes.Count(data[:min(dec.InputOffset(), int64(len(data)))], []byte("\n"))
		return pe
	case errors.As(err, &dup):
		return docError(format, data, dup.offset, dup.Error())
	case errors.As(err, &se):
		// Newer Go releases report a document that ends too soon as a
		// syntax error rather than io.ErrUnexpectedEOF; either way it is
		// said in the same words.
		if strings.Contains(se.Error(), "end of JSON input") {
			return docError(format, data, int64(len(data)), "the JSON value ends before it is complete")
		}
		return docError(format, data, se.Offset, "not valid JSON")
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return docError(format, data, int64(len(data)), "the JSON value ends before it is complete")
	}
	return docError(format, data, dec.InputOffset(), err.Error())
}

// docError is a parse failure at a byte offset of a whole document,
// reported by the line the offset falls on.
func docError(format string, data []byte, offset int64, msg string) error {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}
	line := 1 + bytes.Count(data[:offset], []byte("\n"))
	return lineError(format, line, msg)
}

// invalidUTF8Offset is where the first byte that is not UTF-8 starts.
func invalidUTF8Offset(data []byte) int64 {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return int64(i)
		}
		i += size
	}
	return int64(len(data))
}

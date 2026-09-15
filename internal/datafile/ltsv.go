package datafile

import (
	"bytes"
	"fmt"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// ltsvLine reads one record of LTSV (Labeled Tab-separated Values,
// http://ltsv.org): fields separated by tabs, each a label, a colon and
// the value. The value is everything after the first colon, colons and
// spaces included, since the format gives it no quoting to lose them to.
//
// A label is the characters the specification allows, letters, digits,
// "_", "." and "-", and at least one of them. A field without a colon, a
// label outside that set, an empty field between two tabs and a label
// given twice in one record are refused: each is a line that is not LTSV,
// and reading it would mean inventing the part the line does not say.
func ltsvLine(line []byte, num int, c *counter) (any, error) {
	if err := c.add(num); err != nil {
		return nil, err
	}
	obj := jsonutil.NewObject()
	for i, field := range bytes.Split(line, []byte("\t")) {
		label, value, ok := bytes.Cut(field, []byte(":"))
		if !ok {
			return nil, lineError(LTSV, num, fmt.Sprintf("field %d has no label: %q", i+1, truncate(field)))
		}
		if !validLabel(label) {
			return nil, lineError(LTSV, num, fmt.Sprintf("field %d: %q is not an LTSV label", i+1, truncate(label)))
		}
		key := string(label)
		if _, dup := obj.Get(key); dup {
			return nil, lineError(LTSV, num, fmt.Sprintf("the label %q is given twice", key))
		}
		if err := c.add(num); err != nil {
			return nil, err
		}
		obj.Set(key, string(value))
	}
	return obj, nil
}

func validLabel(label []byte) bool {
	if len(label) == 0 {
		return false
	}
	for _, c := range label {
		switch {
		case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_', c == '.', c == '-':
		default:
			return false
		}
	}
	return true
}

// truncate keeps a quoted piece of the input short in a message.
func truncate(b []byte) string {
	const limit = 60
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "..."
}

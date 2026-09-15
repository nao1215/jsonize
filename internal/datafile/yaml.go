package datafile

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/yaml"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// readYAML reads one YAML document into the JSON it describes.
//
// A scalar written bare is typed the way the YAML 1.2 core schema types
// it: null, true and false in their three spellings, integers in decimal,
// 0x hexadecimal and 0o octal, and decimal numbers. A quoted scalar or a
// block scalar is the string it spells. `.inf` and `.nan` are refused,
// since JSON has no number for them.
//
// jz reads the YAML its definitions are written in, which leaves out
// anchors, aliases, tags and documents after the first. A file using one
// of them is refused with the line, rather than read without it.
func readYAML(data []byte, c *counter) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(data) {
		return nil, docError(YAML, data, invalidUTF8Offset(data), "the text is not valid UTF-8")
	}
	node, err := yaml.Parse(data)
	if err != nil {
		var se *yaml.SyntaxError
		if errors.As(err, &se) {
			return nil, lineError(YAML, se.Line, se.Msg)
		}
		return nil, lineError(YAML, 0, err.Error())
	}
	if node == nil {
		return nil, lineError(YAML, 0, "there is no YAML value")
	}
	return yamlValue(node, c)
}

func yamlValue(n *yaml.Node, c *counter) (any, error) {
	line := 0
	if n != nil {
		line = n.Line
	}
	if err := c.add(line); err != nil {
		return nil, err
	}
	if n == nil {
		// A key with nothing after it is null.
		return nil, nil //nolint:nilnil // null is the value
	}
	switch n.Kind {
	case yaml.MappingNode:
		obj := jsonutil.NewObject()
		for _, p := range n.Pairs {
			v, err := yamlValue(p.Value, c)
			if err != nil {
				return nil, err
			}
			obj.Set(p.Key.Value, v)
		}
		return obj, nil
	case yaml.SequenceNode:
		arr := make([]any, 0, len(n.Items))
		for _, item := range n.Items {
			v, err := yamlValue(item, c)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case yaml.ScalarNode:
		return yamlScalar(n)
	}
	return nil, lineError(YAML, n.Line, "a value of an unknown kind")
}

var (
	yamlDecimalInt = regexp.MustCompile(`^[-+]?[0-9]+$`)
	yamlOctalInt   = regexp.MustCompile(`^0o[0-7]+$`)
	yamlHexInt     = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	yamlFloat      = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
	yamlNonFinite  = regexp.MustCompile(`^(?:[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
)

func yamlScalar(n *yaml.Node) (any, error) {
	if !n.Plain {
		return n.Value, nil
	}
	s := n.Value
	switch {
	case n.IsNull():
		return nil, nil //nolint:nilnil // null is the value
	case s == "true" || s == "True" || s == "TRUE":
		return true, nil
	case s == "false" || s == "False" || s == "FALSE":
		return false, nil
	case yamlDecimalInt.MatchString(s):
		return yamlInt(n.Line, s, 10, strings.TrimPrefix(s, "+"))
	case yamlOctalInt.MatchString(s):
		return yamlInt(n.Line, s, 8, s[2:])
	case yamlHexInt.MatchString(s):
		return yamlInt(n.Line, s, 16, s[2:])
	case yamlFloat.MatchString(s):
		return json.Number(jsonDecimal(s)), nil
	case yamlNonFinite.MatchString(s):
		return nil, lineError(YAML, n.Line, s+" is not a number JSON can hold")
	}
	return s, nil
}

// jsonDecimal writes a YAML decimal in JSON's spelling with the digits it
// was written with, so nothing is rounded: the plus sign and leading zeros
// go, and a decimal point gets a digit on each side (".5" is 0.5, "1." is
// 1.0).
func jsonDecimal(s string) string {
	var b strings.Builder
	switch s[0] {
	case '-':
		b.WriteByte('-')
		s = s[1:]
	case '+':
		s = s[1:]
	}
	mantissa, exponent := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa, exponent = s[:i], s[i:]
	}
	whole, frac, dotted := strings.Cut(mantissa, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	b.WriteString(whole)
	if dotted {
		if frac == "" {
			frac = "0"
		}
		b.WriteByte('.')
		b.WriteString(frac)
	}
	b.WriteString(exponent)
	return b.String()
}

// yamlInt reads an integer. One too large for 64 bits keeps its decimal
// digits as a JSON number, since JSON sets no limit on them and rounding
// an identifier would change it.
func yamlInt(line int, literal string, base int, digits string) (any, error) {
	if i, err := strconv.ParseInt(digits, base, 64); err == nil {
		return i, nil
	}
	if base == 10 {
		// The digits are written again, which drops the leading zeros
		// JSON does not allow ("007" is 7) and the plus sign.
		n, ok := new(big.Int).SetString(digits, 10)
		if !ok {
			return nil, lineError(YAML, line, literal+" is not an integer")
		}
		return json.Number(n.String()), nil
	}
	u, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return nil, lineError(YAML, line, literal+" does not fit in 64 bits")
	}
	return json.Number(strconv.FormatUint(u, 10)), nil
}

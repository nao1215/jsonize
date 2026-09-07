// Package convert turns text captured from command output into typed JSON
// values. Every function is total: it never panics and reports a
// descriptive error for input it cannot interpret.
package convert

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Type names used in conversion errors.
const (
	typeInt   = "int"
	typeFloat = "float"
	typeBool  = "bool"
	typeSize  = "size"
	typeTime  = "time"
)

// Location names the two time zones a definition may name. A zone
// database is not consulted: jz reads text a command printed on this
// machine, so the only two answers that are not guesses are the one the
// text states and the one the machine is in.
const (
	// LocationUTC reads a timestamp that carries no zone as UTC.
	LocationUTC = "utc"
	// LocationLocal reads it in the running system's zone.
	LocationLocal = "local"
)

// Error describes a failed conversion.
type Error struct {
	Type  string
	Input string
	Cause error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cannot convert %q to %s: %v", e.Input, e.Type, e.Cause)
	}
	return fmt.Sprintf("cannot convert %q to %s", e.Input, e.Type)
}

func (e *Error) Unwrap() error { return e.Cause }

// Int parses a base-10 integer. A leading '+' is accepted; thousands
// separators are not, because jsonize always runs commands with LC_ALL=C.
func Int(s string) (int64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, &Error{Type: typeInt, Input: s, Cause: errors.New("empty value")}
	}
	v, err := strconv.ParseInt(strings.TrimPrefix(t, "+"), 10, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) {
			return 0, &Error{Type: typeInt, Input: s, Cause: ne.Err}
		}
		return 0, &Error{Type: typeInt, Input: s, Cause: err}
	}
	return v, nil
}

// Float parses a decimal floating point number. NaN and infinities are
// rejected because JSON cannot represent them.
func Float(s string) (float64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, &Error{Type: typeFloat, Input: s, Cause: errors.New("empty value")}
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) {
			return 0, &Error{Type: typeFloat, Input: s, Cause: ne.Err}
		}
		return 0, &Error{Type: typeFloat, Input: s, Cause: err}
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, &Error{Type: typeFloat, Input: s, Cause: errors.New("non-finite value")}
	}
	return v, nil
}

// DefaultTrueValues and DefaultFalseValues are the case-insensitive spellings
// recognised by Bool when a definition does not list its own.
var (
	DefaultTrueValues  = []string{"true", "yes", "on", "1", "y"}
	DefaultFalseValues = []string{"false", "no", "off", "0", "n"}
)

// Bool parses a boolean. trueValues and falseValues may be nil to use the
// defaults. Matching is case-insensitive.
func Bool(s string, trueValues, falseValues []string) (bool, error) {
	if trueValues == nil {
		trueValues = DefaultTrueValues
	}
	if falseValues == nil {
		falseValues = DefaultFalseValues
	}
	t := strings.TrimSpace(s)
	for _, v := range trueValues {
		if strings.EqualFold(t, v) {
			return true, nil
		}
	}
	for _, v := range falseValues {
		if strings.EqualFold(t, v) {
			return false, nil
		}
	}
	return false, &Error{Type: typeBool, Input: s, Cause: errors.New("not a recognised boolean spelling")}
}

// SizeBase selects the multiplier used by Size for unit suffixes.
type SizeBase int

const (
	// Binary interprets K/M/G as powers of 1024 (the df/free convention).
	Binary SizeBase = 1024
	// Decimal interprets K/M/G as powers of 1000.
	Decimal SizeBase = 1000
)

var sizeExponent = map[byte]int{'b': 0, 'k': 1, 'm': 2, 'g': 3, 't': 4, 'p': 5, 'e': 6}

// Size parses a human readable size such as "3.7G", "955M", "466Gi",
// "1.2 MiB", "0B" or a bare number and returns the amount in bytes. The
// base applies to single-letter suffixes; an explicit "i" (Gi, GiB) always
// means 1024 and an explicit "B" after a bare letter (GB) follows base.
// The result is rounded to the nearest integer; values that overflow int64
// are rejected.
func Size(s string, base SizeBase) (int64, error) {
	if base == 0 {
		base = Binary
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("empty value")}
	}
	// Split numeric prefix from the unit.
	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.' || t[i] == '+' || t[i] == '-') {
		i++
	}
	num, unit := t[:i], strings.TrimSpace(t[i:])
	if num == "" {
		return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("missing number")}
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("invalid number")}
	}
	if f < 0 {
		return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("negative size")}
	}
	mult := 1.0
	if unit != "" {
		u := strings.ToLower(unit)
		u = strings.TrimSuffix(u, "b")
		if u == "" && strings.ToLower(unit) != "b" {
			return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("unknown unit")}
		}
		if u != "" {
			b := float64(base)
			if strings.HasSuffix(u, "i") {
				b = float64(Binary)
				u = strings.TrimSuffix(u, "i")
			}
			if len(u) != 1 {
				return 0, &Error{Type: typeSize, Input: s, Cause: fmt.Errorf("unknown unit %q", unit)}
			}
			exp, ok := sizeExponent[u[0]]
			if !ok {
				return 0, &Error{Type: typeSize, Input: s, Cause: fmt.Errorf("unknown unit %q", unit)}
			}
			mult = math.Pow(b, float64(exp))
		}
	}
	v := math.Round(f * mult)
	// float64(math.MaxInt64) rounds up to 2^63, so >= is the correct
	// overflow test; 8E is exactly 2^63 bytes and must be rejected.
	if v >= math.MaxInt64 {
		return 0, &Error{Type: typeSize, Input: s, Cause: errors.New("value overflows int64")}
	}
	return int64(v), nil
}

// Time parses a timestamp with a Go reference layout and returns it as an
// RFC 3339 string. A layout that states a zone offset uses the one in the
// text; one that does not is read in loc, which is UTC unless the
// definition said otherwise.
//
// The answer is a string and never an epoch number. A timestamp has one
// shape in the output, so that a consumer does not have to ask which
// field carries which of two.
func Time(s, layout string, loc *time.Location) (string, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", &Error{Type: typeTime, Input: s, Cause: errors.New("empty value")}
	}
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation(layout, t, loc)
	if err != nil {
		var pe *time.ParseError
		if errors.As(err, &pe) {
			return "", &Error{Type: typeTime, Input: s, Cause: fmt.Errorf("does not match the layout %q", layout)}
		}
		return "", &Error{Type: typeTime, Input: s, Cause: err}
	}
	return parsed.Format(time.RFC3339Nano), nil
}

// Location resolves a definition's location name. An empty name is UTC.
func Location(name string) (*time.Location, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", LocationUTC:
		return time.UTC, true
	case LocationLocal:
		return time.Local, true
	default:
		return nil, false
	}
}

// Strip removes a prefix and a suffix (each may be empty) and trims
// surrounding whitespace. It is the pre-processing applied before every
// scalar conversion so that "32%" can become 32.
func Strip(s, prefix, suffix string) string {
	t := strings.TrimSpace(s)
	if prefix != "" {
		t = strings.TrimPrefix(t, prefix)
	}
	if suffix != "" {
		t = strings.TrimSuffix(t, suffix)
	}
	return strings.TrimSpace(t)
}

// StripANSI removes the escape sequences a command writes to colour its
// output. Several modern tools (eza, procs, ls --color=always) keep
// colouring even when their output is a pipe, and the escapes would
// otherwise sit inside the values and hide the format from detection.
//
// The input is returned unchanged when it holds no escape, which is the
// ordinary case, so nothing is copied for output that was never coloured.
func StripANSI(b []byte) []byte {
	if !bytes.ContainsRune(b, 0x1b) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] != 0x1b || i+1 >= len(b) {
			out = append(out, b[i])
			i++
			continue
		}
		switch b[i+1] {
		case '[':
			// CSI: parameter and intermediate bytes, then a final byte
			// in the range @ to ~. This is what colour uses.
			j := i + 2
			for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
				j++
			}
			if j < len(b) && b[j] >= 0x40 && b[j] <= 0x7e {
				i = j + 1
				continue
			}
			// Unterminated: keep the bytes rather than eat the rest.
			out = append(out, b[i])
			i++
		case ']':
			// OSC, which ls and eza use for terminal hyperlinks. It runs
			// to a BEL or to ESC \.
			j := i + 2
			for j < len(b) {
				if b[j] == 0x07 {
					j++
					break
				}
				if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		default:
			// Everything else: any intermediate bytes, then one final
			// byte. ESC ( B, which selects a character set, is three
			// bytes rather than two.
			j := i + 1
			for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
				j++
			}
			if j < len(b) {
				j++
			}
			i = j
		}
	}
	return out
}

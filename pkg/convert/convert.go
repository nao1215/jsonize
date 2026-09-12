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
	typeTime  = "time"

	typeDuration = "duration"
)

// The two time zones a definition may name. A zone database is not
// consulted: jz reads text a command printed on this machine, so the only
// two answers that are not guesses are the one the text states and the
// one the machine is in.
const (
	locationUTC   = "utc"
	locationLocal = "local"
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

// Int parses a base-10 integer. One leading sign is accepted, '+' or
// '-'; thousands separators are not, because jsonize always runs
// commands with LC_ALL=C.
func Int(s string) (int64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, &Error{Type: typeInt, Input: s, Cause: errors.New("empty value")}
	}
	v, err := strconv.ParseInt(t, 10, 64)
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
	// strconv also reads Go's own syntax: underscores between digits and
	// hexadecimal with a binary exponent. No command prints a decimal
	// that way, and Int already refuses both.
	for i := 0; i < len(t); i++ {
		if c := t[i]; !isDigit(c) && !strings.ContainsRune(".eE+-", rune(c)) {
			return 0, &Error{Type: typeFloat, Input: s, Cause: strconv.ErrSyntax}
		}
	}
	return v, nil
}

// defaultTrueValues and defaultFalseValues are the case-insensitive spellings
// recognised by Bool when a definition does not list its own.
var (
	defaultTrueValues  = []string{"true", "yes", "on", "1", "y"}
	defaultFalseValues = []string{"false", "no", "off", "0", "n"}
)

// Bool parses a boolean. trueValues and falseValues may be nil to use the
// defaults. Matching is case-insensitive.
func Bool(s string, trueValues, falseValues []string) (bool, error) {
	if trueValues == nil {
		trueValues = defaultTrueValues
	}
	if falseValues == nil {
		falseValues = defaultFalseValues
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

// Assumptions carries what the command line allowed jz to assume about a
// timestamp that does not say it itself. Both are empty by default,
// which is what keeps such a timestamp the string it was printed as.
type Assumptions struct {
	// Year dates a timestamp whose layout carries none. Zero means none
	// was given.
	Year int
	// Zones maps a zone abbreviation to its offset east of UTC in
	// seconds. An abbreviation names a different offset in different
	// parts of the world, so there is no table jz could ship.
	Zones map[string]int
}

// TimeAssuming parses a timestamp with a Go reference layout and returns
// it as an RFC 3339 string. A layout that states a zone offset uses the
// one in the text; one that does not is read in loc, which is UTC unless
// the definition said otherwise. The answer is a string and never an
// epoch number, so that a consumer does not have to ask which of two
// shapes a timestamp field carries.
//
// A layout may be missing its year, its zone or both, and then a is what
// decides. When the value needs an assumption that was not made, the
// text comes back as it was printed: a timestamp nobody dated is a
// string, and turning it into one silently dated by this machine's clock
// would be worse than leaving it alone.
//
// The second result says whether the value was converted, so a caller
// can tell an answer from a passed-through string.
func TimeAssuming(s, layout string, loc *time.Location, a Assumptions) (string, bool, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false, &Error{Type: typeTime, Input: s, Cause: errors.New("empty value")}
	}
	written, assumed := layout, false
	if !strings.Contains(layout, "06") {
		if a.Year == 0 {
			return s, false, nil
		}
		// Prepending is the one place a year can go that no layout can
		// already be using, so the reference year cannot collide with
		// the rest of the layout.
		layout = "2006 " + layout
		t = strconv.Itoa(a.Year) + " " + t
		assumed = true
	}
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation(layout, t, loc)
	if err != nil {
		// A value that fits the layout can still name a day its year does
		// not have (February 29), which is a different thing to say.
		var pe *time.ParseError
		if errors.As(err, &pe) && strings.Contains(pe.Message, "out of range") {
			if assumed {
				return "", false, &Error{Type: typeTime, Input: s, Cause: fmt.Errorf("is not a date in %d, the year it was assumed to be in", a.Year)}
			}
			return "", false, &Error{Type: typeTime, Input: s, Cause: errors.New("is not a date: " + strings.TrimPrefix(pe.Message, ": "))}
		}
		return "", false, &Error{Type: typeTime, Input: s, Cause: fmt.Errorf("does not match the layout %q", written)}
	}
	if strings.Contains(layout, "MST") {
		resolved, ok := resolveZone(parsed, a.Zones)
		if !ok {
			return s, false, nil
		}
		parsed = resolved
	}
	return parsed.Format(time.RFC3339Nano), true, nil
}

// resolveZone replaces the abbreviation a layout read with the offset the
// command line gave for it. Whatever Go made of the abbreviation is
// discarded: it resolves one against the running machine's own zone, so
// the same text would convert differently on two machines, which is the
// opposite of what this tool is for.
func resolveZone(t time.Time, zones map[string]int) (time.Time, bool) {
	name, _ := t.Zone()
	switch name {
	case "UTC", "GMT", "Z", "":
		return t.In(time.UTC), true
	}
	off, ok := zones[name]
	if !ok {
		return t, false
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.FixedZone(name, off)), true
}

// ParseZoneOffset reads the "+0900" half of an --assume-zone entry and
// returns the offset east of UTC in seconds.
func ParseZoneOffset(s string) (int, error) {
	t := strings.TrimSpace(s)
	if len(t) != 5 || (t[0] != '+' && t[0] != '-') || !allDigits(t[1:]) {
		return 0, fmt.Errorf("offset %q is not of the form +0900", s)
	}
	h, m := int(t[1]-'0')*10+int(t[2]-'0'), int(t[3]-'0')*10+int(t[4]-'0')
	if h > 23 || m > 59 {
		return 0, fmt.Errorf("offset %q is not a time of day", s)
	}
	off := h*3600 + m*60
	if t[0] == '-' {
		off = -off
	}
	return off, nil
}

// Location resolves a definition's location name. An empty name is UTC.
func Location(name string) (*time.Location, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", locationUTC:
		return time.UTC, true
	case locationLocal:
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
		// No escape sequence runs past the end of a line. Neither a
		// carriage return nor a newline is the final byte of any of them,
		// and letting one be eaten would join two records into one; it
		// also made the whole-document reader and the streaming one
		// disagree, since the streaming one has already cut the line off.
		limit := len(b)
		if n := bytes.IndexAny(b[i+1:], "\r\n"); n >= 0 {
			limit = i + 1 + n
		}
		end, ok := escapeEnd(b, i, limit)
		if !ok {
			// Unterminated: keep the byte rather than eat the rest.
			out = append(out, b[i])
			i++
			continue
		}
		i = end
	}
	return out
}

// escapeEnd returns the index just past the escape sequence starting at
// i, and whether the sequence ends before limit.
func escapeEnd(b []byte, i, limit int) (int, bool) {
	switch b[i+1] {
	case '[':
		// CSI: parameter and intermediate bytes, then a final byte in the
		// range @ to ~. This is what colour uses.
		j := i + 2
		for j < limit && b[j] >= 0x20 && b[j] <= 0x3f {
			j++
		}
		if j < limit && b[j] >= 0x40 && b[j] <= 0x7e {
			return j + 1, true
		}
		return 0, false
	case ']':
		// OSC, which ls and eza use for terminal hyperlinks. It runs to a
		// BEL or to ESC \.
		for j := i + 2; j < limit; j++ {
			if b[j] == 0x07 {
				return j + 1, true
			}
			if b[j] == 0x1b && j+1 < limit && b[j+1] == '\\' {
				return j + 2, true
			}
		}
		return 0, false
	default:
		// Everything else: any intermediate bytes, then one final byte.
		// ESC ( B, which selects a character set, is three bytes rather
		// than two.
		j := i + 1
		for j < limit && b[j] >= 0x20 && b[j] <= 0x2f {
			j++
		}
		if j >= limit {
			return 0, false
		}
		return j + 1, true
	}
}

// Duration layouts. They say what the last part of a bare two-part
// "A:B" is, which is the one thing the text itself cannot settle: ps
// prints a process time of four minutes fifty seconds as "4:50" and
// uptime prints an uptime of one hour twenty-three minutes as "1:23".
const (
	LayoutHourMinute   = "h:mm"
	LayoutMinuteSecond = "mm:ss"
)

// Duration turns a printed length of time into seconds.
//
// It accepts the spellings commands actually print, and only those that
// name one length exactly:
//
//	3-04:05:06     days-hours:minutes:seconds (ps etime)
//	04:05:06       hours:minutes:seconds
//	04:05          settled by layout: h:mm or mm:ss
//	01:23.45       the same with a fraction on the last part
//	13:42m         a trailing unit names the unit of the last part
//	13 days, 4:30  days and a clock reading (uptime)
//	1 day, 0:01
//	45 min         a number and the unit it is in
//	12 sec
//	3days
//	1h2m3s         units run together, largest first
//	3d4h
//
// The result is an int64 when the length is a whole number of seconds
// and a float64 when it is not. The value decides that and not the
// spelling, so "5.00s" and "5 sec" are the same 5 and "1.5m" is 90.
//
// A rounded reading is refused the way a rounded size is. That rule is
// about what the text names, not about how coarse it is: "13 days, 4:30"
// names exactly 1141800 seconds even though the machine has been up for
// some seconds more, while "1.8T" names no particular number of bytes
// until a base and a precision are chosen for it.
func Duration(s, layout string) (any, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil, &Error{Type: typeDuration, Input: s, Cause: errors.New("empty value")}
	}
	secs, err := parseDuration(t, layout)
	if err != nil {
		return nil, &Error{Type: typeDuration, Input: s, Cause: err}
	}
	// Past this a float64 no longer counts seconds one by one, so a
	// number beyond it would be reported more precisely than it is
	// known. It is some nine billion years, which no command prints.
	const maxSeconds = 1 << 53
	if math.IsNaN(secs) || secs < 0 || secs > maxSeconds {
		return nil, &Error{Type: typeDuration, Input: s, Cause: errors.New("out of range")}
	}
	if secs != math.Trunc(secs) {
		return secs, nil
	}
	return int64(secs), nil
}

// errDurationShape is the message every unreadable spelling gets. Listing
// the forms is more use than naming which of them the text came closest
// to, since a value that fits none of them is usually not a duration.
var errDurationShape = errors.New(`not one of the known forms (3-04:05:06, 04:05:06, 04:05, 13 days, 4:30, 45 min, 1h2m3s)`)

// parseDuration returns the length in seconds.
func parseDuration(t, layout string) (float64, error) {
	if days, rest, ok := splitLeadingDays(t); ok {
		if rest == "" {
			return days * 86400, nil
		}
		secs, err := parseClock(rest, LayoutHourMinute)
		if err != nil {
			return 0, err
		}
		return days*86400 + secs, nil
	}
	if strings.Contains(t, ":") {
		return parseClock(t, layout)
	}
	return parseUnits(t)
}

// splitLeadingDays reads the "13 days," or "1 day," that uptime puts in
// front of a clock reading. What follows it is hours and minutes
// whatever the field's layout says, because a reading that already
// carries its days cannot be minutes and seconds.
func splitLeadingDays(t string) (days float64, rest string, ok bool) {
	i := strings.Index(t, ",")
	if i < 0 {
		return 0, "", false
	}
	head, tail := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
	n, unit, found := splitNumberAndUnit(head)
	if !found || (unit != "day" && unit != "days") {
		return 0, "", false
	}
	return n, tail, true
}

// parseClock reads a colon-separated reading, with an optional leading
// "D-" and an optional trailing unit letter.
func parseClock(t, layout string) (float64, error) {
	var days float64
	if i := strings.Index(t, "-"); i > 0 {
		if !allDigits(t[:i]) {
			return 0, errDurationShape
		}
		n, err := strconv.ParseFloat(t[:i], 64)
		if err != nil {
			return 0, errDurationShape
		}
		days, t = n, t[i+1:]
	}
	// A trailing unit names the unit of the last part, which is how w
	// tells "13:42m" (thirteen hours) from "13:42" (thirteen minutes).
	last := ""
	if n := len(t); n > 0 && !isDigit(t[n-1]) {
		switch t[n-1] {
		case 'm':
			last, t = "m", t[:n-1]
		case 's':
			last, t = "s", t[:n-1]
		case 'h':
			last, t = "h", t[:n-1]
		default:
			return 0, errDurationShape
		}
	}
	parts := strings.Split(t, ":")
	if len(parts) > 3 {
		return 0, errDurationShape
	}
	// The units of the parts, smallest last. Three parts are always
	// hours, minutes and seconds; two are settled by the trailing unit,
	// then by the layout.
	units := []float64{3600, 60, 1}
	if len(parts) == 2 {
		switch {
		case last == "m", layout == LayoutHourMinute && last == "":
			units = []float64{3600, 60}
		case last == "s", last == "h", layout == LayoutMinuteSecond, layout == "":
			units = []float64{60, 1}
		}
	}
	if len(parts) == 1 {
		units = []float64{1}
		if last == "m" {
			units = []float64{60}
		}
		if last == "h" {
			units = []float64{3600}
		}
	}
	units = units[len(units)-len(parts):]
	var total float64
	for i, p := range parts {
		if p == "" || (!allDigits(p) && !isDecimal(p)) {
			return 0, errDurationShape
		}
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || n < 0 {
			return 0, errDurationShape
		}
		// Every part after the first is a minute or a second of the
		// clock, so it is under 60; the first part carries the length
		// and may be any number of hours or minutes. A reading that
		// breaks that is not a length written oddly but text that is
		// not a clock reading, and adding it up would report a number
		// nothing printed. The time type refuses the same way.
		if i > 0 && n >= 60 {
			return 0, fmt.Errorf("%s is not a %s of a clock reading, which is 0 to 59", p, clockUnitName(units[i]))
		}
		total += n * units[i]
	}
	return total + days*86400, nil
}

// clockUnitName names a part of a clock reading by its length in
// seconds, for the message a part out of range gets.
func clockUnitName(seconds float64) string {
	if seconds == 60 {
		return "minute"
	}
	return "second"
}

// isDecimal reports a run of digits with one dot in it, which is what a
// fractional last part of a clock reading looks like.
func isDecimal(s string) bool {
	whole, frac, ok := strings.Cut(s, ".")
	return ok && allDigits(whole) && allDigits(frac)
}

// durationUnits maps every spelling of a unit to its length in seconds.
// The plural and the abbreviation are separate entries rather than a
// rule, because a rule that strips an "s" would also strip the "s" that
// means seconds.
var durationUnits = map[string]float64{
	"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3,
	"s": 1, "sec": 1, "secs": 1, "second": 1, "seconds": 1,
	"m": 60, "min": 60, "mins": 60, "minute": 60, "minutes": 60,
	"h": 3600, "hr": 3600, "hrs": 3600, "hour": 3600, "hours": 3600,
	"d": 86400, "day": 86400, "days": 86400,
	"w": 604800, "wk": 604800, "week": 604800, "weeks": 604800,
}

// durationUnit looks a unit up. An abbreviation of one or two letters is
// matched as written, because its case is part of it: systemd writes a
// month as "M" where "m" is a minute, and "M" after a number is as often
// a megabyte. A unit spelled as a word is matched whatever its case.
func durationUnit(u string) (float64, bool) {
	if mult, ok := durationUnits[u]; ok {
		return mult, true
	}
	if len(u) <= 2 {
		return 0, false
	}
	mult, ok := durationUnits[strings.ToLower(u)]
	return mult, ok
}

// parseUnits reads a number followed by its unit, repeated: "45 min",
// "1h2m3s", "3days". A space between the number and the unit is
// optional, and the parts must run from the largest unit to the
// smallest, so that "1m2h" is refused rather than silently added up.
func parseUnits(t string) (float64, error) {
	var total float64
	prev := math.Inf(1)
	for t != "" {
		t = strings.TrimLeft(t, " \t")
		j := 0
		for j < len(t) && (isDigit(t[j]) || t[j] == '.') {
			j++
		}
		num := t[:j]
		if j == 0 || (!allDigits(num) && !isDecimal(num)) {
			return 0, errDurationShape
		}
		n, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, errDurationShape
		}
		t = strings.TrimLeft(t[j:], " \t")
		k := 0
		for k < len(t) && !isDigit(t[k]) && t[k] != ' ' && t[k] != '\t' {
			k++
		}
		if k == 0 {
			return 0, errDurationShape
		}
		mult, ok := durationUnit(t[:k])
		if !ok || mult >= prev {
			return 0, errDurationShape
		}
		prev = mult
		total += n * mult
		t = t[k:]
	}
	return total, nil
}

// splitNumberAndUnit reads "13 days" as 13 and "days".
func splitNumberAndUnit(s string) (float64, string, bool) {
	i := 0
	for i < len(s) && (isDigit(s[i]) || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, "", false
	}
	return n, strings.ToLower(strings.TrimSpace(s[i:])), true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

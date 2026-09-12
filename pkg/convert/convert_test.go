package convert

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{" 42 ", 42, false},
		{"+7", 7, false},
		{"-13", -13, false},
		// One sign, not two: a second one is not something a number
		// prints, and reading past it would turn "+-1" into -1.
		{"++1", 0, true},
		{"+-1", 0, true},
		{"-+1", 0, true},
		{"--1", 0, true},
		{"9223372036854775807", math.MaxInt64, false},
		{"9223372036854775808", 0, true},
		{"", 0, true},
		{"1,000", 0, true},
		{"1.5", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := Int(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Int(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Int(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFloat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"0.05", 0.05, false},
		{"1", 1, false},
		{"-2.5", -2.5, false},
		{"1e3", 1000, false},
		{"NaN", 0, true},
		{"Inf", 0, true},
		{"", 0, true},
		{"x", 0, true},
		// Go's own number syntax, which no command prints for a decimal
		// and which Int already refuses.
		{"1_000.5", 0, true},
		{"0x1p3", 0, true},
		{"0X1P-2", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := Float(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Float(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Float(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		tv, fv  []string
		want    bool
		wantErr bool
	}{
		{"true", nil, nil, true, false},
		{"YES", nil, nil, true, false},
		{"off", nil, nil, false, false},
		{"0", nil, nil, false, false},
		{"maybe", nil, nil, false, true},
		{"enabled", []string{"enabled"}, []string{"disabled"}, true, false},
		{"disabled", []string{"enabled"}, []string{"disabled"}, false, false},
		{"yes", []string{"enabled"}, []string{"disabled"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := Bool(tt.in, tt.tv, tt.fv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Bool(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Bool(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestStrip(t *testing.T) {
	t.Parallel()
	if got := Strip(" 32% ", "", "%"); got != "32" {
		t.Errorf("Strip = %q, want 32", got)
	}
	if got := Strip("Mem:", "", ":"); got != "Mem" {
		t.Errorf("Strip = %q, want Mem", got)
	}
	if got := Strip("$HOME", "$", ""); got != "HOME" {
		t.Errorf("Strip = %q, want HOME", got)
	}
}

func TestErrorUnwrap(t *testing.T) {
	t.Parallel()
	_, err := Int("x")
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *Error: %T", err)
	}
	if ce.Unwrap() == nil {
		t.Error("expected wrapped cause")
	}
	if ce.Error() == "" {
		t.Error("empty message")
	}
	e := &Error{Type: "int", Input: "x"}
	if e.Error() != `cannot convert "x" to int` {
		t.Errorf("message = %q", e.Error())
	}
}

// A float is a number JSON can carry, and an int or a float is only
// read from text that says the number in decimal.
func FuzzScalars(f *testing.F) {
	for _, s := range []string{"1", "-2.5", "1e3", "", "-", "1e400", "999999999999999999999", "0x1p3", "1_0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if v, err := Float(s); err == nil {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("Float(%q) = %v", s, v)
			}
			if strings.ContainsAny(s, "_xXpP") {
				t.Fatalf("Float(%q) = %v read Go's own syntax", s, v)
			}
		}
		if _, err := Int(s); err == nil && strings.ContainsAny(s, "_xX.eE") {
			t.Fatalf("Int(%q) read something other than decimal digits", s)
		}
		_, _ = Bool(s, nil, nil)
	})
}

// A duration is never negative and never NaN, whatever the text was: a
// value JSON cannot carry, or one that ran backwards, would be worse
// than refusing the input.
func FuzzDuration(f *testing.F) {
	for _, s := range []string{
		"3-04:05:06", "04:05", "13 days, 4:30", "45 min", "1h2m3s", "13:42m",
		"", ":", "-", "1e400s", "99999999999999999999d", "0.00s", "1-2-3:4",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, layout := range []string{LayoutHourMinute, LayoutMinuteSecond} {
			v, err := Duration(s, layout)
			if err != nil {
				continue
			}
			switch n := v.(type) {
			case int64:
				if n < 0 {
					t.Fatalf("negative duration %d from %q", n, s)
				}
			case float64:
				if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
					t.Fatalf("duration %v from %q is not a JSON number", n, s)
				}
			default:
				t.Fatalf("duration of %q is a %T", s, v)
			}
		}
	})
}

func TestStripANSI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is returned untouched", "Filesystem 1K-blocks\n", "Filesystem 1K-blocks\n"},
		{"colour", "\x1b[32mgreen\x1b[0m", "green"},
		{"bold and reset around a cell", "a \x1b[1;31mb\x1b[m c", "a b c"},
		// No escape sequence has a line break as its final byte, and
		// eating one would join two records into one. It also made the
		// whole-document reader and the streaming one disagree, since the
		// streaming one has already cut the line off.
		{"a stray escape at the end of a line keeps the line break", "a\x1b\nb\n", "a\x1b\nb\n"},
		{"a stray escape before a carriage return keeps it", "a\x1b\r\n", "a\x1b\r\n"},
		{"an unterminated CSI at the end of a line keeps the line break", "a\x1b[1\nb\n", "a\x1b[1\nb\n"},
		{"an unterminated hyperlink does not eat the rest of the output", "\x1b]8;;http\nplain\n", "\x1b]8;;http\nplain\n"},
		{"osc hyperlink terminated by BEL", "\x1b]8;;file:///x\x07name\x1b]8;;\x07", "name"},
		{"osc hyperlink terminated by ESC backslash", "\x1b]8;;u\x1b\\name\x1b]8;;\x1b\\", "name"},
		{"two character sequence", "\x1b(Btext", "text"},
		// A lone escape at the end is data as far as jz can tell, so it
		// is kept rather than swallowing what came before it.
		{"unterminated CSI", "a\x1b[32", "a\x1b[32"},
		{"trailing escape", "a\x1b", "a\x1b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(StripANSI([]byte(tt.in))); got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTime(t *testing.T) {
	t.Parallel()
	utc, _ := Location("")
	local, ok := Location("LOCAL")
	if !ok {
		t.Fatal("local is a location")
	}
	tests := []struct {
		name   string
		in     string
		layout string
		loc    *time.Location
		want   string
	}{
		{"offset in the text", "2026-09-07T14:20:01+0900", "2006-01-02T15:04:05-0700", utc, "2026-09-07T14:20:01+09:00"},
		{"colon in the offset", "2026-09-07T14:20:01+09:00", "2006-01-02T15:04:05-07:00", utc, "2026-09-07T14:20:01+09:00"},
		{"fraction kept", "2026-09-07 14:20:01.5 +0900", "2006-01-02 15:04:05.999999999 -0700", utc, "2026-09-07T14:20:01.5+09:00"},
		{"trailing zeros dropped", "2026-09-07 14:20:01.500 +0900", "2006-01-02 15:04:05.999999999 -0700", utc, "2026-09-07T14:20:01.5+09:00"},
		{"no zone reads as UTC", "2026-09-07 14:20:01", "2006-01-02 15:04:05", utc, "2026-09-07T14:20:01Z"},
		{"surrounding space", "  2026-09-07 14:20:01  ", "2006-01-02 15:04:05", nil, "2026-09-07T14:20:01Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := TimeAssuming(tt.in, tt.layout, tt.loc, Assumptions{})
			if err != nil || got != tt.want {
				t.Errorf("Time(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
	// The same text read in the running system's zone names the same
	// wall clock with that zone's offset.
	got, _, err := TimeAssuming("2026-09-07 14:20:01", "2006-01-02 15:04:05", local, Assumptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 7, 14, 20, 1, 0, time.Local).Format(time.RFC3339Nano)
	if got != want {
		t.Errorf("local = %q, want %q", got, want)
	}
	for _, in := range []string{"", "  ", "not a time", "2026-13-45 99:99:99"} {
		if _, _, err := TimeAssuming(in, "2006-01-02 15:04:05", utc, Assumptions{}); err == nil {
			t.Errorf("Time(%q) should fail", in)
		}
	}
	if _, ok := Location("Asia/Tokyo"); ok {
		t.Error("only utc and local are locations")
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		layout string
		want   any
	}{
		// A reading with three or more parts settles itself.
		{"3-04:05:06", LayoutMinuteSecond, int64(273906)},
		{"04:05:06", LayoutMinuteSecond, int64(14706)},
		{"0-00:00:01", LayoutHourMinute, int64(1)},
		// Two parts are what the layout is for.
		{"4:50", LayoutMinuteSecond, int64(290)},
		{"1:23", LayoutHourMinute, int64(4980)},
		// A fraction on the last part survives as one.
		{"01:23.45", LayoutMinuteSecond, 83.45},
		{"0:00.09", LayoutMinuteSecond, 0.09},
		{"00:00:01.5", LayoutMinuteSecond, 1.5},
		// A trailing unit names the unit of the last part, which is how w
		// separates thirteen hours from thirteen minutes.
		{"13:42m", LayoutMinuteSecond, int64(49320)},
		{"13:42", LayoutMinuteSecond, int64(822)},
		{"5.00s", LayoutMinuteSecond, int64(5)},
		// Days in front of a clock reading, which is uptime.
		{"13 days, 4:22", LayoutMinuteSecond, int64(1138920)},
		{"1 day, 0:01", LayoutMinuteSecond, int64(86460)},
		{"3 days,  4:11", LayoutMinuteSecond, int64(274260)},
		// A number and its unit, run together or spelled out.
		{"45 min", LayoutMinuteSecond, int64(2700)},
		{"12 sec", LayoutMinuteSecond, int64(12)},
		{"3days", LayoutMinuteSecond, int64(259200)},
		{"1h2m3s", LayoutMinuteSecond, int64(3723)},
		{"3d4h", LayoutMinuteSecond, int64(273600)},
		{"1.5h", LayoutMinuteSecond, int64(5400)},
		{"250ms", LayoutMinuteSecond, 0.25},
		{"2 hours", LayoutMinuteSecond, int64(7200)},
		// A unit spelled as a word is read whatever its case.
		{"2 Hours", LayoutMinuteSecond, int64(7200)},
		{"5 MIN", LayoutMinuteSecond, int64(300)},
		// systemd writes a long startup this way.
		{"1w 6d 18h 15min 19.086s", LayoutMinuteSecond, 1188919.086},
		{"2min 44.575s", LayoutMinuteSecond, 164.575},
	}
	for _, tt := range tests {
		got, err := Duration(tt.in, tt.layout)
		if err != nil {
			t.Errorf("Duration(%q, %q): %v", tt.in, tt.layout, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Duration(%q, %q) = %#v, want %#v", tt.in, tt.layout, got, tt.want)
		}
	}
}

func TestDurationRefuses(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "   ",
		"2 users",      // a number and a word that is not a unit
		"1.8T",         // a rounded size
		"1m2h",         // units out of order, which would otherwise add up
		"12:34:56:78",  // more parts than a clock reading has
		"-1:00",        // no negative durations
		"1:2x",         // a trailing letter that names no unit
		"::",           // empty parts
		"4:",           // an empty last part
		"Jan  5 10:11", // a date
		"3 days 4:11",  // the comma is what says the days ended
		// A part after the first is a minute or a second of a clock, so
		// it is 0 to 59. The first part carries the length and may be
		// any number of hours or minutes.
		"1:60",
		"1:99",
		"1:60:00",
		"1:00:60",
		"1-00:60:00",
		"13 days, 4:60",
		// The days before the dash are a count, printed as digits and
		// nothing else: Go's number syntax is not what a command prints.
		"1_0-01:02:03",
		"0x1p1-01:02:03",
		"1e3-00:00:00",
		"1.5-01:02:03",
		"+2-01:00:00",
		"inf-01:00:00",
		// A one-letter unit is its case: systemd writes a month as "M"
		// where "m" is a minute, and a size writes a megabyte that way.
		"1M", "2H", "3D", "4MS",
	} {
		if v, err := Duration(in, LayoutMinuteSecond); err == nil {
			t.Errorf("Duration(%q) = %v, want an error", in, v)
		}
	}
}

// A timestamp whose format states no year, or states a zone only by its
// abbreviation, is left as the text it was printed as until the command
// line says what to assume. Dating it by this machine's clock, or
// reading an abbreviation against this machine's own zone, would make
// the same text convert differently on two machines.
func TestTimeAssuming(t *testing.T) {
	t.Parallel()
	const noYear = "Jan _2 15:04"
	if got, ok, err := TimeAssuming("Nov  4 13:17", noYear, nil, Assumptions{}); err != nil || ok || got != "Nov  4 13:17" {
		t.Errorf("no year given: %q %v %v", got, ok, err)
	}
	if got, ok, err := TimeAssuming("Nov  4 13:17", noYear, nil, Assumptions{Year: 2025}); err != nil || !ok || got != "2025-11-04T13:17:00Z" {
		t.Errorf("year assumed: %q %v %v", got, ok, err)
	}
	// February 29 fits the layout and is not a day of 2025. The message
	// says that, and names the layout the definition wrote rather than
	// the one with the assumed year put in front of it.
	if _, _, err := TimeAssuming("Feb 29 10:00", noYear, nil, Assumptions{Year: 2025}); err == nil ||
		!strings.Contains(err.Error(), "is not a date in 2025, the year it was assumed to be in") {
		t.Errorf("a day the assumed year does not have: %v", err)
	}
	if _, _, err := TimeAssuming("29 Feb 10:00", noYear, nil, Assumptions{Year: 2025}); err == nil ||
		!strings.Contains(err.Error(), `does not match the layout "Jan _2 15:04"`) {
		t.Errorf("text the layout does not describe: %v", err)
	}

	const zoned = "Mon Jan _2 15:04:05 MST 2006"
	if got, ok, err := TimeAssuming("Mon Sep  7 10:02:02 JST 2026", zoned, nil, Assumptions{}); err != nil || ok || got != "Mon Sep  7 10:02:02 JST 2026" {
		t.Errorf("no zone given: %q %v %v", got, ok, err)
	}
	a := Assumptions{Zones: map[string]int{"JST": 9 * 3600}}
	if got, ok, err := TimeAssuming("Mon Sep  7 10:02:02 JST 2026", zoned, nil, a); err != nil || !ok || got != "2026-09-07T10:02:02+09:00" {
		t.Errorf("zone assumed: %q %v %v", got, ok, err)
	}
	// UTC and GMT state their own offset, so they need no assumption.
	if got, _, err := TimeAssuming("Mon Sep  7 01:02:02 UTC 2026", zoned, nil, Assumptions{}); err != nil || got != "2026-09-07T01:02:02Z" {
		t.Errorf("utc: %q %v", got, err)
	}
	// An abbreviation with no offset given is left alone even when this
	// machine happens to be in a zone of that name. Only a machine whose
	// zone has an abbreviation can be asked: Windows names its zones in
	// words ("Coordinated Universal Time"), and a layout looking for MST
	// has nothing to say about those.
	local, _ := Location("local")
	if name, _ := time.Now().In(local).Zone(); isZoneAbbreviation(name) && name != "UTC" && name != "GMT" {
		in := "Mon Sep  7 10:02:02 " + name + " 2026"
		if got, ok, _ := TimeAssuming(in, zoned, local, Assumptions{}); ok || got != in {
			t.Errorf("local abbreviation %q was resolved: %q", name, got)
		}
	}
	// A layout that does carry a year ignores the assumption.
	if got, _, err := TimeAssuming("2024-05-06 07:08:09", "2006-01-02 15:04:05", nil, Assumptions{Year: 1999}); err != nil || got != "2024-05-06T07:08:09Z" {
		t.Errorf("year in the layout: %q %v", got, err)
	}
}

// isZoneAbbreviation reports the short all-letters form a layout with MST
// in it can read, as opposed to a zone named in words or as an offset.
func isZoneAbbreviation(name string) bool {
	if len(name) < 2 || len(name) > 5 {
		return false
	}
	for _, r := range name {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func TestParseZoneOffset(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{"+0900": 9 * 3600, "-0500": -5 * 3600, "+0000": 0, "+0530": 5*3600 + 1800} {
		got, err := ParseZoneOffset(in)
		if err != nil || got != want {
			t.Errorf("ParseZoneOffset(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0900", "+900", "+09:00", "JST", "+2400", "+0060", "++900"} {
		if _, err := ParseZoneOffset(in); err == nil {
			t.Errorf("ParseZoneOffset(%q) accepted", in)
		}
	}
}

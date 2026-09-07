package convert

import (
	"errors"
	"math"
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

func TestSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		base    SizeBase
		want    int64
		wantErr bool
	}{
		{"0", Binary, 0, false},
		{"1024", Binary, 1024, false},
		{"0B", Binary, 0, false},
		{"1K", Binary, 1024, false},
		{"1k", Binary, 1024, false},
		{"1.5K", Binary, 1536, false},
		{"955M", Binary, 955 * 1024 * 1024, false},
		{"3.7G", Binary, 3972844749, false},
		{"466Gi", Binary, 466 * 1024 * 1024 * 1024, false},
		{"6.0Gi", Binary, 6 * 1024 * 1024 * 1024, false},
		{"1 MiB", Binary, 1048576, false},
		{"1 MB", Decimal, 1000000, false},
		{"1M", Decimal, 1000000, false},
		{"1Mi", Decimal, 1048576, false},
		{"2T", Binary, 2 * 1024 * 1024 * 1024 * 1024, false},
		{"", Binary, 0, true},
		{"G", Binary, 0, true},
		{"-1K", Binary, 0, true},
		{"1X", Binary, 0, true},
		{"1KX", Binary, 0, true},
		{"1..2K", Binary, 0, true},
		{"99999999999E", Binary, 0, true},
		{"8E", Binary, 0, true},
		{"7E", Binary, 7 << 60, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := Size(tt.in, tt.base)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Size(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Size(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSizeDefaultBase(t *testing.T) {
	t.Parallel()
	got, err := Size("1K", 0)
	if err != nil || got != 1024 {
		t.Fatalf("Size with zero base = %d, %v; want 1024", got, err)
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

func FuzzSize(f *testing.F) {
	for _, s := range []string{"1K", "3.7G", "0B", "1 MiB", "", "-1", "1e400K", "999999999999999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := Size(s, Binary)
		if err == nil && v < 0 {
			t.Fatalf("negative size %d from %q", v, s)
		}
		_, _ = Size(s, Decimal)
		_, _ = Int(s)
		_, _ = Float(s)
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
	// machine happens to be in a zone of that name.
	local, _ := Location("local")
	name, _ := time.Now().In(local).Zone()
	if name != "UTC" && name != "" {
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

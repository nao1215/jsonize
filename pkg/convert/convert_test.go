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
			got, err := Time(tt.in, tt.layout, tt.loc)
			if err != nil || got != tt.want {
				t.Errorf("Time(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
	// The same text read in the running system's zone names the same
	// wall clock with that zone's offset.
	got, err := Time("2026-09-07 14:20:01", "2006-01-02 15:04:05", local)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 7, 14, 20, 1, 0, time.Local).Format(time.RFC3339Nano)
	if got != want {
		t.Errorf("local = %q, want %q", got, want)
	}
	for _, in := range []string{"", "  ", "not a time", "2026-13-45 99:99:99"} {
		if _, err := Time(in, "2006-01-02 15:04:05", utc); err == nil {
			t.Errorf("Time(%q) should fail", in)
		}
	}
	if _, ok := Location("Asia/Tokyo"); ok {
		t.Error("only utc and local are locations")
	}
}

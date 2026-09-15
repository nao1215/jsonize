package recordio

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

// The limit counts the bytes between two separators, so a record at the
// limit is read and the one byte past it is refused, whether or not a
// separator follows it.
func TestReadBoundsARecordByTheBytesBetweenSeparators(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
		sep   byte
		max   int
		want  string
		err   error
	}{
		{name: "at the limit", input: "12345\n", sep: '\n', max: 5, want: "12345"},
		{name: "one byte over", input: "123456\n", sep: '\n', max: 5, err: ErrTooLong},
		{name: "at the limit unterminated", input: "12345", sep: '\n', max: 5, want: "12345", err: io.EOF},
		{name: "one byte over unterminated", input: "123456", sep: '\n', max: 5, err: ErrTooLong},
		{name: "a carriage return is part of the record", input: "1234\r\n", sep: '\n', max: 4, err: ErrTooLong},
		{name: "a carriage return stays on", input: "123\r\n", sep: '\n', max: 4, want: "123\r"},
		{name: "empty record", input: "\na\n", sep: '\n', max: 5, want: ""},
		{name: "NUL separated", input: "12345\x00", sep: 0, max: 5, want: "12345"},
		{name: "NUL separated over the limit", input: "123456\x00", sep: 0, max: 5, err: ErrTooLong},
		{name: "longer than the read buffer", input: strings.Repeat("x", 40) + "\n", sep: '\n', max: 64, want: strings.Repeat("x", 40)},
		{name: "longer than the read buffer and over the limit", input: strings.Repeat("x", 40) + "\n", sep: '\n', max: 30, err: ErrTooLong},
	} {
		// A buffer smaller than the records makes ReadSlice hand the
		// record over in pieces, which is what a reader of a pipe does.
		br := bufio.NewReaderSize(strings.NewReader(tc.input), 16)
		got, err := Read(br, tc.sep, tc.max)
		if tc.err != nil && !errors.Is(err, tc.err) {
			t.Errorf("%s: Read = %q, %v, want %v", tc.name, got, err, tc.err)
			continue
		}
		if tc.err == nil && err != nil {
			t.Errorf("%s: Read = %q, %v", tc.name, got, err)
			continue
		}
		if !errors.Is(err, ErrTooLong) && string(got) != tc.want {
			t.Errorf("%s: Read = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The records come back one at a time, without their separator, and the
// last one of an input that ends without one comes back with io.EOF.
func TestReadHandsOverOneRecordAtATime(t *testing.T) {
	t.Parallel()
	br := bufio.NewReaderSize(strings.NewReader("a\nb\nc"), 16)
	var got []string
	for {
		rec, err := Read(br, '\n', 64)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Read: %v", err)
		}
		if len(rec) == 0 && err != nil {
			break
		}
		got = append(got, string(rec))
		if err != nil {
			break
		}
	}
	if strings.Join(got, "|") != "a|b|c" {
		t.Errorf("records = %q", got)
	}
}

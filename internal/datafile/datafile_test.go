package datafile

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/engine"
)

func TestFromPath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path, format, compression string
	}{
		{"users.csv", CSV, ""},
		{"dir.d/EXPORT.CSV", CSV, ""},
		{"a.tsv", TSV, ""},
		{"access.ltsv", LTSV, ""},
		{"events.jsonl", JSONL, ""},
		{"events.ndjson", JSONL, ""},
		{"data.json", JSON, ""},
		{"conf.yaml", YAML, ""},
		{"conf.YML", YAML, ""},
		{"events.jsonl.gz", JSONL, Gzip},
		{"rows.csv.bz2", CSV, Bzip2},
		{"notes.txt.gz", "", Gzip},
		{"plain.gz", "", Gzip},
		{".json", "", ""},
		{".csv.gz", "", Gzip},
		{"df.txt", "", ""},
		{"/etc/fstab", "", ""},
		{"archive.tar.gz", "", Gzip},
		{"-", "", ""},
	} {
		format, compression := FromPath(tt.path)
		if format != tt.format || compression != tt.compression {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tt.path, format, compression, tt.format, tt.compression)
		}
	}
}

func TestNames(t *testing.T) {
	t.Parallel()
	for _, n := range Names() {
		if !Known(n) {
			t.Errorf("%s is listed and not known", n)
		}
		if Tabular(n) == Streams(n) && Tabular(n) {
			t.Errorf("%s is both tabular and a line format", n)
		}
	}
	if Known("xml") || Known("") {
		t.Error("an unlisted format is known")
	}
	if !Tabular(CSV) || !Tabular(TSV) || Tabular(JSON) {
		t.Error("tabular formats")
	}
	if !Streams(JSONL) || !Streams(LTSV) || Streams(YAML) || Streams(CSV) {
		t.Error("line formats")
	}
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecompress(t *testing.T) {
	t.Parallel()
	r, err := Decompress(bytes.NewReader([]byte("as is")), "")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "as is" {
		t.Errorf("no compression: %q", b)
	}
	r, err = Decompress(bytes.NewReader(gzipped(t, "{\"a\":1}\n")), Gzip)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "{\"a\":1}\n" {
		t.Errorf("gzip: %q", b)
	}
	var ce *CompressionError
	if _, err := Decompress(strings.NewReader("not gzip"), Gzip); !errors.As(err, &ce) || ce.Compression != Gzip || !strings.Contains(err.Error(), "cannot be decompressed") {
		t.Errorf("a file that is not gzip: %v", err)
	}
	// Cut short after the header: the failure comes while reading.
	whole := gzipped(t, strings.Repeat("data ", 100))
	r, err = Decompress(bytes.NewReader(whole[:len(whole)-10]), Gzip)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.As(err, &ce) || errors.Unwrap(err) == nil {
		t.Errorf("a gzip file cut short: %v", err)
	}
	r, err = Decompress(strings.NewReader("BZh9 not really"), Bzip2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.As(err, &ce) || ce.Compression != Bzip2 {
		t.Errorf("a file that is not bzip2: %v", err)
	}
	if _, err := Decompress(strings.NewReader(""), "zstd"); err == nil {
		t.Error("an unknown compression was accepted")
	}
}

func TestStream(t *testing.T) {
	t.Parallel()
	var got []string
	err := Stream(JSONL, strings.NewReader("1\n2\n3"), 10, func(v any) error {
		got = append(got, encode(t, v))
		return nil
	}, nil)
	if err != nil || strings.Join(got, ",") != "1,2,3" {
		t.Errorf("records: %v %v", got, err)
	}
	// A line longer than the limit is refused where it is; the ones
	// before it were handed on already.
	got = nil
	err = Stream(JSONL, strings.NewReader("1\n"+strings.Repeat("2", 5000)+"\n3\n"), 4096, func(v any) error {
		got = append(got, encode(t, v))
		return nil
	}, nil)
	parseError(t, err, 2, "longer than 4096 bytes")
	if len(got) != 1 {
		t.Errorf("the records before the long line: %v", got)
	}
	// A line exactly at the limit is read.
	if err := Stream(LTSV, strings.NewReader("k:"+strings.Repeat("v", 98)+"\n"), 100, func(any) error { return nil }, nil); err != nil {
		t.Errorf("a line at the limit: %v", err)
	}
	stop := errors.New("stop")
	if err := Stream(JSONL, strings.NewReader("1\n2\n"), 10, func(any) error { return stop }, nil); !errors.Is(err, stop) {
		t.Errorf("emit's error: %v", err)
	}
	calls := 0
	err = Stream(JSONL, io.MultiReader(strings.NewReader("1\n"), iotest.ErrReader(iotest.ErrTimeout)), 10, func(any) error {
		calls++
		return nil
	}, nil)
	if !errors.Is(err, iotest.ErrTimeout) || calls != 1 {
		t.Errorf("a reader that fails: %v after %d records", err, calls)
	}
	if err := Stream(JSON, strings.NewReader("1"), 10, func(any) error { return nil }, nil); err == nil {
		t.Error("a document format was streamed")
	}
	if v, err := Read(CSV, []byte("a,b\n1,\"x,y\"\n")); err != nil || encode(t, v) != `[{"a":"1","b":"x,y"}]` {
		t.Errorf("a csv: %v %v", v, err)
	}
	if v, err := Read(TSV, []byte("a\tb\n1\t2\n")); err != nil || encode(t, v) != `[{"a":"1","b":"2"}]` {
		t.Errorf("a tsv: %v %v", v, err)
	}
	if _, err := Read(CSV, []byte("a,b\n1,2,3\n")); err == nil {
		t.Error("a ragged csv was read")
	}
	// A csv is data: an escape sequence is part of a value, and a record
	// of spaces is a record. An empty line holds no record, as in the
	// csv readers of other languages.
	if v, err := Read(CSV, []byte("k\n\x1b[31mred\x1b[0m\n   \n\nx\n")); err != nil || encode(t, v) != `[{"k":"\u001b[31mred\u001b[0m"},{"k":"   "},{"k":"x"}]` {
		t.Errorf("a csv read as data: %s %v", encode(t, v), err)
	}
	if _, err := Read("xml", []byte("<a/>")); err == nil {
		t.Error("an unknown format was read")
	}
}

// A line format reads the same whether the whole of it is read or it is
// streamed, and fails on the same line with the same message.
func FuzzLineFormats(f *testing.F) {
	for _, s := range []string{"a:1\tb:2\n", "{\"a\":1}\n[1]\n", "\n\r\n", "x\n", "a:\xff\n", "\xEF\xBB\xBFa:1"} {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, data string, ltsv bool) {
		format := JSONL
		if ltsv {
			format = LTSV
		}
		whole, werr := Read(format, []byte(data))
		var streamed []any
		serr := Stream(format, iotest.OneByteReader(strings.NewReader(data)), len(data)+1, func(v any) error {
			streamed = append(streamed, v)
			return nil
		}, nil)
		if (werr == nil) != (serr == nil) {
			t.Fatalf("whole %v, streamed %v", werr, serr)
		}
		if werr != nil {
			var pe *engine.ParseError
			if !errors.As(werr, &pe) || werr.Error() != serr.Error() {
				t.Fatalf("whole %v, streamed %v", werr, serr)
			}
			return
		}
		if streamed == nil {
			streamed = []any{}
		}
		if encode(t, whole) != encode(t, streamed) {
			t.Fatalf("whole %s, streamed %s", encode(t, whole), encode(t, streamed))
		}
	})
}

// text is the input as one string, and lines and nul are lists of the
// strings their records hold, with nothing trimmed but the separators.
func TestTextFormats(t *testing.T) {
	t.Parallel()
	const bom = "\xef\xbb\xbf"
	for _, tt := range []struct {
		format, input, want string
	}{
		{TEXT, "", `""`},
		{TEXT, "  two words \r\n\n", `"  two words \r\n\n"`},
		{TEXT, bom + "bom", `"bom"`},
		{TEXT, "日本語\x00κ", `"日本語\u0000κ"`},
		{LINES, "", `[]`},
		{LINES, "\n", `[""]`},
		{LINES, "a\nb\n", `["a","b"]`},
		{LINES, "a\nb", `["a","b"]`},
		{LINES, "a\n\n b \n\n", `["a",""," b ",""]`},
		{LINES, "crlf\r\nlf\ncr\rin\r\n", `["crlf","lf","cr\rin"]`},
		{LINES, "last\r", `["last\r"]`},
		{LINES, bom + "first\n" + bom + "second\n", `["first","` + bom + `second"]`},
		{LINES, bom, `[]`},
		{LINES, bom + "\n", `[""]`},
		{NUL, "", `[]`},
		{NUL, "\x00", `[""]`},
		{NUL, "a\x00b\x00", `["a","b"]`},
		{NUL, "a\x00b", `["a","b"]`},
		{NUL, "a\x00\x00b c\n\r\n\x00", `["a","","b c\n\r\n"]`},
		{NUL, bom + "kept\x00", `["` + bom + `kept"]`},
	} {
		v, err := Read(tt.format, []byte(tt.input))
		if err != nil {
			t.Errorf("%s %q: %v", tt.format, tt.input, err)
			continue
		}
		if got := encode(t, v); got != tt.want {
			t.Errorf("%s %q: got %s, want %s", tt.format, tt.input, got, tt.want)
		}
		if !Streams(tt.format) {
			continue
		}
		var streamed []any
		if err := Stream(tt.format, iotest.OneByteReader(strings.NewReader(tt.input)), len(tt.input)+1, func(v any) error {
			streamed = append(streamed, v)
			return nil
		}, nil); err != nil {
			t.Errorf("%s %q streamed: %v", tt.format, tt.input, err)
		}
		if streamed == nil {
			streamed = []any{}
		}
		if got := encode(t, streamed); got != tt.want {
			t.Errorf("%s %q streamed: got %s, want %s", tt.format, tt.input, got, tt.want)
		}
	}
}

func TestTextFormatsRefuse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		format, input, want string
	}{
		{TEXT, "ok\ncaf\xe9\n", "text: line 2: the text is not valid UTF-8"},
		{LINES, "ok\ncaf\xe9\n", "lines: line 2: the text is not valid UTF-8"},
		{NUL, "ok\x00caf\xe9\x00", "nul: record 2: the text is not valid UTF-8"},
	} {
		_, err := Read(tt.format, []byte(tt.input))
		var pe *engine.ParseError
		if !errors.As(err, &pe) || err.Error() != tt.want {
			t.Errorf("%s: got %v, want %q", tt.format, err, tt.want)
		}
	}
	for _, tt := range []struct {
		format, input, want string
	}{
		{LINES, "short\n" + strings.Repeat("x", 20) + "\n", "lines: line 2: the record is longer than 10 bytes"},
		{NUL, "short\x00" + strings.Repeat("x", 20), "nul: record 2: the record is longer than 10 bytes"},
	} {
		var got []any
		err := Stream(tt.format, strings.NewReader(tt.input), 10, func(v any) error {
			got = append(got, v)
			return nil
		}, nil)
		if err == nil || err.Error() != tt.want || len(got) != 1 {
			t.Errorf("%s: got %v %v, want %q after one record", tt.format, got, err, tt.want)
		}
	}
	if err := Stream(TEXT, strings.NewReader("x"), 10, func(any) error { return nil }, nil); err == nil {
		t.Error("text streamed")
	}
}

// Joining the records of lines or nul with their separator gives back the
// input: nothing but the separators, the CR of a CRLF and a leading byte
// order mark is lost, and no record is made up.
func FuzzStringRecords(f *testing.F) {
	for _, s := range []string{"", "\n", "a\nb", "a\r\n\r\n", "\x00", "a\x00\x00b", "\xef\xbb\xbfx\n", "x\r"} {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, data string, lines bool) {
		format, sep := NUL, "\x00"
		if lines {
			format, sep = LINES, "\n"
		}
		v, err := Read(format, []byte(data))
		if err != nil {
			var pe *engine.ParseError
			if !errors.As(err, &pe) || utf8.ValidString(data) {
				t.Fatalf("%q: %v", data, err)
			}
			return
		}
		records := v.([]any)
		parts := make([]string, len(records))
		for i, r := range records {
			parts[i] = r.(string)
		}
		want := data
		if lines {
			// The CR in front of each LF is part of that line ending.
			want = strings.ReplaceAll(strings.TrimPrefix(want, "\xef\xbb\xbf"), "\r\n", "\n")
		}
		got := strings.Join(parts, sep)
		if strings.HasSuffix(want, sep) {
			got += sep
		}
		if got != want {
			t.Fatalf("%q: records %q join to %q", data, parts, got)
		}
	})
}

// A record whose content cannot be read goes to onError, which leaves it
// out when it returns nil and ends the stream when it returns an error.
// A record too long to hold ends the stream either way.
func TestStreamOnError(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		format, input, want, bad string
	}{
		{JSONL, "1\nnot json\n3\n", "1,3", "jsonl: line 2: the line is not one JSON value"},
		{LTSV, "a:1\nno label\na:3\n", `{"a":"1"},{"a":"3"}`, "ltsv: line 2"},
		{JSONL, "1\n\xff\n3\n", "1,3", "jsonl: line 2: the line is not valid UTF-8"},
		{LINES, "a\n\xff\nc\n", `"a","c"`, "lines: line 2: the text is not valid UTF-8"},
		{NUL, "a\x00\xff\x00c\x00", `"a","c"`, "nul: record 2: the text is not valid UTF-8"},
	} {
		var got []string
		var seen []string
		err := Stream(tt.format, strings.NewReader(tt.input), 100, func(v any) error {
			got = append(got, encode(t, v))
			return nil
		}, func(pe *engine.ParseError) error {
			seen = append(seen, pe.Error())
			return nil
		})
		if err != nil || strings.Join(got, ",") != tt.want || len(seen) != 1 || !strings.Contains(seen[0], tt.bad) {
			t.Errorf("%s skipping: %v %v %v", tt.format, err, got, seen)
		}
		got = nil
		stop := errors.New("stop")
		err = Stream(tt.format, strings.NewReader(tt.input), 100, func(v any) error {
			got = append(got, encode(t, v))
			return nil
		}, func(*engine.ParseError) error { return stop })
		if !errors.Is(err, stop) || len(got) != 1 {
			t.Errorf("%s stopping: %v %v", tt.format, err, got)
		}
	}
	skipped := 0
	err := Stream(JSONL, strings.NewReader("1\n"+strings.Repeat("2", 50)+"\n3\n"), 10, func(any) error { return nil }, func(*engine.ParseError) error {
		skipped++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "longer than 10 bytes") || skipped != 0 {
		t.Errorf("a line too long was skipped: %v, %d skipped", err, skipped)
	}
}

// maxLine counts what a record holds, not the separator that ends it: a
// record of exactly maxLine bytes is read, with its separator or at the
// end of the input without one, and a byte more is refused either way.
func TestRecordLengthLimit(t *testing.T) {
	t.Parallel()
	for _, format := range []string{LINES, NUL, JSONL} {
		sep := "\n"
		if format == NUL {
			sep = "\x00"
		}
		exact, over := "1234567890", "12345678901"
		for _, tt := range []struct {
			input string
			ok    bool
		}{
			{exact + sep, true},
			{exact, true},
			{over + sep, false},
			{over, false},
		} {
			err := Stream(format, strings.NewReader(tt.input), len(exact), func(any) error { return nil }, nil)
			if (err == nil) != tt.ok {
				t.Errorf("%s %q with a limit of %d: %v", format, tt.input, len(exact), err)
			}
		}
	}
}

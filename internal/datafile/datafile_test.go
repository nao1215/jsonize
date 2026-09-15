package datafile

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

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
	})
	if err != nil || strings.Join(got, ",") != "1,2,3" {
		t.Errorf("records: %v %v", got, err)
	}
	// A line longer than the limit is refused where it is; the ones
	// before it were handed on already.
	got = nil
	err = Stream(JSONL, strings.NewReader("1\n"+strings.Repeat("2", 5000)+"\n3\n"), 4096, func(v any) error {
		got = append(got, encode(t, v))
		return nil
	})
	parseError(t, err, 2, "longer than 4096 bytes")
	if len(got) != 1 {
		t.Errorf("the records before the long line: %v", got)
	}
	// A line exactly at the limit is read.
	if err := Stream(LTSV, strings.NewReader("k:"+strings.Repeat("v", 98)+"\n"), 100, func(any) error { return nil }); err != nil {
		t.Errorf("a line at the limit: %v", err)
	}
	stop := errors.New("stop")
	if err := Stream(JSONL, strings.NewReader("1\n2\n"), 10, func(any) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("emit's error: %v", err)
	}
	calls := 0
	err = Stream(JSONL, io.MultiReader(strings.NewReader("1\n"), iotest.ErrReader(iotest.ErrTimeout)), 10, func(any) error {
		calls++
		return nil
	})
	if !errors.Is(err, iotest.ErrTimeout) || calls != 1 {
		t.Errorf("a reader that fails: %v after %d records", err, calls)
	}
	if err := Stream(JSON, strings.NewReader("1"), 10, func(any) error { return nil }); err == nil {
		t.Error("a document format was streamed")
	}
	if _, err := Read(CSV, []byte("a\n")); err == nil {
		t.Error("a csv was read here")
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
		})
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

package cli

import (
	"bufio"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// records splits NDJSON output into the documents it carries, checking
// that every line is a complete document of its own.
func (h *harness) records() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSuffix(h.stdout.String(), "\n"), "\n") {
		if l == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			h.t.Fatalf("line is not a JSON document: %v\n%s", err, l)
		}
		out = append(out, rec)
	}
	return out
}

func TestStreamWritesOneDocumentPerLine(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--stream"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	recs := h.records()
	if len(recs) != 2 || recs[0]["mounted_on"] != "/run" || recs[1]["1k_blocks"] != float64(20000000) {
		t.Errorf("records = %v", recs)
	}
	// The same input read whole carries the same records in the same
	// order; only the brackets and commas differ.
	if code := h.pipe(gnuDF); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	whole := h.rows()
	if len(whole) != len(recs) {
		t.Fatalf("whole=%d streamed=%d", len(whole), len(recs))
	}
	for i := range whole {
		if len(whole[i]) != len(recs[i]) || whole[i]["filesystem"] != recs[i]["filesystem"] {
			t.Errorf("record %d differs: %v vs %v", i, whole[i], recs[i])
		}
	}
}

func TestStreamNarrowsEachRecord(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--stream", "--extract", "filesystem"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	for _, rec := range h.records() {
		if len(rec) != 1 || rec["filesystem"] == nil {
			t.Errorf("record = %v", rec)
		}
	}
	// A key the format does not produce is still an error, and no record
	// is written under a false name.
	if code := h.pipe(gnuDF, "--stream", "--extract", "mountpoint"); code != ExitUsage {
		t.Errorf("unknown key: %d %s", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

func TestStreamRefusesWhatItCannotStream(t *testing.T) {
	h := newHarness(t)
	// uptime reads its whole output into one object.
	const uptimeText = " 14:20:01 up 13 days,  4:30,  2 users,  load average: 0.52, 0.58, 0.59\n"
	if code := h.pipe(uptimeText, "--stream"); code != ExitUsage ||
		!strings.Contains(h.stderr.String(), "has no streaming form") {
		t.Errorf("composite: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(gnuDF, "--stream", "--pretty"); code != ExitUsage ||
		!strings.Contains(h.stderr.String(), "--pretty and --stream") {
		t.Errorf("pretty: %d %s", code, h.stderr.String())
	}
	// Text nothing describes is refused before a single record is written.
	if code := h.pipe("nothing recognisable here\n", "--stream"); code != ExitSelect {
		t.Errorf("unidentified: %d", code)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// A record jz cannot read is left out and the ones after it are still
// written, which is what a command that keeps printing needs: one line
// with no reading for it must not end the stream.
func TestStreamSkipsTheRecordsItCannotRead(t *testing.T) {
	h := newHarness(t)
	lines := strings.SplitAfter(gnuDF, "\n")
	broken := lines[0] + "garbage\n" + strings.Join(lines[1:], "")
	if code := h.pipe(broken, "--stream"); code != ExitParse {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	recs := h.records()
	if len(recs) != 2 || recs[0]["filesystem"] != "tmpfs" || recs[1]["filesystem"] != "/dev/sda1" {
		t.Errorf("records = %v", recs)
	}
	if !strings.Contains(h.stderr.String(), "line 2") {
		t.Errorf("stderr does not name the line: %s", h.stderr.String())
	}
	// Reading the same text as one document keeps the other answer: a
	// line that does not fit means the document is not this format, so
	// nothing is written.
	if code := h.pipe(broken); code != ExitParse || h.stdout.Len() != 0 {
		t.Errorf("whole document: %d stdout=%q", code, h.stdout.String())
	}
}

// The one place the output contract differs: a failure part way through
// leaves the records that were already written where they are.
func TestStreamKeepsTheRecordsAlreadyWritten(t *testing.T) {
	h := newHarness(t)
	broken := gnuDF + "/dev/sdb1  not-a-number 0 0 0% /mnt\n"
	if code := h.pipe(broken, "--stream"); code != ExitParse {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if got := len(h.records()); got != 2 {
		t.Errorf("wrote %d records before the failure", got)
	}
	if !strings.Contains(h.stderr.String(), "cannot convert") {
		t.Errorf("stderr = %s", h.stderr.String())
	}
	// Without --stream the same input leaves nothing behind.
	if code := h.pipe(broken); code != ExitParse || h.stdout.Len() != 0 {
		t.Errorf("whole document: %d stdout=%q", code, h.stdout.String())
	}
}

func TestRunStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	if code := h.run("run", "--stream", "sh", "-c", "echo a=1; echo b=2"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	recs := h.records()
	if len(recs) != 2 || recs[0]["name"] != "a" || recs[1]["value"] != "2" {
		t.Errorf("records = %v", recs)
	}
	// The command's status is mirrored, and what it printed first is
	// still converted.
	if code := h.run("run", "--stream", "sh", "-c", "echo a=1; exit 7"); code != 7 {
		t.Errorf("status: %d", code)
	}
	if got := len(h.records()); got != 1 {
		t.Errorf("records before the failure = %d", got)
	}
	// A format with no streaming form is refused, naming the definition.
	writeRegistry(t, filepath.Join(h.home, "reg"), map[string]string{
		"parsers/whole/default/parser.yaml": "format: 1\ncommand: whole\nvariant: default\n" +
			"detect: {signature: {all: ['^WHOLE=']}}\nparse: {type: kv, as: map}\n",
	})
	if code := h.run("run", "--stream", "--parser", "whole", "--", "sh", "-c", "echo WHOLE=1"); code != ExitUsage ||
		!strings.Contains(h.stderr.String(), "has no streaming form") {
		t.Errorf("no streaming form: %d %s", code, h.stderr.String())
	}
	// A command that prints nothing streams nothing, which is what an
	// empty list looks like one record per line.
	if code := h.run("run", "--stream", "--parser", "sh", "--variant", "default", "sh", "-c", "true"); code != ExitOK ||
		h.stdout.Len() != 0 {
		t.Errorf("empty output: %d stdout=%q stderr=%s", code, h.stdout.String(), h.stderr.String())
	}
	// Unless the format it would have used has no empty form.
	if code := h.run("run", "--stream", "--parser", "whole", "--", "sh", "-c", "true"); code != ExitUsage {
		t.Errorf("empty output, no streaming form: %d %s", code, h.stderr.String())
	}
}

// A composite is streamed part by part, as {"part", "value"} documents,
// and --extract and --exclude name the parts.
func TestStreamComposite(t *testing.T) {
	const ping = "PING 192.0.2.10 (192.0.2.10) 56(84) bytes of data.\n" +
		"64 bytes from 192.0.2.10: icmp_seq=1 ttl=64 time=0.045 ms\n" +
		"From 192.0.2.1 icmp_seq=2 Destination Host Unreachable\n" +
		"\n--- 192.0.2.10 ping statistics ---\n" +
		"2 packets transmitted, 1 received, +1 errors, 50% packet loss, time 1003ms\n" +
		"rtt min/avg/max/mdev = 0.045/0.045/0.045/0.000 ms\n"
	h := newHarness(t)
	if code := h.pipe(ping, "--stream"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	recs := h.records()
	parts := make([]string, 0, len(recs))
	for _, r := range recs {
		if len(r) != 2 || r["value"] == nil {
			t.Fatalf("record %v is not a {part, value} document", r)
		}
		parts = append(parts, r["part"].(string))
	}
	if got := strings.Join(parts, ","); got != "destination,replies,replies,statistics" {
		t.Errorf("parts = %s", got)
	}
	// The same documents with --yaml, one per record.
	if code := h.pipe(ping, "--stream", "--yaml"); code != ExitOK {
		t.Fatalf("--yaml: code=%d stderr=%s", code, h.stderr.String())
	}
	if n := strings.Count(h.stdout.String(), "---\npart: "); n != 4 {
		t.Errorf("%d YAML documents:\n%s", n, h.stdout.String())
	}
	if code := h.pipe(ping, "--stream", "--extract", "replies"); code != ExitOK {
		t.Fatalf("extract: code=%d stderr=%s", code, h.stderr.String())
	}
	for _, r := range h.records() {
		if r["part"] != "replies" {
			t.Errorf("--extract replies wrote %v", r)
		}
	}
	if code := h.pipe(ping, "--stream", "--exclude", "replies"); code != ExitOK || strings.Contains(h.stdout.String(), `"replies"`) {
		t.Errorf("exclude: code=%d stdout=%s", code, h.stdout.String())
	}
	if code := h.pipe(ping, "--stream", "--extract", "reply"); code != ExitUsage || h.stdout.Len() != 0 {
		t.Errorf("an unknown part: code=%d stdout=%s stderr=%s", code, h.stdout.String(), h.stderr.String())
	}
}

// The blank lines before the text are not lines a signature sees, so a
// stream does not count them among the lines it holds back: a report
// that opens with a few hundred empty lines is identified from its
// first lines of text, as the whole document is.
func TestStreamSkipsBlankLinesBeforeTheText(t *testing.T) {
	h := newHarness(t)
	in := strings.Repeat("\n", 300) + gnuDF
	if code := h.pipe(in); code != ExitOK {
		t.Fatalf("whole: %d %s", code, h.stderr.String())
	}
	if code := h.pipe(in, "--stream"); code != ExitOK {
		t.Fatalf("stream: %d %s", code, h.stderr.String())
	}
	if recs := h.records(); len(recs) != 2 || recs[1]["mounted_on"] != "/" {
		t.Errorf("records = %v", recs)
	}
	// Lines of spaces, escape sequences and a byte order mark before
	// the text are blank lines too.
	noise := "\xef\xbb\xbf\x1b[0m\n   \n\t\n" + gnuDF
	if code := h.pipe(noise, "--stream"); code != ExitOK || len(h.records()) != 2 {
		t.Errorf("noise before the text: %d %s", code, h.stderr.String())
	}
	// Named, the same.
	if code := h.pipe(in, "--stream", "--parser", "df"); code != ExitOK || len(h.records()) != 2 {
		t.Errorf("named: %d %s", code, h.stderr.String())
	}
	// Blank lines that never end are bounded by the input limit rather
	// than held for ever.
	if code := h.pipe(strings.Repeat("\n", MaxInputSize+1), "--stream"); code != ExitError ||
		!strings.Contains(h.stderr.String(), "exceed the input limit") {
		t.Errorf("endless blank lines: %d %s", code, h.stderr.String())
	}
}

// A stream of NUL-separated records is read record by record, so a
// variant that reads them, named, needs no newline to write its first
// record. The whole document reads it the same way.
func TestStreamReadsNULSeparatedRecords(t *testing.T) {
	h := newHarness(t)
	const in = "one\x00two\nlines\x00three\x00"
	if code := h.pipe(in, "--stream", "--parser", "ls", "--variant", "names-zero"); code != ExitOK {
		t.Fatalf("stream: %d %s", code, h.stderr.String())
	}
	recs := h.records()
	if len(recs) != 3 || recs[1]["name"] != "two\nlines" {
		t.Errorf("records = %v", recs)
	}
	if code := h.pipe(in, "--parser", "ls", "--variant", "names-zero"); code != ExitOK || len(h.rows()) != 3 {
		t.Errorf("whole: %d %s", code, h.stderr.String())
	}
	// The same input with no NUL at all is one record either way.
	if code := h.pipe("just one", "--stream", "--parser", "ls", "--variant", "names-zero"); code != ExitOK || len(h.records()) != 1 {
		t.Errorf("one record: %d %s", code, h.stderr.String())
	}
}

// readHead hands back the leading records a signature sees, and the
// selector sees the same lines in them that it sees in the whole text:
// what is held back is never fewer lines than the selector would look
// at, unless the input ends first.
func TestReadHeadCountsTheLinesTheSelectorSees(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"plain", "a\nb\nc\n", 2, "a\nb\n"},
		{"leading blank lines", "\n \n\x1b[0m\nb\nc\nd\n", 2, "\n \n\x1b[0m\nb\nc\n"},
		{"a mark on the first line", "\xef\xbb\xbf\nb\nc\n", 1, "\xef\xbb\xbf\nb\n"},
		{"input ends first", "a\n", 5, "a\n"},
		{"no line break at the end", "a\nb", 5, "a\nb"},
		{"blank lines inside the text count", "a\n\nb\n", 2, "a\n\n"},
	} {
		got, err := readHead(bufio.NewReader(strings.NewReader(tc.input)), tc.n, '\n')
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: readHead = %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
	got, err := readHead(bufio.NewReader(strings.NewReader("a\x00\x00b\x00c")), 2, 0)
	if err != nil || string(got) != "a\x00\x00" {
		t.Errorf("NUL: readHead = %q, %v", got, err)
	}
}

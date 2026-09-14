package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
		got, err := readHead(bufio.NewReader(strings.NewReader(tc.input)), tc.n, '\n', never)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: readHead = %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
	got, err := readHead(bufio.NewReader(strings.NewReader("a\x00\x00b\x00c")), 2, 0, never)
	if err != nil || string(got) != "a\x00\x00" {
		t.Errorf("NUL: readHead = %q, %v", got, err)
	}
}

func never([]byte) bool { return false }

// lineWatcher is a writer that hands over each complete line as it is
// written, so a test can wait for output that a stream writes before its
// input has ended.
type lineWatcher struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines chan string
}

func newLineWatcher() *lineWatcher { return &lineWatcher{lines: make(chan string, 1024)} }

func (w *lineWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			return len(p), nil
		}
		w.lines <- string(w.buf.Next(i + 1))
	}
}

// next waits for the next line, for as long as the test is given rather
// than for a fixed time: output that never comes fails the test at its
// deadline, and output that comes ends the wait at once.
func (w *lineWatcher) next(t *testing.T, what string) string {
	t.Helper()
	deadline := time.After(time.Minute)
	if d, ok := t.Deadline(); ok {
		deadline = time.After(time.Until(d) - 5*time.Second)
	}
	select {
	case l := <-w.lines:
		return strings.TrimSuffix(l, "\n")
	case <-deadline:
		t.Fatalf("no line came: %s", what)
		return ""
	}
}

// openStream starts jz on a pipe it can read from while the test writes
// to it, and returns the write end, stdout and stderr as they are
// written, and the exit status once jz has ended.
func openStream(t *testing.T, h *harness, args ...string) (*io.PipeWriter, *lineWatcher, *lineWatcher, <-chan int) {
	t.Helper()
	pr, pw := io.Pipe()
	stdout, stderr := newLineWatcher(), newLineWatcher()
	env := h.env
	env.Stdin, env.Stdout, env.Stderr = pr, stdout, stderr
	done := make(chan int, 1)
	go func() {
		code := Main(args, env)
		// Whatever jz has not read is not waited for once it has ended.
		_ = pr.CloseWithError(io.ErrClosedPipe)
		done <- code
	}()
	t.Cleanup(func() { _ = pw.Close() })
	return pw, stdout, stderr, done
}

// feed writes text to a stream jz is reading. The pipe hands the text
// over only as jz reads it, so the write is done aside from the test.
func feed(pw *io.PipeWriter, text string) {
	go func() { _, _ = io.WriteString(pw, text) }()
}

const vmstatHead = "procs -----------memory---------- ---swap-- -----io---- -system-- -------cpu-------\n" +
	" r  b   swpd   free   buff  cache   si   so    bi    bo   in   cs us sy id wa st gu\n"

const vmstatRecord = " 0  0 1595796 5840084 2080604 38598064    5   17   561  1735 16905    8  3  1 97  0  0  0\n"

// A stream answers as soon as the lines that decide its format have come:
// vmstat's two header lines settle it, on automatic detection, with the
// command named and with the variant named, and its first record is
// written while the input is still open. Before, the first record waited
// for twenty lines, and for two hundred on automatic detection.
func TestStreamWritesTheFirstRecordBeforeTheInputEnds(t *testing.T) {
	for _, args := range [][]string{
		{"--stream"},
		{"--stream", "--parser", "vmstat"},
		{"--stream", "--parser", "vmstat", "--variant", "linux"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			pw, stdout, stderr, done := openStream(t, h, args...)
			feed(pw, vmstatHead+vmstatRecord)
			var rec map[string]any
			if err := json.Unmarshal([]byte(stdout.next(t, "the first record")), &rec); err != nil || rec["free"] != float64(5840084) {
				t.Errorf("first record = %v, %v", rec, err)
			}
			// The input is still open, and the next record follows the
			// first as it comes.
			feed(pw, vmstatRecord)
			if l := stdout.next(t, "the second record"); !strings.HasPrefix(l, `{"r":0`) {
				t.Errorf("second record = %s", l)
			}
			_ = pw.Close()
			if code := <-done; code != ExitOK {
				t.Errorf("code = %d, stderr = %s", code, stderr.buf.String())
			}
		})
	}
}

// A line that could still rule the choice out keeps a stream waiting: it
// commits only when no line that may come can change what the whole text
// would choose. Each input here fits a definition on its first lines and
// is ruled out, or made ambiguous, by a later one; the stream writes no
// record and gives the answer the whole document gives.
func TestStreamWaitsForALineThatCouldStillChangeTheChoice(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.home, "reg")
	writeRegistry(t, dir, map[string]string{
		// Fits from its first line; a line further down rules it out.
		"parsers/tick/default/parser.yaml": "format: 1\ncommand: tick\nvariant: default\n" +
			"detect: {signature: {all: ['\\A\\d+ tick'], none: ['^ERROR ']}}\n" +
			"parse: {type: regex, pattern: '^(?P<n>\\d+) (?P<word>\\S+)$'}\n",
		// Two formats that open the same way, one of them with a line
		// the other may also print.
		"parsers/eq/plain/parser.yaml": "format: 1\ncommand: eq\nvariant: plain\n" +
			"detect: {signature: {all: ['\\A= ']}}\nparse: {type: regex, pattern: '^(?P<line>.*)$'}\n",
		"parsers/eqx/extra/parser.yaml": "format: 1\ncommand: eqx\nvariant: extra\n" +
			"detect: {signature: {all: ['\\A= ', '^extra$']}}\nparse: {type: regex, pattern: '^(?P<line>.*)$'}\n",
	})
	h.registryPath = dir
	for _, tc := range []struct {
		name  string
		input string
		args  []string
		code  int
		text  string
	}{
		{"a none line past the first", "1 tick\n2 tick\nERROR boom\n", []string{"--parser", "tick"}, ExitSelect, "signature.none"},
		{"a none line, automatic detection", "1 tick\n2 tick\nERROR boom\n", nil, ExitSelect, "unable to identify"},
		{"a later line two definitions both fit", "= a\nextra\n", nil, ExitSelect, "multiple parsers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			whole := h.pipe(tc.input, tc.args...)
			if whole != tc.code || h.stdout.Len() != 0 {
				t.Fatalf("whole document: %d stdout=%q", whole, h.stdout.String())
			}
			args := append([]string{"--stream"}, tc.args...)
			if code := h.pipe(tc.input, args...); code != tc.code || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), tc.text) {
				t.Errorf("stream: %d stdout=%q stderr=%s", code, h.stdout.String(), h.stderr.String())
			}
		})
	}
	// Without the late line, the same stream commits on its first line and
	// writes its records before the input ends.
	pw, stdout, _, done := openStream(t, h, "--stream", "--parser", "eq")
	feed(pw, "= a\n")
	if l := stdout.next(t, "a record of eq/plain"); l != `{"line":"= a"}` {
		t.Errorf("record = %s", l)
	}
	_ = pw.Close()
	<-done
}

// With --explain=json a record a stream leaves out is reported when it is
// left out, as a JSON document of its own on standard error, and the
// records after it are still written.
func TestStreamReportsASkippedRecordWhenItHappens(t *testing.T) {
	h := newHarness(t)
	pw, stdout, stderr, done := openStream(t, h, "--stream", "--explain=json")
	feed(pw, vmstatHead+vmstatRecord)
	stdout.next(t, "the first record")
	explained := stderr.next(t, "the explanation")
	if !strings.HasPrefix(explained, `jz: explain: {"outcome":"chosen"`) {
		t.Errorf("explanation = %s", explained)
	}
	feed(pw, "not a sample\n")
	if l := stderr.next(t, "the parse error"); !strings.HasPrefix(l, "jz: vmstat/linux: line 4: ") {
		t.Errorf("diagnostic = %s", l)
	}
	event := skipEvent(t, stderr.next(t, "the skip event"))
	if event["event"] != "skipped" || event["definition"] != "vmstat/linux" || event["line"] != float64(4) ||
		event["skipped"] != float64(1) || !strings.Contains(event["reason"].(string), "not a sample") {
		t.Errorf("event = %v", event)
	}
	// The stream goes on, and the count goes on with it.
	feed(pw, vmstatRecord+"still not one\n")
	stdout.next(t, "the record after the skipped one")
	stderr.next(t, "the second parse error")
	if event := skipEvent(t, stderr.next(t, "the second skip event")); event["skipped"] != float64(2) || event["line"] != float64(6) {
		t.Errorf("second event = %v", event)
	}
	_ = pw.Close()
	if code := <-done; code != ExitParse {
		t.Errorf("code = %d", code)
	}
	// --explain in text gives the same fact on one line.
	if code := h.pipe(vmstatHead+"bad\n"+vmstatRecord, "--stream", "--explain"); code != ExitParse ||
		!strings.Contains(h.stderr.String(), "jz: explain: skipped: a record of vmstat/linux at line 3 (1 record so far): ") {
		t.Errorf("text: %d %s", code, h.stderr.String())
	}
	// Without --explain there is no event, only the diagnostic.
	if code := h.pipe(vmstatHead+"bad\n"+vmstatRecord, "--stream"); code != ExitParse || strings.Contains(h.stderr.String(), "explain") {
		t.Errorf("plain: %d %s", code, h.stderr.String())
	}
	// A definition given with --define reports the same way.
	def := `parse: {type: regex, pattern: '^(?P<k>\w+)=(?P<v>\w+)$'}`
	if code := h.pipe("a=1\nbroken\nb=2\n", "--stream", "--explain=json", "--define", def); code != ExitParse ||
		!strings.Contains(h.stderr.String(), `{"event":"skipped","definition":"inline/inline","line":2,`) || len(h.records()) != 2 {
		t.Errorf("--define: %d %s", code, h.stderr.String())
	}
}

// skipEvent reads the document on a "jz: explain: " line.
func skipEvent(t *testing.T, line string) map[string]any {
	t.Helper()
	doc, ok := strings.CutPrefix(line, "jz: explain: ")
	var event map[string]any
	if !ok || json.Unmarshal([]byte(doc), &event) != nil {
		t.Fatalf("not an explain document: %s", line)
	}
	return event
}

// jz run --stream shares standard error with the command, and an event is
// still a line of its own that opens with "jz: explain: ", among the lines
// the command writes there.
func TestRunStreamReportsASkippedRecordBesideTheCommandsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	script := `echo a=1; echo "a=noise from the command" >&2; echo "not an assignment"; echo "explain: {\"event\":\"skipped\"}" >&2; echo b=2`
	code := h.run("run", "--stream", "--explain=json", "--parser", "sh", "--variant", "default", "--", "sh", "-c", script)
	if code != ExitParse {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if recs := h.records(); len(recs) != 2 || recs[1]["name"] != "b" {
		t.Errorf("records = %v", recs)
	}
	var events []map[string]any
	for _, l := range strings.Split(h.stderr.String(), "\n") {
		if doc, ok := strings.CutPrefix(l, "jz: explain: "); ok && strings.Contains(doc, `"event"`) {
			var ev map[string]any
			if err := json.Unmarshal([]byte(doc), &ev); err != nil {
				t.Fatalf("event line is not JSON: %s", l)
			}
			events = append(events, ev)
		}
	}
	if len(events) != 1 || events[0]["line"] != float64(2) || events[0]["definition"] != "sh/default" {
		t.Errorf("events = %v\nstderr:\n%s", events, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "a=noise from the command\n") {
		t.Errorf("the command's own stderr is missing:\n%s", h.stderr.String())
	}
}

// readHead stops as soon as the lines it has read settle the choice, and
// asks only about lines of text: the blank lines before the text say
// nothing about its format.
func TestReadHeadStopsOnceTheChoiceIsSettled(t *testing.T) {
	t.Parallel()
	var asked []string
	settled := func(head []byte) bool {
		asked = append(asked, string(head))
		return strings.Count(string(head), "x") == 2
	}
	got, err := readHead(bufio.NewReader(strings.NewReader("\n\nx\ny\nx\nz\nw\n")), 20, '\n', settled)
	if err != nil || string(got) != "\n\nx\ny\nx\n" {
		t.Errorf("readHead = %q, %v", got, err)
	}
	if want := []string{"\n\nx\n", "\n\nx\ny\n", "\n\nx\ny\nx\n"}; strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("asked about %q, want %q", asked, want)
	}
	// The last line of the window is read whatever settled would say.
	asked = nil
	if got, _ := readHead(bufio.NewReader(strings.NewReader("a\nb\nc\n")), 2, '\n', settled); string(got) != "a\nb\n" || len(asked) != 1 {
		t.Errorf("full window: %q, asked %d times", got, len(asked))
	}
}

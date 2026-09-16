package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nao1215/jsonize/pkg/engine"
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

// A stream ends at the first record it cannot read. The records before
// it stand, the failure is reported once, and the status is 3: a gap in
// the middle of a stream is an answer nobody can check, so jz stops
// rather than hand on a shape the input does not have.
func TestStreamEndsAtTheRecordItCannotRead(t *testing.T) {
	h := newHarness(t)
	lines := strings.SplitAfter(gnuDF, "\n")
	broken := lines[0] + lines[1] + "garbage\n" + strings.Join(lines[2:], "")
	if code := h.pipe(broken, "--stream"); code != ExitParse {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if recs := h.records(); len(recs) != 1 || recs[0]["filesystem"] != "tmpfs" {
		t.Errorf("records = %v", recs)
	}
	if strings.Count(h.stderr.String(), "line 3") != 1 {
		t.Errorf("stderr = %s", h.stderr.String())
	}
	// Reading the same text as one document keeps the other answer: a
	// line that does not fit means the document is not this format, so
	// nothing is written.
	if code := h.pipe(broken); code != ExitParse || h.stdout.Len() != 0 {
		t.Errorf("whole document: %d stdout=%q", code, h.stdout.String())
	}
}

// --stop-on-error asked for what every stream now does, so it is gone.
// A command line that still has it is refused before anything is read or
// run, with the message that says so rather than the flag package's
// "not defined".
func TestStopOnErrorWasRemoved(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"--stream", "--stop-on-error"},
		{"--stop-on-error"},
		{"run", "--stop-on-error", "jz-no-such-command"},
		{"new", "--each", "--stop-on-error", "n:=@-"},
	} {
		h.env.Stdin = untouchedInput{t}
		if code := h.run(args...); code != ExitUsage || !strings.Contains(h.stderr.String(), "--stop-on-error was removed") {
			t.Errorf("%v: %d %s", args, code, h.stderr.String())
		}
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
	// A record the command printed that jz cannot read ends the stream
	// and returns 3, since the command itself did not fail.
	if code := h.run("run", "--stream", "sh", "-c", "echo a=1; echo nonsense; echo b=2"); code != ExitParse {
		t.Errorf("stop on error: %d %s", code, h.stderr.String())
	}
	if recs := h.records(); len(recs) != 1 || recs[0]["name"] != "a" {
		t.Errorf("records before the stop = %v", recs)
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
		{"a carriage return stays on", "a\r\nb\r\n", 1, "a\r\n"},
	} {
		got, err := readHead(bufio.NewReader(strings.NewReader(tc.input)), tc.n, '\n', MaxLineLength, never)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: readHead = %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
	got, err := readHead(bufio.NewReader(strings.NewReader("a\x00\x00b\x00c")), 2, 0, MaxLineLength, never)
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

// A stream writes its records as they come and ends at the first it
// cannot read: the records before it are already out, the failure is
// reported once, and nothing after it is read. With --explain the
// explanation is still written before the first record, and it is the
// only event there is.
func TestStreamEndsAtABadRecordWhileItRuns(t *testing.T) {
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
	_ = pw.Close()
	if code := <-done; code != ExitParse {
		t.Errorf("code = %d", code)
	}
	// The records that came after the bad one are not read, and the
	// failure is reported once whether or not --explain is on.
	for _, args := range [][]string{{"--stream"}, {"--stream", "--explain"}} {
		if code := h.pipe(vmstatHead+"bad\n"+vmstatRecord, args...); code != ExitParse ||
			strings.Count(h.stderr.String(), "jz: vmstat/linux: line 3: ") != 1 || len(h.records()) != 0 {
			t.Errorf("%v: %d %s", args, code, h.stderr.String())
		}
	}
	// A definition given with --define ends the same way.
	def := `parse: {type: regex, pattern: '^(?P<k>\w+)=(?P<v>\w+)$'}`
	if code := h.pipe("a=1\nbroken\nb=2\n", "--stream", "--define", def); code != ExitParse ||
		!strings.Contains(h.stderr.String(), "jz: inline/inline: line 2: ") || len(h.records()) != 1 {
		t.Errorf("--define: %d %s", code, h.stderr.String())
	}
}

// jz run --stream shares standard error with the command, and the
// diagnostic that ends the stream is still a line of its own, among the
// lines the command writes there.
func TestRunStreamEndsBesideTheCommandsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	h := newHarness(t)
	shellRegistry(t, h)
	script := `echo a=1; echo "a=noise from the command" >&2; echo "not an assignment"; echo b=2`
	code := h.run("run", "--stream", "--parser", "sh", "--variant", "default", "--", "sh", "-c", script)
	if code != ExitParse {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if recs := h.records(); len(recs) != 1 || recs[0]["name"] != "a" {
		t.Errorf("records = %v", recs)
	}
	if strings.Count(h.stderr.String(), "jz: sh/default: line 2: ") != 1 {
		t.Errorf("the diagnostic is not a line of its own:\n%s", h.stderr.String())
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
	got, err := readHead(bufio.NewReader(strings.NewReader("\n\nx\ny\nx\nz\nw\n")), 20, '\n', MaxLineLength, settled)
	if err != nil || string(got) != "\n\nx\ny\nx\n" {
		t.Errorf("readHead = %q, %v", got, err)
	}
	if want := []string{"\n\nx\n", "\n\nx\ny\n", "\n\nx\ny\nx\n"}; strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("asked about %q, want %q", asked, want)
	}
	// The last line of the window is read whatever settled would say.
	asked = nil
	if got, _ := readHead(bufio.NewReader(strings.NewReader("a\nb\nc\n")), 2, '\n', MaxLineLength, settled); string(got) != "a\nb\n" || len(asked) != 1 {
		t.Errorf("full window: %q, asked %d times", got, len(asked))
	}
}

// A record longer than the limit is refused where it passes the limit,
// whether or not a format has been chosen yet: the reading that
// identifies the text and the reading that converts it cut records the
// same way and bound them by the same number.
func TestReadHeadRefusesARecordPastTheLengthLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
		sep   byte
		max   int
		want  string
	}{
		{"at the limit", strings.Repeat("x", 8) + "\n", '\n', 8, strings.Repeat("x", 8) + "\n"},
		{"one byte over", strings.Repeat("x", 9) + "\n", '\n', 8, ""},
		{"at the limit with no line break", strings.Repeat("x", 8), '\n', 8, strings.Repeat("x", 8)},
		{"one byte over with no line break", strings.Repeat("x", 9), '\n', 8, ""},
		{"a carriage return counts", strings.Repeat("x", 8) + "\r\n", '\n', 8, ""},
		{"a second record over the limit", "a\n" + strings.Repeat("x", 9) + "\n", '\n', 8, ""},
		{"NUL separated", strings.Repeat("x", 9) + "\x00", 0, 8, ""},
	} {
		got, err := readHead(bufio.NewReader(strings.NewReader(tc.input)), 5, tc.sep, tc.max, never)
		if tc.want != "" {
			if err != nil || string(got) != tc.want {
				t.Errorf("%s: readHead = %q, %v, want %q", tc.name, got, err, tc.want)
			}
			continue
		}
		if !errors.Is(err, engine.ErrLineTooLong) {
			t.Errorf("%s: readHead = %q, %v, want a refusal", tc.name, got, err)
		}
	}
}

// The line limit holds before the format is known, so a producer that
// never ends a record is refused rather than waited for. The words and
// the status are the ones a stream read with a definition given on the
// command line answers with, which is the reading that already applied
// the limit.
func TestStreamRefusesALongRecordWhetherOrNotItHasChosen(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("x", MaxLineLength+1)
	const msg = "line 1: record exceeds 1048576 bytes: line too long"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"detected", []string{"--stream"}},
		{"named", []string{"--stream", "--parser", "df", "--variant", "gnu"}},
		{"defined", []string{"--stream", "--define", "parse: {type: regex, pattern: '^(?P<text>.*)$'}"}},
	} {
		if code := h.pipe(long, tc.args...); code != ExitParse || !strings.Contains(h.stderr.String(), msg) {
			t.Errorf("%s: %d %s", tc.name, code, h.stderr.String())
		}
		if h.stdout.Len() != 0 {
			t.Errorf("%s: stdout = %q", tc.name, h.stdout.String())
		}
	}
	// A record at the limit is still read, so the refusal is of the byte
	// past it and not of a long record as such.
	at := strings.Repeat("x", MaxLineLength) + "\n"
	if code := h.pipe(at, "--stream", "--define", "parse: {type: regex, pattern: '^(?P<text>.*)$'}"); code != ExitOK {
		t.Errorf("at the limit: %d %s", code, h.stderr.String())
	}
}

// A file name is a guess about the text in it, and a guess never makes
// the answer worse. The whole-document reading drops it once the
// definition it named does not describe the text; a stream drops it the
// same way, since nothing has been written while the choice is made.
func TestStreamDropsAPathGuessThatDoesNotFit(t *testing.T) {
	h := newHarness(t)
	p := h.writeFile("etc/fstab", []byte(gnuDF))
	if code := h.run("--file", p, "--stream"); code != ExitOK {
		t.Fatalf("stream: %d %s", code, h.stderr.String())
	}
	recs := h.records()
	if len(recs) != 2 || recs[0]["mounted_on"] != "/run" {
		t.Errorf("records = %v", recs)
	}
	// The same file read whole carries the same records, which is the
	// reading the stream is held to.
	if code := h.run("--file", p); code != ExitOK || len(h.rows()) != len(recs) {
		t.Errorf("whole: %d %s", code, h.stderr.String())
	}
	// A parser named on the command line is not a guess, so it is not
	// dropped and the text it does not describe is refused.
	if code := h.run("--file", p, "--stream", "--parser", "fstab"); code != ExitSelect {
		t.Errorf("named: %d %s", code, h.stderr.String())
	}
}

// Input that cannot be read at all ends a stream with the status that
// reading it as a whole document ends with: the failure is the same
// failure, and it happens before anything is written either way.
func TestStreamReportsAnUnreadableInputAsTheDocumentReadingDoes(t *testing.T) {
	h := newHarness(t)
	broken := gzipBytes(t, "x")
	broken[len(broken)-5] ^= 1
	p := h.writeFile("broken.txt.gz", broken)
	const msg = "the gzip data cannot be decompressed"
	if code := h.run("--file", p); code != ExitParse || !strings.Contains(h.stderr.String(), msg) {
		t.Errorf("whole: %d %s", code, h.stderr.String())
	}
	if code := h.run("--file", p, "--stream"); code != ExitParse || !strings.Contains(h.stderr.String(), msg) {
		t.Errorf("stream: %d %s", code, h.stderr.String())
	}
}

// A csv gives every record the columns its header names, so a key no
// column has is known once the first record has been read, and nothing
// is written under it: the stream answers as the whole document does.
func TestStreamRefusesAKeyNoColumnHasBeforeWritingARecord(t *testing.T) {
	h := newHarness(t)
	for _, opts := range [][]string{
		{"--format", "csv"},
		{"--parser", "csv", "--variant", "comma"},
		{"--define", "parse: {type: csv}"},
	} {
		for _, stream := range []bool{false, true} {
			args := append(append([]string{}, opts...), "--extract", "nokey")
			if stream {
				args = append(args, "--stream")
			}
			if code := h.pipe("a,b\n1,2\n3,4\n", args...); code != ExitUsage || h.stdout.Len() != 0 ||
				!strings.Contains(h.stderr.String(), `the keys it has are "a", "b"`) {
				t.Errorf("%v: code=%d stdout=%q %s", args, code, h.stdout.String(), h.stderr.String())
			}
		}
	}
	// A key the columns have narrows every record.
	if code := h.pipe("a,b\n1,2\n3,4\n", "--format", "csv", "--stream", "--extract", "b"); code != ExitOK ||
		h.stdout.String() != "{\"b\":\"2\"}\n{\"b\":\"4\"}\n" {
		t.Errorf("code=%d stdout=%q %s", code, h.stdout.String(), h.stderr.String())
	}
}

// A command that prints at an interval (free -s, journalctl -f, ping) has to get its
// first record out while it is still running. Its definition cannot wait
// for a line it prints only at its end, or for a later line to rule a
// rival in, since the signature window may never fill.
func TestIntervalOutputIsChosenOnItsLeadingLines(t *testing.T) {
	for _, tc := range []struct {
		name, head, want string
		args             []string
	}{
		{"free -h -s 1", "               total        used        free      shared  buff/cache   available\nMem:            61Gi        34Gi       8.3Gi        17Gi        36Gi        26Gi\nSwap:          8.0Gi       2.0Gi       6.0Gi\n\n",
			`{"type":"Mem","total":"61Gi"`, []string{"--stream", "--parser", "free"}},
		{"journalctl -f", "Sep 16 19:06:09 host01 sshd[812]: Accepted publickey for alice\nSep 16 19:06:10 host01 systemd[1]: Started session-3.scope.\n",
			`{"timestamp":"Sep 16 19:06:09"`, []string{"--stream", "--parser", "journalctl"}},
		{"avahi-browse -a -p", "+;eno1;IPv4;printer;_ipp._tcp;local\n",
			`{"event":"new","interface":"eno1"`, []string{"--stream", "--parser", "avahi-browse"}},
		{"ping without its summary yet", "PING 127.0.0.1 (127.0.0.1) 56(84) bytes of data.\n64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.032 ms\n",
			`{"part":"destination"`, []string{"--stream", "--parser", "ping"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.env.GOOS = "linux"
			pw, stdout, stderr, done := openStream(t, h, tc.args...)
			feed(pw, tc.head)
			if l := stdout.next(t, "the first record"); !strings.HasPrefix(l, tc.want) {
				t.Errorf("first record = %s, stderr = %s", l, stderr.buf.String())
			}
			_ = pw.Close()
			<-done
		})
	}
}

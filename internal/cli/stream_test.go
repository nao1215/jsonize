package cli

import (
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

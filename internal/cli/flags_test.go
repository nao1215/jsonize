package cli

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// untouchedInput fails the test that reads it: an option pair refused
// before anything is read must not consume the input first.
type untouchedInput struct{ t *testing.T }

func (u untouchedInput) Read([]byte) (int, error) {
	u.t.Error("the input was read")
	return 0, io.EOF
}

// sameValue decodes a JSON and a YAML text and compares what they hold.
// Numbers are compared as float64, which is what JSON has.
func sameValue(t *testing.T, jsonText, yamlText string) {
	t.Helper()
	var fromJSON, fromYAML any
	if err := json.Unmarshal([]byte(jsonText), &fromJSON); err != nil {
		t.Fatalf("JSON: %v\n%s", err, jsonText)
	}
	if err := yaml.Unmarshal([]byte(yamlText), &fromYAML); err != nil {
		t.Fatalf("YAML: %v\n%s", err, yamlText)
	}
	if !reflect.DeepEqual(fromJSON, numbersAsFloat(fromYAML)) {
		t.Errorf("YAML holds something else than JSON:\nJSON %s\nYAML %s", jsonText, yamlText)
	}
}

func numbersAsFloat(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = numbersAsFloat(e)
		}
	case []any:
		for i, e := range t {
			t[i] = numbersAsFloat(e)
		}
	case uint64:
		return float64(t)
	case int64:
		return float64(t)
	case int:
		return float64(t)
	}
	return v
}

func TestYAMLOutputHoldsWhatJSONHolds(t *testing.T) {
	h := newHarness(t)
	for _, input := range []string{gnuDF, humanDF, unameA, lsblkList} {
		if code := h.pipe(input); code != ExitOK {
			t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
		}
		jsonText := h.stdout.String()
		if code := h.pipe(input, "--yaml"); code != ExitOK {
			t.Fatalf("--yaml: code=%d stderr=%s", code, h.stderr.String())
		}
		if h.stderr.Len() != 0 {
			t.Errorf("stderr = %q", h.stderr.String())
		}
		sameValue(t, jsonText, h.stdout.String())
	}
	// The document is the JSON's, key for key in the same order, with the
	// rounded size a quoted string and the number a number.
	if code := h.pipe(humanDF, "--yaml"); code != ExitOK {
		t.Fatal(code)
	}
	want := "- filesystem: tmpfs\n  size: \"1.0G\"\n  used: \"5.0M\"\n  available: \"995M\"\n  use_percent: 1\n  mounted_on: /run\n"
	if h.stdout.String() != want {
		t.Errorf("got\n%s\nwant\n%s", h.stdout.String(), want)
	}
}

// --pretty is about JSON, so asking for it with YAML says two things
// about the output. The pair is refused before the input is read or a
// command is run.
func TestYAMLRefusesPretty(t *testing.T) {
	for _, args := range [][]string{
		{"--yaml", "--pretty"},
		{"-p", "--yaml", "--stream"},
		{"run", "--yaml", "--pretty", "jz-no-such-command"},
	} {
		h := newHarness(t)
		h.env.Stdin = untouchedInput{t}
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%v: code=%d stderr=%s", args, code, h.stderr.String())
		}
		if h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "--pretty and --yaml cannot be used together") {
			t.Errorf("%v: stdout=%q stderr=%q", args, h.stdout.String(), h.stderr.String())
		}
	}
}

// With --stream every record is a document of its own between "---" and
// "...", so a reader knows a record is complete when it arrives.
func TestYAMLStreamIsOneDocumentPerRecord(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe(gnuDF, "--stream"); code != ExitOK {
		t.Fatal(code)
	}
	lines := strings.Split(strings.TrimSuffix(h.stdout.String(), "\n"), "\n")
	if code := h.pipe(gnuDF, "--stream", "--yaml"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	out := h.stdout.String()
	if !strings.HasPrefix(out, "---\n") || !strings.HasSuffix(out, "\n...\n") || strings.Count(out, "---\n") != len(lines) || strings.Count(out, "...\n") != len(lines) {
		t.Fatalf("documents are not marked:\n%s", out)
	}
	docs := strings.Split(strings.TrimSuffix(strings.TrimPrefix(out, "---\n"), "...\n"), "...\n---\n")
	if len(docs) != len(lines) {
		t.Fatalf("%d documents for %d records", len(docs), len(lines))
	}
	for i := range docs {
		sameValue(t, lines[i], docs[i])
	}
	dec := yaml.NewDecoder(strings.NewReader(out))
	n := 0
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != len(lines) {
		t.Errorf("a YAML reader found %d documents, want %d", n, len(lines))
	}
}

// A failure writes nothing to stdout in YAML either, and --explain=json
// still explains in JSON on stderr: it is a report about the conversion,
// not its output.
func TestYAMLFailureAndExplanation(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe("Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs 1 2 3 4% /run\ngarbage line\n", "--yaml", "--parser", "df"); code == ExitOK || h.stdout.Len() != 0 {
		t.Errorf("code=%d stdout=%q", code, h.stdout.String())
	}
	if code := h.pipe(gnuDF, "--yaml", "--explain=json"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	var exp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(h.stderr.String(), "jz: explain: ")), &exp); err != nil {
		t.Errorf("explanation is not JSON: %v\n%s", err, h.stderr.String())
	}
	if !strings.HasPrefix(h.stdout.String(), "- filesystem: tmpfs\n") {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

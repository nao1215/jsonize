package conformance

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/internal/registry"
)

const kvDef = "format: 1\ncommand: kv\nvariant: v\ndetect: {signature: {all: ['=']}}\nparse: {type: kv, as: map}\nfields: {n: {type: int}}\n"

func TestRun(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"parsers/kv/v/parser.yaml":           {Data: []byte(kvDef)},
		"parsers/kv/v/testdata/ok.txt":       {Data: []byte("n=1\n")},
		"parsers/kv/v/testdata/ok.json":      {Data: []byte("{\"n\": 1}")},
		"parsers/kv/v/testdata/bad.txt":      {Data: []byte("n=1\n")},
		"parsers/kv/v/testdata/bad.json":     {Data: []byte("{\"n\": 2}")},
		"parsers/kv/v/testdata/nogold.txt":   {Data: []byte("n=1\n")},
		"parsers/kv/v/testdata/err.txt":      {Data: []byte("n=x\n")},
		"parsers/kv/v/testdata/err.yaml":     {Data: []byte("expect_error: cannot convert\n")},
		"parsers/kv/v/testdata/errno.txt":    {Data: []byte("n=1\n")},
		"parsers/kv/v/testdata/errno.yaml":   {Data: []byte("expect_error: boom\n")},
		"parsers/kv/v/testdata/errmsg.txt":   {Data: []byte("n=x\n")},
		"parsers/kv/v/testdata/errmsg.yaml":  {Data: []byte("expect_error: something else\n")},
		"parsers/kv/v/testdata/nosel.txt":    {Data: []byte("no separator\n")},
		"parsers/kv/v/testdata/nosel.json":   {Data: []byte("{}")},
		"parsers/kv/v/testdata/badjson.txt":  {Data: []byte("n=1\n")},
		"parsers/kv/v/testdata/badjson.json": {Data: []byte("{")},
		"parsers/kv/v/testdata/pfail.txt":    {Data: []byte("n=oops\n")},
		"parsers/kv/v/testdata/pfail.json":   {Data: []byte("{}")},
		"parsers/empty/v/parser.yaml":        {Data: []byte("format: 1\ncommand: empty\nvariant: v\nparse: {type: kv}\n")},
		"parsers/other/v/parser.yaml":        {Data: []byte("format: 1\ncommand: other\nvariant: v\ndetect: {os: [linux]}\nparse: {type: kv}\n")},
		"parsers/other/v/testdata/a.txt":     {Data: []byte("a=b\n")},
		"parsers/other/v/testdata/a.json":    {Data: []byte("[{\"name\":\"a\",\"value\":\"b\"}]")},
		"parsers/other/v/testdata/a.yaml":    {Data: []byte("os: darwin\n")},
	}
	reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(reg, fsys, "s", Options{})
	want := map[string]string{
		"empty/v":      "no testdata cases",
		"kv/v/bad":     "output differs",
		"kv/v/badjson": "expected JSON is invalid",
		"kv/v/err":     "",
		"kv/v/errmsg":  "expected an error containing",
		"kv/v/errno":   "parsing succeeded",
		"kv/v/nogold":  "has no nogold.json",
		"kv/v/nosel":   "variant selection",
		"kv/v/ok":      "",
		"kv/v/pfail":   "cannot convert",
		"other/v/a":    "variant selection",
	}
	if len(results) != len(want) {
		t.Errorf("got %d results, want %d", len(results), len(want))
	}
	for _, r := range results {
		key := r.Definition
		if r.Case != "" {
			key += "/" + r.Case
		}
		w, ok := want[key]
		if !ok {
			t.Errorf("unexpected result %s: %v", key, r.Err)
			continue
		}
		switch {
		case w == "" && r.Err != nil:
			t.Errorf("%s: unexpected error %v", key, r.Err)
		case w != "" && (r.Err == nil || !strings.Contains(r.Err.Error(), w)):
			t.Errorf("%s: got %v, want %q", key, r.Err, w)
		}
	}
	passed, failed := Summary(results)
	if passed != 2 || failed != 9 {
		t.Errorf("summary = %d/%d", passed, failed)
	}
	// Update mode returns Actual for parseable cases and ignores goldens.
	up := Run(reg, fsys, "s", Options{Update: true})
	for _, r := range up {
		if r.Case == "bad" && (r.Err != nil || !strings.Contains(string(r.Actual), `"n": 1`)) {
			t.Errorf("update: %v %s", r.Err, r.Actual)
		}
	}
	// Other source is skipped entirely.
	if got := Run(reg, fsys, "nope", Options{}); len(got) != 0 {
		t.Errorf("wrong source should yield nothing: %v", got)
	}
}

func TestDiff(t *testing.T) {
	t.Parallel()
	if d, err := Diff([]byte(`{"a":1,"b":[1,2]}`), []byte(`{"b":[1,2],"a":1}`)); err != nil || d != "" {
		t.Errorf("order-insensitive: %q %v", d, err)
	}
	if d, err := Diff([]byte(`{"a":1}`), []byte(`{"a":2}`)); err != nil || d == "" {
		t.Errorf("difference expected: %q %v", d, err)
	}
	if _, err := Diff([]byte(`{`), []byte(`{}`)); err == nil {
		t.Error("invalid expected")
	}
	if _, err := Diff([]byte(`{}`), []byte(`{`)); err == nil {
		t.Error("invalid produced")
	}
}

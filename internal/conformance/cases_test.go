package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

func def(cmd, variant string) string {
	return "format: 1\ncommand: " + cmd + "\nvariant: " + variant + "\nparse: {type: kv}\n"
}

func TestCases(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"parsers/df/bsd/parser.yaml":              {Data: []byte(def("df", "bsd"))},
		"parsers/df/bsd/testdata/a.txt":           {Data: []byte("x")},
		"parsers/df/bsd/testdata/a.json":          {Data: []byte("[]")},
		"parsers/df/bsd/testdata/notes.md":        {Data: []byte("ignored")},
		"parsers/uptime/linux/parser.yaml":        {Data: []byte(def("uptime", "linux"))},
		"parsers/uptime/linux/testdata/fail.txt":  {Data: []byte("bad")},
		"parsers/uptime/linux/testdata/fail.yaml": {Data: []byte("expect_error: does not match\nargs: [-a]\nos: linux\n")},
		"parsers/uptime/linux/testdata/ok.txt":    {Data: []byte("x")},
		"parsers/uptime/linux/testdata/ok.json":   {Data: []byte("{}")},
		"parsers/custom/default/parser.yaml":      {Data: []byte(def("custom", "default"))},
	}
	reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	bsd, _ := reg.Lookup("df", "bsd")
	cases, err := Cases(fsys, bsd)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Name != "a" || string(cases[0].Input) != "x" || string(cases[0].Expected) != "[]" {
		t.Errorf("cases = %+v", cases)
	}
	up, _ := reg.Lookup("uptime", "linux")
	cases, err = Cases(fsys, up)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].Name != "fail" || cases[0].Meta.ExpectError != "does not match" || cases[0].Meta.OS != "linux" || cases[1].Name != "ok" {
		t.Errorf("cases = %+v", cases)
	}
	custom, _ := reg.Lookup("custom", "default")
	if cases, err := Cases(fsys, custom); err != nil || len(cases) != 0 {
		t.Errorf("no testdata dir: %v %v", cases, err)
	}
	if testdataPath(custom) != "parsers/custom/default/testdata" {
		t.Error(testdataPath(custom))
	}
}

func TestCasesErrors(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"parsers/a/b/parser.yaml":     {Data: []byte(def("a", "b"))},
		"parsers/a/b/testdata/x.txt":  {Data: []byte("in")},
		"parsers/c/d/parser.yaml":     {Data: []byte(def("c", "d"))},
		"parsers/c/d/testdata/x.txt":  {Data: []byte("in")},
		"parsers/c/d/testdata/x.yaml": {Data: []byte("bogus: 1\n")},
	}
	reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := reg.Lookup("a", "b")
	if cases, err := Cases(fsys, e); err != nil || len(cases) != 1 || cases[0].Expected != nil {
		t.Errorf("missing json should load with nil Expected: %v %v", cases, err)
	}
	e, _ = reg.Lookup("c", "d")
	if _, err := Cases(fsys, e); err == nil || !strings.Contains(err.Error(), "invalid case metadata") {
		t.Errorf("bad meta: %v", err)
	}
}

// A case is found by its .txt, so a .yaml or .json beside no .txt (a
// misnamed fixture) would describe a case that never runs, and a .json
// next to expect_error states an answer that is never compared. Both
// are reported rather than passing in silence.
func TestCasesRefusesWhatWouldNeverRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"yaml without txt", map[string]string{"x.txt": "in", "x.json": "[]", "y.yaml": "source: s\n"}, "y.yaml has no y.txt"},
		{"json without txt", map[string]string{"x.text": "in", "x.json": "[]"}, "x.json has no x.txt"},
		{"json beside expect_error", map[string]string{"x.txt": "in", "x.yaml": "expect_error: no\n", "x.json": "[]"}, "x.json states an answer for a case that expects an error"},
		{"metadata over the definition limit", map[string]string{"x.txt": "in", "x.yaml": "description: " + strings.Repeat("a", 256*1024) + "\n"}, "x.yaml exceeds 262144 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := fstest.MapFS{"parsers/a/b/parser.yaml": {Data: []byte(def("a", "b"))}}
			for name, data := range tc.files {
				fsys["parsers/a/b/testdata/"+name] = &fstest.MapFile{Data: []byte(data)}
			}
			reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
			if err != nil {
				t.Fatal(err)
			}
			e, _ := reg.Lookup("a", "b")
			if _, err := Cases(fsys, e); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Cases = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// A fixture over the input limit is refused where it is read, and no
// more than the limit is read of it.
func TestCasesBoundTheFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "parsers/a/b/testdata"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parsers/a/b/parser.yaml"), []byte(def("a", "b")), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "parsers/a/b/testdata/big.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64*1024*1024 + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	fsys := os.DirFS(dir)
	reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := reg.Lookup("a", "b")
	if _, err := Cases(fsys, e); err == nil || !strings.Contains(err.Error(), "big.txt exceeds 67108864 bytes") {
		t.Errorf("Cases = %v", err)
	}
}

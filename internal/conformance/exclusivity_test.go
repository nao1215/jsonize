package conformance

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

// loose reads anything that has a colon in it, which is also the shape of
// tight's output; tight refuses everything but its own.
const (
	looseDef = "format: 1\ncommand: loose\nvariant: v\naliases: [{name: esool}]\ndetect: {auto_detect: false, signature: {all: ['^\\S+: ']}}\nparse: {type: kv, separator: ':', as: map}\n"
	tightDef = "format: 1\ncommand: tight\nvariant: v\ndetect: {signature: {all: ['^name: \\S']}}\nparse: {type: kv, separator: ':', as: map}\n"
	crashDef = "format: 1\ncommand: crash\nvariant: v\ndetect: {auto_detect: false, signature: {all: ['^\\S+: ']}}\nparse: {type: regex, pattern: '^(?P<n>\\d+)$'}\n"
)

func exclusivityFS() fstest.MapFS {
	return fstest.MapFS{
		"parsers/loose/v/parser.yaml":         {Data: []byte(looseDef)},
		"parsers/loose/v/testdata/a.txt":      {Data: []byte("x: 1\n")},
		"parsers/loose/v/testdata/a.json":     {Data: []byte("{\"x\": \"1\"}")},
		"parsers/tight/v/parser.yaml":         {Data: []byte(tightDef)},
		"parsers/tight/v/testdata/a.txt":      {Data: []byte("name: bob\n")},
		"parsers/tight/v/testdata/a.json":     {Data: []byte("{\"name\": \"bob\"}")},
		"parsers/tight/v/testdata/other.txt":  {Data: []byte("nothing here\n")},
		"parsers/tight/v/testdata/other.yaml": {Data: []byte("expect_error: does not describe\n")},
		"parsers/crash/v/parser.yaml":         {Data: []byte(crashDef)},
		"parsers/crash/v/testdata/a.txt":      {Data: []byte("7\n")},
		"parsers/crash/v/testdata/a.json":     {Data: []byte("[{\"n\": \"7\"}]")},
	}
}

func messages(results []Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			out = append(out, r.Err.Error())
		}
	}
	return out
}

func TestExclusivity(t *testing.T) {
	t.Parallel()
	fsys := exclusivityFS()
	src := registry.Source{Name: "s", FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, problems := Fixtures(reg, []registry.Source{src})
	if len(problems) != 0 {
		t.Fatalf("loading fixtures: %v", problems)
	}
	if len(fixtures) != 4 {
		t.Fatalf("got %d fixtures, want 4", len(fixtures))
	}
	all := func(string) bool { return true }
	got := messages(Exclusivity(reg, fixtures, all, Options{Parallel: 2}))
	want := []string{
		// The tight fixture is the one loose has no business reading, and
		// the alias is a second way in, so it is reported separately.
		"loose/v read parsers/tight/v/testdata/a.txt, a fixture of tight/v",
		"loose/v (named esool) read parsers/tight/v/testdata/a.txt, a fixture of tight/v",
		// crash lets the same text past its signature and then fails,
		// which is the same defect caught one step later.
		"crash/v accepted parsers/tight/v/testdata/a.txt, a fixture of tight/v, and then failed to parse it",
		"crash/v accepted parsers/loose/v/testdata/a.txt, a fixture of loose/v, and then failed to parse it",
	}
	for _, w := range want {
		if !contains(got, w) {
			t.Errorf("missing %q in %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d failures, want %d: %v", len(got), len(want), got)
	}
}

// A pair is only checked when one of its two halves is under test, which
// is what keeps `jz test DIR` from reporting the official registry's
// business back at the user.
func TestExclusivityTargetsOneSource(t *testing.T) {
	t.Parallel()
	fsys := exclusivityFS()
	src := registry.Source{Name: "s", FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, _ := Fixtures(reg, []registry.Source{src})
	none := func(string) bool { return false }
	if got := messages(Exclusivity(reg, fixtures, none, Options{})); len(got) != 0 {
		t.Errorf("got %v, want nothing checked", got)
	}
}

func TestDecoys(t *testing.T) {
	t.Parallel()
	fsys := exclusivityFS()
	reg, err := registry.Load(registry.Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	got := messages(Decoys(reg, []Decoy{
		{Name: "colon.txt", Input: []byte("name: bob\n")},
		{Name: "quiet.txt", Input: []byte("nothing to see\n")},
	}, Options{}))
	for _, w := range []string{
		"automatic detection read the decoy colon.txt as tight/v",
		"loose/v read the decoy colon.txt",
		"loose/v (named esool) read the decoy colon.txt",
		"crash/v read the decoy colon.txt",
	} {
		if !contains(got, w) {
			t.Errorf("missing %q in %v", w, got)
		}
	}
	for _, g := range got {
		if strings.Contains(g, "quiet.txt") {
			t.Errorf("quiet.txt was read: %s", g)
		}
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	fsys := exclusivityFS()
	src := registry.Source{Name: "s", FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	sources := []registry.Source{src}
	results := Check(reg, sources, []string{"s"}, Options{})
	_, failed := Summary(results)
	if failed == 0 {
		t.Error("Check reported no failure although loose/v reads tight/v output")
	}
	skipped := Check(reg, sources, []string{"s"}, Options{SkipExclusivity: true})
	if len(skipped) >= len(results) {
		t.Errorf("SkipExclusivity kept %d of %d results", len(skipped), len(results))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

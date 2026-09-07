package registry

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/conformance"
	"github.com/nao1215/jsonize/internal/registry"
)

// This test is a thin wrapper around the same check `jz test` runs, so
// the official registry is held to the contract third parties are told
// to hold theirs to. `make registry-test` runs it and
// `make registry-update-golden` regenerates the expected JSON.
var update = flag.Bool("update", false, "rewrite testdata/<case>.json golden files from the current output")

func TestRegistry(t *testing.T) {
	const name = "embedded"
	fsys := FS()
	src := registry.Source{Name: name, FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Problems {
		t.Errorf("definition failed to load: %v", p)
	}
	if reg.Len() == 0 {
		t.Fatalf("%s contains no definitions", name)
	}
	opts := conformance.Options{Update: *update}
	if testing.Short() {
		// The cross product of every definition with every fixture is the
		// expensive half; -short keeps the golden cases.
		opts.SkipExclusivity = true
	}
	results := conformance.Check(reg, []registry.Source{src}, []string{name}, opts)
	for _, r := range results {
		caseName := r.Definition
		if r.Case != "" {
			caseName += "/" + r.Case
		}
		t.Run(caseName, func(t *testing.T) {
			if r.Err != nil {
				t.Error(r.Err)
				return
			}
			if *update && r.Actual != nil {
				golden := filepath.FromSlash(strings.TrimSuffix(r.Path, ".txt") + ".json")
				if err := os.WriteFile(golden, r.Actual, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

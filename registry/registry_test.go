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

// This test is the validation entry point for parser authors: it loads a
// registry, checks every definition and runs the golden cases stored next
// to them. `make registry-test` runs it for the official registry,
// `make registry-test DIR=path` for a local one, and
// `make registry-update-golden` regenerates the expected JSON.
var (
	update      = flag.Bool("update", false, "rewrite testdata/<case>.json golden files from the current output")
	registryDir = flag.String("registry-dir", "", "validate this registry directory instead of the embedded one")
)

func TestRegistry(t *testing.T) {
	name, fsys := "embedded", FS()
	if *registryDir != "" {
		abs, err := filepath.Abs(*registryDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(abs, registry.ParsersDir)); err != nil {
			t.Fatalf("%s is not a registry directory: no %s/ inside it", abs, registry.ParsersDir)
		}
		name, fsys = abs, os.DirFS(abs)
	}
	reg, err := registry.Load(registry.Source{Name: name, FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Problems {
		t.Errorf("definition failed to load: %v", p)
	}
	if reg.Len() == 0 {
		t.Fatalf("%s contains no definitions", name)
	}
	results := conformance.Run(reg, fsys, name, conformance.Options{Update: *update})
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
				if *registryDir != "" {
					golden = filepath.Join(name, golden)
				}
				if err := os.WriteFile(golden, r.Actual, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

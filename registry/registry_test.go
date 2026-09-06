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

var update = flag.Bool("update", false, "rewrite testdata/<case>.json golden files from the current output")

// TestEmbeddedRegistry proves every official definition loads, every
// fixture selects its own variant, and every fixture parses to its golden
// JSON. Run `go test ./registry -update` after changing a definition to
// regenerate the golden files, then review the diff.
func TestEmbeddedRegistry(t *testing.T) {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: FS()})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Problems {
		t.Errorf("definition failed to load: %v", p)
	}
	if reg.Len() == 0 {
		t.Fatal("no definitions embedded")
	}
	results := conformance.Run(reg, FS(), "embedded", conformance.Options{Update: *update})
	for _, r := range results {
		name := r.Definition
		if r.Case != "" {
			name += "/" + r.Case
		}
		t.Run(name, func(t *testing.T) {
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

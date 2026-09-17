package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
)

func entryIDs(entries []*registry.Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Def.ID())
	}
	return ids
}

// A name reads, from the part of the built-in registry loaded for it,
// exactly the definitions it reads from the whole one: every command and
// every alias, including the names several commands answer to.
func TestNamedDefinitionsMatchTheWholeRegistry(t *testing.T) {
	t.Parallel()
	whole, err := registry.Load(registry.Source{Name: SourceEmbedded, FS: official.FS()})
	if err != nil {
		t.Fatal(err)
	}
	names := whole.Commands()
	for _, c := range whole.Commands() {
		names = append(names, whole.Aliases(c)...)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		named, ok := namedDefinitions(official.FS(), []string{name})
		if !ok {
			t.Errorf("%s: no definitions found for a name the registry answers to", name)
			continue
		}
		part, err := registry.Load(registry.Source{Name: SourceEmbedded, FS: named})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(part.Problems) > 0 || len(part.Warnings) > 0 {
			t.Errorf("%s: problems %v, warnings %v", name, part.Problems, part.Warnings)
		}
		want, got := entryIDs(whole.Variants(name)), entryIDs(part.Variants(name))
		if !slices.Equal(got, want) {
			t.Errorf("%s: read %v from its part of the registry, %v from the whole", name, got, want)
		}
	}
	if _, ok := namedDefinitions(official.FS(), []string{"no-such-command"}); ok {
		t.Error("a name nothing answers to found definitions")
	}
}

// Naming a parser reads only its definitions, and a name nothing answers
// to reads the whole registry, which is what lists the names jz knows.
func TestLoadRegistryForReadsTheNamedPart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := &app{env: h.env}
	reg, code := a.loadRegistryFor("df", "nice")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	for _, e := range reg.Entries() {
		names := append([]string{e.Def.Command}, e.Def.AliasNames()...)
		if !slices.Contains(names, "df") && !slices.Contains(names, "nice") {
			t.Errorf("loading for df and nice read %s as well", e.Def.ID())
		}
	}
	whole, _ := a.loadRegistryFor()
	if reg.Len() >= whole.Len() {
		t.Errorf("loading for df read %d definitions, the whole registry holds %d", reg.Len(), whole.Len())
	}
	unknown, code := a.loadRegistryFor("no-such-command")
	if code != ExitOK || unknown.Len() != whole.Len() {
		t.Errorf("an unknown name read %d definitions (code %d); the whole registry holds %d", unknown.Len(), code, whole.Len())
	}
}

// A registry of the user's own is read whole, and so is the built-in one
// beside it: its disable list is about definitions a name does not reach,
// and reading part of the built-in one would report its entries as
// matching nothing.
func TestLoadRegistryForWithAUserRegistry(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registry.ManifestFile), []byte("format: 1\ndisable: [uname]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.registryPath = dir
	if code := h.pipe(gnuDF, "--parser", "df"); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, h.stderr.String())
	}
	if strings.Contains(h.stderr.String(), "matches no definition") || h.stderr.Len() != 0 {
		t.Errorf("stderr = %q", h.stderr.String())
	}
	a := &app{env: h.env}
	reg, _ := a.loadRegistryFor("df")
	whole, _ := a.loadRegistryFor()
	if reg.Len() != whole.Len() {
		t.Errorf("with a user registry, loading for df read %d definitions; the whole registry holds %d", reg.Len(), whole.Len())
	}
}

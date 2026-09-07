package cli

import (
	"os"
	"path/filepath"

	"github.com/nao1215/jsonize/pkg/registry"
)

// Source names used in diagnostics and `jz list --sources`.
const (
	SourceEmbedded = "embedded"
	SourceUser     = "user"
)

// EnvRegistryPath lists extra registry directories, separated like PATH.
const EnvRegistryPath = "JSONIZE_REGISTRY_PATH"

// userRegistryDir returns <config>/jsonize/registry.
func (a *app) userRegistryDir() (string, error) {
	dir, err := a.env.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jsonize", "registry"), nil
}

// sources resolves the registry layering in precedence order. jz reads
// only local directories: no network access happens at any point, so a
// given input always converts to the same JSON on a given machine.
//
// Extra registries come from JSONIZE_REGISTRY_PATH, a list separated the
// way PATH is on the platform, so a shell can point jz at a directory of
// definitions without jz growing a flag for it.
func (a *app) sources() ([]registry.Source, error) {
	var out []registry.Source
	if p := a.env.Getenv(EnvRegistryPath); p != "" {
		for _, d := range filepath.SplitList(p) {
			if d == "" {
				continue
			}
			abs, err := filepath.Abs(d)
			if err != nil {
				return nil, err
			}
			out = append(out, registry.Source{Name: abs, FS: os.DirFS(abs), Optional: true})
		}
	}
	if dir, err := a.userRegistryDir(); err == nil {
		out = append(out, registry.Source{Name: SourceUser, FS: os.DirFS(dir), Optional: true})
	}
	emb := a.env.Embedded
	if emb.Name == "" {
		emb.Name = SourceEmbedded
	}
	out = append(out, emb)
	return out, nil
}

// loadRegistry loads the layered registry. Definitions that fail to load
// are reported on stderr but do not abort, so one broken user file cannot
// take every parser down.
func (a *app) loadRegistry() (*registry.Registry, int) {
	srcs, err := a.sources()
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitRegistry
	}
	reg, err := registry.Load(srcs...)
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitRegistry
	}
	for _, p := range reg.Problems {
		a.errorf("warning: skipping definition: %v", p)
	}
	for _, w := range reg.Warnings {
		a.errorf("warning: %v", w)
	}
	return reg, 0
}

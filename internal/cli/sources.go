package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nao1215/jsonize/internal/registry"
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
func (a *app) sources(rf *registryFlags) ([]registry.Source, error) {
	var out []registry.Source
	for _, d := range rf.dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("--registry %s: %w", d, err)
		}
		out = append(out, registry.Source{Name: abs, FS: os.DirFS(abs)})
	}
	if !rf.embeddedOnly {
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
func (a *app) loadRegistry(rf *registryFlags) (*registry.Registry, int) {
	srcs, err := a.sources(rf)
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
	return reg, 0
}

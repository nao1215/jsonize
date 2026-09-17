package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nao1215/jsonize/internal/yaml"
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
	return a.loadRegistryFor()
}

// loadRegistryFor loads the registry a command that names its parser
// needs. names are the parser first and then the other names the
// command line could bring into what jz says, such as the command a
// wrapper runs. With no names it loads every definition.
//
// Reading every definition is what starting jz costs, several times
// what reading a short output does, and a named parser uses only its
// own. So when the built-in registry is the only one there is, only the
// definitions that answer to one of the names are read from it. When
// none answers to the parser, everything is read after all, since what
// jz then says (the names it knows, the command a wrapper runs) is about
// the whole registry. A registry of the user's own is read whole, with
// the built-in one, since its disable list is about definitions the
// names do not reach.
func (a *app) loadRegistryFor(names ...string) (*registry.Registry, int) {
	srcs, err := a.sources()
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitRegistry
	}
	if len(names) > 0 && onlyLast(srcs) {
		last := len(srcs) - 1
		if named, ok := namedDefinitions(srcs[last].FS, names); ok {
			scoped := slices.Clone(srcs)
			scoped[last].FS = named
			if reg, err := registry.Load(scoped...); err == nil && len(reg.Variants(names[0])) > 0 {
				return a.reportLoad(reg)
			}
		}
	}
	reg, err := registry.Load(srcs...)
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitRegistry
	}
	return a.reportLoad(reg)
}

// namedParser is the names loadRegistryFor needs for --parser: the
// parser, or none when no parser is named and the text decides.
func namedParser(parser string) []string {
	if parser == "" {
		return nil
	}
	return []string{parser}
}

// reportLoad names on stderr the definitions a registry could not load
// and what else loading it found worth saying.
func (a *app) reportLoad(reg *registry.Registry) (*registry.Registry, int) {
	for _, p := range reg.Problems {
		a.errorf("warning: skipping definition: %v", p)
	}
	for _, w := range reg.Warnings {
		a.errorf("warning: %v", w)
	}
	return reg, 0
}

// onlyLast reports whether the last of the sources, the built-in
// registry, is the only one that is there.
func onlyLast(srcs []registry.Source) bool {
	for _, s := range srcs[:len(srcs)-1] {
		if _, err := fs.Stat(s.FS, "."); !errors.Is(err, fs.ErrNotExist) {
			return false
		}
	}
	return true
}

// namedDefinitions returns the part of a registry that holds the
// definitions answering to one of names: those under a name's own
// directory, and those that list the name among their aliases. It
// reports false when no definition answers to the first name.
//
// A definition names its command by the directory it is in, which the
// registry checks, so only the definitions that declare aliases have to
// be read to be placed, and a file that does not hold the word aliases
// and one of the names is passed over unread.
func namedDefinitions(fsys fs.FS, names []string) (fs.FS, bool) {
	commands, err := fs.ReadDir(fsys, registry.ParsersDir)
	if err != nil {
		return nil, false
	}
	keep := map[string]bool{}
	found := false
	for _, c := range commands {
		if !c.IsDir() {
			continue
		}
		dir := path.Join(registry.ParsersDir, c.Name())
		variants, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return nil, false
		}
		own := slices.Contains(names, c.Name())
		for _, v := range variants {
			if !v.IsDir() {
				continue
			}
			vdir := path.Join(dir, v.Name())
			answers, first := own, own && c.Name() == names[0]
			if !own {
				answers, first = aliasedTo(fsys, path.Join(vdir, registry.DefinitionFile), names)
			}
			if answers {
				keep[vdir] = true
				found = found || first
			}
		}
	}
	if !found {
		return nil, false
	}
	return &namedFS{fsys: fsys, keep: keep}, true
}

// aliasedTo reports whether the definition at file lists one of names as
// an alias, and whether that is the first of them.
func aliasedTo(fsys fs.FS, file string, names []string) (answers, first bool) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil || !bytes.Contains(data, []byte("aliases:")) || !slices.ContainsFunc(names, func(name string) bool {
		return bytes.Contains(data, []byte(name))
	}) {
		return false, false
	}
	var def struct {
		Aliases []struct {
			Name string `yaml:"name"`
		} `yaml:"aliases"`
	}
	if yaml.Unmarshal(data, &def, false) != nil {
		// The registry reports a definition it cannot read; keeping it
		// lets it do so.
		return true, false
	}
	for _, alias := range def.Aliases {
		if slices.Contains(names, alias.Name) {
			answers = true
			first = first || alias.Name == names[0]
		}
	}
	return answers, first
}

// namedFS is a registry with only some of its definition directories:
// the manifest, the parsers directory, and each kept variant directory
// with everything in it.
type namedFS struct {
	fsys fs.FS
	keep map[string]bool
}

func (n *namedFS) shows(name string) bool {
	if name == "." || name == registry.ManifestFile || name == registry.ParsersDir {
		return true
	}
	parts := strings.SplitN(name, "/", 4)
	if parts[0] != registry.ParsersDir || len(parts) < 2 {
		return false
	}
	if len(parts) == 2 {
		prefix := name + "/"
		for dir := range n.keep {
			if strings.HasPrefix(dir, prefix) {
				return true
			}
		}
		return false
	}
	return n.keep[strings.Join(parts[:3], "/")]
}

// Open opens a file the registry keeps, and reports any other as not
// there.
func (n *namedFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) || !n.shows(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return n.fsys.Open(name)
}

// ReadDir lists what a kept directory holds that the registry keeps.
func (n *namedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) || !n.shows(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(n.fsys, name)
	if err != nil {
		return nil, err
	}
	shown := entries[:0]
	for _, e := range entries {
		if n.shows(path.Join(name, e.Name())) {
			shown = append(shown, e)
		}
	}
	return shown, nil
}

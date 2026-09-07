// Package registry loads parser definitions from one or more sources and
// merges them into a single index.
//
// A registry directory has this layout:
//
//	registry.yaml                          # manifest (format, name, version)
//	parsers/<command>/<variant>/parser.yaml
//	parsers/<command>/<variant>/testdata/<case>.txt   # captured output
//	parsers/<command>/<variant>/testdata/<case>.json  # expected JSON
//	parsers/<command>/<variant>/testdata/<case>.yaml  # optional case metadata
//
// Sources are layered: a definition with the same command/variant in a
// higher-precedence source shadows the lower one, so users can override an
// official definition without editing it. Each entry also carries the
// position of its source in that layering, which is what the selector
// uses to settle a collision between definitions of different commands.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/nao1215/jsonize/internal/definition"
)

// ManifestFile is the name of the registry manifest.
const ManifestFile = "registry.yaml"

// ParsersDir is the directory holding definitions.
const ParsersDir = "parsers"

// DefinitionFile is the file name of a definition inside its variant dir.
const DefinitionFile = "parser.yaml"

// TestdataDir holds fixtures next to a definition.
const TestdataDir = "testdata"

// MaxDefinitions bounds how many definitions one source may contain.
const MaxDefinitions = 10000

// Manifest describes a registry.
type Manifest struct {
	Format      int    `yaml:"format"`
	Name        string `yaml:"name"`
	Version     string `yaml:"version,omitempty"`
	Description string `yaml:"description,omitempty"`
	Source      string `yaml:"source,omitempty"`
}

// Source is one place definitions come from.
type Source struct {
	// Name identifies the source in diagnostics (e.g. "embedded", "user",
	// or a directory path).
	Name string
	// FS is rooted at the registry directory.
	FS fs.FS
	// Optional sources are skipped silently when the directory is absent.
	Optional bool
}

// Entry is a loaded definition with its provenance.
type Entry struct {
	Def    *definition.Definition
	Source string
	// Path is the definition path inside its source FS.
	Path string
	// Precedence is the position of Source in the layering Load was given,
	// 0 being the most preferred. Two entries share it exactly when they
	// come from the same registry.
	Precedence int
	// Shadowed lists sources whose definition for the same id was hidden by
	// this one.
	Shadowed []string
}

// Registry is the merged index.
type Registry struct {
	entries   map[string]*Entry // id -> entry
	byCommand map[string][]*Entry
	// commands holds the names a definition calls its own, which is what
	// Commands reports; byCommand also answers to the aliases.
	commands map[string]bool
	// Problems lists definitions that failed to load. The registry stays
	// usable; callers decide whether problems are fatal.
	Problems []error
}

// LoadError is a problem with one definition file.
type LoadError struct {
	Source string
	Path   string
	Err    error
}

func (e *LoadError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Source, e.Path, e.Err)
}

func (e *LoadError) Unwrap() error { return e.Err }

// Load reads every source in precedence order (first wins).
func Load(sources ...Source) (*Registry, error) {
	r := &Registry{entries: map[string]*Entry{}, byCommand: map[string][]*Entry{}, commands: map[string]bool{}}
	for i, src := range sources {
		if err := r.addSource(i, src); err != nil {
			return nil, err
		}
	}
	for _, list := range r.byCommand {
		sort.Slice(list, func(i, j int) bool { return list[i].Def.Variant < list[j].Def.Variant })
	}
	return r, nil
}

func (r *Registry) addSource(precedence int, src Source) error {
	if src.FS == nil {
		return fmt.Errorf("source %q has no filesystem", src.Name)
	}
	if _, err := fs.Stat(src.FS, "."); err != nil {
		if src.Optional {
			return nil
		}
		return fmt.Errorf("registry %q: %w", src.Name, err)
	}
	// The manifest is read for its format version: a registry written for
	// a newer jsonize must be refused rather than half understood.
	var manifest Manifest
	if data, err := fs.ReadFile(src.FS, ManifestFile); err == nil {
		if err := definition.DecodeYAML(data, &manifest); err != nil {
			return &LoadError{Source: src.Name, Path: ManifestFile, Err: fmt.Errorf("invalid manifest: %w", err)}
		}
		if manifest.Format != definition.CurrentFormat {
			return &LoadError{Source: src.Name, Path: ManifestFile, Err: &definition.FormatError{Source: ManifestFile, Got: manifest.Format}}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &LoadError{Source: src.Name, Path: ManifestFile, Err: err}
	}
	if _, err := fs.Stat(src.FS, ParsersDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return &LoadError{Source: src.Name, Path: ParsersDir, Err: err}
	}
	count := 0
	err := fs.WalkDir(src.FS, ParsersDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &LoadError{Source: src.Name, Path: p, Err: err}
		}
		if d.IsDir() || d.Name() != DefinitionFile {
			return nil
		}
		count++
		if count > MaxDefinitions {
			return &LoadError{Source: src.Name, Path: p, Err: fmt.Errorf("more than %d definitions", MaxDefinitions)}
		}
		data, err := fs.ReadFile(src.FS, p)
		if err != nil {
			return &LoadError{Source: src.Name, Path: p, Err: err}
		}
		def, err := definition.Load(data, src.Name+":"+p)
		if err != nil {
			r.Problems = append(r.Problems, &LoadError{Source: src.Name, Path: p, Err: err})
			return nil //nolint:nilerr // recorded in Problems so other definitions still load
		}
		if err := checkLayout(p, def); err != nil {
			r.Problems = append(r.Problems, &LoadError{Source: src.Name, Path: p, Err: err})
			return nil //nolint:nilerr // recorded in Problems so other definitions still load
		}
		def.Origin = src.Name
		r.add(&Entry{Def: def, Source: src.Name, Path: p, Precedence: precedence})
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// checkLayout enforces parsers/<command>/<variant>/parser.yaml so that the
// path a reviewer sees always matches the identity inside the file.
func checkLayout(p string, def *definition.Definition) error {
	want := path.Join(ParsersDir, def.Command, def.Variant, DefinitionFile)
	if p != want {
		return fmt.Errorf("definition %s must live at %s (found at %s)", def.ID(), want, p)
	}
	return nil
}

func (r *Registry) add(e *Entry) {
	id := e.Def.ID()
	if existing, ok := r.entries[id]; ok {
		existing.Shadowed = append(existing.Shadowed, e.Source)
		return
	}
	r.entries[id] = e
	r.byCommand[e.Def.Command] = append(r.byCommand[e.Def.Command], e)
	r.commands[e.Def.Command] = true
	for _, name := range e.Def.AliasNames() {
		if name == e.Def.Command {
			continue
		}
		r.byCommand[name] = append(r.byCommand[name], e)
	}
}

// Lookup returns the definition for command/variant. The command may be
// an alias, in which case the definition it names answers.
func (r *Registry) Lookup(command, variant string) (*Entry, bool) {
	if e, ok := r.entries[command+"/"+variant]; ok {
		return e, true
	}
	for _, e := range r.byCommand[command] {
		if e.Def.Variant == variant {
			return e, true
		}
	}
	return nil, false
}

// Variants returns the entries for a command sorted by variant name.
func (r *Registry) Variants(command string) []*Entry {
	return r.byCommand[command]
}

// Commands returns the known command names sorted. An alias is not one:
// it answers to Variants and Lookup, and the command it belongs to
// reports it.
func (r *Registry) Commands() []string {
	out := make([]string, 0, len(r.commands))
	for c := range r.commands {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Aliases returns the other names the definitions of a command answer
// to, sorted and without duplicates.
func (r *Registry) Aliases(command string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range r.byCommand[command] {
		if e.Def.Command != command {
			continue
		}
		for _, name := range e.Def.AliasNames() {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Entries returns every entry sorted by id.
func (r *Registry) Entries() []*Entry {
	out := make([]*Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Def.ID() < out[j].Def.ID() })
	return out
}

// Len returns the number of distinct definitions.
func (r *Registry) Len() int {
	return len(r.entries)
}

// TestdataPath returns the testdata directory of an entry inside its FS.
func (e *Entry) TestdataPath() string {
	return path.Join(path.Dir(e.Path), TestdataDir)
}

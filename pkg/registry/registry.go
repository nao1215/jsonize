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
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nao1215/jsonize/pkg/definition"
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

// manifest describes a registry.
type manifest struct {
	Format      int    `yaml:"format"`
	Name        string `yaml:"name"`
	Version     string `yaml:"version,omitempty"`
	Description string `yaml:"description,omitempty"`
	Source      string `yaml:"source,omitempty"`
	// Disable names definitions of the less preferred registries below
	// this one that must not be loaded at all: "command/variant" for one
	// definition, "command" for every variant of it. It is how a user
	// switches off a definition that misreads their output, where
	// shadowing would mean rewriting the whole thing.
	Disable []string `yaml:"disable,omitempty"`
}

// disableRe matches "command" and "command/variant", the two forms a
// disable entry may take.
var disableRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*(/[a-z0-9][a-z0-9-]*)?$`)

// disableRule is one parsed entry of a manifest's disable list.
type disableRule struct {
	// Source is the registry that declared the rule.
	source  string
	text    string
	command string
	// variant is empty when the rule covers every variant of the command.
	variant string
	matched bool
}

func (d disableRule) covers(def *definition.Definition) bool {
	return d.command == def.Command && (d.variant == "" || d.variant == def.Variant)
}

// Source is one place definitions come from.
type Source struct {
	// Name identifies the source in diagnostics (e.g. "embedded", "user",
	// or a directory path).
	Name string
	// FS is rooted at the registry directory.
	FS fs.FS
	// Optional sources are skipped silently when the directory does not
	// exist. A directory that exists and cannot be read is an error even
	// then: falling back to the definitions below it would read the text
	// with a registry the user did not mean.
	Optional bool
}

// Entry is a loaded definition with its provenance. The registry hands
// out its entries as they are, and every lookup of the same definition
// returns the same one: a caller reads them and does not change them.
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
	// all is every entry sorted by id, and names the commands sorted,
	// made once the sources are read: a selection over the whole
	// registry asks for them every time, and the registry does not change
	// after Load.
	all   []*Entry
	names []string
	// Problems lists definitions that failed to load. The registry stays
	// usable; callers decide whether problems are fatal.
	Problems []error
	// Warnings lists things that are worth saying but do not change what
	// the registry holds, such as a disable entry that named nothing.
	Warnings []error

	// rules are the disable entries of the sources read so far. They only
	// apply to the sources read after them.
	rules []disableRule
	// disabled counts, per source, the definitions a preferred registry
	// switched off.
	disabled map[string]int
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
	r := &Registry{
		entries:   map[string]*Entry{},
		byCommand: map[string][]*Entry{},
		commands:  map[string]bool{},
		disabled:  map[string]int{},
	}
	for i, src := range sources {
		if err := r.addSource(i, src); err != nil {
			return nil, err
		}
	}
	for _, rule := range r.rules {
		if !rule.matched {
			r.Warnings = append(r.Warnings, fmt.Errorf("%s: %s: disable %q matches no definition", rule.source, ManifestFile, rule.text))
		}
	}
	for _, list := range r.byCommand {
		slices.SortFunc(list, func(a, b *Entry) int { return strings.Compare(a.Def.Variant, b.Def.Variant) })
	}
	r.all = slices.SortedFunc(maps.Values(r.entries), func(a, b *Entry) int { return strings.Compare(a.Def.ID(), b.Def.ID()) })
	r.names = slices.Sorted(maps.Keys(r.commands))
	return r, nil
}

// addSource reads one registry. What goes wrong in it falls into two
// kinds. A registry that is not the one the caller meant -- no
// filesystem, a root that is not there or cannot be read, a manifest
// that cannot be read or names another format, a disable list that is
// not written as names -- is refused whole, because reading the
// registries below it instead would answer with definitions the caller
// did not ask for. Anything smaller -- one file that is too large, does
// not parse, sits at the wrong path or cannot be read, one directory
// that cannot be read -- is that file's or that directory's problem: it
// is named in Problems and everything beside it still loads.
func (r *Registry) addSource(precedence int, src Source) error {
	if src.FS == nil {
		return fmt.Errorf("source %q has no filesystem", src.Name)
	}
	if _, err := fs.Stat(src.FS, "."); err != nil {
		if src.Optional && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("registry %q: %w", src.Name, err)
	}
	// The manifest is read for its format version: a registry written for
	// a newer jsonize must be refused rather than half understood.
	var m manifest
	if data, err := ReadBounded(src.FS, ManifestFile, definition.MaxDefinitionSize); err == nil {
		if err := definition.DecodeYAML(data, &m); err != nil {
			return &LoadError{Source: src.Name, Path: ManifestFile, Err: fmt.Errorf("invalid manifest: %w", err)}
		}
		if m.Format != definition.CurrentFormat {
			return &LoadError{Source: src.Name, Path: ManifestFile, Err: &definition.FormatError{Source: ManifestFile, Got: m.Format}}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &LoadError{Source: src.Name, Path: ManifestFile, Err: err}
	}
	// A registry's own definitions are never affected by its own disable
	// list, so the rules it declares are collected after its files are
	// read and apply to the registries below it.
	rules, err := parseDisable(src.Name, m.Disable)
	if err != nil {
		return err
	}
	defer func() { r.rules = append(r.rules, rules...) }()
	switch fi, err := fs.Stat(src.FS, ParsersDir); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		r.Problems = append(r.Problems, &LoadError{Source: src.Name, Path: ParsersDir, Err: err})
		return nil
	case !fi.IsDir():
		// Walking a file finds no definitions, so loading such a source
		// as an empty one would leave its author with a registry that
		// changes nothing and no reason why.
		r.Problems = append(r.Problems, &LoadError{Source: src.Name, Path: ParsersDir, Err: errors.New("not a directory")})
		return nil
	}
	files, err := readFiles(src)
	if err != nil {
		return err
	}
	// The files are decoded side by side: a definition says nothing
	// about another, so the order they are read in changes nothing, and
	// the decoding is what a registry of several hundred files costs
	// every time jz starts. What was found is then taken in the order of
	// the walk, so the problems are reported and the definitions indexed
	// the same way every time.
	decodeAll(files, src.Name)
	for _, f := range files {
		if f.err != nil {
			r.Problems = append(r.Problems, &LoadError{Source: src.Name, Path: f.path, Err: f.err})
			continue
		}
		if r.disable(f.def) {
			r.disabled[src.Name]++
			continue
		}
		f.def.Origin = src.Name
		r.add(&Entry{Def: f.def, Source: src.Name, Path: f.path, Precedence: precedence})
	}
	return nil
}

// filesPerWorker is how many files each goroutine of decodeAll is
// given at the least, so that a small registry is not spread thinner
// than starting the goroutines is worth.
const filesPerWorker = 8

// file is one definition file of a source as the walk found it: its
// bytes, or the reason it could not be read, and once decoded the
// definition or the reason it is not one.
type file struct {
	path string
	data []byte
	def  *definition.Definition
	err  error
}

// readFiles walks a source's parsers directory and reads every
// definition file. What cannot be read is kept in the list as that
// file's or that directory's problem, in the position the walk found
// it, so that the problems come out in the order of the walk.
func readFiles(src Source) ([]*file, error) {
	var files []*file
	count := 0
	err := fs.WalkDir(src.FS, ParsersDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory jz may not read is that directory's problem,
			// like a file that does not parse: it is named, and what is
			// beside it still loads.
			files = append(files, &file{path: p, err: err})
			return nil //nolint:nilerr // recorded so the rest of the registry still loads
		}
		if d.IsDir() {
			// The fixtures beside a definition hold no definition of
			// their own, and there are ten of them for every one: the
			// official registry walks 5,600 files to find 596. The
			// directory is skipped only where the layout puts it,
			// parsers/<command>/<variant>/testdata, so a variant that
			// happens to be named that is still walked.
			if d.Name() == TestdataDir && strings.Count(p, "/") == 3 {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != DefinitionFile {
			return nil
		}
		count++
		if count > MaxDefinitions {
			return &LoadError{Source: src.Name, Path: p, Err: fmt.Errorf("more than %d definitions", MaxDefinitions)}
		}
		// One file jz cannot read, for being too large or for any other
		// reason, is that file's problem in the same way.
		data, err := ReadBounded(src.FS, p, definition.MaxDefinitionSize)
		files = append(files, &file{path: p, data: data, err: err})
		return nil
	})
	return files, err
}

// decodeAll decodes the files that were read, as many at a time as
// there are processors to do it on. A registry of a few files is decoded
// one by one: it is done sooner than the goroutines would be started.
func decodeAll(files []*file, source string) {
	decode := func(f *file) {
		if f.err != nil {
			return
		}
		def, err := definition.Load(f.data, source+":"+f.path)
		if err == nil {
			err = checkLayout(f.path, def)
		}
		f.def, f.err, f.data = def, err, nil
	}
	workers := min(runtime.GOMAXPROCS(0), len(files)/filesPerWorker)
	if workers <= 1 {
		for _, f := range files {
			decode(f)
		}
		return
	}
	var (
		wg   sync.WaitGroup
		next atomic.Int64
	)
	for range workers {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= len(files) {
					return
				}
				decode(files[i])
			}
		})
	}
	wg.Wait()
}

// parseDisable turns a manifest's disable list into rules, rejecting an
// entry that is neither "command" nor "command/variant".
func parseDisable(source string, entries []string) ([]disableRule, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	rules := make([]disableRule, 0, len(entries))
	for i, e := range entries {
		if !disableRe.MatchString(e) {
			return nil, &LoadError{
				Source: source,
				Path:   ManifestFile,
				Err:    fmt.Errorf("disable[%d]: %q must be a command or a command/variant", i, e),
			}
		}
		command, variant, _ := strings.Cut(e, "/")
		rules = append(rules, disableRule{source: source, text: e, command: command, variant: variant})
	}
	return rules, nil
}

// disable reports whether a preferred registry switched this definition
// off. A disabled definition is not loaded at all, so it is absent from
// every lookup rather than being hidden from some of them.
func (r *Registry) disable(def *definition.Definition) bool {
	hit := false
	for i := range r.rules {
		if r.rules[i].covers(def) {
			r.rules[i].matched = true
			hit = true
		}
	}
	return hit
}

// Disabled returns how many definitions of a source a preferred registry
// switched off.
func (r *Registry) Disabled(source string) int {
	return r.disabled[source]
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

// Variants returns the entries for a command sorted by variant name. The
// slice is the caller's; the entries in it are the registry's.
func (r *Registry) Variants(command string) []*Entry {
	return slices.Clone(r.byCommand[command])
}

// Commands returns the known command names sorted. An alias is not one:
// it answers to Variants and Lookup, and the command it belongs to
// reports it.
func (r *Registry) Commands() []string {
	return slices.Clone(r.names)
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
	slices.Sort(out)
	return out
}

// Entries returns every entry sorted by id. The slice is the caller's;
// the entries in it are the registry's.
func (r *Registry) Entries() []*Entry {
	return slices.Clone(r.all)
}

// Len returns the number of distinct definitions.
func (r *Registry) Len() int {
	return len(r.entries)
}

// ReadBounded reads a file of at most limit bytes. A file over the limit
// is an error that names it, and no more than limit+1 bytes of it are
// ever read: a registry cannot make jz read a file without bound.
func ReadBounded(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// A file whose length is known is read into one buffer of that
	// length rather than into one that grows by doubling, which is
	// several allocations for a definition of a few kilobytes and is
	// paid for every file of the registry every time jz starts. The
	// length is only a starting size: a file that grew after it was
	// asked for is still read to its end, and what was read is held to
	// the limit either way.
	size := int64(512)
	if fi, serr := f.Stat(); serr == nil && fi.Mode().IsRegular() && fi.Size() >= 0 {
		if fi.Size() > limit {
			return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
		}
		size = fi.Size()
	}
	data := make([]byte, 0, size+1)
	for int64(len(data)) <= limit {
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
		n, rerr := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s: %w", name, rerr)
		}
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return data, nil
}

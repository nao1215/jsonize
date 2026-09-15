package conformance

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// Fixture is one captured output together with the definition it belongs
// to.
type Fixture struct {
	Entry *registry.Entry
	Case  Case
}

// Path returns the fixture path inside its source filesystem.
func (f Fixture) Path() string {
	return path.Join(f.Case.Dir, f.Case.Name+".txt")
}

// Decoy is a text that no definition may read: it belongs to a format the
// registry does not describe.
type Decoy struct {
	// Name identifies the text in diagnostics.
	Name string
	// Input is the text itself.
	Input []byte
}

// MaxDecoyFiles bounds a decoy directory so that naming a large tree
// cannot make the caller read all of it.
const MaxDecoyFiles = 1000

// ReadDecoys reads every regular file under dir, recursively, and names
// each one by its path relative to dir.
func ReadDecoys(dir string) ([]Decoy, error) {
	var out []Decoy
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if len(out) >= MaxDecoyFiles {
			return fmt.Errorf("more than %d files under %s", MaxDecoyFiles, dir)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			rel = p
		}
		// A decoy is an input, and is bounded the way one is before it is
		// read rather than after.
		data, err := registry.ReadBounded(os.DirFS(dir), filepath.ToSlash(rel), engine.DefaultMaxInputSize)
		if err != nil {
			return err
		}
		out = append(out, Decoy{Name: filepath.ToSlash(rel), Input: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// An empty directory would otherwise report a clean run for a check
	// that examined nothing.
	if len(out) == 0 {
		return nil, fmt.Errorf("no files under %s", dir)
	}
	return out, nil
}

// Fixtures loads the fixtures of every entry whose source is among the
// given ones. The second result carries the entries whose testdata
// directory could not be read.
func Fixtures(reg *registry.Registry, sources []registry.Source) ([]Fixture, []Result) {
	byName := map[string]fs.FS{}
	for _, s := range sources {
		if s.FS != nil {
			byName[s.Name] = s.FS
		}
	}
	var (
		out      []Fixture
		problems []Result
	)
	for _, e := range reg.Entries() {
		fsys, ok := byName[e.Source]
		if !ok {
			continue
		}
		cases, err := Cases(fsys, e)
		if err != nil {
			problems = append(problems, Result{Definition: e.Def.ID(), Source: e.Source, Err: err})
			continue
		}
		for _, c := range cases {
			out = append(out, Fixture{Entry: e, Case: c})
		}
	}
	return out, problems
}

// reader is one definition with every name it answers to, its command
// first. An alias is a second way into the same definition, so it is a
// second way for a signature to be bypassed and is checked as well.
type reader struct {
	entry *registry.Entry
	names []string
}

// readersOf lists every definition that makes a claim about its text,
// with the names it can be reached by.
func readersOf(reg *registry.Registry) []reader {
	var out []reader
	for _, e := range reg.Entries() {
		if selector.ShapeOnly(e.Def) {
			// A definition that describes a shape reads anything of that
			// shape, which is what it is for. There is no claim here to
			// check, and checking it would report every fixture in the
			// registry against `table/whitespace`.
			continue
		}
		names := []string{e.Def.Command}
		for _, alias := range e.Def.AliasNames() {
			if alias != e.Def.Command {
				names = append(names, alias)
			}
		}
		out = append(out, reader{entry: e, names: names})
	}
	return out
}

// readingNames returns the names by which the definition accepts input,
// skipping any name skip says cannot reach it, with the result the first
// of them gave. Every name leads to the same definition and the same
// parse, so one result stands for all of them.
func readingNames(reg *registry.Registry, r reader, input []byte, opts Options, skip func(name string) bool) (Result, []string) {
	var (
		res   Result
		names []string
	)
	for _, name := range r.names {
		if skip != nil && skip(name) {
			continue
		}
		got, ok := reads(reg, r.entry, name, input, opts)
		if !ok {
			continue
		}
		if names == nil {
			res = got
		}
		names = append(names, name)
	}
	return res, names
}

// Exclusivity checks the whole registry against itself: every definition
// is named explicitly on every fixture of every other definition, and
// must refuse it. Naming a definition is a claim about the text, not a
// way past its signature, so a definition that reads a neighbour's output
// is producing confident JSON from the wrong parser, which is the failure
// jz exists to prevent.
//
// target decides which sources are under test. A pair is checked when
// either the definition or the fixture comes from a source target
// accepts, which is what lets a third party check their own definitions
// against the official fixtures without checking the official registry
// against itself.
//
// Fixtures that declare expect_error are left out: such a text is one its
// own definition refuses, and is usually another definition's valid
// output, so another definition reading it is right.
func Exclusivity(reg *registry.Registry, fixtures []Fixture, target func(source string) bool, opts Options) []Result {
	readers := readersOf(reg)
	work := make([]Fixture, 0, len(fixtures))
	for _, f := range fixtures {
		if f.Case.Meta.ExpectError == "" {
			work = append(work, f)
		}
	}
	per := make([][]Result, len(work))
	each(len(work), opts.Parallel, func(i int) {
		per[i] = crossFixture(reg, work[i], readers, target, opts)
	})
	var out []Result
	for _, rs := range per {
		out = append(out, rs...)
	}
	return out
}

// Decoys checks that text belonging to no registered format is refused
// both by automatic detection and by every definition named explicitly.
func Decoys(reg *registry.Registry, decoys []Decoy, opts Options) []Result {
	readers := readersOf(reg)
	per := make([][]Result, len(decoys))
	each(len(decoys), opts.Parallel, func(i int) {
		d := decoys[i]
		var out []Result
		// A stream that commits on the leading lines must refuse the decoy
		// the way the whole text does, rather than choose before the line
		// that gives it away has come.
		if err := settlesAsWhole(reg, selector.Context{}, d.Input); err != nil {
			out = append(out, Result{Definition: "(stream)", Case: d.Name, Path: d.Name, Err: fmt.Errorf("automatic detection of the decoy %s: %w", d.Name, err)})
		}
		if sel, err := selector.Select(reg, selector.Context{Input: d.Input}); err == nil {
			out = append(out, Result{
				Definition: sel.Entry.Def.ID(),
				Case:       d.Name,
				Path:       d.Name,
				Source:     sel.Entry.Source,
				Err:        fmt.Errorf("automatic detection read the decoy %s as %s", d.Name, sel.Entry.Def.ID()),
			})
		}
		for _, r := range readers {
			res, names := readingNames(reg, r, d.Input, opts, nil)
			if names == nil {
				continue
			}
			res.Case, res.Path = d.Name, d.Name
			label := readersLabel(r.entry.Def.ID(), r.entry.Def.Command, names)
			// Which half let it through decides what to sharpen: a
			// signature that says too little, or a parser that takes
			// any word where the format has a vocabulary.
			if res.parsed {
				res.Err = fmt.Errorf("%s read the decoy %s", label, d.Name)
			} else {
				res.Err = fmt.Errorf("%s accepted the decoy %s and then failed to parse it", label, d.Name)
			}
			out = append(out, res)
		}
		per[i] = out
	})
	var out []Result
	for _, rs := range per {
		out = append(out, rs...)
	}
	return out
}

func crossFixture(reg *registry.Registry, f Fixture, readers []reader, target func(string) bool, opts Options) []Result {
	owner := f.Entry.Def.ID()
	ownerUnderTest := target(f.Entry.Source)
	var out []Result
	for _, r := range readers {
		if r.entry.Def.ID() == owner {
			continue
		}
		if !ownerUnderTest && !target(r.entry.Source) {
			continue
		}
		res, names := readingNames(reg, r, f.Case.Input, opts, func(name string) bool {
			return unreachableFor(reg, r.entry, name, f)
		})
		if names == nil {
			continue
		}
		res.Case, res.Path = f.Case.Name, f.Path()
		label := readersLabel(r.entry.Def.ID(), r.entry.Def.Command, names)
		if res.parsed {
			res.Err = fmt.Errorf("%s read %s, a fixture of %s", label, res.Path, owner)
		} else {
			res.Err = fmt.Errorf("%s accepted %s, a fixture of %s, and then failed to parse it", label, res.Path, owner)
		}
		out = append(out, res)
	}
	return out
}

// unreachableFor reports a definition that could not be given this text
// however the caller asked. A definition jz will not claim on its own is
// reached by naming it or by `jz run`; when the fixture records the
// command line it came from and the definition's own argument filter
// refuses that command line, neither path leads here. `ls -lG` and
// `ls -lg` print the same text and differ in whether the one name column
// is the owner or the group, and the arguments are the only thing that
// says which; holding each against the other's output would be asking a
// rule about the text for something the text does not carry.
func unreachableFor(reg *registry.Registry, e *registry.Entry, name string, f Fixture) bool {
	// The arguments a fixture records are the ones its own command takes,
	// so they say nothing about a definition for another command.
	if e.Def.Command != f.Entry.Def.Command {
		return false
	}
	if f.Case.Meta.Args == nil || !selector.ExplicitOnly(e.Def) {
		return false
	}
	ctx := selector.Context{
		Parser:  name,
		Variant: e.Def.Variant,
		OS:      f.Case.Meta.OS,
		Args:    f.Case.Meta.Args,
		Input:   f.Case.Input,
	}
	sel, err := selector.Select(reg, ctx)
	return err != nil || sel.Entry.Def.ID() != e.Def.ID()
}

// reads reports whether the definition, named explicitly, accepts the
// text. The second result is false when the signature refused it, which
// is the outcome every pair is supposed to have.
func reads(reg *registry.Registry, e *registry.Entry, name string, input []byte, opts Options) (Result, bool) {
	ctx := selector.Context{Parser: name, Variant: e.Def.Variant, Input: input}
	sel, err := selector.Select(reg, ctx)
	if err != nil || sel.Entry.Def.ID() != e.Def.ID() {
		return Result{}, false
	}
	res := Result{Definition: e.Def.ID(), Source: e.Source}
	if _, err := engine.Parse(e.Def, input, opts.Engine); err == nil {
		res.parsed = true
	}
	return res, true
}

// readersLabel spells out which names reached the definition, so that an
// alias letting text in is as visible as the command name doing it, and a
// definition with many names that reads one text is one report.
func readersLabel(id, command string, names []string) string {
	if len(names) > 0 && names[0] == command {
		if len(names) == 1 {
			return id
		}
		return fmt.Sprintf("%s (also named %s)", id, strings.Join(names[1:], ", "))
	}
	return fmt.Sprintf("%s (named %s)", id, strings.Join(names, ", "))
}

// each runs fn for every index below n, spreading the indexes over
// workers. The results stay in index order because fn writes to a slot of
// its own.
func each(n, parallel int, fn func(i int)) {
	if n == 0 {
		return
	}
	if parallel <= 0 {
		parallel = runtime.NumCPU()
	}
	if parallel > n {
		parallel = n
	}
	var (
		next atomic.Int64
		wg   sync.WaitGroup
	)
	for w := 0; w < parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

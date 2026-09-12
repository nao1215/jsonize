package conformance

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
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

// reader is one definition under one of the names it answers to. An alias
// is a second way into the same definition, so it is a second way for a
// signature to be bypassed and is checked as well.
type reader struct {
	entry *registry.Entry
	name  string
}

// readersOf lists every way a definition can be named.
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
		out = append(out, reader{entry: e, name: e.Def.Command})
		for _, alias := range e.Def.AliasNames() {
			out = append(out, reader{entry: e, name: alias})
		}
	}
	return out
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
			if res, ok := reads(reg, r, d.Input, opts); ok {
				res.Case, res.Path = d.Name, d.Name
				// Which half let it through decides what to sharpen: a
				// signature that says too little, or a parser that takes
				// any word where the format has a vocabulary.
				if res.parsed {
					res.Err = fmt.Errorf("%s read the decoy %s", namedAs(r), d.Name)
				} else {
					res.Err = fmt.Errorf("%s accepted the decoy %s and then failed to parse it", namedAs(r), d.Name)
				}
				out = append(out, res)
			}
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
		if unreachableFor(reg, r, f, opts) {
			continue
		}
		res, ok := reads(reg, r, f.Case.Input, opts)
		if !ok {
			continue
		}
		res.Case, res.Path = f.Case.Name, f.Path()
		if res.parsed {
			res.Err = fmt.Errorf("%s read %s, a fixture of %s", namedAs(r), res.Path, owner)
		} else {
			res.Err = fmt.Errorf("%s accepted %s, a fixture of %s, and then failed to parse it", namedAs(r), res.Path, owner)
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
func unreachableFor(reg *registry.Registry, r reader, f Fixture, opts Options) bool {
	// The arguments a fixture records are the ones its own command takes,
	// so they say nothing about a definition for another command.
	if r.entry.Def.Command != f.Entry.Def.Command {
		return false
	}
	if f.Case.Meta.Args == nil || !selector.ExplicitOnly(r.entry.Def) {
		return false
	}
	ctx := selector.Context{
		Parser:  r.name,
		Variant: r.entry.Def.Variant,
		OS:      f.Case.Meta.OS,
		Args:    f.Case.Meta.Args,
		Input:   f.Case.Input,
	}
	sel, err := selector.Select(reg, ctx)
	return err != nil || sel.Entry.Def.ID() != r.entry.Def.ID()
}

// reads reports whether the definition, named explicitly, accepts the
// text. The second result is false when the signature refused it, which
// is the outcome every pair is supposed to have.
func reads(reg *registry.Registry, r reader, input []byte, opts Options) (Result, bool) {
	ctx := selector.Context{Parser: r.name, Variant: r.entry.Def.Variant, Input: input}
	sel, err := selector.Select(reg, ctx)
	if err != nil || sel.Entry.Def.ID() != r.entry.Def.ID() {
		return Result{}, false
	}
	res := Result{Definition: r.entry.Def.ID(), Source: r.entry.Source}
	if _, err := engine.Parse(r.entry.Def, input, opts.Engine); err == nil {
		res.parsed = true
	}
	return res, true
}

// namedAs spells out which name reached the definition, so that an alias
// letting text in is as visible as a command name doing it.
func namedAs(r reader) string {
	if r.name == r.entry.Def.Command {
		return r.entry.Def.ID()
	}
	return fmt.Sprintf("%s (named %s)", r.entry.Def.ID(), r.name)
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

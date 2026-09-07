package conformance

import (
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/selector"
)

// Fixture is one captured output together with the definition it belongs
// to.
type Fixture struct {
	Entry *registry.Entry
	Case  registry.Case
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
		cases, err := registry.Cases(fsys, e)
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
	var readers []reader
	for _, e := range reg.Entries() {
		readers = append(readers, reader{entry: e, name: e.Def.Command})
		for _, alias := range e.Def.AliasNames() {
			readers = append(readers, reader{entry: e, name: alias})
		}
	}
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
	var readers []reader
	for _, e := range reg.Entries() {
		readers = append(readers, reader{entry: e, name: e.Def.Command})
		for _, alias := range e.Def.AliasNames() {
			readers = append(readers, reader{entry: e, name: alias})
		}
	}
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
				res.Err = fmt.Errorf("%s read the decoy %s", namedAs(r), d.Name)
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

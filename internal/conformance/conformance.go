// Package conformance checks a registry against itself.
//
// Two things are checked. Every <case>.txt fixture stored next to a
// definition is parsed with that definition and compared with
// <case>.json, and is fed through selection to prove that it picks its
// own definition unambiguously. Then every definition is named
// explicitly on every other definition's fixtures and must refuse them,
// which is what keeps a signature from quietly widening until it reads a
// neighbouring format.
//
// The same checks back `go test ./registry` for the embedded registry and
// `jz test` for any other one, so parser authors need no Go toolchain.
package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/google/go-cmp/cmp"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/selector"
)

// Result is the outcome of one case.
type Result struct {
	Definition string
	Case       string
	// Path is the fixture path inside the source FS.
	Path string
	// Source names the registry the checked definition came from.
	Source string
	// Actual is the JSON produced (pretty printed) when parsing succeeded.
	Actual []byte
	// Err is nil when the case passed.
	Err error

	// parsed records, for an exclusivity failure, whether the definition
	// went on to produce JSON or only got past the signature.
	parsed bool
}

// Options configures a run.
type Options struct {
	// Update reports the produced JSON in Result.Actual without failing on
	// golden mismatches; callers write it back to disk.
	Update bool
	// Engine options for parsing.
	Engine engine.Options
	// Parallel bounds the goroutines the exclusivity check uses
	// (0 = one per CPU).
	Parallel int
	// SkipExclusivity leaves out the definition/fixture cross product,
	// which is the expensive half of a run.
	SkipExclusivity bool
	// Decoys are texts no definition may read.
	Decoys []Decoy
}

// Run checks every case of every entry that belongs to the given source
// FS. Entries from other sources are skipped.
func Run(reg *registry.Registry, fsys fs.FS, sourceName string, opts Options) []Result {
	var results []Result
	for _, e := range reg.Entries() {
		if e.Source != sourceName {
			continue
		}
		cases, err := registry.Cases(fsys, e)
		if err != nil {
			results = append(results, Result{Definition: e.Def.ID(), Source: e.Source, Err: err})
			continue
		}
		if len(cases) == 0 {
			results = append(results, Result{Definition: e.Def.ID(), Source: e.Source, Err: fmt.Errorf("no testdata cases; add at least one <case>.txt and <case>.json under %s", e.TestdataPath())})
			continue
		}
		for _, c := range cases {
			results = append(results, runCase(reg, e, c, opts))
		}
	}
	return results
}

// Check is the whole contract of a registry in one call: the definitions
// of the target sources are validated by running their golden cases, and
// the cross product of definitions and fixtures proves that a definition
// reads its own format and nothing else.
//
// sources must list every registry that was loaded, because a third
// party's definition has to be checked against the official fixtures as
// well as its own. targets names the sources under test; a pair whose
// definition and fixture both come from elsewhere is skipped.
func Check(reg *registry.Registry, sources []registry.Source, targets []string, opts Options) []Result {
	underTest := map[string]bool{}
	for _, t := range targets {
		underTest[t] = true
	}
	var results []Result
	for _, s := range sources {
		if underTest[s.Name] && s.FS != nil {
			results = append(results, Run(reg, s.FS, s.Name, opts)...)
		}
	}
	if opts.SkipExclusivity {
		return results
	}
	fixtures, problems := Fixtures(reg, sources)
	results = append(results, problems...)
	results = append(results, Exclusivity(reg, fixtures, func(src string) bool { return underTest[src] }, opts)...)
	return append(results, Decoys(reg, opts.Decoys, opts)...)
}

func runCase(reg *registry.Registry, e *registry.Entry, c registry.Case, opts Options) Result {
	res := Result{Definition: e.Def.ID(), Case: c.Name, Path: path.Join(c.Dir, c.Name+".txt"), Source: e.Source}
	// Selection is part of the contract, not just parsing: a fixture has
	// to identify its own definition the way a user's input would.
	if c.Meta.ExpectError == "" {
		explicit := selector.ExplicitOnly(e.Def)
		if c.Meta.AutoDetects() && !explicit {
			// What `COMMAND | jz` does: no parser, no OS, no arguments.
			if err := selects(reg, e, selector.Context{Input: c.Input}); err != nil {
				res.Err = fmt.Errorf("automatic detection: %w", err)
				return res
			}
		}
		if explicit {
			// A definition that jz will not claim on its own must still be
			// reachable by naming it, and its signature must accept the
			// fixture rather than being skipped.
			if err := selects(reg, e, selector.Context{Parser: e.Def.Command, Input: c.Input}); err != nil {
				res.Err = fmt.Errorf("selection with --parser %s: %w", e.Def.Command, err)
				return res
			}
			// Automatic detection may well land on another definition
			// whose format this text also fits; what it must not do is
			// choose this one.
			if sel, err := selector.Select(reg, selector.Context{Input: c.Input}); err == nil && sel.Entry.Def.ID() == e.Def.ID() {
				res.Err = fmt.Errorf("%s declares auto_detect: false but automatic detection still chose it", e.Def.ID())
				return res
			}
		}
		// What `jz run` does when the metadata records the arguments.
		if c.Meta.OS != "" || c.Meta.Args != nil {
			ctx := selector.Context{Parser: e.Def.Command, OS: c.Meta.OS, Args: c.Meta.Args, Input: c.Input}
			if err := selects(reg, e, ctx); err != nil {
				res.Err = fmt.Errorf("selection with parser %s: %w", e.Def.Command, err)
				return res
			}
		}
	}
	if c.Meta.ExpectError != "" {
		// A definition refuses text either by not describing it or by
		// failing to parse it, and both are things a case may want to
		// pin. Naming the definition is what a caller does when they
		// believe the text is theirs, so that is the path checked.
		ctx := selector.Context{Parser: e.Def.Command, Variant: e.Def.Variant, OS: c.Meta.OS, Args: c.Meta.Args, Input: c.Input}
		_, err := selector.Select(reg, ctx)
		if err == nil {
			_, err = engine.Parse(e.Def, c.Input, opts.Engine)
		}
		switch {
		case err == nil:
			res.Err = fmt.Errorf("expected an error containing %q but the input was read", c.Meta.ExpectError)
		case !strings.Contains(err.Error(), c.Meta.ExpectError):
			res.Err = fmt.Errorf("expected an error containing %q, got: %w", c.Meta.ExpectError, err)
		}
		return res
	}
	got, err := engine.Parse(e.Def, c.Input, opts.Engine)
	if err != nil {
		res.Err = err
		return res
	}
	var buf bytes.Buffer
	if err := jsonutil.Encode(&buf, got, true); err != nil {
		res.Err = fmt.Errorf("encoding result: %w", err)
		return res
	}
	res.Actual = buf.Bytes()
	if opts.Update {
		return res
	}
	if c.Expected == nil {
		res.Err = fmt.Errorf("fixture %s.txt has no %s.json; add the expected JSON (or expect_error in %s.yaml)", c.Name, c.Name, c.Name)
		return res
	}
	if diff, err := Diff(c.Expected, res.Actual); err != nil {
		res.Err = err
	} else if diff != "" {
		res.Err = fmt.Errorf("output differs from %s.json (-want +got):\n%s", c.Name, diff)
	}
	return res
}

// selects reports whether ctx picks exactly the expected definition.
func selects(reg *registry.Registry, want *registry.Entry, ctx selector.Context) error {
	sel, err := selector.Select(reg, ctx)
	if err != nil {
		return err
	}
	if sel.Entry.Def.ID() != want.Def.ID() {
		return fmt.Errorf("picked %s instead of %s; tighten detect.signature", sel.Entry.Def.ID(), want.Def.ID())
	}
	return nil
}

// Diff compares two JSON documents structurally (key order is ignored so
// that hand-written golden files need not match the encoder byte for
// byte) and returns a human readable diff when they differ.
func Diff(want, got []byte) (string, error) {
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		return "", fmt.Errorf("expected JSON is invalid: %w", err)
	}
	if err := json.Unmarshal(got, &g); err != nil {
		return "", fmt.Errorf("produced JSON is invalid: %w", err)
	}
	return cmp.Diff(w, g), nil
}

// Summary counts passed and failed results.
func Summary(results []Result) (passed, failed int) {
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			passed++
		}
	}
	return passed, failed
}

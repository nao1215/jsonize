// Package conformance checks a registry against itself.
//
// Three things are checked. Every <case>.txt fixture stored next to a
// definition is parsed with that definition and compared with
// <case>.json, and is fed through selection to prove that it picks its
// own definition unambiguously, the same with the changes in sameText
// that leave the text as it was. Every fixture is then tampered with (see
// Tampering) to prove the definition reads all of what it is given.
// Last, every definition is named explicitly on every other definition's
// fixtures and must refuse them, which is what keeps a signature from
// quietly widening until it reads a neighbouring format.
//
// The same checks back `go test ./registry` for the embedded registry and
// `jz test` for any other one, so parser authors need no Go toolchain.
package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"strings"

	"github.com/nao1215/jsonize/internal/schema"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
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
	// Parallel bounds the goroutines the checks use (0 = one per CPU).
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
	type job struct {
		e *registry.Entry
		c Case
	}
	var (
		results []Result
		jobs    []job
		at      []int
	)
	for _, e := range reg.Entries() {
		if e.Source != sourceName {
			continue
		}
		cases, err := Cases(fsys, e)
		if err != nil {
			results = append(results, Result{Definition: e.Def.ID(), Source: e.Source, Err: err})
			continue
		}
		if len(cases) == 0 {
			results = append(results, Result{Definition: e.Def.ID(), Source: e.Source, Err: fmt.Errorf("no testdata cases; add at least one <case>.txt and <case>.json under %s", testdataPath(e))})
			continue
		}
		for _, c := range cases {
			jobs = append(jobs, job{e, c})
			at = append(at, len(results))
			results = append(results, Result{})
		}
	}
	// Each case is checked on its own, so the order of the results is
	// the order of the registry however the work is spread.
	each(len(jobs), opts.Parallel, func(i int) {
		results[at[i]] = runCase(reg, jobs[i].e, jobs[i].c, opts)
	})
	return results
}

// Check is the whole contract of a registry in one call: the definitions
// of the target sources are validated by running their golden cases,
// every fixture is tampered with to prove the definition reads all of its
// input, and the cross product of definitions and fixtures proves that a
// definition reads its own format and nothing else.
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
	fixtures, problems := Fixtures(reg, sources)
	for _, p := range problems {
		// Run has already reported the cases of a source under test that
		// could not be loaded; saying it twice is noise.
		if !underTest[p.Source] {
			results = append(results, p)
		}
	}
	target := func(src string) bool { return underTest[src] }
	results = append(results, Tampering(reg, fixtures, target, opts)...)
	if opts.SkipExclusivity {
		return results
	}
	results = append(results, Exclusivity(reg, fixtures, target, opts)...)
	return append(results, Decoys(reg, opts.Decoys, opts)...)
}

func runCase(reg *registry.Registry, e *registry.Entry, c Case, opts Options) Result {
	res := Result{Definition: e.Def.ID(), Case: c.Name, Path: path.Join(c.Dir, c.Name+".txt"), Source: e.Source}
	// Selection is part of the contract, not just parsing: a fixture has
	// to identify its own definition the way a user's input would.
	if c.Meta.ExpectError == "" {
		if err := selectsOwn(reg, e, c, c.Input); err != nil {
			res.Err = err
			return res
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
	if err := streamMatches(e.Def, c.Input, got, opts); err != nil {
		res.Err = err
		return res
	}
	if err := sameAnswer(reg, e, c, got, opts); err != nil {
		res.Err = err
		return res
	}
	var buf bytes.Buffer
	if err := jsonutil.Encode(&buf, got, true); err != nil {
		res.Err = fmt.Errorf("encoding result: %w", err)
		return res
	}
	res.Actual = buf.Bytes()
	// The output contract is derived from the definition, and every
	// fixture is a document the definition produced, so each has to fit
	// the schema: a key it does not name, a null where it promises a
	// value, an integer where it says string. A mismatch is the generator
	// and the engine disagreeing about what the definition produces.
	if errs := schema.Validate(schema.Generate(e.Def, 1), res.Actual); len(errs) > 0 {
		res.Err = fmt.Errorf("the output does not fit the schema derived from the definition: %w", errors.Join(errs...))
		return res
	}
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

// selectsOwn checks that input picks the case's own definition on every
// path its metadata says a user reaches it by.
func selectsOwn(reg *registry.Registry, e *registry.Entry, c Case, input []byte) error {
	explicit := selector.ExplicitOnly(e.Def)
	if c.Meta.AutoDetects() && !explicit {
		// What `COMMAND | jz` does: no parser, no OS, no arguments.
		if err := selects(reg, e, selector.Context{Input: input}); err != nil {
			return fmt.Errorf("automatic detection: %w", err)
		}
	}
	if explicit {
		c.Input = input
		if err := namedSelects(reg, e, c); err != nil {
			return err
		}
	}
	// What `jz run` does when the metadata records the arguments.
	if c.Meta.OS != "" || c.Meta.Args != nil {
		ctx := selector.Context{Parser: e.Def.Command, OS: c.Meta.OS, Args: c.Meta.Args, Input: input}
		if err := selects(reg, e, ctx); err != nil {
			return fmt.Errorf("selection with parser %s: %w", e.Def.Command, err)
		}
	}
	return nil
}

// sameText are changes to a fixture that leave the text what it was: a
// byte order mark, CRLF line endings, no line break after the last line,
// a blank line after it or before the first. Each has to leave the answer
// as it was, both the definition chosen and the JSON. The line endings
// mean nothing to a format whose records end with NUL, where a newline
// belongs to a value.
var sameText = []struct {
	name  string
	lines bool
	apply func([]byte) []byte
}{
	{"a byte order mark", false, func(b []byte) []byte { return append([]byte{0xEF, 0xBB, 0xBF}, b...) }},
	// A checkout with CRLF endings already has them, so the text is
	// brought to LF first rather than given a second carriage return.
	{"CRLF line endings", true, func(b []byte) []byte {
		return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
	}},
	{"no line break at the end", true, func(b []byte) []byte { return bytes.TrimRight(b, "\n") }},
	{"a blank line at the end", true, func(b []byte) []byte { return append(bytes.Clone(b), '\n') }},
	{"a blank line at the start", true, func(b []byte) []byte { return append([]byte{'\n'}, b...) }},
}

// sameAnswer checks the fixture under each change in sameText against
// the answer it gives as it is.
func sameAnswer(reg *registry.Registry, e *registry.Entry, c Case, want any, opts Options) error {
	var w bytes.Buffer
	if err := jsonutil.Encode(&w, want, false); err != nil {
		return err
	}
	for _, t := range sameText {
		if t.lines && e.Def.Input.Separator() != '\n' {
			continue
		}
		input := t.apply(c.Input)
		if err := selectsOwn(reg, e, c, input); err != nil {
			return fmt.Errorf("%s changes the answer: %w", t.name, err)
		}
		got, err := engine.Parse(e.Def, input, opts.Engine)
		if err != nil {
			return fmt.Errorf("%s changes the answer: %w", t.name, err)
		}
		var g bytes.Buffer
		if err := jsonutil.Encode(&g, got, false); err != nil {
			return err
		}
		if g.String() != w.String() {
			return fmt.Errorf("%s changes the answer (-as captured +changed):\n%s", t.name, lineDiff(w.Bytes(), g.Bytes()))
		}
	}
	return nil
}

// namedSelects checks the paths a definition jz will not claim on its
// own is reached by. Naming it must work, and its signature must accept
// the fixture rather than being skipped; automatic detection must not
// choose it.
//
// A shape has no signature and so cannot be told from its siblings by
// text: table/whitespace and table/aligned read the same lines two ways.
// Naming the variant is what picks one, and that is what is required of
// those.
func namedSelects(reg *registry.Registry, e *registry.Entry, c Case) error {
	ctx := selector.Context{Parser: e.Def.Command, Input: c.Input}
	named := "--parser " + e.Def.Command
	if selector.ShapeOnly(e.Def) {
		ctx.Variant = e.Def.Variant
		named += " --variant " + e.Def.Variant
	}
	if err := selects(reg, e, ctx); err != nil {
		return fmt.Errorf("selection with %s: %w", named, err)
	}
	// Automatic detection may well land on another definition whose
	// format this text also fits; what it must not do is choose this one.
	if sel, err := selector.Select(reg, selector.Context{Input: c.Input}); err == nil && sel.Entry.Def.ID() == e.Def.ID() {
		return fmt.Errorf("%s declares auto_detect: false but automatic detection still chose it", e.Def.ID())
	}
	return nil
}

// streamMatches checks that reading the fixture one record at a time
// produces the same records as reading it whole. --stream changes when a
// record is written, never what it says, so a definition whose two
// readings disagree would answer differently depending on a flag.
func streamMatches(def *definition.Definition, input []byte, batch any, opts Options) error {
	if def.Parse.Type == definition.TypeComposite {
		return compositeStreamMatches(def, input, batch, opts)
	}
	list, ok := batch.([]any)
	if !ok || !def.Parse.YieldsArray() {
		return nil
	}
	var want bytes.Buffer
	for _, v := range list {
		if err := jsonutil.Encode(&want, v, false); err != nil {
			return err
		}
	}
	var got bytes.Buffer
	// nil onError: a fixture that reads whole has to read the same way
	// as a stream, so a record the stream skips is a difference to
	// report rather than one to absorb.
	err := engine.Stream(def, bytes.NewReader(input), opts.Engine, func(v any) error {
		return jsonutil.Encode(&got, v, false)
	}, nil)
	if err != nil {
		return fmt.Errorf("reading with --stream: %w", err)
	}
	if want.String() != got.String() {
		return fmt.Errorf("--stream reads this differently than the whole document (-whole +stream):\n%s",
			lineDiff(want.Bytes(), got.Bytes()))
	}
	return nil
}

// compositeStreamMatches checks a composite's stream against its whole
// reading: every document is {"part", "value"}, the values of a part that
// yields a list are its list in order, and a part that yields one value
// has exactly one document holding it.
func compositeStreamMatches(def *definition.Definition, input []byte, batch any, opts Options) error {
	values := map[string][]any{}
	err := engine.Stream(def, bytes.NewReader(input), opts.Engine, func(v any) error {
		doc, ok := v.(*jsonutil.Object)
		if !ok || doc.Len() != 2 {
			return fmt.Errorf("a composite stream wrote %T, not a {part, value} document", v)
		}
		name, _ := doc.Get("part")
		value, _ := doc.Get("value")
		part, _ := name.(string)
		values[part] = append(values[part], value)
		return nil
	}, nil)
	if err != nil {
		return fmt.Errorf("reading with --stream: %w", err)
	}
	rebuilt := jsonutil.NewObject()
	for i := range def.Parse.Parts {
		part := &def.Parse.Parts[i]
		got := values[part.Name]
		delete(values, part.Name)
		if part.Parse.YieldsArray() {
			if got == nil {
				got = []any{}
			}
			rebuilt.Set(part.Name, got)
			continue
		}
		if len(got) != 1 {
			return fmt.Errorf("--stream wrote %d documents for part %q, which is one value", len(got), part.Name)
		}
		rebuilt.Set(part.Name, got[0])
	}
	for name := range values {
		return fmt.Errorf("--stream wrote a document for %q, which is not a part", name)
	}
	var want, got bytes.Buffer
	if err := jsonutil.Encode(&want, batch, true); err != nil {
		return err
	}
	if err := jsonutil.Encode(&got, rebuilt, true); err != nil {
		return err
	}
	if want.String() != got.String() {
		return fmt.Errorf("--stream reads this differently than the whole document (-whole +stream):\n%s", lineDiff(want.Bytes(), got.Bytes()))
	}
	return nil
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
	if reflect.DeepEqual(w, g) {
		return "", nil
	}
	return lineDiff(want, got), nil
}

// lineDiff reports where two pretty-printed JSON documents part company:
// the first line that differs, with a little of what came before it, and
// how many lines each has.
//
// A structural comparison would say more, and go-cmp was what said it
// until a tree fixture made the cost visible: it builds its report as it
// walks, and on a deeply nested value that runs for minutes, which is
// worse than a short answer for a check meant to run on every save. The
// decision above is still structural; only the description is by line.
func lineDiff(want, got []byte) string {
	w := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	g := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	at := len(w)
	for i := range min(len(w), len(g)) {
		if w[i] != g[i] {
			at = i
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "first difference at line %d (want %d lines, got %d)\n", at+1, len(w), len(g))
	const context = 3
	for i := max(0, at-context); i < at; i++ {
		fmt.Fprintf(&b, "  %s\n", w[i])
	}
	fmt.Fprintf(&b, "- %s\n", nth(w, at))
	fmt.Fprintf(&b, "+ %s\n", nth(g, at))
	return b.String()
}

// nth returns one line, or a note that the document ended before it.
func nth(lines []string, i int) string {
	if i >= len(lines) {
		return "(end of document)"
	}
	return lines[i]
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

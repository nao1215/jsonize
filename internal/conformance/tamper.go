package conformance

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// foreignLine is text no format prints. It is what a check puts into a
// fixture to find out whether the definition reads everything it is
// given: a definition either refuses the changed text or reports the line
// somewhere in its result, and the one thing it may not do is succeed
// with the line gone. It is one lower-case word, so that a definition
// that takes it for a table header still reports it under the same
// spelling.
const foreignLine = "jsonizeforeignline7f3a"

// Tampering checks the promise every definition makes by being run by the
// engine: a conversion that succeeds has read all of its input. Golden
// files cannot show that on their own, because they only hold what a
// definition did read; a line it skipped leaves no trace in them.
//
// So each fixture is changed the three ways real input goes wrong, and
// read again with its own definition:
//
//   - a line nothing prints is added at the end, as when two commands'
//     output ends up in one capture;
//   - the same line is put in the middle, as a section this definition
//     has never seen would be;
//   - the fixture is written twice, as when a command is run twice into
//     one file.
//
// The first two must be refused or carry the line into the result. The
// third must be refused or read as more than one copy: a result equal to
// the single one means the second copy was dropped. Nothing here can tell
// a right reading from a wrong one; what it can tell is a reading that
// left text out, which is the failure the engine's accounting exists for.
//
// Each changed text is read the way naming the definition reads it
// (`--parser COMMAND --variant VARIANT`): the signature first, then the
// parse. The signature is part of what a definition promises, and a text
// it refuses is refused.
func Tampering(reg *registry.Registry, fixtures []Fixture, target func(source string) bool, opts Options) []Result {
	var work []Fixture
	for _, f := range fixtures {
		if f.Case.Meta.ExpectError == "" && target(f.Entry.Source) && len(bytes.TrimSpace(f.Case.Input)) > 0 {
			work = append(work, f)
		}
	}
	per := make([][]Result, len(work))
	each(len(work), opts.Parallel, func(i int) {
		per[i] = tamperFixture(reg, work[i], opts)
	})
	var out []Result
	for _, rs := range per {
		out = append(out, rs...)
	}
	return out
}

func tamperFixture(reg *registry.Registry, f Fixture, opts Options) []Result {
	def := f.Entry.Def
	single, acct, err := engine.ParseAccounted(def, f.Case.Input, opts.Engine)
	if err != nil {
		// The golden case reports this one; there is nothing to compare
		// the changed texts against.
		return nil
	}
	sep := string(def.Input.Separator())
	records := strings.SplitAfter(strings.TrimSuffix(string(f.Case.Input), sep), sep)
	withSep := func(s string) string {
		if strings.HasSuffix(s, sep) {
			return s
		}
		return s + sep
	}
	mid := len(records) / 2
	var out []Result
	fail := func(what string, err error) {
		out = append(out, Result{
			Definition: def.ID(),
			Case:       f.Case.Name,
			Path:       f.Path(),
			Source:     f.Entry.Source,
			Err:        fmt.Errorf("%s: %w", what, err),
		})
	}
	for _, t := range []struct {
		what  string
		input string
	}{
		{"a foreign line at the end", withSep(string(f.Case.Input)) + foreignLine + sep},
		{fmt.Sprintf("a foreign line after record %d of %d", mid, len(records)), withSep(strings.Join(records[:mid], "")) + foreignLine + sep + strings.Join(records[mid:], "")},
	} {
		v, err := readNamed(reg, f, []byte(t.input), opts)
		if err != nil {
			continue
		}
		doc, err := encode(v)
		if err != nil {
			fail(t.what, err)
			continue
		}
		if !strings.Contains(doc, foreignLine) {
			fail(t.what, errors.New("the definition read the text and the line is nowhere in the result"))
		}
	}
	if acct.Read == 0 {
		// Everything in the fixture is a line the definition leaves out
		// on purpose (a header over no rows, a banner over nothing), so a
		// second copy of it has nothing in it to lose.
		return out
	}
	list, isList := single.([]any)
	if isList && len(list) == 0 {
		// Two outputs with nothing in them have nothing in them either.
		return out
	}
	twice := withSep(string(f.Case.Input)) + string(f.Case.Input)
	if v, err := readNamed(reg, f, []byte(twice), opts); err == nil {
		a, errA := encode(single)
		b, errB := encode(v)
		// The records of one output, twice: anything else read a line of
		// one copy as something it is not, such as the second copy's
		// header as a row of the first.
		var want string
		if isList && errA == nil {
			want, errA = encode(append(append([]any{}, list...), list...))
		}
		headerRow := isList && errA == nil && errB == nil && headerIsARow(def)
		switch {
		case headerRow:
			// A header the definition does not take for one printed again
			// is a row of the second copy, between the two sets of records.
			if !recordsAroundOne(v, list) {
				fail("the fixture twice", fmt.Errorf("the definition does not take a repeated header for one, so two copies are the records of one, the header as a row and the records of one again; got %s", firstDifference(want, b)))
			}
		case errA != nil:
			fail("the fixture twice", errA)
		case errB != nil:
			fail("the fixture twice", errB)
		case a == b:
			fail("the fixture twice", errors.New("the definition read two copies and returned what it returns for one"))
		case isList && b != want:
			fail("the fixture twice", fmt.Errorf("the definition read two copies and returned something other than the records of one twice: %s", firstDifference(want, b)))
		}
	}
	return out
}

// headerIsARow reports a definition that reads a header line and does not
// take the same line further down for that header printed again.
func headerIsARow(def *definition.Definition) bool {
	switch def.Parse.Type {
	case definition.TypeTable, definition.TypeCSV:
		return !def.Parse.Header.None && !def.Parse.RepeatedHeader()
	}
	return false
}

// recordsAroundOne reports whether v is list, one more record, and list
// again.
func recordsAroundOne(v any, list []any) bool {
	got, ok := v.([]any)
	if !ok || len(got) != 2*len(list)+1 {
		return false
	}
	whole, err := encode(list)
	if err != nil {
		return false
	}
	first, errFirst := encode(got[:len(list)])
	second, errSecond := encode(got[len(list)+1:])
	return errFirst == nil && errSecond == nil && first == whole && second == whole
}

// firstDifference quotes two encodings from a little before where they
// part.
func firstDifference(want, got string) string {
	want, got = strings.TrimSpace(want), strings.TrimSpace(got)
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	from := max(0, i-40)
	clip := func(s string) string {
		s = s[min(from, len(s)):]
		if len(s) > 120 {
			s = s[:120]
		}
		return s
	}
	return fmt.Sprintf("expected …%s…, got …%s…", clip(want), clip(got))
}

// readNamed reads input with the fixture's definition named explicitly.
func readNamed(reg *registry.Registry, f Fixture, input []byte, opts Options) (any, error) {
	ctx := selector.Context{Parser: f.Entry.Def.Command, Variant: f.Entry.Def.Variant, OS: f.Case.Meta.OS, Args: f.Case.Meta.Args, Input: input}
	if _, err := selector.Select(reg, ctx); err != nil {
		return nil, err
	}
	return engine.Parse(f.Entry.Def, input, opts.Engine)
}

func encode(v any) (string, error) {
	var b bytes.Buffer
	if err := jsonutil.Encode(&b, v, false); err != nil {
		return "", err
	}
	return b.String(), nil
}

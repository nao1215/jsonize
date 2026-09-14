package conformance

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// streamSelectsOwn checks that a stream of the fixture chooses what the
// whole fixture chooses, on every path a user reaches a definition by:
// automatic detection, the command named, the variant named, and jz run
// with the arguments the metadata records.
func streamSelectsOwn(reg *registry.Registry, e *registry.Entry, c Case) error {
	paths := []selector.Context{{}, {Parser: e.Def.Command}, {Parser: e.Def.Command, Variant: e.Def.Variant}}
	if c.Meta.OS != "" || c.Meta.Args != nil {
		paths = append(paths, selector.Context{Parser: e.Def.Command, OS: c.Meta.OS, Args: c.Meta.Args})
	}
	for _, ctx := range paths {
		if err := settlesAsWhole(reg, ctx, c.Input); err != nil {
			return fmt.Errorf("streaming, %s: %w", scopeName(ctx), err)
		}
	}
	return nil
}

// settlesAsWhole checks that a stream, which chooses a definition as soon
// as the leading lines settle the choice (selector.Watch), gives the
// answer the whole text gives. The lines are handed over one at a time,
// the way a command prints them and the way jz reads the head of a
// stream, and the first point at which the watch says the choice cannot
// change is where a stream commits: the definition chosen there, or the
// failure, has to be the one Select gives for the whole text.
func settlesAsWhole(reg *registry.Registry, ctx selector.Context, input []byte) error {
	limit, sep := selector.Window(reg, ctx)
	settled := selector.Watch(reg, ctx)
	lines, text, end := 0, false, 0
	for lines < limit && end < len(input) {
		i := bytes.IndexByte(input[end:], sep)
		if i < 0 {
			// The last line has no end, so the input ends with it: there is
			// nothing a stream would still be waiting for.
			return nil
		}
		line := input[end : end+i+1]
		first := end == 0
		end += i + 1
		if end == len(input) {
			// The whole text has come; a stream reads it whole too.
			return nil
		}
		if sep == '\n' && !text && blankLine(line, first) {
			continue
		}
		text = true
		lines++
		if !settled(input[:end]) {
			continue
		}
		early := ctx
		early.Input = input[:end]
		whole := ctx
		whole.Input = input
		got, errEarly := selector.Select(reg, early)
		want, errWhole := selector.Select(reg, whole)
		return sameChoice(lines, got, errEarly, want, errWhole)
	}
	return nil
}

// sameChoice compares the choice a stream settled on, after the given
// number of lines, with the choice for the whole text: the same
// definition, or a failure both times.
func sameChoice(lines int, got *selector.Result, errEarly error, want *selector.Result, errWhole error) error {
	switch {
	case (errEarly == nil) != (errWhole == nil):
		return fmt.Errorf("a stream settles after %d lines with %v, the whole text gives %v", lines, describe(got, errEarly), describe(want, errWhole))
	case errEarly == nil && got.Entry != want.Entry:
		return fmt.Errorf("a stream settles on %s after %d lines, the whole text chooses %s", got.Entry.Def.ID(), lines, want.Entry.Def.ID())
	}
	return nil
}

// scopeName says how a context names the candidates.
func scopeName(ctx selector.Context) string {
	switch {
	case ctx.Args != nil || ctx.OS != "":
		return ctx.Parser + " run with the recorded system and arguments"
	case ctx.Variant != "":
		return "--parser " + ctx.Parser + " --variant " + ctx.Variant
	case ctx.Parser != "":
		return "--parser " + ctx.Parser
	}
	return "automatic detection"
}

func describe(res *selector.Result, err error) string {
	if err != nil {
		line, _, _ := strings.Cut(err.Error(), "\n")
		return fmt.Sprintf("the error %q", line)
	}
	return res.Entry.Def.ID()
}

// blankLine reports a line before the text, the way the selector and a
// stream's head see one.
func blankLine(line []byte, first bool) bool {
	line = convert.StripANSI(line)
	if first {
		line = bytes.TrimPrefix(line, []byte{0xEF, 0xBB, 0xBF})
	}
	return len(bytes.TrimSpace(line)) == 0
}

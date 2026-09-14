package selector

import (
	"bytes"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"sync"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/registry"
)

// Watch follows a selection while the leading lines of an input arrive,
// and returns a function that reports whether the choice those lines make
// is the one Select will make however the input goes on. It is called with
// the leading lines read so far, each of them complete, while the input
// has not ended; every call is given the lines of the one before and more.
//
// It is what lets a stream choose a definition before it has read the
// whole signature window: once no line that could still arrive can change
// the choice, waiting for more of them only delays the first record. The
// rule is the one Select applies, taken line by line, and nothing in it is
// a guess:
//
//   - A definition the system or the arguments rule out stays out.
//   - An expression that matched the lines so far still matches once
//     more lines follow, unless it looks at where the text ends (\z).
//   - An expression that has not matched stays unmatched only when it is
//     anchored to the start of the text and no way through it is left
//     open by the lines already read: `\Aprocs` is out as soon as the
//     first line opens with something else, and `^Filesystem` is not
//     decided until the window is full, because the header could still
//     come.
//   - A signature whose window the lines already fill is decided whole.
//
// A signature.none expression is the case the rule is most careful with:
// a definition that fits the lines so far is only settled once every none
// expression it has is decided, so a line further down that would rule it
// out is still waited for.
//
// The choice is settled when every candidate is decided, or when the
// candidates still undecided would lose to one that fits whatever they
// turn out to be (they come from a later registry, or rank lower by
// detect.priority among variants of one command). The
// candidates are the ones Select looks at: a search of the whole registry
// leaves out the definitions used only when named, and naming a command
// leaves out a shape its other variants claim the text from.
//
// One thing is not waited for. A NUL byte rules out every format read
// line by line wherever it appears in the window, and a stream reports a
// line holding one as a record it could not read. The lines before it are
// records of the chosen format all the same, so a NUL that has not
// arrived yet is not treated as one that might.
func Watch(reg *registry.Registry, ctx Context) func(input []byte) bool {
	candidates, final := watched(reg, ctx)
	if final {
		// The selection is an error whatever the text says.
		return func([]byte) bool { return true }
	}
	// A verdict that is final stays final, so only the definitions still
	// open are looked at again when more lines have come.
	var fits []*registry.Entry
	open := candidates
	return func(input []byte) bool {
		c := ctx
		c.nul = bytes.IndexByte(input, 0) >= 0
		window := signatureWindow(input)
		still := open[:0:0]
		for _, e := range open {
			switch decide(e, &c, window) {
			case decidedFits:
				fits = append(fits, e)
			case undecided:
				still = append(still, e)
			case decidedOut:
			}
		}
		open = still
		return settledBetween(fits, open)
	}
}

// watched returns the definitions Select would check the text against,
// or reports that the selection fails before any text is looked at.
func watched(reg *registry.Registry, ctx Context) ([]*registry.Entry, bool) {
	switch {
	case ctx.Variant != "" && ctx.Parser == "":
		return nil, true
	case ctx.Variant != "":
		e, ok := reg.Lookup(ctx.Parser, ctx.Variant)
		if !ok {
			return nil, true
		}
		return []*registry.Entry{e}, false
	case ctx.Parser != "":
		variants := reg.Variants(ctx.Parser)
		if len(variants) == 0 {
			return nil, true
		}
		claims := ctx.Args == nil && anyClaims(variants)
		var out []*registry.Entry
		for _, e := range variants {
			if claims && ShapeOnly(e.Def) {
				continue
			}
			out = append(out, e)
		}
		return out, false
	}
	var out []*registry.Entry
	for _, e := range reg.Entries() {
		// Held back from a search of the whole registry, so never the
		// answer, whatever the text turns out to be.
		if !ExplicitOnly(e.Def) {
			out = append(out, e)
		}
	}
	return out, false
}

// settledBetween reports whether the definitions that fit, with the ones
// still open, leave one answer whatever the open ones turn out to be.
func settledBetween(fits, open []*registry.Entry) bool {
	if len(open) == 0 {
		return true
	}
	if len(fits) == 0 {
		return false
	}
	// settle is monotone in the way that matters here: a definition that
	// wins over every one that fits and every one still open also wins
	// over any of them that end up fitting.
	best, _, _, _ := settle(append(append([]*registry.Entry{}, fits...), open...))
	return best != nil && slices.Contains(fits, best)
}

type decision int

const (
	undecided decision = iota
	decidedFits
	decidedOut
)

// decide says whether one definition's verdict on the lines so far is
// final, and what it is.
func decide(e *registry.Entry, ctx *Context, window []string) decision {
	d := &e.Def.Detect
	if ctx.nul && e.Def.Input.Separator() == '\n' {
		return decidedOut
	}
	if ctx.OS != "" && len(d.OS) > 0 && !contains(d.OS, ctx.OS) {
		return decidedOut
	}
	if args := e.Def.ArgsFor(ctx.Parser); ctx.Args != nil && !args.IsZero() {
		if _, _, ok := matchArgs(args, ctx.Args); !ok {
			return decidedOut
		}
	}
	if d.Signature.IsZero() {
		return decidedFits
	}
	n := d.Signature.Window
	if n == 0 {
		n = definition.DefaultSignatureWindow
	}
	if len(window) >= n {
		if matchSignature(&d.Signature, window).ok() {
			return decidedFits
		}
		return decidedOut
	}
	if len(window) == 0 {
		// The blank lines before the text are not part of it, so nothing
		// is known yet about how it starts.
		return undecided
	}
	text := strings.Join(window, "\n")
	all, anyOf, none := d.Signature.Compiled()
	result := decidedFits
	for _, re := range all {
		switch judge(re, text) {
		case decidedOut:
			return decidedOut
		case undecided:
			result = undecided
		case decidedFits:
		}
	}
	if len(anyOf) > 0 {
		hit, open := false, false
		for _, re := range anyOf {
			switch judge(re, text) {
			case decidedFits:
				hit = true
			case undecided:
				open = true
			case decidedOut:
			}
		}
		switch {
		case hit:
		case open:
			result = undecided
		default:
			return decidedOut
		}
	}
	for _, re := range none {
		switch judge(re, text) {
		case decidedFits:
			return decidedOut
		case undecided:
			result = undecided
		case decidedOut:
		}
	}
	return result
}

// judge says whether one expression's answer on text, the first lines of
// an input that goes on, is final: decidedFits for a match that stays,
// decidedOut for no match that stays, undecided otherwise.
//
// A match found in the lines so far is a match in the longer text too: the
// next character after them is a line break, and every assertion but the
// end of the text reads a line break where the text ends the way it reads
// the end (a multi-line $ holds before either, and neither is a word
// character).
func judge(re *regexp.Regexp, text string) decision {
	shape := shapeOf(re)
	if re.MatchString(text) {
		if shape.endsText {
			return undecided
		}
		return decidedFits
	}
	// More lines follow the text, so whatever comes next starts with a
	// line break after it.
	if shape.prog != nil && !viable(shape.prog, text+"\n") {
		return decidedOut
	}
	return undecided
}

// shape is what the syntax of an expression says about where it can
// match.
type shape struct {
	// prog is the compiled expression when it is anchored to the start of
	// the text, which is the only kind a text that has not ended can rule
	// out: any other can still match on a line that has not come.
	prog *syntax.Prog
	// endsText is set when the expression looks at the end of the text,
	// which more lines move.
	endsText bool
}

var shapes sync.Map // *regexp.Regexp -> shape

func shapeOf(re *regexp.Regexp) shape {
	if v, ok := shapes.Load(re); ok {
		if s, ok := v.(shape); ok {
			return s
		}
	}
	s := shape{endsText: true} // what an expression that does not parse gets: nothing is final
	if tree, err := syntax.Parse(re.String(), syntax.Perl); err == nil {
		tree = tree.Simplify()
		s.endsText = hasOp(tree, syntax.OpEndText)
		if anchored(tree) {
			if prog, err := syntax.Compile(tree); err == nil {
				s.prog = prog
			}
		}
	}
	shapes.Store(re, s)
	return s
}

// viable reports whether some text that begins with prefix could still be
// matched by prog, an expression anchored to the start of the text: it
// matches within prefix, or a way through it is still open where prefix
// ends. It runs the program as a set of threads over prefix, the way the
// regexp package does, without the capture bookkeeping. At the end of
// prefix the next character is not known, so an assertion there counts
// as met.
func viable(prog *syntax.Prog, prefix string) bool {
	runes := []rune(prefix)
	var (
		cur, next []uint32
		matched   bool
	)
	onList := make([]int, len(prog.Inst))
	for i := range onList {
		onList[i] = -1
	}
	// add follows the instructions that consume nothing from pc, at the
	// position between runes[at-1] and runes[at].
	var add func(list []uint32, pc uint32, at int) []uint32
	add = func(list []uint32, pc uint32, at int) []uint32 {
		if onList[pc] == at {
			return list
		}
		onList[pc] = at
		inst := &prog.Inst[pc]
		switch inst.Op {
		case syntax.InstFail:
		case syntax.InstAlt, syntax.InstAltMatch:
			list = add(list, inst.Out, at)
			list = add(list, inst.Arg, at)
		case syntax.InstNop, syntax.InstCapture:
			list = add(list, inst.Out, at)
		case syntax.InstEmptyWidth:
			// The program keeps the assertion in Arg, which is an EmptyOp.
			if at == len(runes) || syntax.EmptyOp(inst.Arg)&^emptyContext(runes, at) == 0 { //nolint:gosec // an EmptyOp widened to uint32 by the compiler
				list = add(list, inst.Out, at)
			}
		case syntax.InstMatch:
			matched = true
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			list = append(list, pc)
		}
		return list
	}
	cur = add(cur, uint32(prog.Start), 0) //nolint:gosec // an instruction index
	for i, r := range runes {
		if matched {
			return true
		}
		next = next[:0]
		for _, pc := range cur {
			if prog.Inst[pc].MatchRune(r) {
				next = add(next, prog.Inst[pc].Out, i+1)
			}
		}
		cur, next = next, cur
		if len(cur) == 0 {
			return matched
		}
	}
	return matched || len(cur) > 0
}

// emptyContext is what holds at the position between runes[at-1] and
// runes[at].
func emptyContext(runes []rune, at int) syntax.EmptyOp {
	before, after := rune(-1), rune(-1)
	if at > 0 {
		before = runes[at-1]
	}
	if at < len(runes) {
		after = runes[at]
	}
	return syntax.EmptyOpContext(before, after)
}

// anchored reports whether every match of re starts at the start of the
// text.
func anchored(re *syntax.Regexp) bool {
	if re.Op == syntax.OpBeginText {
		return true
	}
	if re.Op == syntax.OpCapture || re.Op == syntax.OpConcat {
		return len(re.Sub) > 0 && anchored(re.Sub[0])
	}
	if re.Op == syntax.OpAlternate {
		for _, sub := range re.Sub {
			if !anchored(sub) {
				return false
			}
		}
		return len(re.Sub) > 0
	}
	return false
}

func hasOp(re *syntax.Regexp, op syntax.Op) bool {
	if re.Op == op {
		return true
	}
	for _, sub := range re.Sub {
		if hasOp(sub, op) {
			return true
		}
	}
	return false
}

// Package selector decides which parser definition describes a piece of
// captured output.
//
// The rules are the same whether jz read the text from standard input or
// produced it by running a command; only the amount of information
// differs. Nothing is ever chosen by similarity or by "closest match":
//
//   - A definition's signature is a necessary condition. If the signature
//     does not match the text, the definition is out, no matter what else
//     is known about the input.
//   - A definition without a signature, or one that declares
//     detect.auto_detect: false because its format is too unremarkable to
//     recognise (three numbers, a number and a path), is only considered
//     once the parser is known (jz run, or --parser). Its signature is
//     still verified then, so naming the parser confirms the format
//     instead of skipping the check.
//   - The operating system and the command arguments are hard filters
//     when they are known, which is the case when jz ran the command
//     itself. They can only remove candidates, never promote one.
//   - When definitions from different registries survive together, the
//     one from the more preferred registry wins. That is not a guess
//     either: the layering is the order the user asked for, and it
//     already decides which definition of the same name applies.
//   - detect.priority breaks a tie between variants of the same command,
//     which is a deliberate statement by the definition author. It never
//     ranks definitions of different commands against each other.
//
// Exactly one survivor means success. Zero and more than one are both
// errors that say what to pass explicitly, because emitting confident but
// wrong JSON is the worst failure this tool can have.
package selector

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/registry"
)

// Context carries everything known about the text to classify.
type Context struct {
	// Parser restricts the candidates to one command's definitions. It is
	// the command name in exec mode and the --parser value in pipe mode.
	// Empty means "consider every parser in the registry".
	Parser string
	// Variant names one definition of Parser explicitly. It requires
	// Parser to be set.
	Variant string
	// OS is the operating system that produced the output, when known.
	OS string
	// Args are the arguments the command was run with. Nil means unknown,
	// which is always the case for piped input.
	Args []string
	// Input is the captured output. Only the leading lines are examined.
	Input []byte
}

// Result is a successful selection.
type Result struct {
	// Entry is the chosen definition.
	Entry *registry.Entry
	// Scanned is the number of definitions the signatures were evaluated
	// against.
	Scanned int
	// Matched renders the conditions the chosen definition stated and the
	// input met, as "signature.all[0] /^Filesystem/". It is what --explain
	// shows, so that a choice can be checked rather than trusted.
	Matched []string
	// Rejections explain the definitions that were considered and left
	// out. Scanning the whole registry rejects almost all of it on the
	// first expression of a signature, which says nothing, so only the
	// ones that came close are kept there; a search scoped to one parser
	// keeps all of its variants.
	Rejections []Rejection
	// Settled names the rule that chose Entry over other definitions that
	// fit the text as well: "registry" for the layering, "priority" for
	// detect.priority. It is empty when Entry was the only one that fit.
	Settled string
	// Outranked are the definitions that fit the text and lost to Entry
	// by the Settled rule, each with the reason in its own terms.
	Outranked []Rejection
	// ExplicitOnly counts the definitions left out because they are only
	// used when named: no signature, or detect.auto_detect: false. It is
	// zero for a search scoped to one parser, where they take part.
	ExplicitOnly int
}

// Rejection records why one definition was not selected.
type Rejection struct {
	Entry  *registry.Entry
	Reason string
	// Close marks a rejection worth reading: the definition satisfied
	// part of what it asks for before it was ruled out. A definition that
	// failed the very first expression of its signature is every
	// unrelated parser in the registry, and listing those explains
	// nothing.
	Close bool
	// ExplicitOnly marks a definition that was not considered because it
	// is only used when named. Close is then true when its signature does
	// fit the text, which makes naming its parser the way forward.
	ExplicitOnly bool
}

// UnknownParserError is returned when --parser or a command name has no
// definitions at all.
type UnknownParserError struct {
	Parser string
	Known  []string
}

func (e *UnknownParserError) Error() string {
	msg := fmt.Sprintf("no parser for %q", e.Parser)
	if s := suggest(e.Parser, e.Known); len(s) > 0 {
		msg += fmt.Sprintf(" (did you mean %s?)", strings.Join(s, ", "))
	}
	return msg + "\nrun `jz list` to see the supported parsers"
}

// UnknownVariantError is returned when --variant names a variant the
// parser does not have.
type UnknownVariantError struct {
	Parser    string
	Variant   string
	Available []string
}

func (e *UnknownVariantError) Error() string {
	return fmt.Sprintf("parser %q has no variant %q (available: %s)\nrun `jz list %s` to see them",
		e.Parser, e.Variant, strings.Join(e.Available, ", "), e.Parser)
}

// VariantWithoutParserError is returned when --variant is given alone.
type VariantWithoutParserError struct {
	Variant string
}

func (e *VariantWithoutParserError) Error() string {
	return fmt.Sprintf("--variant %s needs --parser: a variant name only identifies a definition together with its parser", e.Variant)
}

// MismatchError is returned when an explicitly chosen definition
// contradicts the input.
type MismatchError struct {
	Entry  *registry.Entry
	Reason string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("%s does not describe this input: %s", e.Entry.Def.ID(), e.Reason)
}

// NoMatchError is returned when no definition survives.
type NoMatchError struct {
	// Parser is the scope that was searched; empty means the whole
	// registry.
	Parser string
	// Rejections explain the candidates that were considered. It is only
	// filled when the search was limited to one parser; scanning the whole
	// registry would produce a wall of text.
	Rejections []Rejection
	// Scanned counts the definitions that were evaluated.
	Scanned int
	// Hints are definitions whose signature does fit the text but which
	// jz refuses to choose on its own because their format is too
	// generic. Naming one of them is the way forward.
	Hints []*registry.Entry
	// Reported carries the rejections --explain shows. It is the whole
	// list when one parser was searched and the near misses otherwise,
	// which is the same choice a successful selection makes.
	Reported []Rejection
	// ExplicitOnly counts the definitions left out because they are only
	// used when named.
	ExplicitOnly int
}

func (e *NoMatchError) Error() string {
	var b strings.Builder
	if e.Parser == "" {
		b.WriteString("unable to identify the input format")
		if len(e.Hints) > 0 {
			names := make([]string, 0, len(e.Hints))
			for _, h := range e.Hints {
				names = append(names, h.Def.Command)
			}
			names = dedupe(names)
			fmt.Fprintf(&b, "\nit could be %s output, but that format is too generic for jz to claim on its own",
				strings.Join(quoteAll(names), " or "))
			fmt.Fprintf(&b, "\n\nConfirm it:\n  COMMAND | jz --parser %s", names[0])
			return b.String()
		}
		fmt.Fprintf(&b, "\nno signature of the %d known parsers matched this text", e.Scanned)
		b.WriteString("\n\nName the parser explicitly:\n  COMMAND | jz --parser df\nRun `jz list` to see the supported parsers.")
		return b.String()
	}
	fmt.Fprintf(&b, "no %s variant matches this input", e.Parser)
	for _, r := range e.Rejections {
		fmt.Fprintf(&b, "\n  %s: %s", r.Entry.Def.Variant, r.Reason)
	}
	fmt.Fprintf(&b, "\n\nName the variant explicitly:\n  COMMAND | jz --parser %s --variant %s", e.Parser, firstVariant(e.Rejections))
	return b.String()
}

func firstVariant(rs []Rejection) string {
	if len(rs) == 0 {
		return "VARIANT"
	}
	return rs[0].Entry.Def.Variant
}

// AmbiguousError is returned when several definitions match equally well.
type AmbiguousError struct {
	Candidates []*registry.Entry
	// Reported carries the rejections --explain shows.
	Reported []Rejection
	// Scanned counts the definitions that were evaluated.
	Scanned int
	// ExplicitOnly counts the definitions left out because they are only
	// used when named.
	ExplicitOnly int
	// Unsettled says why no rule chose between the candidates.
	Unsettled string
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	sameCommand := true
	for _, c := range e.Candidates {
		if c.Def.Command != e.Candidates[0].Def.Command {
			sameCommand = false
			break
		}
	}
	if sameCommand {
		fmt.Fprintf(&b, "input matches multiple %s variants:", e.Candidates[0].Def.Command)
	} else {
		b.WriteString("input matches multiple parsers:")
	}
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  %s", c.Def.ID())
	}
	first := e.Candidates[0].Def
	if sameCommand {
		fmt.Fprintf(&b, "\n\nSpecify one explicitly:\n  COMMAND | jz --parser %s --variant %s", first.Command, first.Variant)
	} else {
		fmt.Fprintf(&b, "\n\nSpecify one explicitly:\n  COMMAND | jz --parser %s", first.Command)
	}
	return b.String()
}

// Select classifies ctx.Input.
func Select(reg *registry.Registry, ctx Context) (*Result, error) {
	if ctx.Variant != "" && ctx.Parser == "" {
		return nil, &VariantWithoutParserError{Variant: ctx.Variant}
	}
	// Scoping to one parser is the cheap path: only that command's
	// variants are materialised, never the whole registry.
	var candidates []*registry.Entry
	if ctx.Parser != "" {
		candidates = reg.Variants(ctx.Parser)
		if len(candidates) == 0 {
			return nil, &UnknownParserError{Parser: ctx.Parser, Known: reg.Commands()}
		}
	} else {
		candidates = reg.Entries()
	}
	window := signatureWindow(ctx.Input)

	if ctx.Variant != "" {
		e, ok := reg.Lookup(ctx.Parser, ctx.Variant)
		if !ok {
			return nil, &UnknownVariantError{Parser: ctx.Parser, Variant: ctx.Variant, Available: variantNames(candidates)}
		}
		// A name is not evidence: a variant the user asked for still has
		// to fit the text. If a definition rejects output it should
		// accept, the definition is what needs fixing.
		v := check(e, &ctx, window)
		if !v.ok() {
			return nil, &MismatchError{Entry: e, Reason: v.Reason}
		}
		return &Result{Entry: e, Scanned: 1, Matched: v.Matched}, nil
	}

	var (
		matched      []*registry.Entry
		verdicts     = map[*registry.Entry]verdict{}
		hints        []*registry.Entry
		rejections   []Rejection
		explicitOnly int
	)
	for _, e := range candidates {
		if ctx.Parser == "" && ExplicitOnly(e.Def) {
			explicitOnly++
			// The text cannot vouch for such a definition. A definition
			// that does carry a signature can still say "this looks like
			// me", which becomes a hint naming the parser to pass; one
			// without a signature says nothing at all.
			fits := false
			if !e.Def.Detect.Signature.IsZero() {
				if check(e, &ctx, window).ok() {
					hints = append(hints, e)
					fits = true
				}
			}
			// A definition whose signature does fit the text and is only
			// held back by auto_detect is the near miss most worth
			// naming, since naming its parser is the way forward.
			rejections = append(rejections, Rejection{Entry: e, Reason: "needs --parser " + e.Def.Command, Close: fits, ExplicitOnly: true})
			continue
		}
		v := check(e, &ctx, window)
		if v.ok() {
			matched = append(matched, e)
			verdicts[e] = v
		} else {
			rejections = append(rejections, Rejection{Entry: e, Reason: v.Reason, Close: v.Close})
		}
	}
	// A search of the whole registry rejects nearly all of it on the first
	// expression of a signature, which explains nothing; a search scoped
	// to one parser is short enough to report whole.
	reported := rejections
	if ctx.Parser == "" {
		reported = closeOnly(rejections)
	}
	result := func(e *registry.Entry, settled string, outranked []Rejection) *Result {
		return &Result{
			Entry: e, Scanned: len(candidates), Matched: verdicts[e].Matched, Rejections: reported,
			Settled: settled, Outranked: outranked, ExplicitOnly: explicitOnly,
		}
	}
	switch len(matched) {
	case 1:
		return result(matched[0], "", nil), nil
	case 0:
		err := &NoMatchError{Parser: ctx.Parser, Scanned: len(candidates), Hints: hints, Reported: reported, ExplicitOnly: explicitOnly}
		if ctx.Parser != "" {
			err.Rejections = rejections
		}
		return nil, err
	}
	best, rule, outranked, preferred := settle(matched)
	if best == nil {
		return nil, &AmbiguousError{Candidates: preferred, Reported: reported, Scanned: len(candidates), ExplicitOnly: explicitOnly, Unsettled: unsettled(preferred)}
	}
	return result(best, rule, outranked), nil
}

// settle chooses among several definitions that all fit the text, and
// says by which rule and over which others. It returns a nil entry, and
// the definitions still standing, when no rule decides.
//
// The layering comes first. It is a statement the user made: a registry
// earlier in it is the one whose definitions apply. Applying that here as
// well means a definition someone adds locally cannot turn an official
// parser into an ambiguity, which is what would otherwise happen the
// moment two registries describe overlapping formats.
func settle(matched []*registry.Entry) (*registry.Entry, string, []Rejection, []*registry.Entry) {
	preferred := mostPreferred(matched)
	var outranked []Rejection
	for _, e := range matched {
		if e.Precedence != preferred[0].Precedence {
			outranked = append(outranked, Rejection{Entry: e, Reason: fmt.Sprintf("fits as well, from %s, which comes after %s in the layering", e.Source, preferred[0].Source), Close: true})
		}
	}
	if len(preferred) == 1 {
		return preferred[0], SettledByRegistry, outranked, preferred
	}
	best, ok := breakTie(preferred)
	if !ok {
		return nil, "", nil, preferred
	}
	for _, e := range preferred {
		if e != best {
			outranked = append(outranked, Rejection{Entry: e, Reason: fmt.Sprintf("fits as well, with detect.priority %d against %d", e.Def.Detect.Priority, best.Def.Detect.Priority), Close: true})
		}
	}
	return best, SettledByPriority, outranked, preferred
}

// The rules that choose between definitions that all fit the text.
const (
	// SettledByRegistry is the layering: the earlier registry wins.
	SettledByRegistry = "registry"
	// SettledByPriority is detect.priority, between variants of one
	// command, when one is strictly highest.
	SettledByPriority = "priority"
)

// unsettled says why breakTie could not choose among entries of one
// registry.
func unsettled(entries []*registry.Entry) string {
	for _, e := range entries[1:] {
		if e.Def.Command != entries[0].Def.Command {
			return "they are different commands, and detect.priority only ranks variants of one"
		}
	}
	return "no detect.priority among them is strictly highest"
}

// mostPreferred keeps the entries that come from the earliest registry in
// the layering. Entries of one registry are never ranked against each
// other, so a collision inside a registry stays an error its owner has to
// resolve.
func mostPreferred(matched []*registry.Entry) []*registry.Entry {
	best := matched[0].Precedence
	for _, e := range matched[1:] {
		if e.Precedence < best {
			best = e.Precedence
		}
	}
	out := matched[:0:0]
	for _, e := range matched {
		if e.Precedence == best {
			out = append(out, e)
		}
	}
	return out
}

// breakTie applies detect.priority. It only decides between variants of
// the same command, where the author can meaningfully rank definitions
// against each other, and only when one priority is strictly highest.
func breakTie(matched []*registry.Entry) (*registry.Entry, bool) {
	for _, e := range matched[1:] {
		if e.Def.Command != matched[0].Def.Command {
			return nil, false
		}
	}
	best := matched[0]
	tied := false
	for _, e := range matched[1:] {
		switch {
		case e.Def.Detect.Priority > best.Def.Detect.Priority:
			best, tied = e, false
		case e.Def.Detect.Priority == best.Def.Detect.Priority:
			tied = true
		}
	}
	if tied {
		return nil, false
	}
	return best, true
}

// verdict is the outcome of testing one definition against the input.
type verdict struct {
	// Matched renders the conditions the definition stated and the input
	// met, in the order they were tested.
	Matched []string
	// Reason says why the definition was ruled out; empty means it was
	// not.
	Reason string
	// Close reports that the definition satisfied part of what it asks
	// for before being ruled out, which is what separates a near miss
	// from an unrelated parser.
	Close bool
}

func (v verdict) ok() bool { return v.Reason == "" }

// check reports whether one definition can describe the input, why not
// when it cannot, and what it did meet either way.
func check(e *registry.Entry, ctx *Context, window []string) verdict {
	d := &e.Def.Detect
	var v verdict
	if !d.Signature.IsZero() {
		v = matchSignature(&d.Signature, window)
		if !v.ok() {
			return v
		}
	}
	// Anything past the signature has already met it, so a rejection here
	// is always worth reading.
	if ctx.OS != "" && len(d.OS) > 0 && !contains(d.OS, ctx.OS) {
		v.Reason = fmt.Sprintf("written for %s, not %s", strings.Join(d.OS, "/"), ctx.OS)
		v.Close = true
		return v
	}
	v.Matched = appendCriterion(v.Matched, ctx.OS != "" && len(d.OS) > 0, "detect.os", strings.Join(d.OS, "/"))
	// An alias may need other arguments than the command does, so the
	// filter comes from the name the definition was reached under.
	if args := e.Def.ArgsFor(ctx.Parser); ctx.Args != nil && !args.IsZero() {
		reason, ok := matchArgs(args, ctx.Args)
		if !ok {
			v.Reason, v.Close = reason, true
			return v
		}
		v.Matched = append(v.Matched, "detect.args "+describeArgs(args))
	}
	return v
}

// describeArgs renders an argument filter as the definition states it.
func describeArgs(a *definition.ArgsMatch) string {
	var parts []string
	for _, f := range []struct {
		key  string
		list []string
	}{{"any", a.Any}, {"all", a.All}, {"none", a.None}} {
		if len(f.list) > 0 {
			parts = append(parts, f.key+" ["+strings.Join(f.list, " ")+"]")
		}
	}
	return strings.Join(parts, ", ")
}

func appendCriterion(list []string, when bool, key, detail string) []string {
	if !when {
		return list
	}
	if detail == "" {
		return append(list, key)
	}
	return append(list, key+" "+detail)
}

// Window returns how many leading lines a caller has to hold before a
// selection can be trusted: the largest window any candidate signature
// looks at. Reading fewer would let a definition through whose none[]
// expression sits further down, which is a false accept rather than a
// slower answer.
//
// parser scopes the candidates the way Select does, so naming a command
// (which `jz run` always does) usually brings this down to the default
// twenty lines.
func Window(reg *registry.Registry, parser string) int {
	candidates := reg.Entries()
	if parser != "" {
		candidates = reg.Variants(parser)
	}
	n := 1
	for _, e := range candidates {
		w := e.Def.Detect.Signature.Window
		if w == 0 {
			w = definition.DefaultSignatureWindow
		}
		if w > n {
			n = w
		}
	}
	if n > definition.MaxSignatureWindow {
		n = definition.MaxSignatureWindow
	}
	return n
}

// signatureWindow returns the leading lines of input, which is all a
// signature may look at.
func signatureWindow(input []byte) []string {
	if len(input) == 0 {
		return nil
	}
	input = bytes.TrimPrefix(input, []byte{0xEF, 0xBB, 0xBF})
	// A command that keeps colouring its output through a pipe would
	// otherwise hide its own format behind the escapes.
	input = convert.StripANSI(input)
	var lines []string
	for len(input) > 0 && len(lines) < definition.MaxSignatureWindow {
		i := bytes.IndexByte(input, '\n')
		var l []byte
		if i < 0 {
			l, input = input, nil
		} else {
			l, input = input[:i], input[i+1:]
		}
		lines = append(lines, string(bytes.TrimSuffix(l, []byte{'\r'})))
	}
	return lines
}

func matchSignature(s *definition.Signature, window []string) verdict {
	n := s.Window
	if n == 0 {
		n = definition.DefaultSignatureWindow
	}
	if n > len(window) {
		n = len(window)
	}
	text := strings.Join(window[:n], "\n")
	all, anyOf, none := s.Compiled()
	var v verdict
	for i, re := range all {
		if !re.MatchString(text) {
			v.Reason = fmt.Sprintf("signature.all[%d] %s did not match", i, short(re))
			// Getting past an earlier expression is what makes this
			// worth reading; failing the first one is what almost every
			// definition in the registry does with almost every input.
			v.Close = i > 0
			return v
		}
		v.Matched = append(v.Matched, fmt.Sprintf("signature.all[%d] %s", i, short(re)))
	}
	if len(anyOf) > 0 {
		hit := -1
		for i, re := range anyOf {
			if re.MatchString(text) {
				hit = i
				break
			}
		}
		if hit < 0 {
			v.Reason = "no signature.any[] expression matched"
			v.Close = len(all) > 0
			return v
		}
		v.Matched = append(v.Matched, fmt.Sprintf("signature.any[%d] %s", hit, short(anyOf[hit])))
	}
	for i, re := range none {
		if re.MatchString(text) {
			v.Reason = fmt.Sprintf("signature.none[%d] %s matched", i, short(re))
			v.Close = len(all) > 0 || len(anyOf) > 0
			return v
		}
	}
	if len(none) > 0 {
		v.Matched = append(v.Matched, fmt.Sprintf("signature.none[] (%d expressions, none matched)", len(none)))
	}
	return v
}

func short(re *regexp.Regexp) string {
	s := strings.TrimPrefix(re.String(), "(?m)")
	if len(s) > 50 {
		s = s[:50] + "..."
	}
	return "/" + s + "/"
}

// matchArgs applies any/all/none. Bundled short flags such as -hT count
// as containing -h and -T.
func matchArgs(a *definition.ArgsMatch, args []string) (string, bool) {
	set := map[string]bool{}
	for _, arg := range args {
		set[arg] = true
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			for _, r := range arg[1:] {
				set["-"+string(r)] = true
			}
		}
	}
	if len(a.Any) > 0 {
		hit := false
		for _, want := range a.Any {
			if set[want] {
				hit = true
				break
			}
		}
		if !hit {
			return fmt.Sprintf("needs one of the arguments [%s]", strings.Join(a.Any, ", ")), false
		}
	}
	for _, want := range a.All {
		if !set[want] {
			return "needs the argument " + want, false
		}
	}
	for _, bad := range a.None {
		if set[bad] {
			return "excluded by the argument " + bad, false
		}
	}
	return "", true
}

// ExplicitOnly reports whether a definition may only be used once the
// user has named the parser (or jz ran the command itself), either
// because it has no signature or because it declares its format too
// generic to claim.
func ExplicitOnly(d *definition.Definition) bool {
	return !d.Detect.AutoDetectable() || d.Detect.Signature.IsZero()
}

// ShapeOnly reports a definition that describes a shape rather than a
// command: it declines automatic detection and carries no signature, so
// it says nothing at all about the text and makes no claim to check.
// `table/whitespace` and `csv/comma` are those; naming one is the caller
// saying what the text is, which is the same thing --define does with
// the definition written out.
//
// Such a definition is left out of the checks that ask whether a
// definition reads a neighbour's output, because it reads everything by
// design and would report every fixture in the registry.
func ShapeOnly(d *definition.Definition) bool {
	return !d.Detect.AutoDetectable() && d.Detect.Signature.IsZero()
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := list[:0:0]
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func quoteAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = "`" + s + "`"
	}
	return out
}

func variantNames(entries []*registry.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Def.Variant
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// suggest returns known names within a small edit distance of s.
// suggest names the commands closest to one that does not exist. Only
// the closest are offered: a registry of a few hundred commands has
// several within two edits of any short name, and listing them beside
// the one the caller meant is worse than listing nothing, because the
// answer stops standing out. A name that extends or is extended by a
// command counts as closer than any edit distance.
func suggest(s string, known []string) []string {
	best := -1
	score := map[string]int{}
	for _, k := range known {
		if k == s {
			continue
		}
		// A name that extends or is extended by a command is a candidate
		// however long the extension, but it is ranked by the same
		// distance as everything else, so `lscpuu` offers lscpu rather
		// than lscpu and ls together.
		d := levenshtein(s, k)
		if d > 2 && !strings.HasPrefix(k, s) && !strings.HasPrefix(s, k) {
			continue
		}
		score[k] = d
		if best < 0 || d < best {
			best = d
		}
	}
	var out []string
	for k, d := range score {
		if d == best {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// closeOnly keeps the rejections that explain something: a definition
// that met part of what it asks for before being ruled out.
func closeOnly(rs []Rejection) []Rejection {
	out := rs[:0:0]
	for _, r := range rs {
		if r.Close {
			out = append(out, r)
		}
	}
	return out
}

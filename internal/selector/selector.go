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

	"github.com/nao1215/jsonize/internal/convert"
	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/registry"
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
}

// Rejection records why one definition was not selected.
type Rejection struct {
	Entry  *registry.Entry
	Reason string
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
		if reason, ok := check(e, &ctx, window); !ok {
			return nil, &MismatchError{Entry: e, Reason: reason}
		}
		return &Result{Entry: e, Scanned: 1}, nil
	}

	var (
		matched    []*registry.Entry
		hints      []*registry.Entry
		rejections []Rejection
	)
	for _, e := range candidates {
		if ctx.Parser == "" && ExplicitOnly(e.Def) {
			// The text cannot vouch for such a definition. A definition
			// that does carry a signature can still say "this looks like
			// me", which becomes a hint naming the parser to pass; one
			// without a signature says nothing at all.
			if !e.Def.Detect.Signature.IsZero() {
				if _, ok := check(e, &ctx, window); ok {
					hints = append(hints, e)
				}
			}
			rejections = append(rejections, Rejection{Entry: e, Reason: "needs --parser " + e.Def.Command})
			continue
		}
		if reason, ok := check(e, &ctx, window); ok {
			matched = append(matched, e)
		} else {
			rejections = append(rejections, Rejection{Entry: e, Reason: reason})
		}
	}
	switch len(matched) {
	case 1:
		return &Result{Entry: matched[0], Scanned: len(candidates)}, nil
	case 0:
		err := &NoMatchError{Parser: ctx.Parser, Scanned: len(candidates), Hints: hints}
		if ctx.Parser != "" {
			err.Rejections = rejections
		}
		return nil, err
	}
	// The layering is a statement the user made: a registry earlier in it
	// is the one whose definitions apply. Applying that here as well means
	// a definition someone adds locally cannot turn an official parser
	// into an ambiguity, which is what would otherwise happen the moment
	// two registries describe overlapping formats.
	matched = mostPreferred(matched)
	if len(matched) == 1 {
		return &Result{Entry: matched[0], Scanned: len(candidates)}, nil
	}
	if best, ok := breakTie(matched); ok {
		return &Result{Entry: best, Scanned: len(candidates)}, nil
	}
	return nil, &AmbiguousError{Candidates: matched}
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

// check reports whether one definition can describe the input, and why
// not when it cannot.
func check(e *registry.Entry, ctx *Context, window []string) (string, bool) {
	d := &e.Def.Detect
	if !d.Signature.IsZero() {
		if reason, ok := matchSignature(&d.Signature, window); !ok {
			return reason, false
		}
	}
	if ctx.OS != "" && len(d.OS) > 0 && !contains(d.OS, ctx.OS) {
		return fmt.Sprintf("written for %s, not %s", strings.Join(d.OS, "/"), ctx.OS), false
	}
	// An alias may need other arguments than the command does, so the
	// filter comes from the name the definition was reached under.
	if args := e.Def.ArgsFor(ctx.Parser); ctx.Args != nil && !args.IsZero() {
		if reason, ok := matchArgs(args, ctx.Args); !ok {
			return reason, false
		}
	}
	return "", true
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

func matchSignature(s *definition.Signature, window []string) (string, bool) {
	n := s.Window
	if n == 0 {
		n = definition.DefaultSignatureWindow
	}
	if n > len(window) {
		n = len(window)
	}
	text := strings.Join(window[:n], "\n")
	all, anyOf, none := s.Compiled()
	for i, re := range all {
		if !re.MatchString(text) {
			return fmt.Sprintf("signature all[%d] %s did not match", i, short(re)), false
		}
	}
	if len(anyOf) > 0 {
		hit := false
		for _, re := range anyOf {
			if re.MatchString(text) {
				hit = true
				break
			}
		}
		if !hit {
			return "no signature any[] expression matched", false
		}
	}
	for i, re := range none {
		if re.MatchString(text) {
			return fmt.Sprintf("signature none[%d] %s matched", i, short(re)), false
		}
	}
	return "", true
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
func suggest(s string, known []string) []string {
	var out []string
	for _, k := range known {
		if k == s {
			continue
		}
		if strings.HasPrefix(k, s) || strings.HasPrefix(s, k) || levenshtein(s, k) <= 2 {
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

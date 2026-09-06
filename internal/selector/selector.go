// Package selector chooses which variant of a command's parser definitions
// applies to a given run.
//
// Selection is deterministic and never guesses: every candidate is scored
// by how many of its detect criteria (os, args, signature) were both
// applicable and satisfied; a candidate whose applicable criterion fails is
// rejected. The single most specific survivor wins; ties are broken by
// detect.priority; a remaining tie is reported as ambiguous so the user can
// pass --variant explicitly.
package selector

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/registry"
)

// Context carries the signals available for selection.
type Context struct {
	// Command is the command name (already resolved to a registry key).
	Command string
	// Variant, when set, bypasses detection.
	Variant string
	// OS is the GOOS of the machine that produced the output. Empty means
	// unknown (pipe mode without --os), in which case os criteria are
	// neither satisfied nor violated.
	OS string
	// Args are the command arguments (exec mode). Nil means unknown.
	Args []string
	// Input is the captured output; only the signature window is examined.
	Input []byte
}

// Result describes the chosen definition and the runner-up evaluations.
type Result struct {
	Entry       *registry.Entry
	Evaluations []Evaluation
}

// Evaluation records how one candidate fared.
type Evaluation struct {
	Entry *registry.Entry
	// Accepted is false when an applicable criterion failed.
	Accepted bool
	// Specificity counts satisfied criteria.
	Specificity int
	// Reasons explains rejections or which criteria matched.
	Reasons []string
}

// UnknownCommandError is returned when no definition exists for a command.
type UnknownCommandError struct {
	Command string
	Known   []string
}

func (e *UnknownCommandError) Error() string {
	msg := fmt.Sprintf("no parser definition for command %q", e.Command)
	if s := suggest(e.Command, e.Known); len(s) > 0 {
		msg += fmt.Sprintf(" (did you mean %s?)", strings.Join(s, ", "))
	}
	return msg + "; run `jz list` to see supported commands"
}

// UnknownVariantError is returned when --variant names a missing variant.
type UnknownVariantError struct {
	Command   string
	Variant   string
	Available []string
}

func (e *UnknownVariantError) Error() string {
	return fmt.Sprintf("command %q has no variant %q (available: %s)", e.Command, e.Variant, strings.Join(e.Available, ", "))
}

// NoMatchError is returned when every candidate was rejected.
type NoMatchError struct {
	Command     string
	Evaluations []Evaluation
}

func (e *NoMatchError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "no variant of %q matches the input", e.Command)
	for _, ev := range e.Evaluations {
		fmt.Fprintf(&b, "\n  - %s: %s", ev.Entry.Def.Variant, strings.Join(ev.Reasons, "; "))
	}
	b.WriteString("\nuse --variant to choose one explicitly")
	return b.String()
}

// AmbiguousError is returned when several candidates tie.
type AmbiguousError struct {
	Command    string
	Candidates []Evaluation
}

func (e *AmbiguousError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		names[i] = c.Entry.Def.Variant
	}
	return fmt.Sprintf("ambiguous: variants %s of %q all match the input equally well; use --variant to choose one",
		strings.Join(names, ", "), e.Command)
}

// Select picks a definition for ctx from reg.
func Select(reg *registry.Registry, ctx Context) (*Result, error) {
	candidates := reg.Variants(ctx.Command)
	if len(candidates) == 0 {
		return nil, &UnknownCommandError{Command: ctx.Command, Known: reg.Commands()}
	}
	if ctx.Variant != "" {
		e, ok := reg.Lookup(ctx.Command, ctx.Variant)
		if !ok {
			names := make([]string, len(candidates))
			for i, c := range candidates {
				names[i] = c.Def.Variant
			}
			return nil, &UnknownVariantError{Command: ctx.Command, Variant: ctx.Variant, Available: names}
		}
		return &Result{Entry: e, Evaluations: []Evaluation{{Entry: e, Accepted: true, Reasons: []string{"selected with --variant"}}}}, nil
	}
	window := signatureWindow(ctx.Input)
	evals := make([]Evaluation, 0, len(candidates))
	for _, c := range candidates {
		evals = append(evals, Evaluate(c, ctx, window))
	}
	var accepted []Evaluation
	for _, ev := range evals {
		if ev.Accepted {
			accepted = append(accepted, ev)
		}
	}
	if len(accepted) == 0 {
		return nil, &NoMatchError{Command: ctx.Command, Evaluations: evals}
	}
	sort.SliceStable(accepted, func(i, j int) bool {
		if accepted[i].Specificity != accepted[j].Specificity {
			return accepted[i].Specificity > accepted[j].Specificity
		}
		return accepted[i].Entry.Def.Detect.Priority > accepted[j].Entry.Def.Detect.Priority
	})
	best := accepted[0]
	tied := []Evaluation{best}
	for _, ev := range accepted[1:] {
		if ev.Specificity == best.Specificity && ev.Entry.Def.Detect.Priority == best.Entry.Def.Detect.Priority {
			tied = append(tied, ev)
		}
	}
	if len(tied) > 1 {
		return nil, &AmbiguousError{Command: ctx.Command, Candidates: tied}
	}
	return &Result{Entry: best.Entry, Evaluations: evals}, nil
}

// signatureWindow returns the first MaxSignatureWindow lines of input as
// separate strings so each definition can pick its own window size.
func signatureWindow(input []byte) []string {
	if len(input) == 0 {
		return nil
	}
	input = bytes.TrimPrefix(input, []byte{0xEF, 0xBB, 0xBF})
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

// Evaluate scores one candidate. window holds the leading input lines.
func Evaluate(e *registry.Entry, ctx Context, window []string) Evaluation {
	ev := Evaluation{Entry: e, Accepted: true}
	d := &e.Def.Detect
	if len(d.OS) > 0 {
		switch {
		case ctx.OS == "":
			ev.Reasons = append(ev.Reasons, "os unknown (pass --os to use it)")
		case contains(d.OS, ctx.OS):
			ev.Specificity++
			ev.Reasons = append(ev.Reasons, "os "+ctx.OS+" matched")
		default:
			ev.Accepted = false
			ev.Reasons = append(ev.Reasons, fmt.Sprintf("os %s is not one of [%s]", ctx.OS, strings.Join(d.OS, ", ")))
		}
	}
	if !d.Args.IsZero() {
		if ctx.Args == nil {
			ev.Reasons = append(ev.Reasons, "arguments unknown in pipe mode")
		} else if ok, why := matchArgs(&d.Args, ctx.Args); ok {
			ev.Specificity++
			ev.Reasons = append(ev.Reasons, "arguments matched")
		} else {
			ev.Accepted = false
			ev.Reasons = append(ev.Reasons, why)
		}
	}
	if !d.Signature.IsZero() {
		n := d.Signature.Window
		if n == 0 {
			n = definition.DefaultSignatureWindow
		}
		if n > len(window) {
			n = len(window)
		}
		text := strings.Join(window[:n], "\n")
		ok, why := matchSignature(&d.Signature, text)
		if ok {
			ev.Specificity++
			ev.Reasons = append(ev.Reasons, "signature matched")
		} else {
			ev.Accepted = false
			ev.Reasons = append(ev.Reasons, why)
		}
	}
	if len(ev.Reasons) == 0 {
		ev.Reasons = append(ev.Reasons, "no detect criteria (catch-all)")
	}
	return ev
}

func matchSignature(s *definition.Signature, text string) (bool, string) {
	all, anyOf, none := s.Compiled()
	for i, re := range all {
		if !re.MatchString(text) {
			return false, fmt.Sprintf("signature all[%d] %s did not match", i, short(re))
		}
	}
	if len(anyOf) > 0 {
		matched := false
		for _, re := range anyOf {
			if re.MatchString(text) {
				matched = true
				break
			}
		}
		if !matched {
			return false, "no signature any[] expression matched"
		}
	}
	for i, re := range none {
		if re.MatchString(text) {
			return false, fmt.Sprintf("signature none[%d] %s matched", i, short(re))
		}
	}
	return true, ""
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
func matchArgs(a *definition.ArgsMatch, args []string) (bool, string) {
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
			return false, fmt.Sprintf("none of the arguments [%s] were given", strings.Join(a.Any, ", "))
		}
	}
	for _, want := range a.All {
		if !set[want] {
			return false, fmt.Sprintf("argument %s was not given", want)
		}
	}
	for _, bad := range a.None {
		if set[bad] {
			return false, fmt.Sprintf("argument %s excludes this variant", bad)
		}
	}
	return true, ""
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

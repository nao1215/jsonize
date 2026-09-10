package engine

import (
	"errors"
	"fmt"
	"strings"
)

// A conversion that succeeds has read every line of its input, or can say
// why it did not. That is the whole rule, and this file is where it is
// kept.
//
// A line ends up in one of three places. It is read when a parser turned
// it into part of the result: a row, a node, a key, a line a pattern
// matched. It is ignored when the definition or the format said so before
// any parser saw it: a blank line, a line input.ignore names, a
// continuation input.fold joined onto the line above. Anything else is
// unread, and unread text is a failure rather than a smaller result. A
// definition that takes three lines out of a report and lets the fourth
// fall through a gap between its parts would otherwise answer with
// confident JSON that is missing something, which is the one failure this
// tool exists to prevent.
//
// The rule is applied by the engine and not by the definitions, so a
// definition gets it without asking and cannot opt out: nothing a
// definition writes can make a line disappear, it can only name the lines
// it means to leave out.

// maxUnreadReported bounds how many unread pieces an error quotes. The
// count is always complete; the quotes are for finding the place.
const maxUnreadReported = 3

// UnreadError reports text the definition did not read. It is the cause
// of a *ParseError, so it maps to the same exit status as any other input
// the definition does not describe, and errors.As finds it for a caller
// that wants the positions.
type UnreadError struct {
	// Spans are the first unread pieces in input order. A piece is a
	// whole line, or the part of a line a pattern did not reach.
	Spans []UnreadSpan
	// Total counts every unread piece, including the ones not in Spans.
	Total int
}

// UnreadSpan is one piece of unread text.
type UnreadSpan struct {
	// Line is the 1-based line the text is on.
	Line int
	// Column is the 1-based rune offset where the unread text starts on a
	// line that was otherwise read, and 0 when no part of the line was.
	Column int
	// Text is the unread text, shortened for display.
	Text string
}

func (s UnreadSpan) String() string {
	if s.Column > 0 {
		return fmt.Sprintf("line %d column %d %q", s.Line, s.Column, s.Text)
	}
	return fmt.Sprintf("line %d %q", s.Line, s.Text)
}

func (e *UnreadError) Error() string {
	var b strings.Builder
	switch {
	case e.Total == 1 && len(e.Spans) == 1 && e.Spans[0].Column > 0:
		fmt.Fprintf(&b, "text the pattern did not read at column %d: %q", e.Spans[0].Column, e.Spans[0].Text)
		return b.String()
	case e.Total == 1:
		b.WriteString("a line no part of the definition read: ")
	default:
		fmt.Fprintf(&b, "%d lines no part of the definition read: ", e.Total)
	}
	for i, s := range e.Spans {
		if i > 0 {
			b.WriteString(", ")
		}
		if e.Total == 1 {
			fmt.Fprintf(&b, "%q", s.Text)
			continue
		}
		b.WriteString(s.String())
	}
	if rest := e.Total - len(e.Spans); rest > 0 {
		fmt.Fprintf(&b, " and %d more", rest)
	}
	return b.String()
}

// ErrUnread is matched by errors.Is for any *UnreadError.
var ErrUnread = errors.New("input left unread")

// Is lets errors.Is(err, ErrUnread) find an unread report.
func (e *UnreadError) Is(target error) bool { return target == ErrUnread }

// Account says where the lines of an input went. Every line is in exactly
// one of its counts once a reading has succeeded: read by a parser, left
// out as blank, left out by an input.ignore expression, or joined onto
// the line above by input.fold. It is what --explain reports, so that
// what a definition leaves out is something it can be asked about rather
// than something that happens.
type Account struct {
	// Lines is the number of records the input was split into.
	Lines int
	// Read counts the lines a parser read, headings named by
	// select.after included.
	Read int
	// Folded counts the continuation lines input.fold joined onto the
	// line above them; they were read with it.
	Folded int
	// Blank counts the lines with nothing but whitespace on them.
	Blank int
	// Ignored counts the lines each input.ignore expression left out, in
	// the order the definition lists them. A line is counted under the
	// first expression that matches it, and an expression that matched
	// nothing is not listed.
	Ignored []Ignored
}

// Ignored is what one input.ignore expression left out.
type Ignored struct {
	// Index is the expression's position in input.ignore.
	Index int
	// Expr is the expression as the definition writes it.
	Expr string
	// Lines is how many lines it matched.
	Lines int
}

// ledger records which of the lines handed to the parsers were read. It
// covers the lines that survived input preparation; the ones preparation
// dropped were ignored by the definition's own say-so and never reach it.
type ledger struct {
	base int
	read []bool
}

// newLedger covers the numbers of lines, which are in ascending order.
func newLedger(lines []line) *ledger {
	if len(lines) == 0 {
		return &ledger{}
	}
	base := lines[0].num
	return &ledger{base: base, read: make([]bool, lines[len(lines)-1].num-base+1)}
}

// mark records lines as read. A nil ledger records nothing, which is what
// a reading that accounts for its lines some other way passes.
func (l *ledger) mark(lines []line) {
	for _, x := range lines {
		l.markOne(x)
	}
}

func (l *ledger) markOne(x line) {
	if l == nil {
		return
	}
	if i := x.num - l.base; i >= 0 && i < len(l.read) {
		l.read[i] = true
	}
}

// unread returns the lines of lines nobody read, in input order. A line
// holding nothing but whitespace is never counted: it carries no value
// that could be missing from the result.
func (l *ledger) unread(lines []line) []UnreadSpan {
	var out []UnreadSpan
	for _, x := range lines {
		if l.isRead(x) || blank(x.text) {
			continue
		}
		out = append(out, UnreadSpan{Line: x.num, Text: truncate(x.text, 80)})
	}
	return out
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

// count adds to acct the lines of lines that were read, and the blank
// ones nobody read, which is where a blank line a definition keeps
// (skip_blank: false) ends up when no parser asked for it.
func (l *ledger) count(lines []line, acct *Account) {
	for _, x := range lines {
		if l.isRead(x) {
			acct.Read++
		} else if blank(x.text) {
			acct.Blank++
		}
	}
}

func (l *ledger) isRead(x line) bool {
	i := x.num - l.base
	return i >= 0 && i < len(l.read) && l.read[i]
}

// unreadError builds the error for spans, which must not be empty.
func (r *run) unreadError(spans []UnreadSpan) *ParseError {
	shown := spans
	if len(shown) > maxUnreadReported {
		shown = shown[:maxUnreadReported]
	}
	return &ParseError{
		Definition: r.def.ID(),
		Line:       spans[0].Line,
		Cause:      &UnreadError{Spans: shown, Total: len(spans)},
	}
}

// checkWhole refuses a line a pattern matched only part of. The text on
// either side of the match is a value the definition never looked at, and
// taking the part it did look at as the whole line is how a trailing
// field goes missing without anyone noticing.
func (r *run) checkWhole(l line, start, end int) error {
	if text, col, ok := outside(l.text, start, end); ok {
		return r.unreadError([]UnreadSpan{{Line: l.num, Column: col, Text: text}})
	}
	return nil
}

// markSpan records the lines a pattern matched against the whole input
// covered, given the byte offsets of the match in the lines joined with
// newlines. A line the match does not reach was not read, and a line the
// match starts or ends inside is held to the rule a line matched alone is.
func (r *run) markSpan(lines []line, start, end int) error {
	pos := 0
	for _, l := range lines {
		lo, hi := pos, pos+len(l.text)
		pos = hi + 1
		// A line is reached when the match covers at least one of its
		// characters; a match that starts on the line break after it, or
		// ends on the one before it, has not read it.
		if max(lo, start) >= min(hi, end) {
			continue
		}
		if err := r.checkWhole(l, max(start-lo, 0), min(end-lo, len(l.text))); err != nil {
			return err
		}
		r.ledger.markOne(l)
	}
	return nil
}

// outside reports the text of s that lies before start or after end,
// which is what a pattern that matched only part of a line left behind.
// Whitespace at either end is not text anyone could be missing.
func outside(s string, start, end int) (string, int, bool) {
	if strings.TrimSpace(s[:start]) != "" {
		return truncate(strings.TrimSpace(s[:start]), 80), 1 + len([]rune(s[:start])) - len([]rune(strings.TrimLeft(s[:start], " \t"))), true
	}
	if rest := s[end:]; strings.TrimSpace(rest) != "" {
		lead := len([]rune(s[:end])) + len([]rune(rest)) - len([]rune(strings.TrimLeft(rest, " \t")))
		return truncate(strings.TrimSpace(rest), 80), lead + 1, true
	}
	return "", 0, false
}

// keyLines remembers where each key of an object read from key/value
// lines was set. An object holds one value per key, so a key the text
// prints twice is refused: with two values one of them would be missing
// from the result, and with the same value twice the text is most likely
// two documents read as one, the second of which would vanish into the
// first.
type keyLines struct {
	seen map[string]int
}

func (k *keyLines) add(r *run, key string, ln int) error {
	if k.seen == nil {
		k.seen = map[string]int{}
	}
	if prev, ok := k.seen[key]; ok {
		return r.errorf(ln, key, "the key was already set on line %d, and an object holds one value per key", prev)
	}
	k.seen[key] = ln
	return nil
}

// Package engine executes parser definitions against captured text and
// produces JSON-ready values (ordered objects, arrays and typed scalars).
//
// The engine is deliberately small: a definition selects lines, splits
// them into cells with one of three algorithms (table, regex, key/value),
// optionally combines several such parts, and converts cells with the
// field rules. Nothing in a definition can execute code.
//
// Parse reads a whole text and returns the value for it. Stream reads a
// text that has not finished yet and hands over one record at a time,
// which is the same reading arranged differently: the two agree record
// for record, and the conformance runner checks that they do.
package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/recordio"
	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// Default resource limits.
const (
	DefaultMaxInputSize  = 64 * 1024 * 1024
	DefaultMaxLineLength = 1024 * 1024
	// DefaultMaxValues bounds what one reading retains rather than what
	// it reads: a parse keeps one value per cell, so a header of many
	// columns over many short rows retains far more than the input holds.
	DefaultMaxValues = 4 * 1024 * 1024
)

// Options tunes a parse run.
type Options struct {
	// MaxInputSize bounds the accepted input in bytes (0 = default).
	MaxInputSize int64
	// MaxLineLength bounds a single line in bytes (0 = default).
	MaxLineLength int
	// MaxValues bounds the values one document may hold, or one record
	// of a stream (0 = default). It is the bound on what a reading
	// retains, where MaxInputSize is the bound on what it is given.
	MaxValues int
	// Assume carries what the command line allowed jz to assume about a
	// timestamp that does not say it itself: which year a format that
	// prints none meant, and what offset a zone abbreviation stands for.
	// Both are empty by default, and a value that needs one that was not
	// given stays the string it was printed as.
	Assume convert.Assumptions
	// Raw skips the field rules. Every value is handed over as it was
	// extracted: a string, or null for an empty aligned cell and a regex
	// group that did not take part in the match. Nothing is converted,
	// nothing is trimmed and null_if is not applied, so the output shows
	// what the definition read rather than what it made of it.
	Raw bool
	// KeepEscapes leaves terminal escape sequences in the text. They come
	// off by default, because in command output they are colour and not
	// part of any value; in a data file such as a csv they are part of the
	// value that holds them.
	KeepEscapes bool
	// KeepNames makes a csv's header line the keys as written. By default
	// a heading is normalised the way a table's is ("Use%" is use_percent),
	// which is what a command's output needs for keys a definition can
	// name; in a data file the heading is data, and changing it would lose
	// a name such as one written in Japanese. An empty heading and a
	// repeated one are still given names of their own either way.
	KeepNames bool
}

func (o Options) maxInput() int64 {
	if o.MaxInputSize <= 0 {
		return DefaultMaxInputSize
	}
	return o.MaxInputSize
}

func (o Options) maxValues() int {
	if o.MaxValues <= 0 {
		return DefaultMaxValues
	}
	return o.MaxValues
}

func (o Options) maxLine() int {
	if o.MaxLineLength <= 0 {
		return DefaultMaxLineLength
	}
	return o.MaxLineLength
}

// ParseError describes why the input could not be parsed with a
// definition. Line is 1-based and 0 when not applicable.
type ParseError struct {
	Definition string
	Line       int
	Field      string
	Msg        string
	Cause      error
}

func (e *ParseError) Error() string {
	var b strings.Builder
	if e.Definition != "" {
		b.WriteString(e.Definition)
		b.WriteString(": ")
	}
	if e.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", e.Line)
	}
	if e.Field != "" {
		fmt.Fprintf(&b, "field %q: ", e.Field)
	}
	b.WriteString(e.Msg)
	if e.Cause != nil {
		if e.Msg != "" {
			b.WriteString(": ")
		}
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

func (e *ParseError) Unwrap() error { return e.Cause }

// ErrInputTooLarge is wrapped by ParseReader when the input exceeds the
// configured limit.
var ErrInputTooLarge = errors.New("input too large")

// ErrLineTooLong is wrapped when a single line exceeds the limit. It is
// the error the shared record reader reports, so that a line refused
// while a format is still being identified and one refused while it is
// read are the same error.
var ErrLineTooLong = recordio.ErrTooLong

// ErrTooManyValues is wrapped when a reading produces more values than
// one document, or one record of a stream, may hold.
var ErrTooManyValues = errors.New("too many values")

// Parse applies def to input. The result is either []any (one ordered
// object per record) or *jsonutil.Object, depending on the parse type.
//
// A result comes back only when every line of the input was read or left
// out by a rule the definition states (see Account); anything else is an
// *UnreadError inside the *ParseError.
func Parse(def *definition.Definition, input []byte, opts Options) (any, error) {
	v, _, err := ParseAccounted(def, input, opts)
	return v, err
}

// ParseAccounted is Parse, also reporting where the lines of the input
// went. The account is filled as far as the reading got, so a failure
// still says how much was read before it.
func ParseAccounted(def *definition.Definition, input []byte, opts Options) (any, Account, error) {
	var acct Account
	if int64(len(input)) > opts.maxInput() {
		return nil, acct, &ParseError{Definition: def.ID(), Msg: fmt.Sprintf("input exceeds %d bytes", opts.maxInput()), Cause: ErrInputTooLarge}
	}
	lines, err := splitRecords(input, def.Input.Separator(), opts)
	if err != nil {
		return nil, acct, &ParseError{Definition: def.ID(), Line: err.line, Msg: err.msg, Cause: err.cause}
	}
	acct.Lines = len(lines)
	if def.Parse.Type == definition.TypeCSV {
		// A quoted csv value may hold line breaks, and the lines it holds
		// are part of a record before they are anything else: a blank
		// line inside it is text, not a blank line, and a line that
		// input.ignore would name is a value. So the records are made
		// first, and everything after this sees one record at a time.
		lines = csvRecords(&def.Parse, lines)
	}
	lines, ferr := foldLines(&def.Input, lines)
	if ferr != nil {
		return nil, acct, &ParseError{Definition: def.ID(), Line: ferr.line, Msg: ferr.msg, Cause: ferr.cause}
	}
	acct.Folded = acct.Lines - len(lines)
	prepared := prepare(&def.Input, lines, &acct)
	r := &run{def: def, opts: opts, ledger: newLedger(prepared)}
	selected, heading, end := applySelect(&def.Input.Select, prepared)
	v, perr := r.parse(&def.Parse, def.Fields, selected)
	if perr == nil {
		r.markHeading(&def.Input.Select, heading)
		r.markEnd(&def.Input.Select, end)
	}
	r.ledger.count(prepared, &acct)
	if perr != nil {
		return nil, acct, perr
	}
	if spans := r.ledger.unread(prepared); len(spans) > 0 {
		return nil, acct, r.unreadError(spans)
	}
	return v, acct, nil
}

// line is one input line with its original 1-based number.
type line struct {
	text string
	num  int
}

type splitError struct {
	line  int
	msg   string
	cause error
}

// nulInLine refuses a NUL byte in a format read line by line. A command
// run with -z or --zero ends its records with NUL instead of a newline,
// and read line by line that is one line holding every record, whose last
// field would take all the records after the first.
const nulInLine = "a NUL byte in a format read line by line; a command run with -z or --zero ends its records with NUL"

// splitRecords cuts the input into records on sep, which is a newline for
// ordinary command output and NUL for the record-separated output of
// tools such as `env -0`. Each record is then brought to the form the
// parsers read by prepareRecord and checked by checkRecord, the same two
// steps a stream applies to a record as it arrives, so the two readings
// cannot disagree about a record.
func splitRecords(input []byte, sep byte, opts Options) ([]line, *splitError) {
	if len(input) == 0 {
		return nil, nil
	}
	maxLen := opts.maxLine()
	// The input is copied once, and every record is then a piece of that
	// copy: a record is a string once it is read, and making each its
	// own string would copy the input a record at a time.
	text := strings.TrimSuffix(string(input), string(sep))
	out := make([]line, 0, strings.Count(text, string(sep))+1)
	for num := 1; ; num++ {
		rec := text
		i := strings.IndexByte(text, sep)
		if i >= 0 {
			rec, text = text[:i], text[i+1:]
		}
		// The limit counts the bytes between two separators as they were
		// read, escape sequences and a carriage return included, which is
		// what a stream can count before it has read the whole record.
		if len(rec) > maxLen {
			return nil, &splitError{line: num, msg: fmt.Sprintf("record exceeds %d bytes", maxLen), cause: ErrLineTooLong}
		}
		rec = prepareRecord(rec, sep, num, opts.KeepEscapes)
		if err := checkRecord(rec, sep); err != nil {
			return nil, &splitError{line: num, msg: err.msg}
		}
		out = append(out, line{text: rec, num: num})
		if i < 0 {
			return out, nil
		}
	}
}

// prepareRecord brings one record to the form the parsers read: the
// escape sequences come off first, unless keepEscapes says they are part
// of the text, then the carriage return of a CRLF ending, then the byte
// order mark of the first record. Detection strips the escapes too;
// doing it here as well keeps them out of the values when a parser is
// named instead. The order is the same everywhere a record is prepared,
// since a byte order mark behind a colour code and a carriage return
// inside an escape sequence are only the same text under one order.
func prepareRecord(text string, sep byte, num int, keepEscapes bool) string {
	if !keepEscapes && strings.IndexByte(text, 0x1b) >= 0 {
		text = string(convert.StripANSI([]byte(text)))
	}
	if sep == '\n' {
		text = strings.TrimSuffix(text, "\r")
	}
	if num == 1 {
		text = strings.TrimPrefix(text, "\xEF\xBB\xBF")
	}
	return text
}

// checkRecord refuses a prepared record that no parser reads: one that
// is not UTF-8, and one holding a NUL byte in a format read line by
// line.
func checkRecord(text string, sep byte) *splitError {
	if !utf8.ValidString(text) {
		return &splitError{msg: "input is not valid UTF-8"}
	}
	if sep == '\n' && strings.IndexByte(text, 0) >= 0 {
		return &splitError{msg: nulInLine}
	}
	return nil
}

// foldLines joins a wrapped continuation onto the line above it. It runs
// before prepare, so a continuation is joined even when the line it
// belongs to would otherwise be dropped, and the joined line keeps the
// number of the line it started on.
func foldLines(in *definition.Input, lines []line) ([]line, *splitError) {
	fold := in.FoldPattern()
	if fold == nil {
		return lines, nil
	}
	out := lines[:0:0]
	// The pieces of the line being joined are kept apart until the next
	// line that is not a continuation, so that a value wrapped over many
	// lines is joined once rather than once per line.
	var pieces []string
	flush := func() {
		if len(pieces) > 1 {
			out[len(out)-1].text = strings.Join(pieces, " ")
		}
		pieces = nil
	}
	for _, l := range lines {
		if !fold.MatchString(l.text) {
			flush()
			out = append(out, l)
			pieces = append(pieces, l.text)
			continue
		}
		// A blank line is nothing to join onto either: the join would
		// write a separator in front of the continuation that the text
		// does not hold.
		if len(out) == 0 || strings.TrimSpace(out[len(out)-1].text) == "" {
			return nil, &splitError{line: l.num, msg: fmt.Sprintf("continuation line with nothing to join it to: %q", l.text)}
		}
		pieces = append(pieces, strings.TrimSpace(l.text))
	}
	flush()
	return out, nil
}

// prepare drops ignored and blank lines, counting them in acct when it
// is not nil.
func prepare(in *definition.Input, lines []line, acct *Account) []line {
	// Blank lines before the text and after it are not part of it,
	// whatever skip_blank says: it keeps the blank lines that separate
	// things, and nothing is separated from what comes before the first
	// line or after the last. Where blank lines are kept, a line of spaces
	// may be a value (a file named " "), so only an empty line is taken
	// for the edge of the text there.
	skipBlank := in.SkipBlankLines()
	lead := 0
	for lead < len(lines) && edgeBlank(lines[lead].text, skipBlank) {
		lead++
	}
	trail := len(lines)
	for trail > lead && edgeBlank(lines[trail-1].text, skipBlank) {
		trail--
	}
	if acct != nil {
		acct.Blank += lead + len(lines) - trail
	}
	lines = lines[lead:trail]
	ignore := in.IgnorePatterns()
	if len(ignore) == 0 && !skipBlank {
		return lines
	}
	counts := make([]int, len(ignore))
	out := lines[:0:0]
	for _, l := range lines {
		if skipBlank && strings.TrimSpace(l.text) == "" {
			if acct != nil {
				acct.Blank++
			}
			continue
		}
		if i := firstMatching(ignore, l.text); i >= 0 {
			counts[i]++
			continue
		}
		out = append(out, l)
	}
	if acct != nil {
		for i, n := range counts {
			if n > 0 {
				acct.Ignored = append(acct.Ignored, Ignored{Index: i, Expr: in.Ignore[i], Lines: n})
			}
		}
	}
	return out
}

// edgeBlank reports whether a line before or after the text is blank:
// empty, or with skip_blank on, nothing but whitespace.
func edgeBlank(text string, skipBlank bool) bool {
	if skipBlank {
		return strings.TrimSpace(text) == ""
	}
	return text == ""
}

// firstMatching returns the index of the first expression that matches
// s, or -1.
func firstMatching(res []*regexp.Regexp, s string) int {
	for i, re := range res {
		if re.MatchString(s) {
			return i
		}
	}
	return -1
}

// dropIgnored removes the lines a part declared as belonging to a
// sibling.
func dropIgnored(ignore []*regexp.Regexp, lines []line) []line {
	if len(ignore) == 0 {
		return lines
	}
	out := lines[:0:0]
	for _, l := range lines {
		if !matchesAny(ignore, l.text) {
			out = append(out, l)
		}
	}
	return out
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// applySelect narrows lines with after/until/skip/limit. It also returns
// the line select.after matched, which is the heading of the region and
// not part of it, and the line select.until matched, which ends the
// region and is not part of it either; each is nil when there is none.
func applySelect(sel *definition.Select, lines []line) (region []line, heading, end *line) {
	if sel.IsZero() {
		return lines, nil, nil
	}
	if re := sel.CompiledAfter(); re != nil {
		found := false
		for i, l := range lines {
			if re.MatchString(l.text) {
				heading = &lines[i]
				lines = lines[i+1:]
				found = true
				break
			}
		}
		if !found {
			return nil, nil, nil
		}
	}
	if re := sel.CompiledUntil(); re != nil {
		for i, l := range lines {
			if re.MatchString(l.text) {
				end = &lines[i]
				lines = lines[:i]
				break
			}
		}
	}
	if sel.Skip > 0 {
		if sel.Skip >= len(lines) {
			return nil, heading, end
		}
		lines = lines[sel.Skip:]
	}
	if sel.Limit > 0 && sel.Limit < len(lines) {
		lines = lines[:sel.Limit]
	}
	return lines, heading, end
}

// markHeading records the line select.after matched as read, when the
// expression describes all of it. A heading is where a region starts, and
// an expression that states the whole of one has said everything there is
// to say about it; one that only matches how the line opens has not, and
// the rest of the line is then as unread as any other.
func (r *run) markHeading(sel *definition.Select, heading *line) {
	if heading != nil && sel.Heading(heading.text) {
		r.ledger.markOne(*heading)
	}
}

// markEnd records the line select.until matched at the top level as
// read, when the expression describes all of it: the line that closes the
// output ("The command completed successfully.") has been said everything
// about, and nothing else is given it. Every line after it is left out
// and unread, which is what refuses another command's output behind it.
func (r *run) markEnd(sel *definition.Select, end *line) {
	if end != nil && sel.End(end.text) {
		r.ledger.markOne(*end)
	}
}

type run struct {
	def  *definition.Definition
	opts Options
	// ledger records the lines the parsers read. It is nil where nothing
	// is being accounted for.
	ledger *ledger
	// values counts what the reading has retained so far. A stream
	// starts it over at every record it hands on.
	values int
}

// countValue records one more retained value and refuses the reading
// once there are more than the document may hold. The input limit does
// not see this: a table of many columns over rows of one letter retains
// thousands of times its size, and it is the product that is bounded.
func (r *run) countValue(ln int) error {
	r.values++
	if r.values <= r.opts.maxValues() {
		return nil
	}
	return &ParseError{Definition: r.def.ID(), Line: ln, Msg: fmt.Sprintf("the input yields more than %d values, more than one document holds; --stream reads it one record at a time", r.opts.maxValues()), Cause: ErrTooManyValues}
}

func (r *run) errorf(ln int, field, format string, args ...any) *ParseError {
	return &ParseError{Definition: r.def.ID(), Line: ln, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// parse runs one parser over its lines. A parser that reads line by line
// either reads every line it is given or fails on the one it cannot, so a
// success means all of them were read; the ones that decide for
// themselves which lines they read (composite, records and a pattern
// matched against the whole input) record that on their own.
func (r *run) parse(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	var (
		v   any
		err error
	)
	switch p.Type {
	case definition.TypeComposite:
		return r.parseComposite(p, lines)
	case definition.TypeRecords:
		return r.parseRecords(p, lines)
	case definition.TypeRegex:
		if p.Each == definition.EachInput {
			return r.parseRegex(p, fields, lines)
		}
		v, err = r.parseRegex(p, fields, lines)
	case definition.TypeTable:
		v, err = r.parseTable(p, fields, lines)
	case definition.TypeKV:
		v, err = r.parseKV(p, fields, lines)
	case definition.TypeCSV:
		v, err = r.parseCSV(p, fields, lines)
	case definition.TypeINI:
		v, err = r.parseINI(p, fields, lines)
	case definition.TypeTree:
		v, err = r.parseTree(p, lines)
	default:
		return nil, r.errorf(0, "", "unsupported parse type %q", p.Type)
	}
	if err != nil {
		return nil, err
	}
	r.ledger.mark(lines)
	return v, nil
}

func (r *run) parseComposite(p *definition.Parse, lines []line) (any, error) {
	obj := jsonutil.NewObject()
	for i := range p.Parts {
		part := &p.Parts[i]
		// The region comes first and the ignore list narrows it, so that
		// select.skip and select.limit count the lines as they stand in
		// the output rather than the ones left after dropping.
		// The line a part's until matches is left to its siblings.
		region, heading, _ := applySelect(&part.Select, lines)
		sub := dropIgnored(part.IgnorePatterns(), region)
		v, err := r.parse(&part.Parse, part.Fields, sub)
		if err != nil {
			var pe *ParseError
			if errors.As(err, &pe) && pe.Field == "" {
				pe.Msg = fmt.Sprintf("part %q: %s", part.Name, pe.Msg)
			}
			return nil, err
		}
		r.markHeading(&part.Select, heading)
		obj.Set(part.Name, v)
	}
	return obj, nil
}

// parseRecords handles type: records. A line matching start opens a
// record and everything up to the next such line belongs to it, which is
// what a report of repeating blocks looks like: an interface followed by
// its counters, a crate followed by its binaries. Each record is then
// read the way composite reads a whole input, so a definition that can
// describe one block can describe a file full of them.
func (r *run) parseRecords(p *definition.Parse, lines []line) (any, error) {
	start := p.CompiledStart()
	var groups [][]line
	for _, ln := range lines {
		if start.MatchString(ln.text) {
			groups = append(groups, []line{ln})
			continue
		}
		if len(groups) == 0 {
			return nil, r.errorf(ln.num, "", "line precedes the first record: %q", ln.text)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], ln)
	}
	out := make([]any, 0, len(groups))
	for _, g := range groups {
		v, err := r.parseRecord(p, g)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// parseRecord reads one block of a records parser: by its named regions,
// or, where the block is one value, by the single parser over the whole
// of it.
func (r *run) parseRecord(p *definition.Parse, block []line) (any, error) {
	if p.Record != nil {
		return r.parse(&p.Record.Parse, p.Record.Fields, block)
	}
	return r.parseComposite(p, block)
}

// parseRegex handles type: regex. Several patterns are tried in the
// order the definition lists them; the first one that matches decides how
// the line is read.
func (r *run) parseRegex(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	patterns := p.CompiledPatterns()
	if p.Each == definition.EachInput {
		texts := make([]string, len(lines))
		for i, l := range lines {
			texts[i] = l.text
		}
		text := strings.Join(texts, "\n")
		re, m, which := firstMatch(patterns, text)
		if m == nil {
			return nil, r.errorf(0, "", "input does not match %s", describePatterns(p))
		}
		obj, err := r.objectFromMatch(re, text, m, p.PatternValues(which), fields, firstLine(lines))
		if err != nil {
			return nil, err
		}
		if err := r.markSpan(lines, m[0], m[1]); err != nil {
			return nil, err
		}
		return obj, nil
	}
	out := make([]any, 0, len(lines))
	for _, l := range lines {
		re, m, which := firstMatch(patterns, l.text)
		if m == nil {
			return nil, r.errorf(l.num, "", "line does not match %s: %q", describePatterns(p), truncate(l.text, 80))
		}
		if err := r.checkWhole(l, m[0], m[1]); err != nil {
			return nil, err
		}
		obj, err := r.objectFromMatch(re, l.text, m, p.PatternValues(which), fields, l.num)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

// firstMatch returns the first pattern that matches and its submatch
// indices.
func firstMatch(patterns []*regexp.Regexp, text string) (*regexp.Regexp, []int, int) {
	for i, re := range patterns {
		if m := re.FindStringSubmatchIndex(text); m != nil {
			return re, m, i
		}
	}
	return nil, nil, -1
}

// describePatterns renders the alternatives for an error message.
func describePatterns(p *definition.Parse) string {
	sources := p.PatternSources()
	if len(sources) == 1 {
		return "pattern " + shortPattern(sources[0])
	}
	parts := make([]string, len(sources))
	for i, s := range sources {
		parts[i] = shortPattern(s)
	}
	return fmt.Sprintf("any of the %d patterns %s", len(sources), strings.Join(parts, ", "))
}

func firstLine(lines []line) int {
	if len(lines) == 0 {
		return 0
	}
	return lines[0].num
}

func shortPattern(p string) string {
	return "/" + truncate(p, 60) + "/"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// objectFromMatch builds an object from a submatch index slice. vals are
// the fixed values the pattern that matched adds, which come first.
func (r *run) objectFromMatch(re *regexp.Regexp, text string, m []int, vals []definition.Value, fields map[string]*definition.Field, ln int) (*jsonutil.Object, error) {
	obj := jsonutil.NewObject()
	for _, v := range vals {
		if err := r.setField(obj, v.Name, some(v.Value), fields[v.Name], ln); err != nil {
			return nil, err
		}
	}
	names := re.SubexpNames()
	present := presentGroups(re, m, fields)
	for i, name := range names {
		if name == "" {
			continue
		}
		if err := r.setMatched(obj, name, group(text, m, i), fields[name], ln, present); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// group returns what the i-th group of a match read: its text, or
// nothing when it took no part in the match.
func group(text string, m []int, i int) raw {
	if m[2*i] < 0 {
		return raw{}
	}
	return some(text[m[2*i]:m[2*i+1]])
}

// presentGroups lists the named groups of a match that took part in it
// with some text, which is what an unescape rule's when is decided by.
// It is nil when no rule among fields asks, which is nearly every line
// of nearly every format, so the list is not made for those.
func presentGroups(re *regexp.Regexp, m []int, fields map[string]*definition.Field) map[string]bool {
	if !asksPresent(fields) {
		return nil
	}
	present := map[string]bool{}
	for i, name := range re.SubexpNames() {
		if name != "" && m[2*i] >= 0 && m[2*i+1] > m[2*i] {
			present[name] = true
		}
	}
	return present
}

// asksPresent reports whether a rule among fields decides by which
// groups took part in the match.
func asksPresent(fields map[string]*definition.Field) bool {
	for _, f := range fields {
		if f != nil && f.Unescape != nil && f.Unescape.When != "" {
			return true
		}
	}
	return false
}

// unquote removes one matching pair of surrounding quotes, which is what
// a shell-quoted file such as /etc/os-release writes around a value that
// contains spaces.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q == '"' || q == '\'') && s[len(s)-1] == q {
		return s[1 : len(s)-1]
	}
	return s
}

// parseKV handles type: kv.
func (r *run) parseKV(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	sep := p.Separator
	if sep == "" {
		sep = "="
	}
	const keyName, valueName = "name", "value"
	asMap := p.As == definition.AsMap
	var list []any
	var obj *jsonutil.Object
	if asMap {
		obj = jsonutil.NewObject()
	} else {
		list = make([]any, 0, len(lines))
	}
	trim := p.TrimCells()
	var keys keyLines
	for _, l := range lines {
		idx := strings.Index(l.text, sep)
		key := ""
		if idx >= 0 {
			key = l.text[:idx]
			if trim {
				key = strings.TrimSpace(key)
			}
		}
		if idx < 0 || key == "" {
			return nil, r.errorf(l.num, "", "expected \"key%svalue\": %q", sep, truncate(l.text, 80))
		}
		value := l.text[idx+len(sep):]
		if trim {
			value = strings.TrimSpace(value)
		}
		if p.Unquote {
			value = unquote(value)
		}
		if asMap {
			if err := keys.add(r, key, l.num); err != nil {
				return nil, err
			}
			if err := r.setField(obj, key, some(value), fields[key], l.num); err != nil {
				return nil, err
			}
			continue
		}
		entry := jsonutil.NewObject()
		entry.Set(keyName, key)
		if err := r.setField(entry, valueName, some(value), fields[key], l.num); err != nil {
			return nil, err
		}
		list = append(list, entry)
	}
	if asMap {
		return obj, nil
	}
	return list, nil
}

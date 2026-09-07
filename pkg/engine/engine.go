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
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// Default resource limits.
const (
	DefaultMaxInputSize  = 64 * 1024 * 1024
	DefaultMaxLineLength = 1024 * 1024
)

// Options tunes a parse run.
type Options struct {
	// MaxInputSize bounds the accepted input in bytes (0 = default).
	MaxInputSize int64
	// MaxLineLength bounds a single line in bytes (0 = default).
	MaxLineLength int
}

func (o Options) maxInput() int64 {
	if o.MaxInputSize <= 0 {
		return DefaultMaxInputSize
	}
	return o.MaxInputSize
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

// ErrLineTooLong is wrapped when a single line exceeds the limit.
var ErrLineTooLong = errors.New("line too long")

// Parse applies def to input. The result is either []any (one ordered
// object per record) or *jsonutil.Object, depending on the parse type.
func Parse(def *definition.Definition, input []byte, opts Options) (any, error) {
	if int64(len(input)) > opts.maxInput() {
		return nil, &ParseError{Definition: def.ID(), Msg: fmt.Sprintf("input exceeds %d bytes", opts.maxInput()), Cause: ErrInputTooLarge}
	}
	lines, err := splitRecords(input, def.Input.Separator(), opts.maxLine())
	if err != nil {
		return nil, &ParseError{Definition: def.ID(), Line: err.line, Msg: err.msg, Cause: err.cause}
	}
	lines, ferr := foldLines(&def.Input, lines)
	if ferr != nil {
		return nil, &ParseError{Definition: def.ID(), Line: ferr.line, Msg: ferr.msg, Cause: ferr.cause}
	}
	lines = prepare(&def.Input, lines)
	lines = applySelect(&def.Input.Select, lines)
	r := &run{def: def, opts: opts}
	return r.parse(&def.Parse, def.Fields, lines)
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

// splitRecords cuts the input into records on sep, which is a newline for
// ordinary command output and NUL for the record-separated output of
// tools such as `env -0`.
func splitRecords(input []byte, sep byte, maxLen int) ([]line, *splitError) {
	if len(input) == 0 {
		return nil, nil
	}
	input = bytes.TrimPrefix(input, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	// Detection strips these too; doing it here as well keeps the
	// escapes out of the values when a parser is named instead.
	input = convert.StripANSI(input)
	if !utf8.Valid(input) {
		return nil, &splitError{msg: "input is not valid UTF-8"}
	}
	parts := bytes.Split(input, []byte{sep})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1] // trailing separator
	}
	out := make([]line, 0, len(parts))
	for i, p := range parts {
		if len(p) > maxLen {
			return nil, &splitError{line: i + 1, msg: fmt.Sprintf("record exceeds %d bytes", maxLen), cause: ErrLineTooLong}
		}
		if sep == '\n' {
			p = bytes.TrimSuffix(p, []byte{'\r'})
		}
		out = append(out, line{text: string(p), num: i + 1})
	}
	return out, nil
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
	for _, l := range lines {
		if !fold.MatchString(l.text) {
			out = append(out, l)
			continue
		}
		if len(out) == 0 {
			return nil, &splitError{line: l.num, msg: fmt.Sprintf("continuation line with nothing to join it to: %q", l.text)}
		}
		out[len(out)-1].text += " " + strings.TrimSpace(l.text)
	}
	return out, nil
}

// prepare drops ignored and blank lines.
func prepare(in *definition.Input, lines []line) []line {
	ignore := in.IgnorePatterns()
	skipBlank := in.SkipBlankLines()
	if len(ignore) == 0 && !skipBlank {
		return lines
	}
	out := lines[:0:0]
	for _, l := range lines {
		if skipBlank && strings.TrimSpace(l.text) == "" {
			continue
		}
		if matchesAny(ignore, l.text) {
			continue
		}
		out = append(out, l)
	}
	return out
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

// applySelect narrows lines with after/until/skip/limit.
func applySelect(sel *definition.Select, lines []line) []line {
	if sel.IsZero() {
		return lines
	}
	if re := sel.CompiledAfter(); re != nil {
		found := false
		for i, l := range lines {
			if re.MatchString(l.text) {
				lines = lines[i+1:]
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if re := sel.CompiledUntil(); re != nil {
		for i, l := range lines {
			if re.MatchString(l.text) {
				lines = lines[:i]
				break
			}
		}
	}
	if sel.Skip > 0 {
		if sel.Skip >= len(lines) {
			return nil
		}
		lines = lines[sel.Skip:]
	}
	if sel.Limit > 0 && sel.Limit < len(lines) {
		lines = lines[:sel.Limit]
	}
	return lines
}

type run struct {
	def  *definition.Definition
	opts Options
}

func (r *run) errorf(ln int, field, format string, args ...any) *ParseError {
	return &ParseError{Definition: r.def.ID(), Line: ln, Field: field, Msg: fmt.Sprintf(format, args...)}
}

func (r *run) parse(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	switch p.Type {
	case definition.TypeTable:
		return r.parseTable(p, fields, lines)
	case definition.TypeRegex:
		return r.parseRegex(p, fields, lines)
	case definition.TypeKV:
		return r.parseKV(p, fields, lines)
	case definition.TypeComposite:
		return r.parseComposite(p, lines)
	case definition.TypeRecords:
		return r.parseRecords(p, lines)
	default:
		return nil, r.errorf(0, "", "unsupported parse type %q", p.Type)
	}
}

func (r *run) parseComposite(p *definition.Parse, lines []line) (any, error) {
	obj := jsonutil.NewObject()
	for i := range p.Parts {
		part := &p.Parts[i]
		// The region comes first and the ignore list narrows it, so that
		// select.skip and select.limit count the lines as they stand in
		// the output rather than the ones left after dropping.
		sub := dropIgnored(part.IgnorePatterns(), applySelect(&part.Select, lines))
		v, err := r.parse(&part.Parse, part.Fields, sub)
		if err != nil {
			var pe *ParseError
			if errors.As(err, &pe) && pe.Field == "" {
				pe.Msg = fmt.Sprintf("part %q: %s", part.Name, pe.Msg)
			}
			return nil, err
		}
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
		v, err := r.parseComposite(p, g)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
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
		re, m := firstMatch(patterns, text)
		if m == nil {
			return nil, r.errorf(0, "", "input does not match %s", describePatterns(p))
		}
		return r.objectFromMatch(re, text, m, fields, firstLine(lines))
	}
	out := make([]any, 0, len(lines))
	for _, l := range lines {
		re, m := firstMatch(patterns, l.text)
		if m == nil {
			return nil, r.errorf(l.num, "", "line does not match %s: %q", describePatterns(p), truncate(l.text, 80))
		}
		obj, err := r.objectFromMatch(re, l.text, m, fields, l.num)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

// firstMatch returns the first pattern that matches and its submatch
// indices.
func firstMatch(patterns []*regexp.Regexp, text string) (*regexp.Regexp, []int) {
	for _, re := range patterns {
		if m := re.FindStringSubmatchIndex(text); m != nil {
			return re, m
		}
	}
	return nil, nil
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

// objectFromMatch builds an object from a submatch index slice.
func (r *run) objectFromMatch(re *regexp.Regexp, text string, m []int, fields map[string]*definition.Field, ln int) (*jsonutil.Object, error) {
	obj := jsonutil.NewObject()
	names := re.SubexpNames()
	for i, name := range names {
		if name == "" {
			continue
		}
		var raw any
		if m[2*i] >= 0 {
			raw = text[m[2*i]:m[2*i+1]]
		}
		if err := r.setField(obj, name, raw, fields[name], ln); err != nil {
			return nil, err
		}
	}
	return obj, nil
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
			if err := r.setField(obj, key, value, fields[key], l.num); err != nil {
				return nil, err
			}
			continue
		}
		entry := jsonutil.NewObject()
		entry.Set(keyName, key)
		if err := r.setField(entry, valueName, value, fields[key], l.num); err != nil {
			return nil, err
		}
		list = append(list, entry)
	}
	if asMap {
		return obj, nil
	}
	return list, nil
}

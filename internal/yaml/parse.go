// Package yaml reads the YAML that definitions, registry manifests and
// fixture metadata are written in, and decodes it into Go values.
//
// It reads the part of YAML that such files use: block and flow mappings
// and sequences, plain, quoted and block scalars, comments, and one
// document per file. It refuses what they do not use, anchors, aliases,
// tags and keys that are not one scalar, with a message that says so,
// rather than reading them half. Every error names the line it was
// found on.
//
// A parser definition may come from a registry jz was pointed at, so
// nothing here trusts the text: the depth of nesting is bounded, and no
// input makes the parser panic.
package yaml

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/limits"
)

// Kind is what a node holds.
type Kind int

// The kinds of node.
const (
	ScalarNode Kind = iota + 1
	SequenceNode
	MappingNode
)

// Node is one value of a document.
type Node struct {
	Kind Kind
	// Value is the text of a scalar.
	Value string
	// Plain reports a scalar written bare, without quotes or a block
	// indicator. Only such a scalar can be read as null, a number or a
	// bool; a quoted one is the string it spells.
	Plain bool
	// Items are the elements of a sequence.
	Items []*Node
	// Pairs are the entries of a mapping in the order written.
	Pairs []Pair
	// Line and Col are where the node starts, both counted from 1.
	Line, Col int
}

// Pair is one entry of a mapping.
type Pair struct {
	Key, Value *Node
}

// IsNull reports a plain scalar that spells null, or the absence of a
// value.
func (n *Node) IsNull() bool {
	if n == nil {
		return true
	}
	if n.Kind != ScalarNode || !n.Plain {
		return false
	}
	switch n.Value {
	case "", "~", "null", "Null", "NULL":
		return true
	}
	return false
}

// SyntaxError is text that is not the YAML this package reads.
type SyntaxError struct {
	Line, Col int
	Msg       string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// MaxDepth bounds how deeply collections may nest. It is the depth a
// JSON document is held to as well, so the same nesting is read or
// refused whichever of the two it is written in.
const MaxDepth = limits.MaxDepth

// parser walks the text by byte offset. Every position it takes has a
// line and a column, which is what errors are reported with.
type parser struct {
	src   []byte
	lines []int // offset of every line start
	depth int
}

// Parse reads one document. An empty document, or one holding only
// comments, is a nil node.
func Parse(data []byte) (*Node, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	p := &parser{src: data, lines: []int{0}}
	for i, b := range data {
		if b == '\n' {
			p.lines = append(p.lines, i+1)
		}
	}
	if off, r, ok := forbidden(data); !ok {
		return nil, p.errorf(off, "the character U+%04X is not allowed in YAML text", r)
	}
	return p.document()
}

// forbidden finds the first character YAML does not allow in text, which
// is every control character but tab, line feed and carriage return, and
// the two non-characters at the end of the basic plane. A character
// written as an escape (\0 in a double quoted scalar) is not one of
// these: what is refused is the byte itself standing in the text, where
// it would be read as part of a value and handed on without ever being
// seen.
//
// Bytes that are not valid UTF-8 are left to the caller, which says so
// in its own words: they decode as U+FFFD, which is allowed.
func forbidden(data []byte) (off int, r rune, ok bool) {
	for i := 0; i < len(data); {
		b := data[i]
		if b >= 0x20 && b < 0x7F || b == '\t' || b == '\n' || b == '\r' {
			i++
			continue
		}
		if b < 0x80 {
			return i, rune(b), false
		}
		c, size := utf8.DecodeRune(data[i:])
		switch {
		case c == 0x85, c >= 0xA0 && c <= 0xD7FF, c >= 0xE000 && c <= 0xFFFD, c >= 0x10000:
		default:
			return i, c, false
		}
		i += size
	}
	return 0, 0, true
}

// lineCol returns the 1-based line and column of an offset.
func (p *parser) lineCol(off int) (line, col int) {
	i := sort.Search(len(p.lines), func(i int) bool { return p.lines[i] > off }) - 1
	return i + 1, off - p.lines[i] + 1
}

// col returns the 0-based column of an offset.
func (p *parser) col(off int) int {
	_, c := p.lineCol(off)
	return c - 1
}

func (p *parser) errorf(off int, format string, args ...any) error {
	line, col := p.lineCol(off)
	return &SyntaxError{Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) at(off int) byte {
	if off < len(p.src) {
		return p.src[off]
	}
	return '\n'
}

func (p *parser) eof(off int) bool { return off >= len(p.src) }

// eol returns the offset of the line break that ends the line holding
// off, or the end of the text.
func (p *parser) eol(off int) int {
	if i := bytes.IndexByte(p.src[min(off, len(p.src)):], '\n'); i >= 0 {
		return off + i
	}
	return len(p.src)
}

// nextLine returns the offset of the line after the one holding off.
func (p *parser) nextLine(off int) int {
	return min(p.eol(off)+1, len(p.src))
}

// skipSpace moves past the spaces and tabs of the current line.
func (p *parser) skipSpace(off int) int {
	for off < len(p.src) && (p.src[off] == ' ' || p.src[off] == '\t') {
		off++
	}
	return off
}

// isBreak reports a line break or the end of the text.
func (p *parser) isBreak(off int) bool { return off >= len(p.src) || p.src[off] == '\n' }

// lineEnds reports whether the rest of the line from off is blank or a
// comment.
func (p *parser) lineEnds(off int) bool {
	off = p.skipSpace(off)
	return p.isBreak(off) || p.src[off] == '#'
}

// nextContent returns the offset of the first content character on the
// first line after the one holding off that holds any, skipping blank
// lines and comment lines, or the end of the text. A tab in the
// indentation of such a line is refused.
func (p *parser) nextContent(off int) (int, error) {
	off = p.nextLine(off)
	for off < len(p.src) {
		c := off
		for c < len(p.src) && p.src[c] == ' ' {
			c++
		}
		switch p.at(c) {
		case '\n':
			off = p.nextLine(c)
			continue
		case '#':
			off = p.nextLine(c)
			continue
		case '\t':
			t := p.skipSpace(c)
			if p.isBreak(t) || p.src[t] == '#' {
				off = p.nextLine(t)
				continue
			}
			return 0, p.errorf(c, "a tab is not indentation; indent with spaces")
		}
		return c, nil
	}
	return len(p.src), nil
}

// document reads the one document of the text.
func (p *parser) document() (*Node, error) {
	off, err := p.firstContent()
	if err != nil {
		return nil, err
	}
	if p.eof(off) {
		return nil, nil //nolint:nilnil // an empty document has no node
	}
	if p.hasPrefix(off, "---") && p.separates(off+3) {
		off = p.skipSpace(off + 3)
		if p.lineEnds(off) {
			if off, err = p.nextContent(off); err != nil {
				return nil, err
			}
			if p.eof(off) {
				return nil, nil //nolint:nilnil // an empty document has no node
			}
		}
	}
	if p.hasPrefix(off, "...") && p.separates(off+3) {
		return nil, nil //nolint:nilnil // an empty document has no node
	}
	n, next, err := p.block(off, -1)
	if err != nil {
		return nil, err
	}
	if next, err = p.nextContent(next); err != nil {
		return nil, err
	}
	if p.eof(next) {
		return n, nil
	}
	if p.hasPrefix(next, "...") && p.separates(next+3) {
		if next, err = p.nextContent(next); err != nil {
			return nil, err
		}
		if p.eof(next) {
			return n, nil
		}
	}
	if p.hasPrefix(next, "---") && p.separates(next+3) {
		return nil, p.errorf(next, "a file holds one document")
	}
	return nil, p.errorf(next, "unexpected text after the document")
}

// firstContent is nextContent for the first line of the text.
func (p *parser) firstContent() (int, error) {
	if len(p.src) == 0 {
		return 0, nil
	}
	c := 0
	for c < len(p.src) && p.src[c] == ' ' {
		c++
	}
	switch p.at(c) {
	case '\n', '#':
		return p.nextContent(c)
	case '\t':
		if p.lineEnds(c) {
			return p.nextContent(c)
		}
		return 0, p.errorf(c, "a tab is not indentation; indent with spaces")
	}
	return c, nil
}

func (p *parser) hasPrefix(off int, s string) bool {
	return bytes.HasPrefix(p.src[min(off, len(p.src)):], []byte(s))
}

// separates reports that off is a line break, a space or the end, which
// is what has to follow "---", "..." and a "-" that opens an item.
func (p *parser) separates(off int) bool {
	return p.isBreak(off) || p.src[off] == ' ' || p.src[off] == '\t'
}

// dash reports a "-" that opens a sequence item.
func (p *parser) dash(off int) bool {
	return p.at(off) == '-' && p.separates(off+1)
}

// enter counts one collection the reading is inside of. Only a
// sequence, a mapping and a flow collection count, so the depth is the
// nesting a JSON document of the same shape would be held to, and the
// same document is refused whichever of the two it is written in. Every
// way the parser recurses goes through one of them.
func (p *parser) enter(off int) error {
	p.depth++
	if p.depth > MaxDepth {
		return p.errorf(off, "nested deeper than %d levels", MaxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

// block reads the node that starts at off, a content character on a
// line indented deeper than parent, and returns it with the offset
// where it ended.
func (p *parser) block(off, parent int) (*Node, int, error) {
	c := p.col(off)
	if p.dash(off) {
		return p.sequence(off, c)
	}
	if ok, err := p.keyAhead(off); err != nil {
		return nil, 0, err
	} else if ok {
		return p.mapping(off, c)
	}
	return p.value(off, parent)
}

// keyAhead reports whether the text at off opens a mapping entry: a
// scalar key and then ":" followed by a space or the end of the line.
func (p *parser) keyAhead(off int) (bool, error) {
	switch p.at(off) {
	case '"', '\'':
		end, err := p.quotedEnd(off)
		if err != nil {
			return false, nil //nolint:nilerr // not a key; value reads it and reports
		}
		end = p.skipSpace(end)
		return p.at(end) == ':' && p.separates(end+1), nil
	case '[', '{', '|', '>', '#':
		return false, nil
	case '?':
		if p.separates(off + 1) {
			return true, nil
		}
	case '&', '*', '!':
		return false, p.errorf(off, "anchors, aliases and tags are not supported")
	case '@', '`':
		return false, p.errorf(off, "%q cannot start a value", p.src[off])
	}
	_, ok := p.plainKeyEnd(off)
	return ok, nil
}

// plainKeyEnd returns the offset of the ":" that ends a plain key
// starting at off, when there is one on the line.
func (p *parser) plainKeyEnd(off int) (int, bool) {
	for i := off; i < len(p.src) && p.src[i] != '\n'; i++ {
		switch p.src[i] {
		case ':':
			if p.separates(i + 1) {
				return i, true
			}
		case '#':
			if i > off && (p.src[i-1] == ' ' || p.src[i-1] == '\t') {
				return 0, false
			}
		}
	}
	return 0, false
}

// quotedEnd returns the offset just past the quoted scalar starting at
// off, on this line only.
func (p *parser) quotedEnd(off int) (int, error) {
	q := p.src[off]
	for i := off + 1; i < len(p.src) && p.src[i] != '\n'; i++ {
		switch {
		case q == '"' && p.src[i] == '\\':
			i++
		case p.src[i] == q && q == '\'' && p.at(i+1) == '\'':
			i++
		case p.src[i] == q:
			return i + 1, nil
		}
	}
	return 0, p.errorf(off, "unterminated quoted key")
}

// mapping reads a block mapping whose keys stand at column c.
func (p *parser) mapping(off, c int) (*Node, int, error) {
	if err := p.enter(off); err != nil {
		return nil, 0, err
	}
	defer p.leave()
	line, col := p.lineCol(off)
	n := &Node{Kind: MappingNode, Line: line, Col: col}
	seen := map[string]bool{}
	for {
		key, after, err := p.key(off)
		if err != nil {
			return nil, 0, err
		}
		if seen[key.Value] {
			return nil, 0, p.errorf(off, "duplicate key %q", key.Value)
		}
		seen[key.Value] = true
		var value *Node
		next := p.skipSpace(after)
		if p.lineEnds(next) {
			value, next, err = p.nested(next, c)
		} else {
			value, next, err = p.value(next, c)
		}
		if err != nil {
			return nil, 0, err
		}
		n.Pairs = append(n.Pairs, Pair{Key: key, Value: value})
		if off, err = p.nextContent(next); err != nil {
			return nil, 0, err
		}
		if p.eof(off) || p.col(off) < c || p.marker(off) {
			return n, next, nil
		}
		if p.col(off) > c {
			return nil, 0, p.errorf(off, "unexpected indentation")
		}
		ok, err := p.keyAhead(off)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			if p.dash(off) {
				return nil, 0, p.errorf(off, "a sequence item where a key was expected")
			}
			return nil, 0, p.errorf(off, "expected a key")
		}
	}
}

// key reads a mapping key at off and returns it with the offset just
// past its ":".
func (p *parser) key(off int) (*Node, int, error) {
	line, col := p.lineCol(off)
	if p.at(off) == '?' && p.separates(off+1) {
		return p.explicitKey(off)
	}
	if p.at(off) == '"' || p.at(off) == '\'' {
		text, end, err := p.quoted(off)
		if err != nil {
			return nil, 0, err
		}
		end = p.skipSpace(end)
		if p.at(end) != ':' || !p.separates(end+1) {
			return nil, 0, p.errorf(end, "expected \":\" after the key")
		}
		return &Node{Kind: ScalarNode, Value: text, Line: line, Col: col}, end + 1, nil
	}
	end, ok := p.plainKeyEnd(off)
	if !ok {
		return nil, 0, p.errorf(off, "expected a key")
	}
	text := strings.TrimRight(string(p.src[off:end]), " \t")
	if text == "" {
		return nil, 0, p.errorf(off, "empty key")
	}
	return &Node{Kind: ScalarNode, Value: text, Plain: true, Line: line, Col: col}, end + 1, nil
}

// explicitKey reads a key written after "? " on a line of its own, with
// its ":" at the start of the next, which is how a key too long to be
// written before its ":" is written. The key is one scalar; a
// collection as a key has no field to name.
func (p *parser) explicitKey(off int) (*Node, int, error) {
	line, col := p.lineCol(off)
	c := p.col(off)
	start := p.skipSpace(off + 1)
	var (
		text string
		end  int
		err  error
	)
	switch p.at(start) {
	case '"', '\'':
		if text, end, err = p.quoted(start); err != nil {
			return nil, 0, err
		}
		if !p.lineEnds(end) {
			return nil, 0, p.errorf(end, "unexpected text after the quoted key")
		}
	case '[', '{', '\n', '#':
		return nil, 0, p.errorf(start, "a key written after \"? \" is one scalar")
	default:
		text, end = p.plainLine(start)
	}
	next, err := p.nextContent(end)
	if err != nil {
		return nil, 0, err
	}
	if p.eof(next) || p.col(next) != c || p.at(next) != ':' || !p.separates(next+1) {
		return nil, 0, p.errorf(end, "a key written after \"? \" needs its \":\" at the start of the next line")
	}
	return &Node{Kind: ScalarNode, Value: text, Plain: p.at(start) != '"' && p.at(start) != '\'', Line: line, Col: col}, next + 1, nil
}

// nested reads the value of a key or an item whose line ends after the
// indicator: the node on the lines below, indented deeper than c, a
// sequence at column c itself, which a mapping value may be, or nothing.
func (p *parser) nested(off, c int) (*Node, int, error) {
	next, err := p.nextContent(off)
	if err != nil {
		return nil, 0, err
	}
	if p.eof(next) || p.col(next) < c {
		return nil, off, nil
	}
	if p.col(next) == c {
		if p.dash(next) && !p.dash(off) {
			return p.sequence(next, c)
		}
		return nil, off, nil
	}
	return p.block(next, c)
}

// sequence reads a block sequence whose dashes stand at column c.
func (p *parser) sequence(off, c int) (*Node, int, error) {
	if err := p.enter(off); err != nil {
		return nil, 0, err
	}
	defer p.leave()
	line, col := p.lineCol(off)
	n := &Node{Kind: SequenceNode, Line: line, Col: col}
	for {
		var (
			item *Node
			next int
			err  error
		)
		after := p.skipSpace(off + 1)
		if p.lineEnds(after) {
			item, next, err = p.nested(off, c)
		} else {
			item, next, err = p.block(after, c)
		}
		if err != nil {
			return nil, 0, err
		}
		n.Items = append(n.Items, item)
		if off, err = p.nextContent(next); err != nil {
			return nil, 0, err
		}
		if p.eof(off) || p.col(off) < c || p.marker(off) {
			return n, next, nil
		}
		if p.col(off) > c {
			return nil, 0, p.errorf(off, "unexpected indentation")
		}
		if !p.dash(off) {
			return n, next, nil
		}
	}
}

// value reads a scalar or a flow collection that starts at off, on the
// line of the key or dash it belongs to, whose continuation lines have
// to be indented deeper than parent.
func (p *parser) value(off, parent int) (*Node, int, error) {
	line, col := p.lineCol(off)
	n := &Node{Kind: ScalarNode, Line: line, Col: col}
	switch p.at(off) {
	case '|', '>':
		text, next, err := p.blockScalar(off, parent)
		if err != nil {
			return nil, 0, err
		}
		n.Value = text
		return n, next, nil
	case '[', '{':
		return p.flow(off)
	case '"', '\'':
		text, next, err := p.quoted(off)
		if err != nil {
			return nil, 0, err
		}
		n.Value = text
		if !p.lineEnds(next) {
			return nil, 0, p.errorf(next, "unexpected text after the quoted value")
		}
		return n, next, nil
	case '&', '*', '!':
		return nil, 0, p.errorf(off, "anchors, aliases and tags are not supported")
	case '@', '`':
		return nil, 0, p.errorf(off, "%q cannot start a value", p.src[off])
	}
	text, next, err := p.plain(off, parent)
	if err != nil {
		return nil, 0, err
	}
	n.Value, n.Plain = text, true
	return n, next, nil
}

// plainLine returns the text of a plain scalar's line from off to the
// comment or the line break, trimmed, with the offset of that end.
func (p *parser) plainLine(off int) (string, int) {
	end := off
	for end < len(p.src) && p.src[end] != '\n' {
		if p.src[end] == '#' && end > off && (p.src[end-1] == ' ' || p.src[end-1] == '\t') {
			break
		}
		end++
	}
	return strings.TrimRight(string(p.src[off:end]), " \t"), end
}

// plain reads a plain scalar in block context. A line indented deeper
// than parent continues it, a line break between two lines folds to a
// space, and an empty line is a line break kept.
func (p *parser) plain(off, parent int) (string, int, error) {
	first, next := p.plainLine(off)
	if strings.Contains(first, ": ") || strings.HasSuffix(first, ":") {
		return "", 0, p.errorf(off, "a mapping value is not allowed here")
	}
	var b strings.Builder
	b.WriteString(first)
	for {
		c, empty, err := p.continuation(next, parent)
		if err != nil {
			return "", 0, err
		}
		if c < 0 {
			return b.String(), next, nil
		}
		text, end := p.plainLine(c)
		if strings.Contains(text, ": ") || strings.HasSuffix(text, ":") {
			return "", 0, p.errorf(c, "a mapping value is not allowed here")
		}
		fold(&b, empty)
		b.WriteString(text)
		next = end
	}
}

// continuation returns the offset of the content on the next line when
// it continues a plain scalar whose parent stands at column parent: not
// a comment, indented deeper than parent, and not a document marker.
// It is -1 when the scalar ends. empty counts the blank lines skipped
// on the way, which the break between the lines stands for.
func (p *parser) continuation(off, parent int) (c, empty int, err error) {
	for off = p.nextLine(off); off < len(p.src); off = p.nextLine(off) {
		c = off
		for c < len(p.src) && p.src[c] == ' ' {
			c++
		}
		if p.isBreak(c) {
			empty++
			continue
		}
		if p.at(c) == '\t' {
			t := p.skipSpace(c)
			if p.isBreak(t) {
				empty++
				continue
			}
			if p.src[t] != '#' {
				return 0, 0, p.errorf(c, "a tab is not indentation; indent with spaces")
			}
			c = t
		}
		if p.src[c] == '#' || p.col(c) <= parent || p.marker(c) {
			return -1, 0, nil
		}
		return c, empty, nil
	}
	return -1, 0, nil
}

// marker reports a document marker at the start of a line, which ends
// whatever stands before it.
func (p *parser) marker(off int) bool {
	return p.col(off) == 0 && (p.hasPrefix(off, "---") || p.hasPrefix(off, "...")) && p.separates(off+3)
}

// fold writes what a line break stands for: a space, or a line break
// for each empty line that followed it.
func fold(b *strings.Builder, empty int) {
	if empty == 0 {
		b.WriteByte(' ')
		return
	}
	for range empty {
		b.WriteByte('\n')
	}
}

// quoted reads a single- or double-quoted scalar, which may run over
// several lines, and returns the offset just past the closing quote.
func (p *parser) quoted(off int) (string, int, error) {
	q := p.src[off]
	var b strings.Builder
	i := off + 1
	for {
		if i >= len(p.src) {
			return "", 0, p.errorf(off, "unterminated quoted value")
		}
		ch := p.src[i]
		switch {
		case ch == q && q == '\'' && p.at(i+1) == '\'':
			b.WriteByte('\'')
			i += 2
		case ch == q:
			return b.String(), i + 1, nil
		case ch == '\\' && q == '"':
			n, err := p.escape(&b, i)
			if err != nil {
				return "", 0, err
			}
			i = n
		case ch == '\n':
			// The break folds: the spaces around it are not part of the
			// value, and an empty line is a line break kept.
			trimmed := strings.TrimRight(b.String(), " \t")
			b.Reset()
			b.WriteString(trimmed)
			empty := 0
			j := i + 1
			for {
				k := p.skipSpace(j)
				if p.isBreak(k) && !p.eof(k) {
					empty++
					j = k + 1
					continue
				}
				j = k
				break
			}
			fold(&b, empty)
			i = j
		default:
			b.WriteByte(ch)
			i++
		}
	}
}

// escape writes what the escape at off stands for and returns the
// offset past it.
func (p *parser) escape(b *strings.Builder, off int) (int, error) {
	if off+1 >= len(p.src) {
		return 0, p.errorf(off, "unterminated escape")
	}
	c := p.src[off+1]
	simple := map[byte]string{
		'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v",
		'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': "\"", '/': "/", '\\': "\\",
		'N': "\xc2\x85", '_': "\xc2\xa0", 'L': "\xe2\x80\xa8", 'P': "\xe2\x80\xa9",
	}
	if s, ok := simple[c]; ok {
		b.WriteString(s)
		return off + 2, nil
	}
	digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}
	if n, ok := digits[c]; ok {
		hex := off + 2
		if hex+n > len(p.src) {
			return 0, p.errorf(off, "unterminated escape")
		}
		v, err := strconv.ParseInt(string(p.src[hex:hex+n]), 16, 32)
		if err != nil || !utf8.ValidRune(rune(v)) {
			return 0, p.errorf(off, "invalid escape %q", p.src[off:hex+n])
		}
		b.WriteRune(rune(v))
		return hex + n, nil
	}
	if c == '\n' {
		// An escaped line break joins the lines with nothing between.
		j := p.skipSpace(off + 2)
		return j, nil
	}
	return 0, p.errorf(off, "unknown escape \\%c", c)
}

// blockScalar reads a literal (|) or folded (>) scalar whose indicator
// stands at off and whose parent is at column parent.
func (p *parser) blockScalar(off, parent int) (string, int, error) {
	literal := p.src[off] == '|'
	chomp, explicit, i := p.blockHeader(off + 1)
	if !p.lineEnds(i) {
		return "", 0, p.errorf(i, "unexpected text after the block scalar indicator")
	}
	indent := -1
	if explicit > 0 {
		indent = max(parent, 0) + explicit
	}
	lines, next := p.blockLines(p.eol(i), parent, indent)
	// Trailing empty lines are what chomping decides about.
	trailing := 0
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailing++
	}
	var b strings.Builder
	if literal {
		b.WriteString(strings.Join(lines, "\n"))
	} else {
		foldLines(&b, lines)
	}
	if b.Len() > 0 && chomp != '-' {
		b.WriteByte('\n')
	}
	if chomp == '+' {
		for range trailing {
			b.WriteByte('\n')
		}
	}
	return b.String(), next, nil
}

// blockHeader reads what may follow a block scalar indicator, a
// chomping sign and an indentation digit in either order, and returns
// them with the offset after them.
func (p *parser) blockHeader(i int) (chomp byte, explicit, next int) {
	for ; i < len(p.src); i++ {
		switch c := p.src[i]; {
		case (c == '-' || c == '+') && chomp == 0:
			chomp = c
		case c >= '1' && c <= '9' && explicit == 0:
			explicit = int(c - '0')
		default:
			return chomp, explicit, i
		}
	}
	return chomp, explicit, i
}

// blockLines collects the lines of a block scalar after the line
// ending at next, stripped of their indentation: the given one, or the
// first content line's when it is not given. It returns them with the
// offset of the end of the last line taken.
func (p *parser) blockLines(next, parent, indent int) ([]string, int) {
	var lines []string
	for {
		start := p.nextLine(next)
		if start >= len(p.src) {
			return lines, next
		}
		end := p.eol(start)
		text := string(p.src[start:end])
		spaces := len(text) - len(strings.TrimLeft(text, " "))
		if spaces == len(text) {
			// A blank line belongs to the scalar whatever its width.
			lines = append(lines, "")
			next = end
			continue
		}
		if indent < 0 {
			if spaces <= parent {
				return lines, next
			}
			indent = spaces
		}
		if spaces < indent {
			return lines, next
		}
		lines = append(lines, text[indent:])
		next = end
	}
}

// foldLines joins the lines of a folded scalar: a break between two
// lines of text is a space, an empty line is a break kept, and a line
// indented deeper than the others keeps the breaks around it.
func foldLines(b *strings.Builder, lines []string) {
	prevText := false
	empty := 0
	for _, l := range lines {
		if l == "" {
			empty++
			continue
		}
		more := l[0] == ' ' || l[0] == '\t'
		switch {
		case b.Len() == 0 && !prevText:
			for range empty {
				b.WriteByte('\n')
			}
		case prevText && !more:
			fold(b, empty)
		default:
			b.WriteByte('\n')
			for range empty {
				b.WriteByte('\n')
			}
		}
		b.WriteString(l)
		prevText = !more
		empty = 0
	}
}

// flow reads a flow sequence or mapping starting at off. Line breaks
// and comments between its tokens are spacing.
func (p *parser) flow(off int) (*Node, int, error) {
	if err := p.enter(off); err != nil {
		return nil, 0, err
	}
	defer p.leave()
	if p.src[off] == '[' {
		return p.flowSequence(off)
	}
	return p.flowMapping(off)
}

// flowSequence reads "[a, b]".
func (p *parser) flowSequence(off int) (*Node, int, error) {
	line, col := p.lineCol(off)
	n := &Node{Kind: SequenceNode, Line: line, Col: col}
	i := off + 1
	for {
		var err error
		if i, err = p.flowSpace(i); err != nil {
			return nil, 0, err
		}
		if p.at(i) == ']' {
			return n, i + 1, nil
		}
		item, next, err := p.flowValue(i)
		if err != nil {
			return nil, 0, err
		}
		n.Items = append(n.Items, item)
		if i, err = p.flowSpace(next); err != nil {
			return nil, 0, err
		}
		switch p.at(i) {
		case ',':
			i++
		case ']':
			return n, i + 1, nil
		default:
			return nil, 0, p.errorf(i, "expected \",\" or \"]\"")
		}
	}
}

// flowMapping reads "{a: 1, b: 2}". A key with no ":" holds null.
func (p *parser) flowMapping(off int) (*Node, int, error) {
	line, col := p.lineCol(off)
	n := &Node{Kind: MappingNode, Line: line, Col: col}
	seen := map[string]bool{}
	i := off + 1
	for {
		var err error
		if i, err = p.flowSpace(i); err != nil {
			return nil, 0, err
		}
		if p.at(i) == '}' {
			return n, i + 1, nil
		}
		key, next, err := p.flowScalar(i)
		if err != nil {
			return nil, 0, err
		}
		if seen[key.Value] {
			return nil, 0, p.errorf(i, "duplicate key %q", key.Value)
		}
		seen[key.Value] = true
		value, next, err := p.flowPairValue(next)
		if err != nil {
			return nil, 0, err
		}
		n.Pairs = append(n.Pairs, Pair{Key: key, Value: value})
		switch p.at(next) {
		case ',':
			i = next + 1
		case '}':
			return n, next + 1, nil
		default:
			return nil, 0, p.errorf(next, "expected \",\" or \"}\"")
		}
	}
}

// flowPairValue reads what follows a key inside a flow mapping: nothing,
// or ":" and a value. It returns the offset of the "," or "}" after it.
func (p *parser) flowPairValue(off int) (*Node, int, error) {
	i, err := p.flowSpace(off)
	if err != nil {
		return nil, 0, err
	}
	if p.at(i) != ':' {
		return nil, i, nil
	}
	if i, err = p.flowSpace(i + 1); err != nil {
		return nil, 0, err
	}
	if p.at(i) == ',' || p.at(i) == '}' {
		return nil, i, nil
	}
	value, next, err := p.flowValue(i)
	if err != nil {
		return nil, 0, err
	}
	if next, err = p.flowSpace(next); err != nil {
		return nil, 0, err
	}
	return value, next, nil
}

// flowSpace moves past spacing inside a flow collection: spaces, line
// breaks and comments.
func (p *parser) flowSpace(off int) (int, error) {
	for {
		off = p.skipSpace(off)
		if p.eof(off) {
			return 0, p.errorf(off, "unterminated flow collection")
		}
		switch p.src[off] {
		case '\n':
			off++
		case '#':
			off = p.eol(off)
		default:
			return off, nil
		}
	}
}

// flowValue reads one value inside a flow collection.
func (p *parser) flowValue(off int) (*Node, int, error) {
	switch p.src[off] {
	case '[', '{':
		return p.flow(off)
	case '&', '*', '!':
		return nil, 0, p.errorf(off, "anchors, aliases and tags are not supported")
	case '|', '>':
		return nil, 0, p.errorf(off, "a block scalar cannot be written inside a flow collection")
	}
	return p.flowScalar(off)
}

// flowScalar reads a plain or quoted scalar inside a flow collection,
// where "," "]" "}" and ": " end a plain one.
func (p *parser) flowScalar(off int) (*Node, int, error) {
	line, col := p.lineCol(off)
	n := &Node{Kind: ScalarNode, Line: line, Col: col}
	if p.src[off] == '"' || p.src[off] == '\'' {
		text, next, err := p.quoted(off)
		if err != nil {
			return nil, 0, err
		}
		n.Value = text
		return n, next, nil
	}
	var b strings.Builder
	i := off
	lineStart := off
	for {
		if p.eof(i) {
			return nil, 0, p.errorf(off, "unterminated flow collection")
		}
		c := p.src[i]
		switch {
		case c == ',' || c == ']' || c == '}':
			goto done
		case c == ':' && (p.separates(i+1) || p.at(i+1) == ',' || p.at(i+1) == ']' || p.at(i+1) == '}'):
			goto done
		case c == '#' && i > lineStart && (p.src[i-1] == ' ' || p.src[i-1] == '\t'):
			goto done
		case c == '\n':
			// The break folds the way a quoted scalar's does: to a space,
			// or to the empty lines it stands before.
			trimmed := strings.TrimRight(b.String(), " \t")
			b.Reset()
			b.WriteString(trimmed)
			empty := 0
			j := i + 1
			for {
				k := p.skipSpace(j)
				if p.isBreak(k) && !p.eof(k) {
					empty++
					j = k + 1
					continue
				}
				j = k
				break
			}
			fold(&b, empty)
			i, lineStart = j, j
			continue
		}
		b.WriteByte(c)
		i++
	}
done:
	text := strings.TrimRight(b.String(), " \t")
	if text == "" {
		return nil, 0, p.errorf(off, "expected a value")
	}
	n.Value, n.Plain = text, true
	return n, i, nil
}

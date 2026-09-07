package engine

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/convert"
	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/jsonutil"
)

// NoStreamError reports a format whose records cannot be handed over one
// at a time. A composite result is one object built from the whole text,
// and a kv map or a regex matched against the whole input is one object
// too; none of them exists until the last line has been read.
type NoStreamError struct {
	Definition string
}

func (e *NoStreamError) Error() string {
	return fmt.Sprintf("format %s has no streaming form: it reads the whole output into one object", e.Definition)
}

// Stream reads input and calls emit once per record, as soon as that
// record is complete, instead of building the whole array first. It is
// the same reading as Parse, arranged so that a command that keeps
// printing (ping, vmstat 1) is answered while it is still running.
//
// Only a definition whose Parse.YieldsArray reports true has records to
// hand over one at a time; anything else is a NoStreamError.
func Stream(def *definition.Definition, r io.Reader, opts Options, emit func(any) error) error {
	if !def.Parse.YieldsArray() {
		return &NoStreamError{Definition: def.ID()}
	}
	s := &streamer{
		run:    run{def: def, opts: opts},
		p:      &def.Parse,
		fields: def.Fields,
		emit:   emit,
		fold:   def.Input.FoldPattern(),
		ignore: def.Input.IgnorePatterns(),
		blank:  def.Input.SkipBlankLines(),
		sel:    &def.Input.Select,
	}
	if def.Parse.Type == definition.TypeTable && def.Parse.Header.None {
		s.columns()
	}
	br := bufio.NewReaderSize(r, 64*1024)
	sep := def.Input.Separator()
	num := 0
	for !s.done {
		raw, err := readRecord(br, sep, opts.maxLine())
		if errors.Is(err, ErrLineTooLong) {
			return &ParseError{Definition: def.ID(), Line: num + 1, Msg: fmt.Sprintf("record exceeds %d bytes", opts.maxLine()), Cause: ErrLineTooLong}
		}
		if len(raw) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		num++
		if !utf8.Valid(raw) {
			return &ParseError{Definition: def.ID(), Line: num, Msg: "input is not valid UTF-8"}
		}
		text := string(convert.StripANSI(raw))
		if num == 1 {
			text = trimBOM(text)
		}
		if perr := s.feedRaw(line{text: text, num: num}); perr != nil {
			return perr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return s.finish()
}

// readRecord reads one record up to sep, refusing one longer than maxLen
// rather than letting a producer with no separators in its output grow
// the buffer without bound. The record comes back without the separator,
// and io.EOF alongside the last one.
func readRecord(br *bufio.Reader, sep byte, maxLen int) ([]byte, error) {
	var out []byte
	for {
		chunk, err := br.ReadSlice(sep)
		if len(out)+len(chunk) > maxLen+1 {
			return nil, ErrLineTooLong
		}
		out = append(out, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == sep {
			out = out[:len(out)-1]
		}
		if sep == '\n' {
			out = bytes.TrimSuffix(out, []byte{'\r'})
		}
		return out, err
	}
}

func trimBOM(s string) string {
	return string(bytes.TrimPrefix([]byte(s), []byte{0xEF, 0xBB, 0xBF}))
}

// streamer holds the state the batch reader keeps in slices: the line a
// fold is still waiting on, how far input.select has got, the resolved
// table header, and the record block that is still open.
type streamer struct {
	run
	p      *definition.Parse
	fields map[string]*definition.Field
	emit   func(any) error

	fold   *regexp.Regexp
	ignore []*regexp.Regexp
	blank  bool
	sel    *definition.Select

	// held is the line a fold may still be joined to.
	held    *line
	started bool // input.select.after has been seen
	skipped int
	taken   int
	done    bool

	cols   []column
	header bool
	block  []line
}

// feedRaw applies input.fold, which is the one stage that needs to see
// the next line before it can release the previous one.
func (s *streamer) feedRaw(l line) error {
	if s.fold == nil {
		return s.feedFolded(l)
	}
	if s.fold.MatchString(l.text) {
		if s.held == nil {
			return &ParseError{Definition: s.def.ID(), Line: l.num, Msg: fmt.Sprintf("continuation line with nothing to join it to: %q", l.text)}
		}
		s.held.text += " " + strings.TrimSpace(l.text)
		return nil
	}
	prev := s.held
	held := l
	s.held = &held
	if prev == nil {
		return nil
	}
	return s.feedFolded(*prev)
}

// feedFolded drops the lines input.ignore and skip_blank name.
func (s *streamer) feedFolded(l line) error {
	if s.blank && strings.TrimSpace(l.text) == "" {
		return nil
	}
	if matchesAny(s.ignore, l.text) {
		return nil
	}
	return s.feedSelected(l)
}

// feedSelected applies input.select in the order after, until, skip,
// limit, which is the order applySelect walks a slice in.
func (s *streamer) feedSelected(l line) error {
	if s.sel.CompiledAfter() != nil && !s.started {
		if s.sel.CompiledAfter().MatchString(l.text) {
			s.started = true
		}
		return nil
	}
	if re := s.sel.CompiledUntil(); re != nil && re.MatchString(l.text) {
		s.done = true
		return nil
	}
	if s.skipped < s.sel.Skip {
		s.skipped++
		return nil
	}
	if s.sel.Limit > 0 && s.taken >= s.sel.Limit {
		s.done = true
		return nil
	}
	s.taken++
	return s.feedRecord(l)
}

// feedRecord hands one line to the parser for its type.
func (s *streamer) feedRecord(l line) error {
	switch s.p.Type {
	case definition.TypeTable:
		return s.feedTable(l)
	case definition.TypeRegex:
		obj, err := s.regexObject(l)
		if err != nil {
			return err
		}
		return s.emit(obj)
	case definition.TypeKV:
		obj, err := s.kvEntry(l)
		if err != nil {
			return err
		}
		return s.emit(obj)
	case definition.TypeRecords:
		return s.feedBlock(l)
	default:
		return &NoStreamError{Definition: s.def.ID()}
	}
}

func (s *streamer) columns() {
	s.cols = make([]column, len(s.p.Header.Columns))
	for i, c := range s.p.Header.Columns {
		s.cols[i] = column{name: c}
	}
	s.header = true
}

func (s *streamer) feedTable(l line) error {
	if !s.header {
		// The header is the first line the selection let through, which
		// is where the batch reader takes it from too.
		cols, err := s.resolveHeader(s.p, l, s.split())
		if err != nil {
			return err
		}
		s.cols, s.header = cols, true
		return nil
	}
	obj, err := s.row(l)
	if err != nil {
		return err
	}
	return s.emit(obj)
}

func (s *streamer) split() string {
	if s.p.Split == "" {
		return definition.SplitWhitespace
	}
	return s.p.Split
}

func (s *streamer) row(l line) (*jsonutil.Object, error) {
	var (
		cells []any
		err   error
	)
	switch s.split() {
	case definition.SplitAligned:
		cells = alignedCells(l.text, s.cols)
	case definition.SplitDelimiter:
		cells, err = s.delimitedCells(s.p, l, len(s.cols))
	default:
		cells, err = s.whitespaceCells(s.p, l, len(s.cols))
	}
	if err != nil {
		return nil, err
	}
	obj := jsonutil.NewObject()
	for i, c := range s.cols {
		var raw any
		if i < len(cells) {
			raw = cells[i]
		}
		if err := s.setField(obj, c.name, raw, s.fields[c.name], l.num); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

func (s *streamer) regexObject(l line) (any, error) {
	re, m := firstMatch(s.p.CompiledPatterns(), l.text)
	if m == nil {
		return nil, s.errorf(l.num, "", "line does not match %s: %q", describePatterns(s.p), truncate(l.text, 80))
	}
	return s.objectFromMatch(re, l.text, m, s.fields, l.num)
}

func (s *streamer) kvEntry(l line) (any, error) {
	v, err := s.parseKV(s.p, s.fields, []line{l})
	if err != nil {
		return nil, err
	}
	list, ok := v.([]any)
	if !ok || len(list) != 1 {
		return nil, s.errorf(l.num, "", "expected one key/value entry")
	}
	return list[0], nil
}

// feedBlock accumulates a record and releases the previous one as soon as
// the next start line proves it is finished.
func (s *streamer) feedBlock(l line) error {
	if s.p.CompiledStart().MatchString(l.text) {
		if err := s.flushBlock(); err != nil {
			return err
		}
		s.block = []line{l}
		return nil
	}
	if len(s.block) == 0 {
		return s.errorf(l.num, "", "line precedes the first record: %q", l.text)
	}
	s.block = append(s.block, l)
	return nil
}

func (s *streamer) flushBlock() error {
	if len(s.block) == 0 {
		return nil
	}
	v, err := s.parseComposite(s.p, s.block)
	s.block = nil
	if err != nil {
		return err
	}
	return s.emit(v)
}

// finish releases what the lookahead and the open record were holding.
func (s *streamer) finish() error {
	if s.held != nil && !s.done {
		held := *s.held
		s.held = nil
		if err := s.feedFolded(held); err != nil {
			return err
		}
	}
	return s.flushBlock()
}

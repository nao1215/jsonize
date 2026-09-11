package engine

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// NoStreamError reports a format whose records cannot be handed over one
// at a time. A kv map, an ini file and a regex matched against the whole
// input are one object each, and none of them exists until the last line
// has been read.
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
// Only a definition whose Parse.Streams reports true has records to hand
// over one at a time; anything else is a NoStreamError. A composite is
// handed over part by part (see composite_stream.go).
//
// onError decides what a record jz could not read means. Returning nil
// keeps the stream going and the record is left out; returning an error
// ends the read with it, which is what a caller wants when a partial
// answer is worse than none. A nil onError is the second of those: the
// first failure ends the read. Only a record's own content reaches
// onError. A format with no streaming form, an emit that fails and a
// record over the length limit are not records to skip, so they end the
// read whatever onError says.
//
// An error from r other than io.EOF is returned as it is, and the record
// the read was in the middle of is not emitted: a reader that says its
// input was cut short gets the records before the cut and none after.
func Stream(def *definition.Definition, r io.Reader, opts Options, emit func(any) error, onError func(*ParseError) error) error {
	if !def.Parse.Streams() {
		return &NoStreamError{Definition: def.ID()}
	}
	s := &streamer{
		run:     run{def: def, opts: opts},
		p:       &def.Parse,
		fields:  def.Fields,
		emit:    emit,
		onError: onError,
		fold:    def.Input.FoldPattern(),
		ignore:  def.Input.IgnorePatterns(),
		blank:   def.Input.SkipBlankLines(),
		sel:     &def.Input.Select,
	}
	if def.Parse.Type == definition.TypeTable && def.Parse.Header.None {
		s.columns()
	}
	if def.Parse.Type == definition.TypeCSV && def.Parse.Header.None && len(def.Parse.Header.Columns) > 0 {
		s.csvCols = def.Parse.Header.Columns
	}
	if def.Parse.Type == definition.TypeComposite {
		s.startComposite()
	}
	br := bufio.NewReaderSize(r, 64*1024)
	sep := def.Input.Separator()
	num := 0
	// The whole input is read even once input.select has closed its
	// range: what follows the range still has to be accounted for, and a
	// line nobody reads there is as much a failure as one inside it.
	for {
		raw, err := readRecord(br, sep, opts.maxLine())
		if errors.Is(err, ErrLineTooLong) {
			return &ParseError{Definition: def.ID(), Line: num + 1, Msg: fmt.Sprintf("record exceeds %d bytes", opts.maxLine()), Cause: ErrLineTooLong}
		}
		// A read that fails ends the input where it stopped, which is not
		// where a record ends: the record it was in the middle of, and one
		// of several lines it left open, are not read.
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(raw) == 0 && err != nil {
			break
		}
		num++
		if rerr := s.feedRecordText(def, prepareRecord(raw, sep, num), num); rerr != nil {
			return unwrapStopped(rerr)
		}
		if err != nil {
			break
		}
	}
	if err := s.finish(); err != nil {
		return unwrapStopped(s.report(err))
	}
	return nil
}

// unwrapStopped hands back the error a composite stream stopped on as it
// was reported.
func unwrapStopped(err error) error {
	var done *reportedError
	if errors.As(err, &done) {
		return done.err
	}
	return err
}

// prepareRecord brings one record to the form the whole-document reader
// produces: the escape sequences come off first, then the carriage
// return of a CRLF ending, then the byte order mark of the first record.
// The order is the whole point — doing any of it the other way round
// made the two readings disagree on text neither of them should have
// treated specially.
func prepareRecord(raw []byte, sep byte, num int) []byte {
	out := convert.StripANSI(raw)
	if sep == '\n' {
		out = bytes.TrimSuffix(out, []byte{'\r'})
	}
	if num == 1 {
		out = bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	}
	return out
}

// feedRecordText hands one prepared record to the parser, reporting a
// record that is not valid UTF-8 the same way as one that does not fit.
func (s *streamer) feedRecordText(def *definition.Definition, text []byte, num int) error {
	if !utf8.Valid(text) {
		return s.report(&ParseError{Definition: def.ID(), Line: num, Msg: "input is not valid UTF-8"})
	}
	if def.Input.Separator() == '\n' && bytes.IndexByte(text, 0) >= 0 {
		return s.report(&ParseError{Definition: def.ID(), Line: num, Msg: nulInLine})
	}
	if perr := s.feedRaw(line{text: string(text), num: num}); perr != nil {
		return s.report(perr)
	}
	return nil
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
		// The carriage return of a CRLF line ending is trimmed by the
		// caller rather than here, because the whole-document reader
		// removes the escape sequences first and the two have to agree.
		return out, err
	}
}

// streamer holds the state the batch reader keeps in slices: the line a
// fold is still waiting on, how far input.select has got, the resolved
// table header, and the record block that is still open.
type streamer struct {
	run
	p       *definition.Parse
	fields  map[string]*definition.Field
	emit    func(any) error
	onError func(*ParseError) error

	fold   *regexp.Regexp
	ignore []*regexp.Regexp
	blank  bool
	sel    *definition.Select

	// held is the line a fold may still be joined to.
	held *line
	// empty holds the empty lines a definition that keeps blank lines
	// has seen since the last line with text.
	empty []line
	// text is set by the first line that is not blank.
	text    bool
	started bool // input.select.after has been seen
	skipped int
	taken   int
	done    bool

	cols   []column
	header bool
	block  []line
	// headerText is the table's header line, which a second table repeats.
	headerText string

	// csvPending holds the lines of a record a quoted value has not
	// finished yet; csvCols is the header once it has been read, and
	// csvHeader the record it was read from.
	csvPending []line
	csvCols    []string
	csvHeader  []string
	// boxRow holds the lines between two rules of a drawn table, and
	// boxHeader the cells of the header row, read from boxHeaderLines;
	// boxBody is set by the first group of lines after the header.
	boxRow         []line
	boxHeader      []any
	boxHeaderLines []line
	boxBody        bool
	// tree holds the lines of the top-level node that is still open.
	tree []line
	// comp is the state of a composite, whose parts are streamed apart.
	comp *composite
}

// report hands a failed record to onError. It returns nil when the read
// should carry on, and the error that must end it otherwise. Anything
// that is not a record jz failed to read passes straight through.
func (s *streamer) report(err error) error {
	var done *reportedError
	if err == nil || s.onError == nil || errors.As(err, &done) {
		return err
	}
	var pe *ParseError
	if !errors.As(err, &pe) || errors.Is(err, ErrLineTooLong) {
		return err
	}
	return s.onError(pe)
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

// feedFolded drops the lines input.ignore and skip_blank name, and the
// blank lines before the text and after it, which prepare drops from a
// whole document. Where blank lines are kept, an empty line is held
// until a line with text follows it, since until then it may be one of
// the lines after the text.
func (s *streamer) feedFolded(l line) error {
	if edgeBlank(l.text, s.blank) {
		if s.blank || !s.text {
			return nil
		}
		s.empty = append(s.empty, l)
		return nil
	}
	s.text = true
	for len(s.empty) > 0 {
		e := s.empty[0]
		s.empty = s.empty[1:]
		if err := s.feedSelected(e); err != nil {
			return err
		}
	}
	if matchesAny(s.ignore, l.text) {
		return nil
	}
	return s.feedSelected(l)
}

// feedSelected applies input.select in the order after, until, skip,
// limit, which is the order applySelect walks a slice in. A line the
// selection leaves out is unread, the way it is in a whole document,
// except for the heading select.after states in full.
func (s *streamer) feedSelected(l line) error {
	if s.done {
		return s.leftOut(l)
	}
	if s.sel.CompiledAfter() != nil && !s.started {
		if s.sel.CompiledAfter().MatchString(l.text) {
			s.started = true
			if s.sel.Heading(l.text) {
				return nil
			}
		}
		return s.leftOut(l)
	}
	if re := s.sel.CompiledUntil(); re != nil && re.MatchString(l.text) {
		s.done = true
		return s.leftOut(l)
	}
	if s.skipped < s.sel.Skip {
		s.skipped++
		return s.leftOut(l)
	}
	if s.sel.Limit > 0 && s.taken >= s.sel.Limit {
		s.done = true
		return s.leftOut(l)
	}
	s.taken++
	return s.feedRecord(l)
}

// leftOut reports a line the selection did not hand to the parser. Only
// a line with nothing on it can be left out without a word.
func (s *streamer) leftOut(l line) error {
	if blank(l.text) {
		return nil
	}
	return s.unreadError([]UnreadSpan{{Line: l.num, Text: truncate(l.text, 80)}})
}

// feedRecord hands one line to the parser for its type.
func (s *streamer) feedRecord(l line) error {
	switch s.p.Type {
	case definition.TypeTable:
		if s.split() == definition.SplitBox {
			return s.feedBox(l)
		}
		return s.feedTable(l)
	case definition.TypeCSV:
		return s.feedCSV(l)
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
	case definition.TypeTree:
		return s.feedTree(l)
	case definition.TypeComposite:
		return s.feedComposite(l)
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
		s.cols, s.header, s.headerText = cols, true, l.text
		return nil
	}
	if !s.p.Header.None && sameHeader(s.p, s.split(), l.text, s.headerText) {
		// A second table, whose header says where its own columns are.
		cols, err := s.resolveHeader(s.p, l, s.split())
		if err != nil {
			return err
		}
		s.cols = cols
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
		cells, err = s.alignedRow(l, s.cols)
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
	re, m, which := firstMatch(s.p.CompiledPatterns(), l.text)
	if m == nil {
		return nil, s.errorf(l.num, "", "line does not match %s: %q", describePatterns(s.p), truncate(l.text, 80))
	}
	if err := s.checkWhole(l, m[0], m[1]); err != nil {
		return nil, err
	}
	return s.objectFromMatch(re, l.text, m, s.p.PatternValues(which), s.fields, l.num)
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
		// The line opens the next record whether or not the one before it
		// could be read: a caller that skips a bad record still gets the
		// ones after it.
		err := s.flushBlock()
		s.block = []line{l}
		return err
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
	block := s.block
	s.block = nil
	// A record is read the way a whole composite is, so its lines are
	// accounted for the same way, one record at a time.
	s.ledger = newLedger(block)
	defer func() { s.ledger = nil }()
	v, err := s.parseComposite(s.p, block)
	if err != nil {
		return err
	}
	if spans := s.ledger.unread(block); len(spans) > 0 {
		return s.unreadError(spans)
	}
	return s.emit(v)
}

// finish releases what the lookahead and the open record were holding.
func (s *streamer) finish() error {
	if s.held != nil {
		held := *s.held
		s.held = nil
		if err := s.feedFolded(held); err != nil {
			return err
		}
	}
	if err := s.flushBox(); err != nil {
		return err
	}
	if s.header && !s.boxBody && len(s.boxHeaderLines) > 1 {
		return s.noHeaderRule(s.boxHeaderLines)
	}
	if err := s.flushTree(); err != nil {
		return err
	}
	if len(s.csvPending) > 0 {
		// A quoted value that never closes is a record the input did not
		// finish, and reporting it is better than dropping it.
		pending := s.csvPending
		s.csvPending = nil
		return s.emitCSV(pending)
	}
	if s.comp != nil {
		return s.finishComposite()
	}
	return s.flushBlock()
}

// feedTree accumulates a top-level node and its descendants, and
// releases it when the next line at depth zero proves it is finished. A
// node is complete only once nothing deeper follows it, so a stream of a
// tree is a stream of whole top-level nodes.
func (s *streamer) feedTree(l line) error {
	depth, _, err := s.treeDepth(s.p, l)
	if err != nil {
		return err
	}
	if depth == 0 {
		if err := s.flushTree(); err != nil {
			return err
		}
	} else if len(s.tree) == 0 {
		return s.errorf(l.num, "", "indented %d levels below a line at level 0, so it has no parent", depth)
	}
	s.tree = append(s.tree, l)
	return nil
}

func (s *streamer) flushTree() error {
	if len(s.tree) == 0 {
		return nil
	}
	group := s.tree
	s.tree = nil
	nodes, _, err := s.treeNodes(s.p, group, 0, 0)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := s.emit(n); err != nil {
			return err
		}
	}
	return nil
}

// feedCSV holds a line back while a quoted value is still open, and hands
// the record over once it is closed.
func (s *streamer) feedCSV(l line) error {
	s.csvPending = append(s.csvPending, l)
	texts := make([]string, len(s.csvPending))
	for i, p := range s.csvPending {
		texts[i] = p.text
	}
	if csvQuoteOpen(strings.Join(texts, "\n"), csvDelimiter(s.p)) {
		return nil
	}
	pending := s.csvPending
	s.csvPending = nil
	return s.emitCSV(pending)
}

// csvQuoteOpen reports whether text ends inside a quoted value. Only a
// quote that begins a value opens one, and inside it a quote written
// twice is a quote. Counting the quotes instead took one in the middle
// of a value, which opens nothing and is an error of its own line, for
// a value going on to the next line, and held every line after it.
func csvQuoteOpen(text string, delim rune) bool {
	quoted, start := false, true
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quoted && r == '"' && i+1 < len(runes) && runes[i+1] == '"':
			i++
		case quoted && r == '"':
			quoted = false
		case quoted:
		case start && r == '"':
			quoted, start = true, false
		case r == delim || r == '\n':
			start = true
		default:
			start = false
		}
	}
	return quoted
}

// emitCSV reads one finished record, taking the first one as the header
// unless the definition named the columns.
func (s *streamer) emitCSV(pending []line) error {
	texts := make([]string, len(pending))
	for i, p := range pending {
		texts[i] = p.text
	}
	rows, err := readCSV(strings.Join(texts, "\n"), s.p)
	if err != nil {
		return s.errorf(pending[0].num, "", "%s", err.Error())
	}
	for _, row := range rows {
		switch {
		case s.csvCols != nil:
		case s.p.Header.None:
			s.csvCols = csvNumbered(len(row))
		default:
			s.csvCols, s.csvHeader = csvColumns(s.p, row), row
			continue
		}
		if !s.p.Header.None && slices.Equal(row, s.csvHeader) {
			continue // the header of a second file joined to the first
		}
		obj, err := s.csvRow(s.p, s.fields, s.csvCols, row, pending[0].num)
		if err != nil {
			return err
		}
		if err := s.emit(obj); err != nil {
			return err
		}
	}
	return nil
}

// feedBox accumulates the lines of a drawn table. The rules close the
// header, and after it a line is a row of its own unless its first cell
// is empty, in which case it continues the row above — the same reading
// the whole-document parser does, arranged one line at a time.
func (s *streamer) feedBox(l line) error {
	if err := s.boxLine(l); err != nil {
		return err
	}
	if isBoxRule(l.text) {
		if !s.header {
			return s.closeBoxHeader()
		}
		return s.flushBox()
	}
	if !s.header {
		s.boxRow = append(s.boxRow, l)
		return nil
	}
	if cells := boxCells(l.text); len(s.boxRow) > 0 && (len(cells) == 0 || cells[0] == "") {
		s.boxRow = append(s.boxRow, l)
		return nil
	}
	if err := s.flushBox(); err != nil {
		return err
	}
	s.boxRow = []line{l}
	return nil
}

// closeBoxHeader names the columns from the lines above the first rule.
func (s *streamer) closeBoxHeader() error {
	if len(s.boxRow) == 0 {
		return nil
	}
	group := s.boxRow
	s.boxRow = nil
	return s.closeBoxHeaderFrom(group)
}

func (s *streamer) flushBox() error {
	if len(s.boxRow) == 0 {
		return nil
	}
	group := s.boxRow
	s.boxRow = nil
	if !s.header {
		return s.closeBoxHeaderFrom(group)
	}
	s.boxBody = true
	cells := boxJoin(group, "\n")
	if slices.Equal(cells, s.boxHeader) {
		return nil // the header of a second table drawn after the first
	}
	obj, err := s.boxObject(s.cols, cells, s.fields, group[0].num)
	if err != nil {
		return err
	}
	return s.emit(obj)
}

func (s *streamer) closeBoxHeaderFrom(group []line) error {
	cols, err := s.boxColumns(s.p, group)
	if err != nil {
		return err
	}
	s.cols, s.header, s.boxHeader, s.boxHeaderLines = cols, true, boxJoin(group, "\n"), group
	return nil
}

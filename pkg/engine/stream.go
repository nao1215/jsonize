package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

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
		held:    &hold{limit: int(opts.maxInput())},
	}
	s.emit = s.handingOn(emit)
	if def.Parse.Type == definition.TypeTable && def.Parse.Header.None {
		s.columns()
	}
	if def.Parse.Type == definition.TypeCSV {
		// The records are made before anything else sees the lines, the
		// way the whole-document reader makes them (see ParseAccounted).
		q := newCSVQuote(csvDelimiter(&def.Parse))
		s.csvIn = &q
		s.startCSV()
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
		if rerr := s.feedRecordText(def, prepareRecord(string(raw), sep, num), num); rerr != nil {
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

// handingOn wraps emit so that a record handed on is no longer counted:
// what a stream retains is one record, so the bound on values starts
// over with each.
func (s *streamer) handingOn(emit func(any) error) func(any) error {
	return func(v any) error {
		s.values = 0
		return emit(v)
	}
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

// feedRecordText hands one prepared record to the parser, reporting a
// record that is not valid UTF-8 the same way as one that does not fit.
func (s *streamer) feedRecordText(def *definition.Definition, text string, num int) error {
	if err := checkRecord(text, def.Input.Separator()); err != nil {
		return s.report(&ParseError{Definition: def.ID(), Line: num, Msg: err.msg})
	}
	if perr := s.feedPhysical(line{text: text, num: num}); perr != nil {
		return s.report(perr)
	}
	return nil
}

// readRecord reads one record up to sep, refusing one longer than maxLen
// rather than letting a producer with no separators in its output grow
// the buffer without bound. The record comes back without the separator,
// and io.EOF alongside the last one. The limit counts the bytes between
// two separators as they were read, the way the whole-document reader
// counts them, whether or not a separator follows the last of them.
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
		if len(out) > maxLen {
			return nil, ErrLineTooLong
		}
		// The carriage return of a CRLF line ending is trimmed by the
		// caller rather than here, because the whole-document reader
		// removes the escape sequences first and the two have to agree.
		return out, err
	}
}

// hold is what a stream is holding back while it waits for a record to
// finish: the line a fold may still join, the lines of a block or a
// node, a quoted csv value, the region of a single-value part. A whole
// document is bounded by its size, and a record of a stream is bounded
// the same way, so that a producer that never finishes a record cannot
// make jz hold its output without bound. Every streamer of one stream
// shares one hold, since the parts of a composite hold at the same time.
type hold struct {
	bytes int
	limit int
}

// take charges n bytes to the hold and reports the record that exceeds
// it. The error is not one a record could be skipped over: what came
// before is not a record, and what comes after is more of the same.
func (h *hold) take(n int, def *definition.Definition, ln int) error {
	h.bytes += n
	if h.bytes > h.limit {
		return &ParseError{Definition: def.ID(), Line: ln, Msg: fmt.Sprintf("a record held while waiting for its end exceeds %d bytes", h.limit), Cause: ErrInputTooLarge}
	}
	return nil
}

// give returns n bytes to the hold once the lines are released.
func (h *hold) give(n int) { h.bytes -= n }

// lineBytes is what one held line costs: its text and its line break,
// so that a run of empty lines is bounded by its count.
func lineBytes(text string) int { return len(text) + 1 }

func linesBytes(lines []line) int {
	n := 0
	for _, l := range lines {
		n += lineBytes(l.text)
	}
	return n
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

	// held bounds what the stream is holding back.
	held *hold
	// folding is the line a fold may still join, in pieces: the line and
	// each continuation joined to it so far.
	folding []string
	foldNum int
	// csvIn groups the physical lines of a csv into records before they
	// are folded, dropped or selected, the way the whole-document reader
	// does; csvRec holds the lines of the record that is still open.
	csvIn  *csvQuote
	csvRec []string
	csvNum int
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
	// finished yet, which csvQuote follows; csvCols is the header once it
	// has been read, and csvHeader the record it was read from.
	csvPending []string
	csvPendNum int
	csvQuote   csvQuote
	csvCols    []string
	csvHeader  []string
	// boxRow holds the lines between two rules of a drawn table, and
	// boxHeader the cells of the header row, read from boxHeaderLines;
	// boxBody is set by the first group of lines after the header.
	boxRow         []line
	boxHeader      []raw
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
	var (
		done    *reportedError
		missing *MissingColumnError
	)
	if err == nil || s.onError == nil || errors.As(err, &done) || errors.As(err, &missing) {
		return err
	}
	var pe *ParseError
	if !errors.As(err, &pe) || errors.Is(err, ErrLineTooLong) || errors.Is(err, ErrInputTooLarge) {
		return err
	}
	return s.onError(pe)
}

// feedPhysical takes one line as the input split it. For a csv it is
// grouped into a record first, since a quoted value may hold the line
// break, and the record is what the stages after this one see.
func (s *streamer) feedPhysical(l line) error {
	if s.csvIn == nil {
		return s.feedRaw(l)
	}
	if s.csvRec == nil {
		s.csvNum = l.num
	}
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
	}
	s.csvRec = append(s.csvRec, l.text)
	if s.csvIn.feed(l.text) {
		return nil
	}
	return s.flushCSVRecord()
}

// flushCSVRecord hands the record being grouped on, whole.
func (s *streamer) flushCSVRecord() error {
	if s.csvRec == nil {
		return nil
	}
	rec := line{text: strings.Join(s.csvRec, "\n"), num: s.csvNum}
	s.held.give(lineBytes(rec.text))
	s.csvRec = nil
	return s.feedRaw(rec)
}

// feedRaw applies input.fold, which is the one stage that needs to see
// the next line before it can release the previous one.
func (s *streamer) feedRaw(l line) error {
	if s.fold == nil {
		return s.feedFolded(l)
	}
	if s.fold.MatchString(l.text) {
		if s.folding == nil {
			return &ParseError{Definition: s.def.ID(), Line: l.num, Msg: fmt.Sprintf("continuation line with nothing to join it to: %q", l.text)}
		}
		piece := strings.TrimSpace(l.text)
		if err := s.held.take(lineBytes(piece), s.def, l.num); err != nil {
			return err
		}
		s.folding = append(s.folding, piece)
		return nil
	}
	prev := s.releaseFolding()
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
	}
	s.folding, s.foldNum = []string{l.text}, l.num
	if prev == nil {
		return nil
	}
	return s.feedFolded(*prev)
}

// releaseFolding joins the pieces of the line a fold was holding and
// hands it back, or nil when none was held.
func (s *streamer) releaseFolding() *line {
	if s.folding == nil {
		return nil
	}
	held := line{text: strings.Join(s.folding, " "), num: s.foldNum}
	for _, piece := range s.folding {
		s.held.give(lineBytes(piece))
	}
	s.folding = nil
	return &held
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
		if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
			return err
		}
		s.empty = append(s.empty, l)
		return nil
	}
	s.text = true
	for len(s.empty) > 0 {
		e := s.empty[0]
		s.empty = s.empty[1:]
		s.held.give(lineBytes(e.text))
		// A line held back is still a line input.ignore may name, the
		// way it is in a whole document.
		if matchesAny(s.ignore, e.text) {
			continue
		}
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
// except for the heading select.after states in full and the closing line
// select.until states in full.
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
		if s.sel.End(l.text) {
			return nil
		}
		return s.leftOut(l)
	}
	if s.skipped < s.sel.Skip {
		s.skipped++
		return s.leftOut(l)
	}
	// Past the limit the selection stays open for until: the whole
	// document looks for the closing line before it counts the limit, so
	// a closing line after the last line taken is still read.
	if s.sel.Limit > 0 && s.taken >= s.sel.Limit {
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
		// is where the batch reader takes it from too. A header jz
		// cannot read ends the stream rather than leaving out a record:
		// no row can be cut without the columns, and the next row would
		// be taken for another header and report the same failure again
		// against a line that is not one.
		cols, err := s.resolveHeader(s.p, l, s.split())
		if err != nil {
			return stopped(err)
		}
		s.cols, s.header, s.headerText = cols, true, l.text
		return nil
	}
	if s.p.RepeatedHeader() && sameHeader(s.p, s.split(), l.text, s.headerText) {
		// A second table, whose header says where its own columns are.
		cols, err := s.resolveHeader(s.p, l, s.split())
		if err != nil {
			return stopped(err)
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
	cells, err := s.rowCells(s.p, s.split(), l, s.cols, nil)
	if err != nil {
		return nil, err
	}
	obj := jsonutil.NewObject()
	for i, c := range s.cols {
		var v raw
		if i < len(cells) {
			v = cells[i]
		}
		if err := s.setField(obj, c.name, v, s.fields[c.name], l.num); err != nil {
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
		if herr := s.held.take(lineBytes(l.text), s.def, l.num); herr != nil {
			return herr
		}
		s.block = []line{l}
		return err
	}
	if len(s.block) == 0 {
		return s.errorf(l.num, "", "line precedes the first record: %q", l.text)
	}
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
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
	s.held.give(linesBytes(block))
	// A record is read the way a whole composite is, so its lines are
	// accounted for the same way, one record at a time.
	s.ledger = newLedger(block)
	defer func() { s.ledger = nil }()
	v, err := s.parseRecord(s.p, block)
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
	// A quoted value that never closes is a record the input did not
	// finish, and reporting it is better than dropping it.
	if err := s.flushCSVRecord(); err != nil {
		return err
	}
	if held := s.releaseFolding(); held != nil {
		if err := s.feedFolded(*held); err != nil {
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
	if err := s.flushCSV(); err != nil {
		return err
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
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
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
	s.held.give(linesBytes(group))
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

// startCSV prepares the csv state: the columns the definition names, and
// the quoting rules the lines of a record are followed with.
func (s *streamer) startCSV() {
	if s.p.Header.None && len(s.p.Header.Columns) > 0 {
		s.csvCols = s.p.Header.Columns
	}
	s.csvQuote = newCSVQuote(csvDelimiter(s.p))
}

// feedCSV holds a line back while a quoted value is still open, and hands
// the record over once it is closed. At the top level every line is a
// record already (see feedPhysical); the lines of a csv part of a
// composite are grouped here.
func (s *streamer) feedCSV(l line) error {
	if s.csvPending == nil {
		s.csvPendNum = l.num
	}
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
	}
	s.csvPending = append(s.csvPending, l.text)
	if s.csvQuote.feed(l.text) {
		return nil
	}
	return s.flushCSV()
}

// flushCSV reads the record being held, whole, or nothing when none is.
func (s *streamer) flushCSV() error {
	if s.csvPending == nil {
		return nil
	}
	rec := line{text: strings.Join(s.csvPending, "\n"), num: s.csvPendNum}
	s.held.give(lineBytes(rec.text))
	s.csvPending = nil
	return s.emitCSV(rec)
}

// emitCSV reads one finished record, taking the first one as the header
// unless the definition named the columns.
func (s *streamer) emitCSV(rec line) error {
	rows, err := readCSV(rec.text, s.p)
	if err != nil {
		ln, msg := csvFailure(rec, err)
		return s.errorf(ln, "", "%s", msg)
	}
	for _, row := range rows {
		switch {
		case s.csvCols != nil:
		case s.p.Header.None:
			s.csvCols = csvNumbered(len(row))
			if err := s.checkColumns(s.p, s.csvCols, rec.num); err != nil {
				return err
			}
		default:
			s.csvCols, s.csvHeader = csvColumns(s.p, row), row
			if err := s.checkColumns(s.p, s.csvCols, rec.num); err != nil {
				return err
			}
			continue
		}
		// A csv file is data: a row that holds the header's values is a
		// row, unless the definition says the header is printed again.
		if s.p.RepeatedHeader() && slices.Equal(row, s.csvHeader) {
			continue
		}
		obj, err := s.csvRow(s.p, s.fields, s.csvCols, row, rec.num)
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
	if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
		return err
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
	s.held.give(linesBytes(group))
	return s.closeBoxHeaderFrom(group)
}

func (s *streamer) flushBox() error {
	if len(s.boxRow) == 0 {
		return nil
	}
	group := s.boxRow
	s.boxRow = nil
	s.held.give(linesBytes(group))
	if !s.header {
		return s.closeBoxHeaderFrom(group)
	}
	s.boxBody = true
	cells := boxJoin(group, "\n")
	if s.p.RepeatedHeader() && slices.Equal(cells, s.boxHeader) {
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

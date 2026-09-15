package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/nao1215/jsonize/internal/recordio"
	"github.com/nao1215/jsonize/internal/runner"
	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// stream reads r and writes one JSON document per line as each record
// becomes readable.
//
// Identification still happens the way it always does, on the leading
// lines: jz holds them back until the choice they make can no longer
// change (selector.Watch), until it has as many of them as the widest
// signature in scope looks at, or until the input ends, and only then
// commits to a definition. A command that prints a line a second gets its
// first record once the lines that decide its format have come, not once
// the whole window has. The lines it held take the same path as the ones that
// follow, so nothing is read twice or read differently.
//
// An explanation is written as soon as the choice is made, before the
// first record: a stream may never end, and the choice is what there is
// to explain up front. It says nothing about how the lines were read.
//
// retract, when it is not nil, is what the choice falls back to once the
// definition a file path named turns out not to describe the text. A
// path is a guess, and a guess never makes the answer worse, here as
// much as when the whole document is read. It is taken back only while
// nothing has been written yet, which is the case until the format is
// chosen: a record already handed on cannot be taken back, so a reading
// that fails after that stands as the failure it is.
func (a *app) stream(reg *registry.Registry, r io.Reader, ctx selector.Context, retract *selector.Context, out *outputOptions, knownProducer bool, exp *explanation) int {
	filter, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	br := bufio.NewReaderSize(r, 64*1024)
	// The lines held back have to be enough for the wider of the two
	// scopes, or the guess could not be taken back: the text is read
	// once, and what was not held for the second choice is gone.
	wide := ctx
	if retract != nil {
		wide = *retract
	}
	n, sep := selector.Window(reg, wide)
	head, err := readHead(br, n, sep, MaxLineLength, selector.Watch(reg, wide))
	if err != nil {
		// A record refused for its length is the parse failure it would
		// be after the format was known, and says so in the same words;
		// anything else is the input failing to be read at all, reported
		// the way the whole-document reader reports it.
		var pe *engine.ParseError
		if !errors.As(err, &pe) {
			err = fmt.Errorf("reading input: %w", err)
		}
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	if len(head) == 0 && knownProducer {
		// jz started the command, so it knows what the format was meant
		// to be. A list with nothing in it is no lines at all here, the
		// same answer `[]` gives when the whole document is written.
		return a.emptyFormats(reg, ctx, true, exp)
	}
	ctx.Input = head
	chosen, err := selector.Select(reg, ctx)
	if err != nil && retract != nil {
		exp.dropPath(err)
		ctx = *retract
		ctx.Input = head
		exp.scope(ctx, fromRegistry)
		chosen, err = selector.Select(reg, ctx)
	}
	if err != nil {
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.chose(chosen)
	a.explainWrite(exp)
	def := a.reading(chosen.Entry.Def)
	// A key the format does not produce is a usage error, the same as it
	// is for a whole document, so it is kept apart from a parse failure.
	// One the definition does not name is refused before a record is
	// written; one the input decides is judged when the stream ends.
	if err := filter.know(def); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	emit := filter.streamEmit(out.recordWriter(a.env.Stdout), def)
	skipped, onError := a.skipping(exp, def.ID(), out.stopOnError)
	// A stream has no total size to bound; the line limit bounds a
	// record that never ends, and the input limit bounds what is held
	// while a record waits for its end.
	eopts := out.engineOptions()
	eopts.MaxInputSize = 0
	err = engine.Stream(def, io.MultiReader(bytes.NewReader(head), br), eopts, emit, onError)
	return a.streamEnd(err, filter, *skipped)
}

// skipping returns the count of records left out so far and what a
// stream does with a record it cannot read.
//
// With stop, the stream ends there: the records before it stand, the
// failure is reported once as the stream's end, and the status is 3. A
// caller that would rather have no more records than a stream with a gap
// in it asks for that.
//
// Otherwise the record is reported and left out, and the ones after it
// are still written. A command that keeps printing (ping, rsync) puts a line jz has
// no reading for among thousands it has, and ending the stream there
// would throw away everything still to come. Reading a whole document is
// the other answer and keeps it: there, one unreadable line means the
// document is not the format it claimed to be, so nothing is written at
// all. With --explain the record left out is also reported as a fact of
// its own, when it happens, so a stream that never ends can be watched
// for what it drops.
func (a *app) skipping(exp *explanation, def string, stop bool) (*int, func(*engine.ParseError) error) {
	skipped := new(int)
	return skipped, func(pe *engine.ParseError) error {
		if stop {
			return pe
		}
		*skipped++
		a.errorf("%v", pe)
		a.explainSkip(exp, def, pe, *skipped)
		return nil
	}
}

// streamEnd settles what a stream returns once its input has ended.
func (a *app) streamEnd(err error, filter *keyFilter, skipped int) int {
	if nerr := filter.unseen(); nerr != nil && (err == nil || errors.Is(err, runner.ErrCut)) {
		a.errorf("%v", nerr)
		return ExitUsage
	}
	switch {
	case err != nil && !errors.Is(err, runner.ErrCut):
		// A command ended from outside leaves its last record half
		// written. The engine has left it out; the records before it
		// stand, and the command's status says how it ended.
		return a.exitForStream(err)
	case skipped > 0:
		return ExitParse
	}
	return ExitOK
}

// streamWith streams the input with a definition given on the command
// line. Detection is what the leading lines are held back for, and there
// is none here, so the first record is written as soon as it is read.
func (a *app) streamWith(def *definition.Definition, r io.Reader, out *outputOptions, exp *explanation) int {
	filter, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if err := filter.know(def); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	emit := filter.streamEmit(out.recordWriter(a.env.Stdout), def)
	skipped, onError := a.skipping(exp, def.ID(), out.stopOnError)
	eopts := out.engineOptions()
	eopts.MaxInputSize = 0
	err = engine.Stream(def, r, eopts, emit, onError)
	return a.streamEnd(err, filter, *skipped)
}

// emptyFormats settles what a command that succeeded without printing
// anything is an answer to. The formats its name and its arguments admit
// have to be lists, or the empty answer is not knowable: a format that
// yields one object has no empty form, and with --stream it has no
// streaming form either, which is the error reported then. Arguments
// that no definition reads the output of are no request for a list at
// all, the same as they would be with output.
//
// The explanation is written here, since there is no text to choose by:
// it names the variants the answer was judged against.
func (a *app) emptyFormats(reg *registry.Registry, ctx selector.Context, stream bool, exp *explanation) int {
	candidates := selector.Candidates(reg, ctx)
	exp.printedNothing(candidates)
	err := emptyForm(ctx, candidates, stream)
	code := ExitOK
	var ns *engine.NoStreamError
	switch {
	case errors.As(err, &ns):
		code = a.exitForStream(err)
	case err != nil:
		a.errorf("%v", err)
		code = ExitSelect
	}
	if err != nil {
		exp.fail(err, code)
	}
	a.explainWrite(exp)
	return code
}

// emptyForm says why no output is not an answer from the candidates, or
// nil when every one of them reads a list.
func emptyForm(ctx selector.Context, candidates []*registry.Entry, stream bool) error {
	if len(candidates) == 0 {
		if ctx.Variant != "" {
			return fmt.Errorf("%s printed nothing, and %s/%s does not read what it prints with these arguments", ctx.Parser, ctx.Parser, ctx.Variant)
		}
		return fmt.Errorf("%s printed nothing, and no %s variant reads what it prints with these arguments", ctx.Parser, ctx.Parser)
	}
	for _, e := range candidates {
		switch {
		case e.Def.Parse.YieldsArray():
		case stream && !e.Def.Parse.Streams():
			return &engine.NoStreamError{Definition: e.Def.ID()}
		default:
			return fmt.Errorf("%s printed nothing, and %s reads a format that has no empty form", ctx.Parser, e.Def.ID())
		}
	}
	return nil
}

// exitForStream maps a streaming failure. A format with no streaming form
// is a request jz cannot carry out, which is a usage error; anything else
// is the ordinary parse failure, reported after the records that were
// already written.
func (a *app) exitForStream(err error) int {
	if outputClosed(err) {
		return ExitOutputClosed
	}
	var ns *engine.NoStreamError
	if errors.As(err, &ns) {
		a.errorf("%v\nDrop --stream to read it as one document.", err)
		return ExitUsage
	}
	return a.exitFor(err)
}

// readHead reads the leading records, ended by sep, that a signature
// sees: at most n of them, and fewer once settled reports that the ones
// read so far decide the choice. They are handed back rather than left in
// the reader, and the caller replays them, so the lines detection looked
// at are read exactly once and by the same code as the rest.
//
// The records are cut by the reader the engine cuts them with, under the
// same length limit, so a producer that never ends a record is refused
// here at the byte the engine would refuse it at rather than waited for
// until the format is known. The refusal is the parse failure it is
// after detection, with no definition named because none was chosen.
//
// The blank lines before the text are not lines a signature sees (the
// selector leaves them out), so they are not counted here either, and
// settled is not asked about them: a report that opens with a few hundred
// empty lines is still identified from its first lines of text. The input
// limit bounds them the way it bounds everything read.
func readHead(br *bufio.Reader, n int, sep byte, maxLen int, settled func([]byte) bool) ([]byte, error) {
	var out []byte
	lines, num, text := 0, 0, false
	for lines < n {
		rec, err := recordio.Read(br, sep, maxLen)
		if errors.Is(err, recordio.ErrTooLong) {
			return nil, &engine.ParseError{
				Line:  num + 1,
				Msg:   fmt.Sprintf("record exceeds %d bytes", maxLen),
				Cause: engine.ErrLineTooLong,
			}
		}
		ended := err == nil
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, runner.ErrCut) {
			// The reader of a command ended from outside says so again
			// when the rest is read, which is where the cut is dealt with.
			return nil, err
		}
		size := int64(len(out)) + int64(len(rec))
		if ended {
			size++
		}
		if size > MaxInputSize {
			return nil, errors.New("the lines jz needs to identify the format exceed the input limit")
		}
		out = append(out, rec...)
		if ended {
			out = append(out, sep)
		} else {
			break
		}
		num++
		if sep == '\n' && !text && blankHead(rec, num == 1) {
			continue
		}
		text = true
		lines++
		if lines < n && settled(out) {
			break
		}
	}
	return out, nil
}

// blankHead reports a line before the text: nothing on it once the
// escape sequences and, on the first line, the byte order mark are off,
// which is how the selector reads it.
func blankHead(line []byte, first bool) bool {
	line = convert.StripANSI(line)
	if first {
		line = bytes.TrimPrefix(line, []byte{0xEF, 0xBB, 0xBF})
	}
	return len(bytes.TrimSpace(line)) == 0
}

// syncWriter serialises writes to one destination. In streaming exec mode
// the child's standard error is copied through while jz may be writing a
// diagnostic of its own, and the two share a writer.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

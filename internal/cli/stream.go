package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/nao1215/jsonize/internal/runner"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// stream reads r and writes one JSON document per line as each record
// becomes readable.
//
// Identification still happens the way it always does, on the leading
// lines: jz holds back until it has as many of them as the widest
// signature in scope looks at, or until the input ends, and only then
// commits to a definition. The lines it held take the same path as the
// ones that follow, so nothing is read twice or read differently.
//
// An explanation is written as soon as the choice is made, before the
// first record: a stream may never end, and the choice is what there is
// to explain up front. It says nothing about how the lines were read.
func (a *app) stream(reg *registry.Registry, r io.Reader, ctx selector.Context, out *outputOptions, knownProducer bool, exp *explanation) int {
	filter, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	br := bufio.NewReaderSize(r, 64*1024)
	head, err := readHead(br, selector.Window(reg, ctx.Parser))
	if err != nil {
		a.errorf("reading input: %v", err)
		return ExitError
	}
	if len(head) == 0 && knownProducer {
		// jz started the command, so it knows what the format was meant
		// to be. A list with nothing in it is no lines at all here, the
		// same answer `[]` gives when the whole document is written.
		return a.emptyFormats(reg, ctx, true, exp)
	}
	ctx.Input = head
	chosen, err := selector.Select(reg, ctx)
	if err != nil {
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.chose(chosen)
	a.explainWrite(exp)
	// A key the format does not produce is a usage error, the same as it
	// is for a whole document, so it is kept apart from a parse failure.
	// One the definition does not name is refused before a record is
	// written; one the input decides is judged when the stream ends.
	if err := filter.know(chosen.Entry.Def); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	emit := func(v any) error {
		return jsonutil.Encode(a.env.Stdout, filter.narrowRecord(v), false)
	}
	// A record jz cannot read is reported and left out, and the ones
	// after it are still written. A command that keeps printing (ping,
	// rsync) puts a line jz has no reading for among thousands it has,
	// and ending the stream there would throw away everything still to
	// come. Reading a whole document is the other answer and keeps it:
	// there, one unreadable line means the document is not the format it
	// claimed to be, so nothing is written at all.
	skipped := 0
	onError := func(pe *engine.ParseError) error {
		skipped++
		a.errorf("%v", pe)
		return nil
	}
	// A stream has no total size to bound; a record that never ends is
	// what the line limit is there for.
	eopts := out.engineOptions()
	eopts.MaxInputSize = 0
	err = engine.Stream(chosen.Entry.Def, io.MultiReader(bytes.NewReader(head), br), eopts, emit, onError)
	return a.streamEnd(err, filter, skipped)
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
func (a *app) streamWith(def *definition.Definition, r io.Reader, out *outputOptions) int {
	filter, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if err := filter.know(def); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	emit := func(v any) error {
		return jsonutil.Encode(a.env.Stdout, filter.narrowRecord(v), false)
	}
	skipped := 0
	eopts := out.engineOptions()
	eopts.MaxInputSize = 0
	err = engine.Stream(def, r, eopts, emit, func(pe *engine.ParseError) error {
		skipped++
		a.errorf("%v", pe)
		return nil
	})
	return a.streamEnd(err, filter, skipped)
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
		case stream:
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

// readHead reads the leading n lines. They are handed back rather than
// left in the reader, and the caller replays them, so the lines detection
// looked at are read exactly once and by the same code as the rest.
func readHead(br *bufio.Reader, n int) ([]byte, error) {
	var out []byte
	for lines := 0; lines < n; {
		chunk, err := br.ReadSlice('\n')
		if int64(len(out))+int64(len(chunk)) > MaxInputSize {
			return nil, errors.New("the lines jz needs to identify the format exceed the input limit")
		}
		out = append(out, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			// The reader of a command ended from outside says so again
			// when the rest is read, which is where the cut is dealt with.
			if errors.Is(err, io.EOF) || errors.Is(err, runner.ErrCut) {
				break
			}
			return nil, err
		}
		lines++
	}
	return out, nil
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

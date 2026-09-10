package cli

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"sync"

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
func (a *app) stream(reg *registry.Registry, r io.Reader, ctx selector.Context, out *outputOptions, knownProducer, explain bool) int {
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
		return a.emptyStream(reg, ctx.Parser, ctx.Variant)
	}
	ctx.Input = head
	chosen, err := selector.Select(reg, ctx)
	if err != nil {
		code := a.exitFor(err)
		if explain {
			a.explainFailure(err)
		}
		return code
	}
	if explain {
		a.explain(chosen)
	}
	// A key the format does not produce is a usage error, the same as it
	// is for a whole document, so it is kept apart from a parse failure.
	var narrowErr error
	emit := func(v any) error {
		narrowed, err := filter.apply(v)
		if err != nil {
			narrowErr = err
			return err
		}
		return jsonutil.Encode(a.env.Stdout, narrowed, false)
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
	switch {
	case narrowErr != nil:
		a.errorf("%v", narrowErr)
		return ExitUsage
	case err != nil:
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
	var narrowErr error
	emit := func(v any) error {
		narrowed, err := filter.apply(v)
		if err != nil {
			narrowErr = err
			return err
		}
		return jsonutil.Encode(a.env.Stdout, narrowed, false)
	}
	skipped := 0
	eopts := out.engineOptions()
	eopts.MaxInputSize = 0
	err = engine.Stream(def, r, eopts, emit, func(pe *engine.ParseError) error {
		skipped++
		a.errorf("%v", pe)
		return nil
	})
	switch {
	case narrowErr != nil:
		a.errorf("%v", narrowErr)
		return ExitUsage
	case err != nil:
		return a.exitForStream(err)
	case skipped > 0:
		return ExitParse
	}
	return ExitOK
}

// emptyStream answers a command that succeeded without printing
// anything. Every definition it could have chosen has to have a
// streaming form, or writing nothing would be claiming an empty list for
// a format that has none.
func (a *app) emptyStream(reg *registry.Registry, parser, variant string) int {
	candidates := reg.Variants(parser)
	if variant != "" {
		e, ok := reg.Lookup(parser, variant)
		if !ok {
			return ExitSelect
		}
		candidates = []*registry.Entry{e}
	}
	if len(candidates) == 0 {
		return ExitSelect
	}
	for _, e := range candidates {
		if !e.Def.Parse.YieldsArray() {
			return a.exitForStream(&engine.NoStreamError{Definition: e.Def.ID()})
		}
	}
	return ExitOK
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
			if errors.Is(err, io.EOF) {
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

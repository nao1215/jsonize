package engine

import (
	"errors"
	"regexp"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// A composite reads one object out of a whole text, one key per part. Its
// stream is the same reading handed over as the parts become readable:
// one document {"part": NAME, "value": VALUE} per record of a part that
// yields a list, as each record is read, and one per part that yields a
// single value, once the lines of its region have all come. A command
// that prints a header, then a line per event, then a summary (ping) is
// answered event by event rather than when it ends.
//
// What the stream says is what the whole document says, rearranged: the
// values of one list part, in the order they come, are that part's list,
// and a single part's one document is that part. A part whose region
// holds nothing yields no document when it is a list, which is the empty
// list, and is read at the end when it is a single value, as it is in the
// whole document.

// partKey and valueKey name the two members of a composite stream's
// documents.
const (
	partKey  = "part"
	valueKey = "value"
)

// partStream is one part of a composite being streamed.
type partStream struct {
	part *definition.Part
	sel  region
	// sub reads the part when it yields a list, one record at a time.
	sub *streamer
	// lines holds the region of a part that yields a single value, which
	// is read when the region closes.
	lines []line
	// read is set once a single-value part has been read, or once a list
	// part whose region has ended has handed over what it was holding.
	read bool
}

// region follows a part's select through the lines as they come, in the
// order applySelect walks a slice in: after, until, skip, limit.
type region struct {
	sel     *definition.Select
	started bool
	done    bool
	skipped int
	taken   int
}

// admit says what a line is to the part: inside its region, or the
// heading select.after states in full, or neither.
func (g *region) admit(text string) (inside, heading bool) {
	if g.done {
		return false, false
	}
	if after := g.sel.CompiledAfter(); after != nil && !g.started {
		if after.MatchString(text) {
			g.started = true
			return false, g.sel.Heading(text)
		}
		return false, false
	}
	if until := g.sel.CompiledUntil(); until != nil && until.MatchString(text) {
		g.done = true
		return false, false
	}
	if g.skipped < g.sel.Skip {
		g.skipped++
		return false, false
	}
	if g.sel.Limit > 0 && g.taken >= g.sel.Limit {
		g.done = true
		return false, false
	}
	g.taken++
	if g.sel.Limit > 0 && g.taken >= g.sel.Limit {
		// The last line the part takes: the region is closed now rather
		// than when the next line comes, which may be a long time later.
		g.done = true
	}
	return true, false
}

// fate is what became of a line only a single-value part took: it is read
// once one of them read it, and unread once all of them have been read
// and none did.
type fate struct {
	pending int
	read    bool
}

// composite is the state of a composite stream.
type composite struct {
	parts []*partStream
	fates map[int]*fate
}

func (s *streamer) startComposite() {
	c := &composite{fates: map[int]*fate{}}
	for i := range s.p.Parts {
		part := &s.p.Parts[i]
		ps := &partStream{part: part, sel: region{sel: &part.Select}}
		if part.Parse.YieldsArray() {
			name := part.Name
			ps.sub = newStreamer(s.def, &part.Parse, part.Fields, s.opts, s.held, func(v any) error {
				return s.emit(partDocument(name, v))
			}, s.onError)
		}
		c.parts = append(c.parts, ps)
	}
	s.comp = c
}

// partDocument wraps one value of a part.
func partDocument(name string, v any) *jsonutil.Object {
	doc := jsonutil.NewObject()
	doc.Set(partKey, name)
	doc.Set(valueKey, v)
	return doc
}

// feedComposite hands one line to every part whose region it is in.
func (s *streamer) feedComposite(l line) error {
	c := s.comp
	claimed, accounted := false, false
	var single []*partStream
	for _, ps := range c.parts {
		if ps.read {
			continue
		}
		inside, heading := ps.sel.admit(l.text)
		if heading {
			claimed, accounted = true, true
		}
		if inside && !matchesAny(ps.part.IgnorePatterns(), l.text) {
			claimed = true
			if ps.sub != nil {
				// A part that reads line by line reads the line or says why
				// it cannot, so the line cannot go missing there.
				accounted = true
				if err := s.report(ps.sub.feedRecord(l)); err != nil {
					return stopped(err)
				}
			} else {
				if err := s.held.take(lineBytes(l.text), s.def, l.num); err != nil {
					return err
				}
				ps.lines = append(ps.lines, l)
				single = append(single, ps)
			}
		}
	}
	switch {
	case !claimed && !blank(l.text):
		if err := s.report(s.unreadError([]UnreadSpan{{Line: l.num, Text: truncate(l.text, 80)}})); err != nil {
			return stopped(err)
		}
	case len(single) > 0:
		c.fates[l.num] = &fate{pending: len(single), read: accounted}
	}
	// A region that closed on this line is read now: a single part whole,
	// and a list part's record that was waiting for the next one to begin.
	for _, ps := range c.parts {
		if ps.read || !ps.sel.done {
			continue
		}
		if ps.sub == nil {
			if err := s.readSingle(ps); err != nil {
				return err
			}
			continue
		}
		ps.read = true
		if err := s.report(ps.sub.finish()); err != nil {
			return stopped(err)
		}
	}
	return nil
}

// readSingle reads the region of a part that yields one value, and says
// which of its lines nobody read.
func (s *streamer) readSingle(ps *partStream) error {
	ps.read = true
	lines := ps.lines
	ps.lines = nil
	s.held.give(linesBytes(lines))
	r := run{def: s.def, opts: s.opts, ledger: newLedger(lines)}
	v, err := r.parse(&ps.part.Parse, ps.part.Fields, lines)
	var pe *ParseError
	if errors.As(err, &pe) && pe.Field == "" {
		pe.Msg = "part \"" + ps.part.Name + "\": " + pe.Msg
	}
	if err == nil {
		if err := s.emit(partDocument(ps.part.Name, v)); err != nil {
			return stopped(err)
		}
	} else if rerr := s.report(err); rerr != nil {
		return stopped(rerr)
	}
	var unread []UnreadSpan
	for _, l := range lines {
		f := s.comp.fates[l.num]
		if f == nil {
			continue
		}
		// A part that failed has said what was wrong with its lines.
		f.read = f.read || err != nil || r.ledger.isRead(l)
		f.pending--
		if f.pending > 0 {
			continue
		}
		delete(s.comp.fates, l.num)
		if !f.read && !blank(l.text) {
			unread = append(unread, UnreadSpan{Line: l.num, Text: truncate(l.text, 80)})
		}
	}
	if len(unread) > 0 {
		if err := s.report(s.unreadError(unread)); err != nil {
			return stopped(err)
		}
	}
	return nil
}

// finishComposite closes what the end of the input closes: the records a
// list part was holding and the region of every single part not read yet,
// in the order the definition lists them.
func (s *streamer) finishComposite() error {
	for _, ps := range s.comp.parts {
		var err error
		switch {
		case ps.read:
		case ps.sub != nil:
			ps.read = true
			err = ps.sub.finish()
		default:
			err = s.readSingle(ps)
		}
		if err := s.report(err); err != nil {
			return stopped(err)
		}
	}
	return nil
}

// reportedError is an error that has been through report already: the
// caller that decided to stop on it is not asked again.
type reportedError struct{ err error }

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

func stopped(err error) error {
	var done *reportedError
	if errors.As(err, &done) {
		return err
	}
	return &reportedError{err: err}
}

// newStreamer prepares a streamer for one parser with no input stage of
// its own: the lines it is given have been through the definition's. It
// holds lines against the same bound as the stream it is part of.
func newStreamer(def *definition.Definition, p *definition.Parse, fields map[string]*definition.Field, opts Options, held *hold, emit func(any) error, onError func(*ParseError) error) *streamer {
	s := &streamer{
		run:     run{def: def, opts: opts},
		p:       p,
		fields:  fields,
		emit:    emit,
		onError: onError,
		sel:     &definition.Select{},
		ignore:  []*regexp.Regexp{},
		held:    held,
	}
	s.emit = s.handingOn(emit)
	if p.Type == definition.TypeTable && p.Header.None {
		s.columns()
	}
	if p.Type == definition.TypeCSV {
		s.startCSV()
	}
	return s
}

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// explainMode is what --explain asked for. The flag is a switch that also
// takes one value: `--explain` writes lines for a person to read and
// `--explain=json` writes the same facts as one JSON document.
type explainMode int

const (
	explainOff explainMode = iota
	explainText
	explainJSON
)

func (m *explainMode) String() string {
	switch *m {
	case explainText:
		return "true"
	case explainJSON:
		return "json"
	case explainOff:
		return "false"
	}
	return "false"
}

// Set implements flag.Value.
func (m *explainMode) Set(v string) error {
	switch v {
	case "true":
		*m = explainText
	case "json":
		*m = explainJSON
	case "false":
		*m = explainOff
	default:
		return fmt.Errorf("--explain takes no value, or =json for one JSON document; got %q", v)
	}
	return nil
}

// IsBoolFlag lets --explain stand on its own.
func (m *explainMode) IsBoolFlag() bool { return true }

// Where the parser the selection was scoped to came from.
const (
	fromRegistry  = ""          // nothing named it: every definition in the registry
	fromFlag      = "--parser"  // the caller named it
	fromCommand   = "command"   // jz run took it from the name of the command it ran
	fromPath      = "path"      // the file's directory named it
	fromDefine    = "--define"  // the definition was given on the command line
	fromFormat    = "--format"  // the caller named a data file format
	fromExtension = "extension" // the extension of the file named its format
)

// explanation collects what --explain reports about one conversion: how
// the candidates were scoped, which definition was chosen and on what,
// which ones were left out and why, how the chosen one's reading
// accounted for the input, and what the command jz ran did.
//
// Everything in it is derived from the input and the registry, in
// registry order, and nothing reads a clock, so two runs over the same
// input explain themselves byte for byte the same. That is what lets an
// explanation be compared between runs and pasted into a report.
type explanation struct {
	mode explainMode

	from    string
	ctx     selector.Context
	path    string
	dropped string // why the definition the path named was dropped

	selected *selector.Result
	failure  error
	exit     int
	account  *engine.Account
	defined  string // the id of a --define definition
	// format is a data file format read by its own reader rather than a
	// definition, and records how many values its reading produced.
	format  string
	records int
	counted bool

	// A command that printed nothing leaves no text to choose by; the
	// answer rests on the variants its name and arguments leave.
	empty      bool
	candidates []*registry.Entry

	command  string
	args     []string
	status   int
	finished bool
}

func newExplanation(mode explainMode) *explanation {
	return &explanation{mode: mode}
}

func (e *explanation) on() bool { return e != nil && e.mode != explainOff }

// scope records the context the selection runs with and where its parser
// came from.
func (e *explanation) scope(ctx selector.Context, from string) {
	if e == nil {
		return
	}
	e.ctx, e.from = ctx, from
}

// dataFile records that the input is read as a data file format, named
// by --format or by the extension of path.
func (e *explanation) dataFile(format, from, path string) {
	if e == nil {
		return
	}
	e.format, e.from = format, from
	if from == fromExtension {
		e.path = path
	}
}

// readRecords records how many values a data file reading produced.
func (e *explanation) readRecords(n int) {
	if e != nil {
		e.records, e.counted = n, true
	}
}

// dropPath records that the definition a file path named did not fit, so
// the text was read on its own terms.
func (e *explanation) dropPath(err error) {
	if e == nil {
		return
	}
	e.dropped = firstLine(err.Error())
	e.selected = nil
}

// chose records the selection.
func (e *explanation) chose(res *selector.Result) {
	if e != nil {
		e.selected = res
	}
}

// printedNothing records that the command jz ran printed nothing, and the
// variants the empty answer was judged against.
func (e *explanation) printedNothing(candidates []*registry.Entry) {
	if e != nil {
		e.empty, e.candidates = true, candidates
	}
}

// read records how the chosen definition accounted for the input.
func (e *explanation) read(acct engine.Account) {
	if e != nil {
		e.account = &acct
	}
}

// failed reports a failure the way every conversion reports one: the
// explanation records it with the status it maps to, the explanation is
// written, and the status is handed back to be returned. exitFor has
// already put the message on standard error.
func (a *app) failed(e *explanation, err error, code int) int {
	e.fail(err, code)
	a.explainWrite(e)
	return code
}

// fail records the error the conversion ends with and the status it maps
// to.
func (e *explanation) fail(err error, code int) {
	if e != nil {
		e.failure, e.exit = err, code
	}
}

// ran records the command jz started and the status it ended with.
func (e *explanation) ran(name string, args []string, status int) {
	if e != nil {
		e.command, e.args, e.status, e.finished = name, args, status, true
	}
}

// outcome names how the selection ended.
func (e *explanation) outcome() string {
	var (
		nm *selector.NoMatchError
		am *selector.AmbiguousError
		me *selector.MismatchError
		up *selector.UnknownParserError
		uv *selector.UnknownVariantError
	)
	switch {
	case e.defined != "":
		return "defined"
	case e.format != "":
		return "format"
	case e.selected != nil:
		return "chosen"
	case e.empty:
		return "empty"
	case errors.As(e.failure, &nm):
		return "unidentified"
	case errors.As(e.failure, &am):
		return "ambiguous"
	case errors.As(e.failure, &me):
		return "mismatch"
	case errors.As(e.failure, &up):
		return "unknown-parser"
	case errors.As(e.failure, &uv):
		return "unknown-variant"
	default:
		return "failed"
	}
}

// write reports the explanation on standard error. The text form is one
// fact per line, each opening with "jz: explain: ", so it can be told
// apart from the other diagnostics and filtered with grep; the JSON form
// is one line with the same opening and one document after it.
func (a *app) explainWrite(e *explanation) {
	if !e.on() {
		return
	}
	if e.mode == explainJSON {
		var b bytes.Buffer
		if err := jsonutil.Encode(&b, e.document(), false); err != nil {
			a.errorf("explain: %v", err)
			return
		}
		a.errorf("explain: %s", strings.TrimSpace(b.String()))
		return
	}
	for _, l := range e.lines() {
		a.errorf("explain: %s", l)
	}
}

// headline is the first line of the explanation: what the outcome was,
// in the words the outcome deserves.
func (e *explanation) headline() string {
	switch e.outcome() {
	case "defined":
		return fmt.Sprintf("defined %s: the definition was given with --define, so nothing was chosen", e.defined)
	case "format":
		if e.from == fromExtension {
			return fmt.Sprintf("read as %s: the format the extension of %s names, so nothing was chosen", e.format, e.path)
		}
		return fmt.Sprintf("read as %s: the format was given with --format, so nothing was chosen", e.format)
	case "chosen":
		return fmt.Sprintf("chose %s from %s", e.selected.Entry.Def.ID(), e.selected.Entry.Source)
	case "empty":
		return "empty: the command printed nothing, so no definition was chosen by its text"
	case "unidentified":
		return "unidentified: no definition fits the text"
	case "ambiguous":
		var am *selector.AmbiguousError
		errors.As(e.failure, &am)
		return fmt.Sprintf("ambiguous: %s all fit the text, and %s", ids(am.Candidates), am.Unsettled)
	case "mismatch":
		var me *selector.MismatchError
		errors.As(e.failure, &me)
		return fmt.Sprintf("mismatch: %s was named and does not fit: %s", me.Entry.Def.ID(), me.Reason)
	case "unknown-parser":
		return fmt.Sprintf("unknown parser: %s names no definition", e.ctx.Parser)
	case "unknown-variant":
		return fmt.Sprintf("unknown variant: %s has no variant %s", e.ctx.Parser, e.ctx.Variant)
	}
	return "failed before a definition was chosen"
}

// lines renders the explanation for a person.
func (e *explanation) lines() []string {
	out := []string{e.headline()}
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	out = append(out, e.scopeLines()...)
	if e.empty {
		if len(e.candidates) == 0 {
			add("candidates: none")
		} else {
			add("candidates: %s", ids(e.candidates))
		}
	}
	if r := e.selected; r != nil {
		for _, m := range r.Matched {
			add("matched: %s", m)
		}
		if r.Settled != "" {
			add("settled by %s over %s that fit as well", settledRule(r.Settled), count(len(r.Outranked), "other definition", "other definitions"))
			for _, o := range r.Outranked {
				add("outranked: %s: %s", o.Entry.Def.ID(), o.Reason)
			}
		}
	}
	rejected, held, notConsidered := e.rejections()
	for _, r := range rejected {
		add("rejected: %s: %s", r.Entry.Def.ID(), r.Reason)
	}
	for _, r := range held {
		add("held back: %s: its signature fits, but it is only used when named (--parser %s)", r.Entry.Def.ID(), r.Entry.Def.Command)
	}
	if notConsidered > 0 {
		add("not considered: %s only used when named", count(notConsidered, "definition", "definitions"))
	}
	if e.counted {
		add("read: %s", count(e.records, "value", "values"))
	}
	if acct := e.account; acct != nil {
		add("read: %s", describeAccount(acct))
	}
	if e.finished {
		add("command: %s (exit %d)", commandLine(e.command, e.args), e.status)
	}
	return out
}

// scopeLines says which definitions were candidates and what narrowed
// them.
func (e *explanation) scopeLines() []string {
	var out []string
	c := e.ctx
	switch e.from {
	case fromDefine:
		return nil
	case fromRegistry:
		out = append(out, "scope: every definition in the registry, by its signature alone")
	case fromFlag:
		if c.Variant != "" {
			out = append(out, fmt.Sprintf("scope: %s/%s, named by --parser and --variant", c.Parser, c.Variant))
		} else {
			out = append(out, fmt.Sprintf("scope: the variants of %s, named by --parser", c.Parser))
		}
	case fromCommand:
		out = append(out, fmt.Sprintf("scope: the variants of %s, from the name of the command jz ran", c.Parser))
	case fromPath:
		out = append(out, fmt.Sprintf("scope: %s/%s, from the file path %s", c.Parser, c.Variant, e.path))
	}
	if e.from == fromCommand || (e.from == fromFlag && c.Args != nil) {
		narrowed := "the system it ran on (" + c.OS + ")"
		if len(c.Args) > 0 {
			narrowed += " and its arguments (" + strings.Join(c.Args, " ") + ")"
		} else {
			narrowed += " and its arguments (none)"
		}
		out = append(out, "scope: narrowed by "+narrowed)
	}
	if e.dropped != "" {
		out = append(out, fmt.Sprintf("scope: the file path %s named a definition that did not fit (%s), so the text was read on its own", e.path, e.dropped))
	}
	return out
}

// rejections splits what the selection left out into the definitions it
// ruled out, the ones held back only because they are used when named,
// and how many of those it did not consider at all.
func (e *explanation) rejections() (rejected, held []selector.Rejection, explicitOnly int) {
	var reported []selector.Rejection
	var (
		nm *selector.NoMatchError
		am *selector.AmbiguousError
	)
	switch {
	case e.selected != nil:
		reported, explicitOnly = e.selected.Rejections, e.selected.ExplicitOnly
	case errors.As(e.failure, &nm):
		reported, explicitOnly = nm.Reported, nm.ExplicitOnly
	case errors.As(e.failure, &am):
		reported, explicitOnly = am.Reported, am.ExplicitOnly
	}
	for _, r := range reported {
		if r.ExplicitOnly {
			held = append(held, r)
			continue
		}
		rejected = append(rejected, r)
	}
	return rejected, held, explicitOnly - len(held)
}

// document renders the explanation as JSON. The keys are always present,
// null or empty when they do not apply, so a consumer reads every
// explanation the same way. This function is where their order is
// stated; each of them is built beside it.
func (e *explanation) document() *jsonutil.Object {
	doc := jsonutil.NewObject()
	doc.Set("outcome", e.outcome())
	doc.Set("scope", e.scopeDoc())
	doc.Set("chosen", e.chosenDoc())
	doc.Set("candidates", e.candidatesDoc())
	rejected, held, notConsidered := e.rejections()
	doc.Set("rejected", rejectionList(rejected))
	doc.Set("held_back", stringsToAny(idList(entriesOf(held))))
	doc.Set("not_considered", int64(notConsidered))
	doc.Set("read", e.readDoc())
	doc.Set("command", e.commandDoc())
	doc.Set("error", e.errorDoc())
	return doc
}

// scopeDoc says what the choice was made within: where the scope came
// from, and the parser, variant, system, arguments and path that narrow
// it.
func (e *explanation) scopeDoc() *jsonutil.Object {
	scope := jsonutil.NewObject()
	scope.Set("from", nullable(e.from))
	scope.Set("parser", nullable(e.ctx.Parser))
	scope.Set("variant", nullable(e.ctx.Variant))
	scope.Set("os", nullable(e.ctx.OS))
	var args any
	if e.ctx.Args != nil {
		args = stringsToAny(e.ctx.Args)
	}
	scope.Set("args", args)
	scope.Set("path", nullable(e.path))
	scope.Set("path_dropped", nullable(e.dropped))
	return scope
}

// chosenDoc names the definition the input was read with, and null when
// none was. A format named on the command line and a definition given
// there were not chosen from candidates, so they have nothing to say
// about what they beat.
func (e *explanation) chosenDoc() any {
	switch {
	case e.selected != nil:
		c := jsonutil.NewObject()
		c.Set("definition", e.selected.Entry.Def.ID())
		c.Set("registry", e.selected.Entry.Source)
		c.Set("matched", stringsToAny(e.selected.Matched))
		c.Set("settled_by", nullable(e.selected.Settled))
		c.Set("outranked", rejectionList(e.selected.Outranked))
		return c
	case e.format != "":
		return statedChoice(e.format, e.from)
	case e.defined != "":
		return statedChoice(e.defined, fromDefine)
	}
	return nil
}

// statedChoice is a definition the caller named rather than one jz chose.
func statedChoice(definition, registry string) *jsonutil.Object {
	c := jsonutil.NewObject()
	c.Set("definition", definition)
	c.Set("registry", registry)
	c.Set("matched", []any{})
	c.Set("settled_by", nil)
	c.Set("outranked", []any{})
	return c
}

// candidatesDoc lists the definitions an ambiguous input fits, or the
// ones an empty output was judged against. It is empty otherwise.
func (e *explanation) candidatesDoc() []any {
	var am *selector.AmbiguousError
	switch {
	case e.empty:
		return stringsToAny(idList(e.candidates))
	case errors.As(e.failure, &am):
		return stringsToAny(idList(am.Candidates))
	}
	return []any{}
}

// readDoc says where the input went. A definition accounts for lines; a
// data file is read by its format's own reader, which counts the values
// it made instead. The keys are the same either way, null where the one
// reading has nothing to say.
func (e *explanation) readDoc() any {
	if e.counted {
		r := jsonutil.NewObject()
		r.Set("lines", nil)
		r.Set("read", nil)
		r.Set("folded", nil)
		r.Set("blank", nil)
		r.Set("ignored", []any{})
		r.Set("values", int64(e.records))
		return r
	}
	acct := e.account
	if acct == nil {
		return nil
	}
	r := jsonutil.NewObject()
	r.Set("lines", int64(acct.Lines))
	r.Set("read", int64(acct.Read))
	r.Set("folded", int64(acct.Folded))
	r.Set("blank", int64(acct.Blank))
	ignored := make([]any, 0, len(acct.Ignored))
	for _, ig := range acct.Ignored {
		o := jsonutil.NewObject()
		o.Set("rule", fmt.Sprintf("input.ignore[%d]", ig.Index))
		o.Set("expression", ig.Expr)
		o.Set("lines", int64(ig.Lines))
		ignored = append(ignored, o)
	}
	r.Set("ignored", ignored)
	r.Set("values", nil)
	return r
}

// commandDoc reports the command jz started, once it has ended.
func (e *explanation) commandDoc() any {
	if !e.finished {
		return nil
	}
	c := jsonutil.NewObject()
	c.Set("name", e.command)
	c.Set("args", stringsToAny(nonNil(e.args)))
	c.Set("exit", int64(e.status))
	return c
}

// errorDoc reports what the conversion ended with, when it failed.
func (e *explanation) errorDoc() any {
	if e.failure == nil {
		return nil
	}
	f := jsonutil.NewObject()
	f.Set("message", e.failure.Error())
	f.Set("exit", int64(e.exit))
	return f
}

// entriesOf takes the entries of rejections, which held_back names.
func entriesOf(rs []selector.Rejection) []*registry.Entry {
	out := make([]*registry.Entry, len(rs))
	for i, r := range rs {
		out[i] = r.Entry
	}
	return out
}

func rejectionList(rs []selector.Rejection) []any {
	out := make([]any, 0, len(rs))
	for _, r := range rs {
		o := jsonutil.NewObject()
		o.Set("definition", r.Entry.Def.ID())
		o.Set("reason", r.Reason)
		out = append(out, o)
	}
	return out
}

// describeAccount says where the lines of the input went.
func describeAccount(acct *engine.Account) string {
	parts := []string{fmt.Sprintf("%d read", acct.Read)}
	if acct.Folded > 0 {
		parts = append(parts, fmt.Sprintf("%d joined to the line above by input.fold", acct.Folded))
	}
	if acct.Blank > 0 {
		parts = append(parts, fmt.Sprintf("%d blank", acct.Blank))
	}
	for _, ig := range acct.Ignored {
		parts = append(parts, fmt.Sprintf("%d left out by input.ignore[%d] /%s/", ig.Lines, ig.Index, ig.Expr))
	}
	return fmt.Sprintf("%s: %s", count(acct.Lines, "line", "lines"), strings.Join(parts, ", "))
}

// count writes n with the noun that agrees with it.
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func settledRule(rule string) string {
	switch rule {
	case selector.SettledByRegistry:
		return "the registry layering"
	case selector.SettledByPriority:
		return "detect.priority"
	}
	return rule
}

func commandLine(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func idList(entries []*registry.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Def.ID()
	}
	return out
}

func ids(entries []*registry.Entry) string {
	return strings.Join(idList(entries), ", ")
}

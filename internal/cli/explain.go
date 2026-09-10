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
	fromRegistry = ""         // nothing named it: every definition in the registry
	fromFlag     = "--parser" // the caller named it
	fromCommand  = "command"  // jz run took it from the name of the command it ran
	fromPath     = "path"     // the file's directory named it
	fromDefine   = "--define" // the definition was given on the command line
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

// read records how the chosen definition accounted for the input.
func (e *explanation) read(acct engine.Account) {
	if e != nil {
		e.account = &acct
	}
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
	case e.selected != nil:
		return "chosen"
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

// lines renders the explanation for a person.
func (e *explanation) lines() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	switch e.outcome() {
	case "defined":
		add("defined %s: the definition was given with --define, so nothing was chosen", e.defined)
	case "chosen":
		add("chose %s from %s", e.selected.Entry.Def.ID(), e.selected.Entry.Source)
	case "unidentified":
		add("unidentified: no definition fits the text")
	case "ambiguous":
		var am *selector.AmbiguousError
		errors.As(e.failure, &am)
		add("ambiguous: %s all fit the text, and %s", ids(am.Candidates), am.Unsettled)
	case "mismatch":
		var me *selector.MismatchError
		errors.As(e.failure, &me)
		add("mismatch: %s was named and does not fit: %s", me.Entry.Def.ID(), me.Reason)
	case "unknown-parser":
		add("unknown parser: %s names no definition", e.ctx.Parser)
	case "unknown-variant":
		add("unknown variant: %s has no variant %s", e.ctx.Parser, e.ctx.Variant)
	default:
		add("failed before a definition was chosen")
	}
	out = append(out, e.scopeLines()...)
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
// explanation the same way.
func (e *explanation) document() *jsonutil.Object {
	doc := jsonutil.NewObject()
	doc.Set("outcome", e.outcome())

	scope := jsonutil.NewObject()
	scope.Set("from", nullable(e.from))
	scope.Set("parser", nullable(e.ctx.Parser))
	scope.Set("variant", nullable(e.ctx.Variant))
	scope.Set("os", nullable(e.ctx.OS))
	if e.ctx.Args != nil {
		scope.Set("args", stringsToAny(e.ctx.Args))
	} else {
		scope.Set("args", nil)
	}
	scope.Set("path", nullable(e.path))
	scope.Set("path_dropped", nullable(e.dropped))
	doc.Set("scope", scope)

	var chosen any
	switch {
	case e.selected != nil:
		c := jsonutil.NewObject()
		c.Set("definition", e.selected.Entry.Def.ID())
		c.Set("registry", e.selected.Entry.Source)
		c.Set("matched", stringsToAny(e.selected.Matched))
		c.Set("settled_by", nullable(e.selected.Settled))
		c.Set("outranked", rejectionList(e.selected.Outranked))
		chosen = c
	case e.defined != "":
		c := jsonutil.NewObject()
		c.Set("definition", e.defined)
		c.Set("registry", fromDefine)
		c.Set("matched", []any{})
		c.Set("settled_by", nil)
		c.Set("outranked", []any{})
		chosen = c
	}
	doc.Set("chosen", chosen)

	var candidates []any
	var am *selector.AmbiguousError
	if errors.As(e.failure, &am) {
		candidates = stringsToAny(idList(am.Candidates))
	}
	if candidates == nil {
		candidates = []any{}
	}
	doc.Set("candidates", candidates)

	rejected, held, notConsidered := e.rejections()
	doc.Set("rejected", rejectionList(rejected))
	heldList := make([]any, 0, len(held))
	for _, r := range held {
		heldList = append(heldList, r.Entry.Def.ID())
	}
	doc.Set("held_back", heldList)
	doc.Set("not_considered", int64(notConsidered))

	var read any
	if acct := e.account; acct != nil {
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
		read = r
	}
	doc.Set("read", read)

	var command any
	if e.finished {
		c := jsonutil.NewObject()
		c.Set("name", e.command)
		c.Set("args", stringsToAny(nonNil(e.args)))
		c.Set("exit", int64(e.status))
		command = c
	}
	doc.Set("command", command)

	var failure any
	if e.failure != nil {
		f := jsonutil.NewObject()
		f.Set("message", e.failure.Error())
		f.Set("exit", int64(e.exit))
		failure = f
	}
	doc.Set("error", failure)
	return doc
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

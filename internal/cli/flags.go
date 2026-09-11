package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/jsonize/internal/yamlout"
	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// optionSet registers command line options and renders them. Both jobs go
// through the same calls, so the help can never list an option that does
// not exist or miss one that does, and a short and a long form appear as
// the single option they are instead of as two entries.
type optionSet struct {
	fs   *flag.FlagSet
	docs []optionDoc
	// valued names the options that take a value, under both their
	// names. An empty value for one of them names nothing, and taking it
	// for the option not being given would answer a question nobody
	// asked.
	valued map[string]bool
	// given counts how often each option that takes one value was set,
	// under its long name, whichever name it was given by. A second value
	// is a second answer, and letting the last one win drops the first
	// without a word.
	given map[string]*int
}

// once is an option that takes one value. It counts how often it was set
// so that parse can refuse a second one.
type once struct {
	flag.Value
	n *int
}

// Set counts the value and hands it to the option.
func (o once) Set(s string) error {
	*o.n++
	return o.Value.Set(s)
}

type optionDoc struct {
	short string // without the dash, empty when there is none
	long  string // without the dashes
	arg   string // placeholder for the value, empty for a switch
	// optional is the value a switch may be given with "=", shown as
	// --long[=optional]; empty when it takes none.
	optional string
	help     string
}

func newOptions(name string) *optionSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &optionSet{fs: fs, valued: map[string]bool{}, given: map[string]*int{}}
}

// single registers v under long and short as an option that takes one
// value.
func (o *optionSet) single(v flag.Value, long, short, help string) {
	n := new(int)
	o.given[long] = n
	o.fs.Var(once{Value: v, n: n}, long, help)
	if short != "" {
		o.fs.Var(once{Value: v, n: n}, short, help)
	}
}

// repeated returns the first option that takes one value and was given
// more than one, by its long name.
func (o *optionSet) repeated() string {
	for _, d := range o.docs {
		if n, ok := o.given[d.long]; ok && *n > 1 {
			return "--" + d.long
		}
	}
	return ""
}

func (o *optionSet) doc(short, long, arg, help string) {
	o.docs = append(o.docs, optionDoc{short: short, long: long, arg: arg, help: help})
}

// stringOpt registers a string option under its long name and, when given,
// its short one.
func (o *optionSet) stringOpt(p *string, long, short, arg, value, help string) {
	*p = value
	o.single((*stringValue)(p), long, short, help)
	o.valued[long] = true
	if short != "" {
		o.valued[short] = true
	}
	o.doc(short, long, arg, help)
}

// stringValue is a string option's value, the way flag.StringVar keeps
// one.
type stringValue string

// Set stores the value as given.
func (s *stringValue) Set(v string) error { *s = stringValue(v); return nil }

func (s *stringValue) String() string {
	if s == nil {
		return ""
	}
	return string(*s)
}

// emptyValue returns the first option given an empty value, as it was
// written on the command line.
func (o *optionSet) emptyValue() string {
	var name string
	o.fs.Visit(func(f *flag.Flag) {
		if name == "" && o.valued[f.Name] && f.Value.String() == "" {
			name = "--" + f.Name
			if len(f.Name) == 1 {
				name = "-" + f.Name
			}
		}
	})
	return name
}

func (o *optionSet) boolOpt(p *bool, long, short, help string) {
	o.fs.BoolVar(p, long, false, help)
	if short != "" {
		o.fs.BoolVar(p, short, false, help)
	}
	o.doc(short, long, "", help)
}

// switchOpt registers a switch that may also be given one value with
// "=", such as --explain and --explain=json.
func (o *optionSet) switchOpt(p flag.Value, long, optional, help string) {
	o.fs.Var(p, long, help)
	o.docs = append(o.docs, optionDoc{long: long, optional: optional, help: help})
}

func (o *optionSet) durationOpt(p *time.Duration, long, arg string, value time.Duration, help string) {
	*p = value
	o.single((*durationValue)(p), long, "", help)
	o.doc("", long, arg, help)
}

// durationValue is a duration option's value, the way flag.DurationVar
// keeps one.
type durationValue time.Duration

// Set reads the value as a Go duration ("500ms", "2m").
func (d *durationValue) Set(v string) error {
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return errors.New("parse error")
	}
	*d = durationValue(parsed)
	return nil
}

func (d *durationValue) String() string {
	if d == nil {
		return ""
	}
	return time.Duration(*d).String()
}

func (o *optionSet) listOpt(p *stringList, long, arg, help string) {
	o.fs.Var(p, long, help)
	o.doc("", long, arg, help)
}

// print writes the options as one aligned line each.
func (o *optionSet) print(w io.Writer) {
	if len(o.docs) == 0 {
		return
	}
	names := make([]string, len(o.docs))
	width := 0
	for i, d := range o.docs {
		var b strings.Builder
		if d.short != "" {
			fmt.Fprintf(&b, "-%s, ", d.short)
		} else {
			b.WriteString("    ")
		}
		fmt.Fprintf(&b, "--%s", d.long)
		if d.optional != "" {
			fmt.Fprintf(&b, "[=%s]", d.optional)
		}
		if d.arg != "" {
			fmt.Fprintf(&b, " %s", d.arg)
		}
		names[i] = b.String()
		if len(names[i]) > width {
			width = len(names[i])
		}
	}
	for i, d := range o.docs {
		fmt.Fprintf(w, "  %-*s  %s\n", width, names[i], d.help)
	}
}

// outputOptions control how the JSON is written.
type outputOptions struct {
	pretty  bool
	yaml    bool
	stream  bool
	raw     bool
	extract stringList
	exclude stringList
	// year and zones are the assumptions the caller allows about a
	// timestamp the format does not fully state.
	year  string
	zones stringList
}

func (f *outputOptions) bind(o *optionSet) {
	o.boolOpt(&f.pretty, "pretty", "p", "indent JSON output")
	o.boolOpt(&f.yaml, "yaml", "", "write YAML instead of JSON")
	o.boolOpt(&f.stream, "stream", "", "write each record as soon as it is read")
	o.boolOpt(&f.raw, "raw", "", "skip the field rules and report every value as text")
	o.listOpt(&f.extract, "extract", "KEY", "keep only this key (repeatable)")
	o.listOpt(&f.exclude, "exclude", "KEY", "drop this key (repeatable)")
	o.stringOpt(&f.year, "assume-year", "", "YEAR", "", "date the timestamps a format prints without a year (or \"now\")")
	o.listOpt(&f.zones, "assume-zone", "ABBR=+HHMM", "give a zone abbreviation an offset (repeatable)")
}

// write writes one whole document in the format the options ask for.
// Nothing is written when the value cannot be written.
func (f *outputOptions) write(w io.Writer, v any) error {
	if f.yaml {
		return yamlout.Encode(w, v)
	}
	return jsonutil.Encode(w, v, f.pretty)
}

// recordWriter returns what writes one record of a stream: a line of
// JSON, or a YAML document between "---" and "..." lines.
func (f *outputOptions) recordWriter(w io.Writer) func(any) error {
	if f.yaml {
		return func(v any) error { return yamlout.EncodeDocument(w, v) }
	}
	return func(v any) error { return jsonutil.Encode(w, v, false) }
}

// engineOptions is what the output options say about reading, as opposed
// to about writing. The assumptions are checked when the options are
// parsed, so nothing here can fail.
func (f *outputOptions) engineOptions() engine.Options {
	a, _ := f.assumptions()
	return engine.Options{MaxInputSize: MaxInputSize, Raw: f.raw, Assume: a}
}

// assumptions reads --assume-year and --assume-zone. An assumption that
// no field in the chosen format asks for is not an error: it makes no
// difference to the output, and refusing it would mean a caller who
// writes the same command line for several formats has to know which of
// them prints a bare year. That is unlike a key named to --extract,
// which changes the shape a consumer reads and so has to be right.
func (f *outputOptions) assumptions() (convert.Assumptions, error) {
	var a convert.Assumptions
	switch f.year {
	case "":
	case "now":
		a.Year = time.Now().Year()
	default:
		n, err := strconv.Atoi(f.year)
		if err != nil || len(f.year) != 4 || n < 1000 {
			return a, fmt.Errorf("--assume-year expects a four digit year or \"now\", got %q", f.year)
		}
		a.Year = n
	}
	for _, e := range f.zones {
		name, off, ok := strings.Cut(e, "=")
		if !ok || name == "" {
			return a, fmt.Errorf("--assume-zone expects ABBR=+HHMM, got %q", e)
		}
		secs, err := convert.ParseZoneOffset(off)
		if err != nil {
			return a, fmt.Errorf("--assume-zone %s: %w", e, err)
		}
		if a.Zones == nil {
			a.Zones = map[string]int{}
		}
		if old, dup := a.Zones[name]; dup && old != secs {
			return a, fmt.Errorf("--assume-zone gives %s two offsets", name)
		}
		a.Zones[name] = secs
	}
	return a, nil
}

// check reports the option pairs that state two answers at once. It is
// called before anything is read or executed.
func (f *outputOptions) check() error {
	if f.pretty && f.yaml {
		return errors.New("--pretty and --yaml cannot be used together: --pretty indents JSON, and YAML is always written one value per line")
	}
	if f.pretty && f.stream {
		return errors.New("--pretty and --stream cannot be used together: a stream is one record per line, and indenting spreads a record over several")
	}
	if _, err := f.assumptions(); err != nil {
		return err
	}
	_, err := f.filter()
	return err
}

// filter returns the narrowing the caller asked for. Asking to keep some
// keys and drop others at the same time states two different answers for
// a key named in both, so the pair is refused rather than resolved by a
// rule nobody would remember. With neither option the filter drops
// nothing, which is the same as not having one.
func (f *outputOptions) filter() (*keyFilter, error) {
	switch {
	case len(f.extract) > 0 && len(f.exclude) > 0:
		return nil, errors.New("--extract and --exclude cannot be used together")
	case len(f.extract) > 0:
		return newKeyFilter(f.extract, true), nil
	}
	return newKeyFilter(f.exclude, false), nil
}

// selectOptions narrow or pin the automatic detection, and ask for the
// choice to be shown.
type selectOptions struct {
	parser  string
	variant string
	define  string
	columns string
	explain explainMode
}

func (f *selectOptions) bind(o *optionSet) {
	o.stringOpt(&f.parser, "parser", "", "NAME", "", "restrict detection to one parser")
	o.stringOpt(&f.variant, "variant", "", "NAME", "", "use a variant of --parser")
	o.stringOpt(&f.define, "define", "", "YAML", "", "read with a definition given here instead of a registered one")
	o.stringOpt(&f.columns, "columns", "", "NAME,...", "", "name the columns of a csv read without a header line")
	o.switchOpt(&f.explain, "explain", "json", "report the chosen definition and why, on stderr")
}

// check reports the option pairs that state two answers at once.
func (f *selectOptions) check() error {
	if f.define != "" && (f.parser != "" || f.variant != "") {
		return errors.New("--define and --parser/--variant cannot be used together: --define is the definition, so there is nothing left to choose")
	}
	if f.variant != "" && f.parser == "" {
		return &selector.VariantWithoutParserError{Variant: f.variant}
	}
	if f.columns != "" {
		if f.define == "" && f.variant == "" {
			return errors.New("--columns names the columns of a csv read without a header line, so it needs the variant that reads one: --parser csv --variant comma-no-header (or tab-no-header), or a --define")
		}
		if _, err := f.columnNames(); err != nil {
			return err
		}
	}
	return nil
}

// columnNames splits --columns at its commas. A space around a name is
// not part of it; an empty name names nothing.
func (f *selectOptions) columnNames() ([]string, error) {
	var out []string
	for _, n := range strings.Split(f.columns, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			return nil, fmt.Errorf("--columns %q holds an empty name", f.columns)
		}
		out = append(out, n)
	}
	return out, nil
}

// definition returns the inline definition. The bool result is false
// when --define was not given, which is not a failure.
func (f *selectOptions) definition() (*definition.Definition, bool, error) {
	if f.define == "" {
		return nil, false, nil
	}
	d, err := definition.LoadInline([]byte(f.define), "--define")
	if err != nil {
		return nil, false, err
	}
	return d, true, nil
}

// helpDoc is the line for -h/--help. The flag package answers those names
// itself, so there is nothing to register.
func (o *optionSet) helpDoc() {
	o.doc("h", "help", "", "show help")
}

// parse reads args and prints usage on --help or on an error. The bool
// result is true when the caller should return the given exit code.
func (a *app) parse(o *optionSet, args []string, usage string) (int, bool) {
	err := o.fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.env.Stdout, usage)
		o.print(a.env.Stdout)
		fmt.Fprintln(a.env.Stdout)
		printLinks(a.env.Stdout)
		return ExitOK, true
	}
	if err == nil {
		if name := o.emptyValue(); name != "" {
			err = fmt.Errorf("%s was given an empty value, which names nothing", name)
		} else if name := o.repeated(); name != "" {
			err = fmt.Errorf("%s was given twice, and it takes one value", name)
		}
	}
	if err != nil {
		a.errorf("%v", err)
		fmt.Fprint(a.env.Stderr, usage)
		o.print(a.env.Stderr)
		return ExitUsage, true
	}
	return 0, false
}

// splitEnvFlag validates NAME=value entries.
func splitEnvFlag(entries []string) ([]string, error) {
	for _, e := range entries {
		k, _, ok := strings.Cut(e, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--env expects NAME=value, got %q", e)
		}
	}
	return entries, nil
}

// nameColumns applies --columns before anything is read or run. The
// definition it applies to is the one named: the inline one, which is
// returned renamed, or the registered variant, which is read renamed from
// then on (reading). A definition that has no columns to name is a usage
// error here rather than a reading that ignores the names.
func (a *app) nameColumns(reg *registry.Registry, sel *selectOptions, inline *definition.Definition) (*definition.Definition, int) {
	if sel.columns == "" {
		return inline, ExitOK
	}
	names, err := sel.columnNames()
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitUsage
	}
	target := inline
	if target == nil {
		e, ok := reg.Lookup(sel.parser, sel.variant)
		if !ok {
			return nil, a.exitFor(&selector.UnknownVariantError{Parser: sel.parser, Variant: sel.variant, Available: variantNames(reg.Variants(sel.parser))})
		}
		target = e.Def
	}
	renamed, err := target.WithColumns(names)
	if err != nil {
		a.errorf("--columns: %v", err)
		return nil, ExitUsage
	}
	if inline != nil {
		return renamed, ExitOK
	}
	a.named, a.renamed = target, renamed
	return nil, ExitOK
}

// reading returns the definition to read with once d has been chosen:
// d itself, or its copy with the names --columns gave when d is the
// variant --columns applies to.
func (a *app) reading(d *definition.Definition) *definition.Definition {
	if a.renamed != nil && d == a.named {
		return a.renamed
	}
	return d
}

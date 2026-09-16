package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/jsonize/internal/datafile"

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
	// heading is the title of the group the options after it belong to;
	// a doc with a heading is no option.
	heading string
	short   string // without the dash, empty when there is none
	long    string // without the dashes
	arg     string // placeholder for the value, empty for a switch
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

// group starts a group of options under title in the help.
func (o *optionSet) group(title string) {
	o.docs = append(o.docs, optionDoc{heading: title})
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

// Set reads the value as a Go duration ("500ms", "2m"). A negative one
// is no length of time, and 0 already says there is no limit.
func (d *durationValue) Set(v string) error {
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("expects a duration such as 30s or 2m, got %q", v)
	}
	if parsed < 0 {
		return fmt.Errorf("expects a duration such as 30s or 2m, got %q; 0 is no limit", v)
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

// print writes the options as one aligned line each, under the headings
// of their groups.
func (o *optionSet) print(w io.Writer) {
	if len(o.docs) == 0 {
		return
	}
	names := make([]string, len(o.docs))
	width := 0
	for i, d := range o.docs {
		if d.heading != "" {
			continue
		}
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
		if d.heading != "" {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, d.heading)
			continue
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, names[i], d.help)
	}
}

// outputOptions control how the JSON is written.
type outputOptions struct {
	pretty bool
	stream bool
	raw    bool

	extract stringList
	exclude stringList
	// year and zones are the assumptions the caller allows about a
	// timestamp the format does not fully state.
	year  string
	zones stringList
	// keepEscapes reads escape sequences as part of the text, which they
	// are in a data file.
	keepEscapes bool
}

// bindOutput registers the options about what is written.
func (f *outputOptions) bindOutput(o *optionSet) {
	o.boolOpt(&f.pretty, "pretty", "p", "indent JSON output")
	o.boolOpt(&f.stream, "stream", "", "write each record as a line of JSON as soon as it is read")
	o.listOpt(&f.extract, "extract", "KEY", "keep only this key of each object (repeatable)")
	o.listOpt(&f.exclude, "exclude", "KEY", "drop this key from each object (repeatable)")
}

// bindReading registers the options about how a definition reads the
// text: its field rules left out, and what a timestamp does not say.
func (f *outputOptions) bindReading(o *optionSet) {
	o.boolOpt(&f.raw, "raw", "", "show the text a definition cut, without its field rules (types, trims, null words)")
	o.stringOpt(&f.year, "assume-year", "", "YEAR", "", "date the timestamps a format prints without a year (or \"now\")")
	o.listOpt(&f.zones, "assume-zone", "ABBR=+HHMM", "give a zone abbreviation an offset (repeatable)")
}

// write writes one whole document, indented when --pretty asks for it.
// Nothing is written when the value cannot be written.
func (f *outputOptions) write(w io.Writer, v any) error {
	return jsonutil.Encode(w, v, f.pretty)
}

// recordWriter returns what writes one record of a stream: one line of
// JSON.
func (f *outputOptions) recordWriter(w io.Writer) func(any) error {
	return func(v any) error { return jsonutil.Encode(w, v, false) }
}

// engineOptions is what the output options say about reading, as opposed
// to about writing. The assumptions are checked when the options are
// parsed, so nothing here can fail.
func (f *outputOptions) engineOptions() engine.Options {
	a, _ := f.assumptions()
	opts := engine.Options{MaxInputSize: MaxInputSize, Raw: f.raw, Assume: a}
	if f.keepEscapes {
		opts = datafile.TabularOptions(opts)
	}
	return opts
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
		a.Recent = time.Now()
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
	case len(f.exclude) > 0:
		return newKeyFilter(f.exclude, false), nil
	}
	// No key named, no filter: a nil *keyFilter hands every value on as
	// it is.
	return nil, nil //nolint:nilnil // a nil filter is the filter that keeps everything
}

// knowKeys refuses a key the caller named that a record def reads cannot
// have, which is known before the input is read.
func (f *outputOptions) knowKeys(def *definition.Definition) error {
	filter, err := f.filter()
	if err != nil {
		return err
	}
	return filter.know(def)
}

// selectOptions narrow or pin the automatic detection, and ask for the
// choice to be shown.
type selectOptions struct {
	parser  string
	variant string
	define  string
	columns string
	types   stringList
	explain explainMode
	// tabular is set when the input is a csv or a tsv read as data, which
	// is read with a fixed definition rather than a registered one and
	// takes --columns and --type the way a named csv variant does.
	tabular bool
}

// bindColumns registers the options about the columns of a csv.
func (f *selectOptions) bindColumns(o *optionSet) {
	o.stringOpt(&f.columns, "columns", "", "NAME,...", "", "name the columns of a csv read without a header line")
	o.listOpt(&f.types, "type", "COLUMN=TYPE", "convert a csv or tsv column to int, float or bool (repeatable)")
}

// bindParser registers the options that name or show the definition the
// text is read with.
func (f *selectOptions) bindParser(o *optionSet) {
	o.stringOpt(&f.parser, "parser", "", "NAME", "", "restrict detection to one parser")
	o.stringOpt(&f.variant, "variant", "", "NAME", "", "use a variant of --parser")
	o.stringOpt(&f.define, "define", "", "YAML", "", "read with a definition given here instead of a registered one")
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
		if f.define == "" && f.variant == "" && !f.tabular {
			return errors.New("--columns names the columns of a csv read without a header line, so it needs the variant that reads one: --parser csv --variant comma-no-header (or tab-no-header), or a --define")
		}
		if _, err := f.columnNames(); err != nil {
			return err
		}
	}
	if len(f.types) > 0 {
		switch {
		case f.define != "":
			return errors.New("--type converts the columns of a csv a registered definition reads, and --define states its own: give the column a field there")
		case f.variant == "" && !f.tabular:
			return errors.New("--type converts the columns of a csv or a tsv: read a .csv or .tsv file, give --format csv or tsv, or name --parser csv --variant")
		}
		if _, err := f.columnTypes(); err != nil {
			return err
		}
	}
	return nil
}

// checkReading reports the pairs of reading and selecting options that
// state two answers: --raw leaves out every field rule, and a --type
// column is one.
func checkReading(out *outputOptions, sel *selectOptions) error {
	if out.raw && len(sel.types) > 0 {
		return errors.New("--type and --raw cannot be used together: --raw leaves out the field rules, and --type converts a column with one")
	}
	return nil
}

// columnTypes reads --type COLUMN=TYPE. A column typed twice states two
// answers, even when they agree.
func (f *selectOptions) columnTypes() (map[string]string, error) {
	out := map[string]string{}
	for _, e := range f.types {
		column, typ, ok := strings.Cut(e, "=")
		switch {
		case !ok || column == "" || typ == "":
			return nil, fmt.Errorf("--type expects COLUMN=TYPE with a type of %s, got %q", strings.Join(definition.ColumnTypes(), ", "), e)
		case !slices.Contains(definition.ColumnTypes(), typ):
			return nil, fmt.Errorf("--type %s: unknown type %q; the types are %s", e, typ, strings.Join(definition.ColumnTypes(), ", "))
		case out[column] != "":
			return nil, fmt.Errorf("--type gives the column %q twice", column)
		}
		out[column] = typ
	}
	return out, nil
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
// itself, so there is nothing to register. In a help whose options are
// grouped, it is a group of its own.
func (o *optionSet) helpDoc() {
	if len(o.docs) > 0 && o.docs[0].heading != "" {
		o.group("Help:")
	}
	o.doc("h", "help", "", "show help")
}

// parse reads args and prints usage on --help or on an error. The bool
// result is true when the caller should return the given exit code.
func (a *app) parse(o *optionSet, args []string, usage string) (int, bool) {
	err := optionError(o.fs.Parse(args))
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

// removedOptions are options jz no longer has, with what to do instead.
// A name that is simply unknown gets the flag package's own message; one
// that used to work gets told where the job went, since a script written
// for an older jz is the likeliest place it comes from.
var removedOptions = map[string]string{
	"stop-on-error": "--stop-on-error was removed: a stream now ends at the first record it cannot read, which is what it asked for; drop it",
	"yaml":          "--yaml was removed: jz writes JSON only; pipe the JSON to a YAML tool to get YAML (for example: jz ... | yq -P)",
}

// optionError rewrites a refusal of the flag package in the terms jz's
// help uses: an option is named with two dashes, or one for a letter, and
// a value it cannot take says what it takes. An option jz used to have
// says what replaced it.
func optionError(err error) error {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return err
	}
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: -"); ok {
		name = strings.TrimPrefix(name, "-")
		if removed, ok := removedOptions[name]; ok {
			return errors.New(removed)
		}
		return fmt.Errorf("unknown option %s", optionName(name))
	}
	if name, ok := strings.CutPrefix(msg, "flag needs an argument: -"); ok {
		return fmt.Errorf("%s needs a value", optionName(name))
	}
	// invalid boolean value "V" for -NAME: REASON
	if rest, ok := strings.CutPrefix(msg, "invalid boolean value "); ok {
		value, name, reason := invalidValue(rest, " for -")
		if strings.HasPrefix(reason, "--") {
			return errors.New(reason)
		}
		return fmt.Errorf("%s takes no value, got %s", optionName(name), value)
	}
	// invalid value "V" for flag -NAME: REASON
	if rest, ok := strings.CutPrefix(msg, "invalid value "); ok {
		_, name, reason := invalidValue(rest, " for flag -")
		if strings.HasPrefix(reason, "--") {
			return errors.New(reason)
		}
		return fmt.Errorf("%s %s", optionName(name), reason)
	}
	if arg, ok := strings.CutPrefix(msg, "bad flag syntax: "); ok {
		return fmt.Errorf("%q is not an option", arg)
	}
	return err
}

// invalidValue splits `"VALUE"<sep>NAME: REASON`, the tail of the flag
// package's refusal of a value. The value is quoted with %q, so it ends at
// the last separator before the name.
func invalidValue(rest, sep string) (value, name, reason string) {
	i := strings.LastIndex(rest, sep)
	if i < 0 {
		return "", "", rest
	}
	value = rest[:i]
	name, reason, _ = strings.Cut(rest[i+len(sep):], ": ")
	return value, name, reason
}

// optionName writes a name the way the help does.
func optionName(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
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

// settle checks the options that state two answers at once and reads a
// definition given with --define. Both modes do this in the same order
// and answer the same way: a pair that cannot work is a usage error
// before anything is read or run, and a --define that does not load is a
// registry error. The bool result is true when --define gave one.
func (a *app) settle(out *outputOptions, sel *selectOptions) (*definition.Definition, bool, int) {
	for _, check := range []func() error{sel.check, out.check, func() error { return checkReading(out, sel) }} {
		if err := check(); err != nil {
			a.errorf("%v", err)
			return nil, false, ExitUsage
		}
	}
	inline, hasInline, err := sel.definition()
	if err != nil {
		a.errorf("%v", err)
		return nil, false, ExitRegistry
	}
	return inline, hasInline, ExitOK
}

// nameColumns applies --type and --columns before anything is read or
// run. The definition they apply to is the one named: the inline one,
// which is returned changed, or the registered variant, which is read
// changed from then on (reading). A definition that has no columns to
// name or convert is a usage error here rather than a reading that
// ignores the options.
func (a *app) nameColumns(reg *registry.Registry, sel *selectOptions, inline *definition.Definition) (*definition.Definition, int) {
	if sel.columns == "" && len(sel.types) == 0 {
		return inline, ExitOK
	}
	target := inline
	if target == nil {
		e, ok := reg.Lookup(sel.parser, sel.variant)
		if !ok {
			return nil, a.exitFor(&selector.UnknownVariantError{Parser: sel.parser, Variant: sel.variant, Available: variantNames(reg.Variants(sel.parser))})
		}
		target = e.Def
	}
	changed := target
	if len(sel.types) > 0 {
		types, err := sel.columnTypes()
		if err == nil {
			changed, err = changed.WithTypes(types)
		}
		if err != nil {
			a.errorf("--type: %v", err)
			return nil, ExitUsage
		}
		a.typed = types
	}
	if sel.columns != "" {
		names, err := sel.columnNames()
		for _, column := range slices.Sorted(maps.Keys(a.typed)) {
			if err == nil && !slices.Contains(names, column) {
				a.errorf("--type: no column %q; --columns names %s", column, strings.Join(names, ", "))
				return nil, ExitUsage
			}
		}
		if err == nil {
			changed, err = changed.WithColumns(names)
		}
		if err != nil {
			a.errorf("--columns: %v", err)
			return nil, ExitUsage
		}
	}
	if inline != nil {
		return changed, ExitOK
	}
	a.named, a.renamed = target, changed
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

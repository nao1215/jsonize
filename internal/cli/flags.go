package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// optionSet registers command line options and renders them. Both jobs go
// through the same calls, so the help can never list an option that does
// not exist or miss one that does, and a short and a long form appear as
// the single option they are instead of as two entries.
type optionSet struct {
	fs   *flag.FlagSet
	docs []optionDoc
}

type optionDoc struct {
	short string // without the dash, empty when there is none
	long  string // without the dashes
	arg   string // placeholder for the value, empty for a switch
	help  string
}

func newOptions(name string) *optionSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &optionSet{fs: fs}
}

func (o *optionSet) doc(short, long, arg, help string) {
	o.docs = append(o.docs, optionDoc{short: short, long: long, arg: arg, help: help})
}

// stringOpt registers a string option under its long name and, when given,
// its short one.
func (o *optionSet) stringOpt(p *string, long, short, arg, value, help string) {
	o.fs.StringVar(p, long, value, help)
	if short != "" {
		o.fs.StringVar(p, short, value, help)
	}
	o.doc(short, long, arg, help)
}

func (o *optionSet) boolOpt(p *bool, long, short, help string) {
	o.fs.BoolVar(p, long, false, help)
	if short != "" {
		o.fs.BoolVar(p, short, false, help)
	}
	o.doc(short, long, "", help)
}

func (o *optionSet) durationOpt(p *time.Duration, long, arg string, value time.Duration, help string) {
	o.fs.DurationVar(p, long, value, help)
	o.doc("", long, arg, help)
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
	extract stringList
	exclude stringList
}

func (f *outputOptions) bind(o *optionSet) {
	o.boolOpt(&f.pretty, "pretty", "p", "indent JSON output")
	o.listOpt(&f.extract, "extract", "KEY", "keep only this key (repeatable)")
	o.listOpt(&f.exclude, "exclude", "KEY", "drop this key (repeatable)")
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

// selectOptions narrow or pin the automatic detection.
type selectOptions struct {
	parser  string
	variant string
}

func (f *selectOptions) bind(o *optionSet) {
	o.stringOpt(&f.parser, "parser", "", "NAME", "", "restrict detection to one parser")
	o.stringOpt(&f.variant, "variant", "", "NAME", "", "use a variant of --parser")
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

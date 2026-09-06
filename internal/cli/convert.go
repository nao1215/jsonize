package cli

import (
	"flag"
	"io"
	"os"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/selector"
)

//nolint:dupword // the usage block repeats "jz" on purpose
const convertUsage = `Usage: COMMAND | jz [options]
       jz [options] < FILE
       jz [options] --file FILE

Reads output that a command produced earlier and converts it to JSON.
Nothing is executed. The parser is identified from the text itself: every
parser signature is tested and jz succeeds only when exactly one matches.
Text that matches none, or more than one, is an error naming what to pass.

  df -h | jz
  jz --file captured.txt
  df -h | jz --parser df
  df -h | jz --parser df --variant gnu-human

Options:
`

// convertOptions are the options of the default mode.
type convertOptions struct {
	file    string
	output  outputOptions
	selects selectOptions
}

func (c *convertOptions) bind(o *optionSet) {
	o.stringOpt(&c.file, "file", "f", "PATH", "-", "read input from PATH instead of stdin")
	c.output.bind(o)
	c.selects.bind(o)
	o.helpDoc()
}

func (a *app) cmdConvert(args []string) int {
	o := newOptions("jz")
	var co convertOptions
	co.bind(o)
	if code, done := a.parse(o, args, convertUsage); done {
		return code
	}
	if o.fs.NArg() > 0 {
		return a.unexpectedArgs(o.fs)
	}
	if co.selects.variant != "" && co.selects.parser == "" {
		a.errorf("%v", &selector.VariantWithoutParserError{Variant: co.selects.variant})
		return ExitUsage
	}

	reg, code := a.loadRegistry()
	if code != 0 {
		return code
	}
	r := a.env.Stdin
	if co.file != "-" {
		f, err := os.Open(co.file)
		if err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		defer f.Close()
		r = f
	}
	// The size limit is not negotiable from the command line: it exists so
	// that a runaway producer cannot make jz allocate without bound.
	data, err := io.ReadAll(io.LimitReader(r, MaxInputSize+1))
	if err != nil {
		a.errorf("reading input: %v", err)
		return ExitError
	}
	if int64(len(data)) > MaxInputSize {
		a.errorf("input exceeds the %d byte limit", MaxInputSize)
		return ExitParse
	}
	sel, err := selector.Select(reg, selector.Context{
		Parser:  co.selects.parser,
		Variant: co.selects.variant,
		Input:   data,
	})
	if err != nil {
		return a.exitFor(err)
	}
	out, err := engine.Parse(sel.Entry.Def, data, engine.Options{MaxInputSize: MaxInputSize})
	if err != nil {
		return a.exitFor(err)
	}
	if err := jsonutil.Encode(a.env.Stdout, out, co.output.pretty); err != nil {
		a.errorf("writing output: %v", err)
		return ExitError
	}
	return ExitOK
}

// unexpectedArgs reports a positional argument in the default mode, where
// a mistyped subcommand is the most likely cause.
func (a *app) unexpectedArgs(fs *flag.FlagSet) int {
	arg := fs.Arg(0)
	if lookup(arg) == nil {
		a.errorf("unknown command %q", arg)
	} else {
		a.errorf("%q must come before the options", arg)
	}
	a.usage(a.env.Stderr)
	return ExitUsage
}

package cli

import (
	"io"
	"os"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
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
  vmstat 1 | jz --stream               # one JSON document per line

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
		return a.unexpectedArgs(o, args)
	}
	if err := co.selects.check(); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if err := co.output.check(); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	inline, hasInline, err := co.selects.definition()
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
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
	exp := newExplanation(co.selects.explain)
	if hasInline {
		return a.convertWith(inline, r, &co.output, exp)
	}
	ctx := selector.Context{Parser: co.selects.parser, Variant: co.selects.variant}
	from := fromRegistry
	if co.selects.parser != "" {
		from = fromFlag
	}
	if co.selects.parser == "" && co.file != "-" {
		// A path is evidence about the text the same way argv is in exec
		// mode, and it is the only evidence a file gives.
		if p, v, ok := parserFromPath(reg, co.file); ok {
			ctx.Parser, ctx.Variant = p, v
			from = fromPath
			exp.path = co.file
		}
	}
	exp.scope(ctx, from)
	if co.output.stream {
		return a.stream(reg, r, ctx, &co.output, false, exp)
	}
	// The size limit is not negotiable from the command line: it exists so
	// that a runaway producer cannot make jz allocate without bound.
	data, code := a.readAll(r)
	if code != ExitOK {
		return code
	}
	ctx.Input = data
	sel, out, acct, err := readWith(reg, ctx, data, co.output.engineOptions())
	if err != nil && ctx.Parser != co.selects.parser {
		// The path was a guess, so it never makes the answer worse. It
		// is dropped when the definition it named does not describe the
		// text and equally when it describes it but cannot read it:
		// either way the guess was wrong, and the text is then read on
		// its own terms.
		exp.dropPath(err)
		ctx.Parser, ctx.Variant = co.selects.parser, co.selects.variant
		exp.scope(ctx, fromRegistry)
		sel, out, acct, err = readWith(reg, ctx, data, co.output.engineOptions())
	}
	exp.chose(sel)
	if err != nil {
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.read(acct)
	a.explainWrite(exp)
	if out, code = a.narrow(out, &co.output, sel.Entry.Def); code != ExitOK {
		return code
	}
	if err := jsonutil.Encode(a.env.Stdout, out, co.output.pretty); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// unexpectedArgs reports a positional argument in the default mode, where
// a mistyped subcommand is the most likely cause. A file there is a file
// meant to be read, and the refusal says how, with the options it came
// with.
func (a *app) unexpectedArgs(o *optionSet, args []string) int {
	arg := o.fs.Arg(0)
	if lookup(arg) == nil {
		if fi, err := os.Stat(arg); err == nil && fi.Mode().IsRegular() && *o.given["file"] == 0 {
			opts := args[:len(args)-o.fs.NArg()]
			line := append(append([]string{"jz"}, opts...), "--file", arg)
			a.errorf("unknown command %q; a file is read with --file:\n  %s", arg, shellLine(line))
			return ExitUsage
		}
		a.errorf("unknown command %q", arg)
	} else {
		a.errorf("%q must come before the options", arg)
	}
	a.usage(a.env.Stderr)
	return ExitUsage
}

// readAll reads the whole input. The size limit is not negotiable from
// the command line: it exists so that a runaway producer cannot make jz
// allocate without bound.
func (a *app) readAll(r io.Reader) ([]byte, int) {
	data, err := io.ReadAll(io.LimitReader(r, MaxInputSize+1))
	if err != nil {
		a.errorf("reading input: %v", err)
		return nil, ExitError
	}
	if int64(len(data)) > MaxInputSize {
		a.errorf("input exceeds the %d byte limit", MaxInputSize)
		return nil, ExitParse
	}
	return data, ExitOK
}

// convertWith reads the input with a definition given on the command
// line. Nothing is selected, so nothing can be selected wrongly: the
// caller stated the format and gets either the reading of it or the
// reason it does not fit.
func (a *app) convertWith(def *definition.Definition, r io.Reader, out *outputOptions, exp *explanation) int {
	if exp != nil {
		exp.defined = def.ID()
		exp.scope(selector.Context{}, fromDefine)
	}
	if out.stream {
		a.explainWrite(exp)
		return a.streamWith(def, r, out)
	}
	data, code := a.readAll(r)
	if code != ExitOK {
		return code
	}
	v, acct, err := engine.ParseAccounted(def, data, out.engineOptions())
	if err != nil {
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.read(acct)
	a.explainWrite(exp)
	if v, code = a.narrow(v, out, def); code != ExitOK {
		return code
	}
	if err := jsonutil.Encode(a.env.Stdout, v, out.pretty); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// readWith chooses a definition for the text and reads it with that one.
// The two steps are taken together because a caller that may retry has
// to treat them the same way: a definition that does not describe the
// text and one that cannot read it are both the wrong definition. The
// selection comes back even when the reading fails, since which
// definition failed is part of explaining the failure.
func readWith(reg *registry.Registry, ctx selector.Context, data []byte, opts engine.Options) (*selector.Result, any, engine.Account, error) {
	sel, err := selector.Select(reg, ctx)
	if err != nil {
		return nil, nil, engine.Account{}, err
	}
	out, acct, err := engine.ParseAccounted(sel.Entry.Def, data, opts)
	if err != nil {
		return sel, nil, acct, err
	}
	return sel, out, acct, nil
}

package cli

import (
	"io"
	"os"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/selector"
)

const parseUsage = `Usage: jz parse [flags] <command>

Reads output that <command> produced earlier from standard input (or from
--file) and converts it to JSON. Nothing is executed. Because the origin
of piped text is unknown, the variant is chosen from the text itself
unless --os or --variant narrows it down.

Flags:
`

func (a *app) cmdParse(args []string) int {
	fs := newFlagSet("parse")
	var rf registryFlags
	var of outputFlags
	var sf selectFlags
	var file string
	var maxInput int64
	rf.bind(fs)
	of.bind(fs)
	sf.bind(fs, "")
	fs.StringVar(&file, "file", "-", "read the output from `path` instead of stdin")
	fs.Int64Var(&maxInput, "max-input", engine.DefaultMaxInputSize, "maximum input size in `bytes`")
	if code, done := a.parseFlags(fs, args, parseUsage); done {
		return code
	}
	if fs.NArg() != 1 {
		a.errorf("parse: expected exactly one command name, got %d arguments", fs.NArg())
		return ExitUsage
	}
	key := commandKey(fs.Arg(0))
	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	if len(reg.Variants(key)) == 0 {
		return a.exitFor(&selector.UnknownCommandError{Command: key, Known: reg.Commands()})
	}
	r := a.env.Stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, maxInput+1))
	if err != nil {
		a.errorf("reading input: %v", err)
		return ExitError
	}
	if int64(len(data)) > maxInput {
		a.errorf("input exceeds %d bytes (raise --max-input if intended)", maxInput)
		return ExitParse
	}
	sel, err := selector.Select(reg, selector.Context{Command: key, Variant: sf.variant, OS: sf.os, Input: data})
	if err != nil {
		return a.exitFor(err)
	}
	out, err := engine.Parse(sel.Entry.Def, data, engine.Options{Raw: of.raw, MaxInputSize: maxInput})
	if err != nil {
		return a.exitFor(err)
	}
	if err := emit(a.env.Stdout, out, sel.Entry, &of, nil); err != nil {
		a.errorf("writing output: %v", err)
		return ExitError
	}
	return ExitOK
}

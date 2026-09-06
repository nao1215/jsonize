package cli

import (
	"flag"
	"io"
	"os"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/selector"
)

//nolint:dupword // the usage block repeats "jz" on purpose
const convertUsage = `Usage: COMMAND | jz [flags]
       jz [flags] < FILE
       jz [flags] --file FILE

Reads output that some command produced earlier and converts it to JSON.
Nothing is executed. The parser is identified from the text itself: every
parser signature in the registry is tested, and jz succeeds only when
exactly one of them matches. Text that matches none, or more than one, is
an error that names what to pass explicitly.

  df -h | jz
  jz < captured.txt
  df -h | jz --parser df                    # only consider df's variants
  df -h | jz --parser df --variant gnu-human

Flags:
`

// convertOptions are the flags specific to the conversion mode.
type convertOptions struct {
	file     string
	osHint   string
	maxInput int64
}

// bindConvertFlags registers every flag of the default mode. It is shared
// with the help output so the two can never drift apart.
func bindConvertFlags(fs *flag.FlagSet, rf *registryFlags, of *outputFlags, sf *selectFlags, co *convertOptions) {
	rf.bind(fs)
	of.bind(fs)
	sf.bind(fs, true)
	fs.StringVar(&co.file, "file", "-", "read the output from `path` instead of stdin")
	fs.StringVar(&co.osHint, "os", "", "operating `system` that produced the output (linux, darwin, ...)")
	fs.Int64Var(&co.maxInput, "max-input", engine.DefaultMaxInputSize, "maximum input size in `bytes`")
}

func (a *app) cmdConvert(args []string) int {
	fs := newFlagSet("jz")
	var (
		rf registryFlags
		of outputFlags
		sf selectFlags
		co convertOptions
	)
	bindConvertFlags(fs, &rf, &of, &sf, &co)
	if code, done := a.parseFlags(fs, args, convertUsage); done {
		return code
	}
	if fs.NArg() > 0 {
		return a.unexpectedArgs(fs)
	}
	if sf.variant != "" && sf.parser == "" {
		a.errorf("%v", &selector.VariantWithoutParserError{Variant: sf.variant})
		return ExitUsage
	}

	reg, code := a.loadRegistry(&rf)
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
	data, err := io.ReadAll(io.LimitReader(r, co.maxInput+1))
	if err != nil {
		a.errorf("reading input: %v", err)
		return ExitError
	}
	if int64(len(data)) > co.maxInput {
		a.errorf("input exceeds %d bytes (raise --max-input if that is intended)", co.maxInput)
		return ExitParse
	}
	sel, err := selector.Select(reg, selector.Context{
		Parser:  sf.parser,
		Variant: sf.variant,
		Force:   sf.force,
		OS:      co.osHint,
		Input:   data,
	})
	if err != nil {
		return a.exitFor(err)
	}
	out, err := engine.Parse(sel.Entry.Def, data, engine.Options{Raw: of.raw, MaxInputSize: co.maxInput})
	if err != nil {
		return a.exitFor(err)
	}
	if err := emit(a.env.Stdout, out, sel.Entry, &of, nil); err != nil {
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
		a.errorf("%q must come before the conversion flags", arg)
	}
	a.usage(a.env.Stderr)
	return ExitUsage
}

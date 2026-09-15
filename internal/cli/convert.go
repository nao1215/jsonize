package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nao1215/jsonize/internal/datafile"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
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

A data file is read as the format its extension names: .csv, .tsv,
.ltsv, .jsonl (or .ndjson), .json and .yaml (or .yml), each also
compressed as .gz or .bz2. Input from a pipe is read as a data file when
--format names the format. Nothing is detected for a data file.

  df -h | jz
  jz --file captured.txt
  df -h | jz --parser df
  df -h | jz --parser df --variant gnu-human
  vmstat 1 | jz --stream               # one JSON document per line
  curl -sIL https://example.com | jz   # one record per response
  df -h | jz --yaml                    # the same values as YAML
  jz --parser csv --variant comma-no-header --columns id,name --file rows.csv
  jz --file users.csv                  # one object per row, keys from the header
  jz --file events.jsonl.gz --stream   # a data file, decompressed, record by record
  kubectl get pods -o yaml | jz --format yaml

Options:
`

// convertOptions are the options of the default mode.
type convertOptions struct {
	file    string
	format  string
	output  outputOptions
	selects selectOptions
}

func (c *convertOptions) bind(o *optionSet) {
	o.stringOpt(&c.file, "file", "f", "PATH", "-", "read input from PATH instead of stdin")
	o.stringOpt(&c.format, "format", "", "NAME", "", "read a data file of this format: "+strings.Join(datafile.Names(), ", "))
	c.output.bind(o)
	c.selects.bind(o)
	o.helpDoc()
}

// dataFormat settles which data file format the input is, and from what:
// --format, or the extension of --file when neither --format nor a
// parser or a definition was named. A csv or a tsv is then read with the
// csv shape of the registry, which the options are pointed at here, so
// that --columns, --stream and --explain treat it the way they treat that
// shape named with --parser. compression is what the file name says the
// file is compressed with, whatever its format.
func (a *app) dataFormat(co *convertOptions) (format, from, compression string, code int) {
	byName, compression := "", ""
	if co.file != "-" {
		byName, compression = datafile.FromPath(co.file)
	}
	switch {
	case co.format != "":
		if !datafile.Known(co.format) {
			a.errorf("unknown format %q; the formats are %s", co.format, strings.Join(datafile.Names(), ", "))
			return "", "", "", ExitUsage
		}
		if co.selects.parser != "" || co.selects.variant != "" || co.selects.define != "" {
			a.errorf("--format and --parser/--variant/--define cannot be used together: --format already says how the input is read")
			return "", "", "", ExitUsage
		}
		format, from = co.format, fromFormat
	case byName != "" && co.selects.parser == "" && co.selects.variant == "" && co.selects.define == "":
		format, from = byName, fromExtension
	default:
		return "", "", compression, ExitOK
	}
	if co.output.stream && !datafile.Tabular(format) && !datafile.Streams(format) {
		a.errorf("--stream writes the records of a line format (%s), and a %s document is one value", strings.Join(lineFormats(), ", "), format)
		return "", "", "", ExitUsage
	}
	if co.selects.columns != "" && !datafile.Tabular(format) {
		a.errorf("--columns names the columns of a csv or a tsv, and %s is read as %s", describeInput(co.file), format)
		return "", "", "", ExitUsage
	}
	if datafile.Tabular(format) {
		co.selects.parser = "csv"
		co.selects.variant = "comma"
		if format == datafile.TSV {
			co.selects.variant = "tab"
		}
		if co.selects.columns != "" {
			co.selects.variant += "-no-header"
		}
	}
	return format, from, compression, ExitOK
}

func describeInput(file string) string {
	if file == "-" {
		return "standard input"
	}
	return file
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
	format, formatFrom, compression, code := a.dataFormat(&co)
	if code != ExitOK {
		return code
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
	// A definition given on the command line is the whole of what is
	// needed, so the registries are not read: a broken one cannot stand
	// in the way of a definition that does not use it.
	exp := newExplanation(co.selects.explain)
	if format != "" && !datafile.Tabular(format) {
		r, closeInput, code := a.openInput(co.file, compression)
		if code != ExitOK {
			return code
		}
		defer closeInput()
		exp.dataFile(format, formatFrom, co.file)
		return a.convertData(format, r, &co.output, exp)
	}
	var reg *registry.Registry
	if !hasInline {
		if reg, code = a.loadRegistry(); code != 0 {
			return code
		}
	}
	if inline, code = a.nameColumns(reg, &co.selects, inline); code != ExitOK {
		return code
	}
	// Every refusal the options can earn has been given by now, so the
	// input is opened only for a conversion that will read it.
	r, closeInput, code := a.openInput(co.file, compression)
	if code != ExitOK {
		return code
	}
	defer closeInput()
	if hasInline {
		return a.convertWith(inline, r, &co.output, exp)
	}
	ctx := selector.Context{Parser: co.selects.parser, Variant: co.selects.variant}
	from := fromRegistry
	switch {
	case formatFrom != "":
		from = formatFrom
		exp.path = co.file
	case co.selects.parser != "":
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
	sel, out, acct, err := a.readWith(reg, ctx, data, co.output.engineOptions())
	if err != nil && ctx.Parser != co.selects.parser {
		// The path was a guess, so it never makes the answer worse. It
		// is dropped when the definition it named does not describe the
		// text and equally when it describes it but cannot read it:
		// either way the guess was wrong, and the text is then read on
		// its own terms.
		exp.dropPath(err)
		ctx.Parser, ctx.Variant = co.selects.parser, co.selects.variant
		exp.scope(ctx, fromRegistry)
		sel, out, acct, err = a.readWith(reg, ctx, data, co.output.engineOptions())
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
	if out, code = a.narrow(out, &co.output, a.reading(sel.Entry.Def)); code != ExitOK {
		return code
	}
	if err := co.output.write(a.env.Stdout, out); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// openInput opens what the conversion reads: standard input, or the
// file, through the compression its name ends in.
func (a *app) openInput(file, compression string) (io.Reader, func(), int) {
	r := a.env.Stdin
	closeInput := func() {}
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			a.errorf("%v", err)
			return nil, closeInput, ExitError
		}
		closeInput = func() { _ = f.Close() }
		r = f
	}
	dr, err := datafile.Decompress(r, compression)
	if err != nil {
		closeInput()
		return nil, func() {}, a.exitFor(fmt.Errorf("%s: %w", file, err))
	}
	return dr, closeInput, ExitOK
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
		if code := a.exitFor(fmt.Errorf("reading input: %w", err)); code != ExitError {
			return nil, code
		}
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
		return a.streamWith(def, r, out, exp)
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
	if err := out.write(a.env.Stdout, v); err != nil {
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
func (a *app) readWith(reg *registry.Registry, ctx selector.Context, data []byte, opts engine.Options) (*selector.Result, any, engine.Account, error) {
	sel, err := selector.Select(reg, ctx)
	if err != nil {
		return nil, nil, engine.Account{}, err
	}
	out, acct, err := engine.ParseAccounted(a.reading(sel.Entry.Def), data, opts)
	if err != nil {
		return sel, nil, acct, err
	}
	return sel, out, acct, nil
}

// convertData reads a data file format with its own reader. There is
// nothing to choose and no definition to read with: the caller or the
// file name stated the format, and the reader either reads all of the
// input as that format or says where it stops being it.
func (a *app) convertData(format string, r io.Reader, out *outputOptions, exp *explanation) int {
	filter, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if out.stream {
		a.explainWrite(exp)
		write := out.recordWriter(a.env.Stdout)
		// A stream has no total to bound; the line limit bounds what is
		// held while one line waits for its end.
		err := datafile.Stream(format, r, int(MaxInputSize), func(v any) error {
			return write(filter.narrowRecord(v))
		})
		if err != nil {
			return a.exitFor(err)
		}
		if err := filter.unseen(); err != nil {
			a.errorf("%v", err)
			return ExitUsage
		}
		return ExitOK
	}
	data, code := a.readAll(r)
	if code != ExitOK {
		return code
	}
	v, err := datafile.Read(format, data)
	if err != nil {
		code := a.exitFor(err)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	// A line format is a list of the records its lines hold; a JSON or
	// YAML document is one value, whatever it holds.
	if list, ok := v.([]any); ok && datafile.Streams(format) {
		exp.readRecords(len(list))
	} else {
		exp.readRecords(1)
	}
	a.explainWrite(exp)
	narrowed, err := filter.apply(v)
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if err := out.write(a.env.Stdout, narrowed); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

func lineFormats() []string {
	var out []string
	for _, n := range datafile.Names() {
		if datafile.Streams(n) {
			out = append(out, n)
		}
	}
	return out
}

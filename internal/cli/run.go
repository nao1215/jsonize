package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/internal/runner"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

const runUsage = `Usage: jz run [options] COMMAND [args...]

Runs COMMAND with the given arguments (no shell involved), captures its
stdout and converts that to JSON. Standard input is handed to the command
and its stderr is passed through untouched. The command runs with
LC_ALL=C so that its output is stable; use --keep-locale to inherit the
current locale instead.

The command name selects the parser and its arguments narrow the
variants, but the output still has to match the variant's signature, so a
command that prints something unexpected fails instead of being
mis-parsed.

Options for jz come before COMMAND; everything from COMMAND onwards is
passed to it untouched, so its own options never reach jz. A bare -- can
state that boundary explicitly:

  jz run df -h
  jz run --pretty ps aux
  jz run mytool --pretty              # --pretty goes to mytool
  jz run -- mytool --pretty           # the same, stated explicitly
  jz run --parser df -- sudo df -h    # a wrapper whose name is not the parser
  jz run --stream vmstat 1            # one JSON document per line

--format reads the output as a data format instead, the way
COMMAND | jz --format NAME does, and chooses no parser:

  jz run --format csv sqlite3 -csv -header app.db 'select * from users'

--stream answers a command that does not end: each record is written as
soon as it can be read, instead of one array once the command has
finished; a composite (ping) is written part by part. A format read into
one object is a usage error, given before the command is run, as is a
key to --extract or --exclude that no format left can have.

If the command exits non-zero, jz still parses whatever it printed,
reports the status on stderr and exits with that same status; when it
printed nothing, nothing is written. A command terminated by a signal
yields 128+signal.

`

// runOptions are the options of jz run.
type runOptions struct {
	out        outputOptions
	sel        selectOptions
	format     string
	envs       stringList
	keepLocale bool
	timeout    time.Duration
}

func (r *runOptions) bind(o *optionSet) {
	o.group("The command:")
	o.listOpt(&r.envs, "env", "NAME=VALUE", "set a variable in the command's environment (repeatable)")
	o.boolOpt(&r.keepLocale, "keep-locale", "", "do not force LC_ALL=C for the command")
	o.durationOpt(&r.timeout, "timeout", "DURATION", 0, "kill the command after this long (0 = no limit)")
	o.group("Input:")
	o.stringOpt(&r.format, "format", "", "NAME", "", "read the output as this format: "+strings.Join(datafile.Names(), ", "))
	r.sel.bindColumns(o)
	o.group("Output:")
	r.out.bindOutput(o)
	o.group("Choosing and checking the parser of command output:")
	r.sel.bindParser(o)
	r.out.bindReading(o)
	o.helpDoc()
}

func (a *app) cmdRun(args []string) int {
	o := newOptions(modeRun)
	var ro runOptions
	ro.bind(o)
	if code, done := a.parse(o, args, runUsage); done {
		return code
	}
	out, sel, envs, keepLocale, timeout := ro.out, ro.sel, ro.envs, ro.keepLocale, ro.timeout
	rest := o.fs.Args()
	if len(rest) == 0 {
		a.errorf("run: no command given")
		fmt.Fprint(a.env.Stderr, runUsage)
		o.print(a.env.Stderr)
		return ExitUsage
	}
	extraEnv, err := splitEnvFlag(envs)
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	name, cmdArgs := rest[0], rest[1:]
	format, _, _, code := a.dataFormat("-", ro.format, "the output of "+name, &out, &sel)
	if code != ExitOK {
		return code
	}
	inline, hasInline, code := a.settle(&out, &sel)
	if code != ExitOK {
		return code
	}
	if sel.tabular {
		// A csv or a tsv read as data is read the way a file of it is,
		// with the fixed definition and no registry.
		if inline, err = datafile.TabularDefinition(format, sel.columns == ""); err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		hasInline = true
	}
	// The command name is the parser unless the user says otherwise,
	// which is what makes a wrapper (`jz run --parser df -- sudo df -h`)
	// readable.
	parser := sel.parser
	if parser == "" {
		parser = parserKey(name)
	}

	// Nothing is executed until jz knows it can parse the result. A
	// definition given on the command line is that knowledge already,
	// and the registries are then not read at all.
	var reg *registry.Registry
	if !hasInline && format == "" {
		if reg, code = a.runRegistry(&sel, parser, args[:len(args)-len(rest)], rest); code != ExitOK {
			return code
		}
	}
	if inline, code = a.nameColumns(reg, &sel, inline); code != ExitOK {
		return code
	}
	// The environment a definition asks for is the one the definition
	// that will read the output asks for: the one given on the command
	// line, the variant named, or what the variants the system and the
	// arguments leave agree on.
	sctx := selector.Context{Parser: parser, Variant: sel.variant, OS: a.env.GOOS, Args: cmdArgs}
	var env []string
	switch {
	case hasInline:
		env = append(envList(inline.Exec.Env), extraEnv...)
		code = a.refuseBefore([]*definition.Definition{inline}, &out)
	case format != "":
		env = extraEnv
	default:
		env = append(execEnv(reg, sctx), extraEnv...)
		code = a.refuseBefore(a.readings(selector.Candidates(reg, sctx)), &out)
	}
	if code != ExitOK {
		return code
	}

	ctx := a.env.Context
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	command := runner.Command{
		Name: name,
		Args: cmdArgs,
		Env:  env,
		// jz reads nothing from standard input in this mode, so the
		// command gets it: `printf ... | jz run wc` has to reach wc.
		Stdin:      a.env.Stdin,
		KeepLocale: keepLocale,
		MaxOutput:  MaxInputSize,
	}
	exp := newExplanation(sel.explain)
	switch {
	case format != "" && !sel.tabular:
		exp.dataFile(format, fromFormat, "-")
		return a.runReading(ctx, command, out.stream, timeout, exp, func(r io.Reader) int {
			return a.convertData(format, r, &out, exp)
		})
	case hasInline:
		if sel.tabular {
			exp.dataFile(format, fromFormat, "-")
		}
		return a.runReading(ctx, command, out.stream, timeout, exp, func(r io.Reader) int {
			return a.convertWith(inline, r, &out, exp)
		})
	}
	if sel.parser != "" {
		exp.scope(sctx, fromFlag)
	} else {
		exp.scope(sctx, fromCommand)
	}
	if out.stream {
		return a.runStream(ctx, reg, command, sctx, &out, timeout, exp)
	}
	res, code := a.runChild(ctx, command)
	if code != ExitOK {
		return code
	}
	return a.readRun(ctx, reg, sctx, &out, exp, command, res, timeout)
}

// readRun turns what a command that has ended printed into the answer:
// it reports how the command ended, answers an empty listing, chooses the
// definition with the command's name, arguments and system in hand, and
// writes the JSON. The status is the command's own unless reading failed.
func (a *app) readRun(ctx context.Context, reg *registry.Registry, sctx selector.Context, out *outputOptions, exp *explanation, cmd runner.Command, res *runner.Result, timeout time.Duration) int {
	name, cmdArgs := cmd.Name, cmd.Args
	a.reportChild(ctx, name, res, timeout)
	exp.ran(name, cmdArgs, res.ExitCode)
	if res.Cut {
		a.cutShort(name)
		a.explainWrite(exp)
		return res.ExitCode
	}
	if blank(res.Stdout) {
		if res.ExitCode != 0 {
			a.explainWrite(exp)
			return res.ExitCode
		}
		// The command succeeded and printed nothing, which is what a
		// command that lists things does when there is nothing to list.
		// Here, unlike on a pipe, jz knows which format was meant, so it
		// can say the list is empty instead of that it could not tell.
		return a.emptyResult(reg, sctx, name, *out, exp)
	}

	// jz ran the command, so it knows the arguments and the system it ran
	// on; both narrow the variants before the output is checked.
	sctx.Input = res.Stdout
	chosen, err := selector.Select(reg, sctx)
	if err == nil {
		exp.chose(chosen)
		err = out.knowKeys(a.reading(chosen.Entry.Def))
	}
	if err != nil {
		return a.failed(exp, err, a.failedRun(err, res.ExitCode))
	}
	data, acct, err := engine.ParseAccounted(a.reading(chosen.Entry.Def), res.Stdout, out.engineOptions())
	if err != nil {
		return a.failed(exp, err, a.failedRun(err, res.ExitCode))
	}
	exp.read(acct)
	// The command's own status is what jz returns once the output has
	// been written, or once reading it has failed: jz read it, so the
	// interesting number is the command's.
	if code := a.writeDocument(exp, data, out, a.reading(chosen.Entry.Def)); code != ExitOK && res.ExitCode == 0 {
		return code
	}
	return res.ExitCode
}

// runChild runs the command and collects what it printed. A command that
// could not be started or that printed past the input limit is reported
// here; the limit is a parse failure, since the output jz would have read
// is what was too large. The int result is ExitOK when the command ran,
// whatever status it ended with.
func (a *app) runChild(ctx context.Context, cmd runner.Command) (*runner.Result, int) {
	res, err := runner.Run(ctx, cmd, a.env.Stderr)
	if err != nil {
		a.errorf("%v", err)
		if errors.Is(err, runner.ErrOutputTooLarge) {
			return nil, ExitParse
		}
		return nil, ExitError
	}
	return res, ExitOK
}

// runRegistry loads the registries for jz run and checks that the
// command has a parser and, when named, the variant, before anything is
// executed. opts are jz's own options and command what follows them.
func (a *app) runRegistry(sel *selectOptions, parser string, opts, command []string) (*registry.Registry, int) {
	names := []string{parser}
	if sel.parser == "" {
		// The wrapper hint asks after every argument that could be the
		// command a wrapper runs, so their definitions are read too.
		for _, arg := range command[1:] {
			if !strings.HasPrefix(arg, "-") {
				names = append(names, parserKey(arg))
			}
		}
	}
	reg, code := a.loadRegistryFor(names...)
	if code != ExitOK {
		return nil, code
	}
	if sel.parser == "" {
		a.wrapperHint = wrapperHint(reg, opts, command)
	}
	if len(reg.Variants(parser)) == 0 {
		known := reg.Commands()
		if a.wrapperHint != "" {
			known = nil
		} else if sel.parser == "" {
			a.wrapperHint = namingHint(opts, command)
		}
		return nil, a.exitFor(&selector.UnknownParserError{Parser: parser, Known: known})
	}
	if sel.variant != "" {
		if _, ok := reg.Lookup(parser, sel.variant); !ok {
			return nil, a.exitFor(&selector.UnknownVariantError{
				Parser:    parser,
				Variant:   sel.variant,
				Available: variantNames(reg.Variants(parser)),
			})
		}
	}
	return reg, ExitOK
}

// errStreamFailed marks a streaming failure that has already been
// reported, so that the child still gets waited for and its own status
// still wins.
var errStreamFailed = errors.New("streaming failed")

// runStream is `jz run --stream`: the command's output is read through a
// pipe and converted as it arrives, instead of being collected first.
// The child's status is mirrored the way it is without --stream, and a
// command that printed nothing is answered once its status is known, the
// way it is then.
func (a *app) runStream(ctx context.Context, reg *registry.Registry, cmd runner.Command, sctx selector.Context, out *outputOptions, timeout time.Duration, exp *explanation) int {
	code, res := a.streamingChild(ctx, cmd, func(r io.Reader) int {
		return a.stream(reg, r, sctx, nil, out, true, exp)
	})
	if res == nil || code != exitPrintedNothing {
		return a.streamStatus(ctx, cmd, res, code, timeout, exp)
	}
	a.reportChild(ctx, cmd.Name, res, timeout)
	exp.ran(cmd.Name, cmd.Args, res.ExitCode)
	if res.ExitCode != 0 {
		a.explainWrite(exp)
		return res.ExitCode
	}
	return a.emptyFormats(reg, sctx, cmd.Name, true, exp)
}

// exitPrintedNothing is what reading a stream returns for a command jz
// started that printed nothing but blank lines. It is no status: what it
// answers depends on how the command ended, which is known only once it
// has.
const exitPrintedNothing = -1

// streamingChild runs cmd with its output going to convert as it
// arrives. The child writes its own standard error through the writer jz
// shares with it, and a failure in the conversion stops the child rather
// than being reported when it ends. A command that could not be run is
// reported here and comes back without a result.
func (a *app) streamingChild(ctx context.Context, cmd runner.Command, convert func(io.Reader) int) (int, *runner.Result) {
	childStderr := a.shareStderr()
	defer a.restoreStderr(childStderr)
	code := ExitOK
	res, err := runner.Stream(ctx, cmd, childStderr, func(r io.Reader) error {
		if code = convert(r); code != ExitOK && code != exitPrintedNothing {
			return errStreamFailed
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStreamFailed) {
		a.errorf("%v", err)
		return ExitError, nil
	}
	return code, res
}

// streamStatus settles what jz run --stream returns once the command has
// ended. The command's own status wins over a failure in the stream,
// which is still on standard error; a command jz had to stop because
// the stream had failed has no status of its own to mirror, so the
// failure is what is returned and the stop is not reported as the
// command's doing.
func (a *app) streamStatus(ctx context.Context, cmd runner.Command, res *runner.Result, code int, timeout time.Duration, exp *explanation) int {
	if res == nil {
		return code
	}
	if res.Stopped {
		a.explainCommand(exp, cmd.Name, cmd.Args, res.ExitCode)
		return code
	}
	a.reportChild(ctx, cmd.Name, res, timeout)
	a.explainCommand(exp, cmd.Name, cmd.Args, res.ExitCode)
	if code != ExitOK && res.ExitCode == 0 {
		return code
	}
	return res.ExitCode
}

// shareStderr prepares standard error for a command whose output jz
// reads while it runs: the command's own stderr is copied through while
// jz may be writing a diagnostic, so the two go through one lock rather
// than interleaving mid-line. When standard error is a file, the command
// writes to it directly instead, which is what lets the command's end be
// waited for without also waiting for whatever it left holding the
// stream. The writer to give the command is returned.
func (a *app) shareStderr() io.Writer {
	plain := a.env.Stderr
	a.env.Stderr = &syncWriter{w: plain}
	if f, ok := plain.(*os.File); ok {
		return f
	}
	return a.env.Stderr
}

// restoreStderr undoes shareStderr.
func (a *app) restoreStderr(io.Writer) {
	if sw, ok := a.env.Stderr.(*syncWriter); ok {
		a.env.Stderr = sw.w
	}
}

// emptyResult answers a command that succeeded without printing
// anything with the empty list, when that is what it means.
func (a *app) emptyResult(reg *registry.Registry, sctx selector.Context, command string, out outputOptions, exp *explanation) int {
	if code := a.emptyFormats(reg, sctx, command, false, exp); code != ExitOK {
		return code
	}
	if err := out.write(a.env.Stdout, []any{}); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// failedRun reports a selection or parse failure. A command that already
// failed keeps its own status, which is the more useful signal.
func (a *app) failedRun(err error, childStatus int) int {
	code := a.exitFor(err)
	if childStatus != 0 {
		return childStatus
	}
	return code
}

// parserKey maps an executable path to its registry key: the base name
// without a Windows extension.
func parserKey(name string) string {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		if strings.HasSuffix(lower, ext) {
			return base[:len(base)-len(ext)]
		}
	}
	return base
}

// wrapperHint is the line a refusal adds when the command looks like a
// wrapper (nice, stdbuf, env): it is named where the parser is looked for,
// so when what it runs is a command jz knows, that command's parser is the
// way forward, and the names that merely look like the wrapper's are no
// help. opts are jz's own options and command is everything after them.
// It is "" when no argument names a command jz has a parser for.
func wrapperHint(reg *registry.Registry, opts, command []string) string {
	name := command[0]
	for _, arg := range command[1:] {
		key := parserKey(arg)
		if strings.HasPrefix(arg, "-") || key == parserKey(name) || !runsAsCommand(reg, key) || notRunnable(arg) {
			continue
		}
		return fmt.Sprintf("If %s runs %s, name that parser: %s", name, key, shellLine(namedRun(opts, key, command)))
	}
	return ""
}

// runsAsCommand reports a name whose definitions describe a command's
// output. A name whose definitions all describe a shape (csv, table) is
// a format rather than a program, and as an argument it is an option's
// value: `systeminfo /fo csv` does not run csv.
func runsAsCommand(reg *registry.Registry, key string) bool {
	for _, e := range reg.Variants(key) {
		if !selector.ShapeOnly(e.Def) {
			return true
		}
	}
	return false
}

// namingHint is the line a refusal adds when jz has no parser for the
// command's name and nothing in its arguments names one: the command may
// print a format jz reads under another name, as `gmd5sum --tag` prints
// what md5 does, and --parser is how to say so.
func namingHint(opts, command []string) string {
	return fmt.Sprintf("If %s prints a format jz reads under another name, name its parser: %s", command[0], shellLine(namedRun(opts, "PARSER", command)))
}

// namedRun is the jz run command line with jz's own options, --parser
// and the command after "--".
func namedRun(opts []string, parser string, command []string) []string {
	if n := len(opts); n > 0 && opts[n-1] == "--" {
		opts = opts[:n-1]
	}
	return append(append(append([]string{"jz", modeRun}, opts...), "--parser", parser, "--"), command...)
}

// notRunnable reports whether path names something that exists and is
// not a program: a directory, or on a system with execute bits a file
// without one. That is something the command reads (`tree /etc/apt`,
// `stat /proc/uptime`) rather than a command a wrapper runs. A name that
// names nothing here is left alone; it is looked up on PATH.
func notRunnable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		return true
	}
	return runtime.GOOS != "windows" && fi.Mode()&0o111 == 0
}

// shellLine writes words as a shell would need them typed, quoting the
// ones that carry anything but plain characters.
func shellLine(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		if w != "" && strings.Trim(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:=,+@%") == "" {
			out[i] = w
			continue
		}
		out[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}

// execEnv is the exec.env the command runs with. A variant named on the
// command line is the definition that will read the output, so its
// entries apply as they stand. With the variant still unknown, the
// entries of the variants the system and the arguments leave are
// collected, since those are the ones that can read the output, and a
// variable two of them set differently is not set at all, so that no
// variant is favoured before the command has even run.
func execEnv(reg *registry.Registry, ctx selector.Context) []string {
	values := map[string]string{}
	conflict := map[string]bool{}
	for _, e := range selector.Candidates(reg, ctx) {
		for k, v := range e.Def.Exec.Env {
			if old, ok := values[k]; ok && old != v {
				conflict[k] = true
			}
			values[k] = v
		}
	}
	for k := range conflict {
		delete(values, k)
	}
	return envList(values)
}

// envList renders a map as NAME=VALUE entries in a fixed order.
func envList(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, k := range slices.Sorted(maps.Keys(values)) {
		out = append(out, k+"="+values[k])
	}
	return out
}

func variantNames(entries []*registry.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Def.Variant
	}
	return out
}

// runReading runs the command and reads its output with read, which
// knows how to read it without choosing: with a definition given on the
// command line, the one a csv or a tsv has, or a data format's own
// reader. It is the pipe case with jz starting the producer, so the
// command's status is mirrored the way it is when jz chooses, and so is
// its output when it printed nothing but blank lines.
func (a *app) runReading(ctx context.Context, cmd runner.Command, stream bool, timeout time.Duration, exp *explanation, read func(io.Reader) int) int {
	if stream {
		code, res := a.streamingChild(ctx, cmd, read)
		return a.streamStatus(ctx, cmd, res, code, timeout, exp)
	}
	res, code := a.runChild(ctx, cmd)
	if code != ExitOK {
		return code
	}
	a.reportChild(ctx, cmd.Name, res, timeout)
	exp.ran(cmd.Name, cmd.Args, res.ExitCode)
	if res.Cut {
		a.cutShort(cmd.Name)
		a.explainWrite(exp)
		return res.ExitCode
	}
	if res.ExitCode != 0 {
		if !blank(res.Stdout) {
			read(bytes.NewReader(res.Stdout))
		} else {
			a.explainWrite(exp)
		}
		return res.ExitCode
	}
	return read(bytes.NewReader(res.Stdout))
}

// refuseBefore gives the refusals the options earn against every
// definition that could read the output, before the command is run: a
// stream none of them can write, and a key none of them can have. With no
// definition left there is nothing to judge, and the reading says why.
func (a *app) refuseBefore(defs []*definition.Definition, out *outputOptions) int {
	if len(defs) == 0 {
		return ExitOK
	}
	if out.stream && !slices.ContainsFunc(defs, func(d *definition.Definition) bool { return d.Parse.Streams() }) {
		return a.exitForStream(&engine.NoStreamError{Definition: defs[0].ID()})
	}
	filter, err := out.filter()
	if err == nil {
		err = filter.knowBefore(defs)
	}
	if err != nil {
		return a.exitFor(err)
	}
	return ExitOK
}

// readings returns the definitions entries are read with.
func (a *app) readings(entries []*registry.Entry) []*definition.Definition {
	out := make([]*definition.Definition, len(entries))
	for i, e := range entries {
		out[i] = a.reading(e.Def)
	}
	return out
}

// blank reports output that holds nothing but white space, which a
// command that lists things prints when there is nothing to list.
func blank(output []byte) bool {
	return len(bytes.TrimSpace(output)) == 0
}

// reportChild writes what the command did with itself.
func (a *app) reportChild(ctx context.Context, name string, res *runner.Result, timeout time.Duration) {
	switch {
	case res.Signal != "":
		a.errorf("%s was terminated by %s", name, res.Signal)
	case res.ExitCode != 0:
		a.errorf("%s exited with status %d", name, res.ExitCode)
	}
	if ctx.Err() != nil && timeout > 0 {
		a.errorf("timeout of %s reached", timeout)
	}
	if res.LeftOpen {
		// What that process writes is not the command's output, and
		// waiting for it would wait as long as it runs.
		a.errorf("%s ended, and a process it started still held its output open; what came before it ended was read", name)
	}
}

// cutShort says why the output of a command ended from outside is not
// read as a document. It stops wherever the command had got to, so its
// last record may be half written, and a document of it would pass for
// the whole of what the command prints.
func (a *app) cutShort(name string) {
	a.errorf("%s was ended before it finished its output, so none of it is read; --stream writes the records that came before", name)
}

// explainCommand reports, after a stream has ended, the command jz ran
// and the status it gave. The rest of a stream's explanation was written
// before its first record; in JSON that document stands as it was, and
// the status is on the line jz writes for any command that fails.
func (a *app) explainCommand(exp *explanation, name string, args []string, status int) {
	if exp == nil || exp.mode != explainText {
		return
	}
	a.errorf("explain: command: %s (exit %d)", commandLine(name, args), status)
}

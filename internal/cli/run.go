package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

--stream answers a command that does not end: each record is written as
soon as it can be read, instead of one array once the command has
finished. It applies to the formats that yield records; a format read
into one object is a usage error.

If the command exits non-zero, jz still parses whatever it printed,
reports the status on stderr and exits with that same status. A command
terminated by a signal yields 128+signal.

Options:
`

func (a *app) cmdRun(args []string) int {
	o := newOptions("run")
	var (
		out        outputOptions
		sel        selectOptions
		envs       stringList
		keepLocale bool
		timeout    time.Duration
	)
	out.bind(o)
	sel.bind(o)
	o.listOpt(&envs, "env", "NAME=VALUE", "set a variable in the command's environment (repeatable)")
	o.boolOpt(&keepLocale, "keep-locale", "", "do not force LC_ALL=C for the command")
	o.durationOpt(&timeout, "timeout", "DURATION", 0, "kill the command after this long (0 = no limit)")
	o.helpDoc()
	if code, done := a.parse(o, args, runUsage); done {
		return code
	}
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
	if err := sel.check(); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	if err := out.check(); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	inline, hasInline, err := sel.definition()
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	name, cmdArgs := rest[0], rest[1:]
	// The command name is the parser unless the user says otherwise,
	// which is what makes a wrapper (`jz run --parser df -- sudo df -h`)
	// readable.
	parser := sel.parser
	if parser == "" {
		parser = parserKey(name)
	}

	reg, code := a.loadRegistry()
	if code != 0 {
		return code
	}
	if sel.parser == "" && !hasInline {
		a.wrapperHint = wrapperHint(reg, args[:len(args)-len(rest)], rest)
	}
	// Nothing is executed until jz knows it can parse the result. A
	// definition given on the command line is that knowledge already.
	if !hasInline && len(reg.Variants(parser)) == 0 {
		known := reg.Commands()
		if a.wrapperHint != "" {
			known = nil
		}
		return a.exitFor(&selector.UnknownParserError{Parser: parser, Known: known})
	}
	if sel.variant != "" && !hasInline {
		if _, ok := reg.Lookup(parser, sel.variant); !ok {
			return a.exitFor(&selector.UnknownVariantError{
				Parser:    parser,
				Variant:   sel.variant,
				Available: variantNames(reg.Variants(parser)),
			})
		}
	}
	if inline, code = a.nameColumns(reg, &sel, inline); code != ExitOK {
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
		Env:  append(mergedExecEnv(reg, parser), extraEnv...),
		// jz reads nothing from standard input in this mode, so the
		// command gets it: `printf ... | jz run wc` has to reach wc.
		Stdin:      a.env.Stdin,
		KeepLocale: keepLocale,
		MaxOutput:  MaxInputSize,
	}
	exp := newExplanation(sel.explain)
	if hasInline {
		return a.runWith(ctx, inline, command, &out, timeout, exp)
	}
	sctx := selector.Context{Parser: parser, Variant: sel.variant, OS: a.env.GOOS, Args: cmdArgs}
	if sel.parser != "" {
		exp.scope(sctx, fromFlag)
	} else {
		exp.scope(sctx, fromCommand)
	}
	if out.stream {
		return a.runStream(ctx, reg, command, sctx, &out, timeout, exp)
	}
	res, err := runner.Run(ctx, command, a.env.Stderr)
	if err != nil {
		a.errorf("%v", err)
		if errors.Is(err, runner.ErrOutputTooLarge) {
			return ExitParse
		}
		return ExitError
	}
	a.reportChild(ctx, name, res, timeout)
	exp.ran(name, cmdArgs, res.ExitCode)
	if res.Cut {
		a.cutShort(name)
		a.explainWrite(exp)
		return res.ExitCode
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		if res.ExitCode != 0 {
			a.explainWrite(exp)
			return res.ExitCode
		}
		// The command succeeded and printed nothing, which is what a
		// command that lists things does when there is nothing to list.
		// Here, unlike on a pipe, jz knows which format was meant, so it
		// can say the list is empty instead of that it could not tell.
		return a.emptyResult(reg, sctx, out, exp)
	}

	// jz ran the command, so it knows the arguments and the system it ran
	// on; both narrow the variants before the output is checked.
	sctx.Input = res.Stdout
	chosen, err := selector.Select(reg, sctx)
	if err != nil {
		code := a.failedRun(err, res.ExitCode)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.chose(chosen)
	data, acct, err := engine.ParseAccounted(a.reading(chosen.Entry.Def), res.Stdout, out.engineOptions())
	if err != nil {
		code := a.failedRun(err, res.ExitCode)
		exp.fail(err, code)
		a.explainWrite(exp)
		return code
	}
	exp.read(acct)
	a.explainWrite(exp)
	narrowed, code := a.narrow(data, &out, a.reading(chosen.Entry.Def))
	if code != ExitOK {
		return code
	}
	data = narrowed
	if err := out.write(a.env.Stdout, data); err != nil {
		return a.writeFailed(err)
	}
	return res.ExitCode
}

// errStreamFailed marks a streaming failure that has already been
// reported, so that the child still gets waited for and its own status
// still wins.
var errStreamFailed = errors.New("streaming failed")

// runStream is `jz run --stream`: the command's output is read through a
// pipe and converted as it arrives, instead of being collected first.
// The child's status is mirrored the way it is without --stream.
func (a *app) runStream(ctx context.Context, reg *registry.Registry, cmd runner.Command, sctx selector.Context, out *outputOptions, timeout time.Duration, exp *explanation) int {
	childStderr := a.shareStderr()
	defer a.restoreStderr(childStderr)
	code := ExitOK
	res, err := runner.Stream(ctx, cmd, childStderr, func(r io.Reader) error {
		if code = a.stream(reg, r, sctx, out, true, exp); code != ExitOK {
			return errStreamFailed
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStreamFailed) {
		a.errorf("%v", err)
		return ExitError
	}
	return a.streamStatus(ctx, cmd, res, code, timeout, exp)
}

// streamStatus settles what jz run --stream returns once the command has
// ended. The command's own status wins over a failure in the stream,
// which is still on standard error; a command jz had to stop because
// the stream had failed has no status of its own to mirror, so the
// failure is what is returned and the stop is not reported as the
// command's doing.
func (a *app) streamStatus(ctx context.Context, cmd runner.Command, res *runner.Result, code int, timeout time.Duration, exp *explanation) int {
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
func (a *app) emptyResult(reg *registry.Registry, sctx selector.Context, out outputOptions, exp *explanation) int {
	if code := a.emptyFormats(reg, sctx, false, exp); code != ExitOK {
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
		if strings.HasPrefix(arg, "-") || key == parserKey(name) || len(reg.Variants(key)) == 0 || notRunnable(arg) {
			continue
		}
		if n := len(opts); n > 0 && opts[n-1] == "--" {
			opts = opts[:n-1]
		}
		line := append(append(append([]string{"jz", "run"}, opts...), "--parser", key, "--"), command...)
		return fmt.Sprintf("If %s runs %s, name that parser: %s", name, key, shellLine(line))
	}
	return ""
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

// mergedExecEnv collects exec.env entries of every variant of a command.
// Conflicting values are dropped so that no variant is favoured before
// the command has even run.
func mergedExecEnv(reg *registry.Registry, parser string) []string {
	values := map[string]string{}
	conflict := map[string]bool{}
	for _, e := range reg.Variants(parser) {
		for k, v := range e.Def.Exec.Env {
			if old, ok := values[k]; ok && old != v {
				conflict[k] = true
			}
			values[k] = v
		}
	}
	var out []string
	for _, k := range sortedKeys(values) {
		if !conflict[k] {
			out = append(out, k+"="+values[k])
		}
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

// runWith runs the command and reads its output with a definition given
// on the command line. It is the pipe case with jz starting the
// producer, so the command's status is mirrored the way it always is.
func (a *app) runWith(ctx context.Context, def *definition.Definition, cmd runner.Command, out *outputOptions, timeout time.Duration, exp *explanation) int {
	if out.stream {
		childStderr := a.shareStderr()
		defer a.restoreStderr(childStderr)
		code := ExitOK
		res, err := runner.Stream(ctx, cmd, childStderr, func(r io.Reader) error {
			if code = a.convertWith(def, r, out, exp); code != ExitOK {
				return errStreamFailed
			}
			return nil
		})
		if err != nil && !errors.Is(err, errStreamFailed) {
			a.errorf("%v", err)
			return ExitError
		}
		return a.streamStatus(ctx, cmd, res, code, timeout, exp)
	}
	res, err := runner.Run(ctx, cmd, a.env.Stderr)
	if err != nil {
		a.errorf("%v", err)
		if errors.Is(err, runner.ErrOutputTooLarge) {
			return ExitParse
		}
		return ExitError
	}
	a.reportChild(ctx, cmd.Name, res, timeout)
	exp.ran(cmd.Name, cmd.Args, res.ExitCode)
	if res.Cut {
		a.cutShort(cmd.Name)
		a.explainWrite(exp)
		return res.ExitCode
	}
	code := a.convertWith(def, bytes.NewReader(res.Stdout), out, exp)
	if res.ExitCode != 0 {
		return res.ExitCode
	}
	return code
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

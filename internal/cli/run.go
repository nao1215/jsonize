package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/runner"
	"github.com/nao1215/jsonize/internal/selector"
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
	if sel.variant != "" && sel.parser == "" {
		a.errorf("%v", &selector.VariantWithoutParserError{Variant: sel.variant})
		return ExitUsage
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
	// Nothing is executed until jz knows it can parse the result.
	if len(reg.Variants(parser)) == 0 {
		return a.exitFor(&selector.UnknownParserError{Parser: parser, Known: reg.Commands()})
	}
	if sel.variant != "" {
		if _, ok := reg.Lookup(parser, sel.variant); !ok {
			return a.exitFor(&selector.UnknownVariantError{
				Parser:    parser,
				Variant:   sel.variant,
				Available: variantNames(reg.Variants(parser)),
			})
		}
	}

	ctx := a.env.Context
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	res, err := runner.Run(ctx, runner.Command{
		Name: name,
		Args: cmdArgs,
		Env:  append(mergedExecEnv(reg, parser), extraEnv...),
		// jz reads nothing from standard input in this mode, so the
		// command gets it: `printf ... | jz run wc` has to reach wc.
		Stdin:      a.env.Stdin,
		KeepLocale: keepLocale,
		MaxOutput:  MaxInputSize,
	}, a.env.Stderr, a.env.Signals)
	if err != nil {
		a.errorf("%v", err)
		if errors.Is(err, runner.ErrOutputTooLarge) {
			return ExitParse
		}
		return ExitError
	}
	switch {
	case res.Signal != "":
		a.errorf("%s was terminated by %s", name, res.Signal)
	case res.ExitCode != 0:
		a.errorf("%s exited with status %d", name, res.ExitCode)
	}
	if ctx.Err() != nil && timeout > 0 {
		a.errorf("timeout of %s reached", timeout)
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		if res.ExitCode != 0 {
			return res.ExitCode
		}
		// The command succeeded and printed nothing, which is what a
		// command that lists things does when there is nothing to list.
		// Here, unlike on a pipe, jz knows which format was meant, so it
		// can say the list is empty instead of that it could not tell.
		return a.emptyResult(reg, parser, sel.variant, out)
	}

	// jz ran the command, so it knows the arguments and the system it ran
	// on; both narrow the variants before the output is checked.
	chosen, err := selector.Select(reg, selector.Context{
		Parser:  parser,
		Variant: sel.variant,
		OS:      a.env.GOOS,
		Args:    cmdArgs,
		Input:   res.Stdout,
	})
	if err != nil {
		return a.failedRun(err, res.ExitCode)
	}
	data, err := engine.Parse(chosen.Entry.Def, res.Stdout, engine.Options{MaxInputSize: MaxInputSize})
	if err != nil {
		return a.failedRun(err, res.ExitCode)
	}
	narrowed, code := a.narrow(data, &out)
	if code != ExitOK {
		return code
	}
	data = narrowed
	if err := jsonutil.Encode(a.env.Stdout, data, out.pretty); err != nil {
		a.errorf("writing output: %v", err)
		return ExitError
	}
	return res.ExitCode
}

// emptyResult answers a command that succeeded without printing
// anything. Every definition the parser could have chosen has to produce
// an array, or the empty answer is not knowable: a format that yields one
// object has no empty form.
func (a *app) emptyResult(reg *registry.Registry, parser, variant string, out outputOptions) int {
	var candidates []*registry.Entry
	if variant != "" {
		e, ok := reg.Lookup(parser, variant)
		if !ok {
			return ExitSelect
		}
		candidates = []*registry.Entry{e}
	} else {
		candidates = reg.Variants(parser)
	}
	if len(candidates) == 0 {
		return ExitSelect
	}
	for _, e := range candidates {
		if !e.Def.Parse.YieldsArray() {
			a.errorf("%s printed nothing, and %s reads a format that has no empty form", parser, e.Def.ID())
			return ExitSelect
		}
	}
	if err := jsonutil.Encode(a.env.Stdout, []any{}, out.pretty); err != nil {
		a.errorf("writing output: %v", err)
		return ExitError
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

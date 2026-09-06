package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/runner"
	"github.com/nao1215/jsonize/internal/selector"
)

const runUsage = `Usage: jz run [flags] COMMAND [args...]

Runs COMMAND with the given arguments (no shell involved), captures its
standard output and converts it to JSON. Standard error is passed
through. The command runs with LC_ALL=C so that its output is stable; use
--keep-locale to inherit the current locale instead.

The command name selects the parser and its arguments narrow the
variants, but the output still has to match the variant's signature, so a
command that prints something unexpected fails instead of being
mis-parsed.

Flags for jz come before COMMAND; everything from COMMAND onwards is
passed to it untouched, so its own flags never reach jz:

  jz run df -h
  jz run --pretty ps aux
  jz run mytool --pretty          # --pretty goes to mytool
  jz run -- mytool --pretty       # the same, stated explicitly

If the command exits non-zero, jz still parses whatever it printed,
reports the status on stderr and exits with that same status. A command
terminated by a signal yields 128+signal.

Flags:
`

func (a *app) cmdRun(args []string) int {
	fs := newFlagSet("run")
	var (
		rf         registryFlags
		of         outputFlags
		sf         selectFlags
		envs       stringList
		keepLocale bool
		timeout    time.Duration
		maxOutput  int64
	)
	rf.bind(fs)
	of.bind(fs)
	sf.bind(fs, false)
	fs.Var(&envs, "env", "set `NAME=value` in the command's environment (repeatable)")
	fs.BoolVar(&keepLocale, "keep-locale", false, "do not force LC_ALL=C for the command")
	fs.DurationVar(&timeout, "timeout", 0, "kill the command after this `duration` (0 = no limit)")
	fs.Int64Var(&maxOutput, "max-output", engine.DefaultMaxInputSize, "maximum stdout size in `bytes`")
	if code, done := a.parseFlags(fs, args, runUsage); done {
		return code
	}
	rest := fs.Args()
	if len(rest) == 0 {
		a.errorf("run: no command given")
		fmt.Fprint(a.env.Stderr, runUsage)
		return ExitUsage
	}
	extraEnv, err := splitEnvFlag(envs)
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	name, cmdArgs := rest[0], rest[1:]
	parser := parserKey(name)

	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	// Nothing is executed until jz knows it can parse the result.
	if len(reg.Variants(parser)) == 0 {
		return a.exitFor(&selector.UnknownParserError{Parser: parser, Known: reg.Commands()})
	}
	if sf.variant != "" {
		if _, ok := reg.Lookup(parser, sf.variant); !ok {
			return a.exitFor(&selector.UnknownVariantError{
				Parser:    parser,
				Variant:   sf.variant,
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
		Name:       name,
		Args:       cmdArgs,
		Env:        append(mergedExecEnv(reg, parser), extraEnv...),
		KeepLocale: keepLocale,
		MaxOutput:  maxOutput,
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
	if len(strings.TrimSpace(string(res.Stdout))) == 0 && res.ExitCode != 0 {
		return res.ExitCode
	}

	sel, err := selector.Select(reg, selector.Context{
		Parser:  parser,
		Variant: sf.variant,
		Force:   sf.force,
		OS:      a.env.GOOS,
		Args:    cmdArgs,
		Input:   res.Stdout,
	})
	if err != nil {
		return a.failedRun(err, res.ExitCode)
	}
	data, err := engine.Parse(sel.Entry.Def, res.Stdout, engine.Options{Raw: of.raw, MaxInputSize: maxOutput})
	if err != nil {
		return a.failedRun(err, res.ExitCode)
	}
	extra := map[string]any{
		"exit_status": int64(res.ExitCode),
		"argv":        stringsToAny(append([]string{name}, cmdArgs...)),
	}
	if err := emit(a.env.Stdout, data, sel.Entry, &of, extra); err != nil {
		a.errorf("writing output: %v", err)
		return ExitError
	}
	return res.ExitCode
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

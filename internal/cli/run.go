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

const runUsage = `Usage: jz run [flags] <command> [args...]

Runs <command> with the given arguments (no shell involved), captures its
standard output and converts it to JSON. Standard error is passed through.
The command runs with LC_ALL=C so that its output is stable; use
--keep-locale to inherit the current locale instead.

If the command exits with a non-zero status, jz still parses whatever it
printed, reports the status on stderr and exits with that same status. A
command terminated by a signal yields 128+signal.

Flags:
`

func (a *app) cmdRun(args []string) int {
	fs := newFlagSet("run")
	var rf registryFlags
	var of outputFlags
	var sf selectFlags
	var envs stringList
	var keepLocale bool
	var timeout time.Duration
	var maxOutput int64
	rf.bind(fs)
	of.bind(fs)
	sf.bind(fs, a.env.GOOS)
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
		return ExitUsage
	}
	extraEnv, err := splitEnvFlag(envs)
	if err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	name, cmdArgs := rest[0], rest[1:]
	key := commandKey(name)

	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	// Refuse to execute anything jz cannot parse, before touching the
	// system.
	if len(reg.Variants(key)) == 0 {
		return a.exitFor(&selector.UnknownCommandError{Command: key, Known: reg.Commands()})
	}
	if sf.variant != "" {
		if _, ok := reg.Lookup(key, sf.variant); !ok {
			_, err := selector.Select(reg, selector.Context{Command: key, Variant: sf.variant})
			return a.exitFor(err)
		}
	}

	ctx := a.env.Context
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	// Definition exec.env applies only once the variant is known; since
	// several variants may share a command, apply the union of their env
	// when they agree and let --env override.
	defEnv := mergedExecEnv(reg, key)
	res, err := runner.Run(ctx, runner.Command{
		Name:       name,
		Args:       cmdArgs,
		Env:        append(defEnv, extraEnv...),
		KeepLocale: keepLocale,
		MaxOutput:  maxOutput,
		Stdin:      nil,
	}, a.env.Stderr, a.env.Signals)
	if err != nil {
		if errors.Is(err, runner.ErrOutputTooLarge) {
			a.errorf("%v", err)
			return ExitParse
		}
		a.errorf("%v", err)
		return ExitError
	}
	if res.Signal != "" {
		a.errorf("%s was terminated by %s", name, res.Signal)
	} else if res.ExitCode != 0 {
		a.errorf("%s exited with status %d", name, res.ExitCode)
	}
	if ctx.Err() != nil && timeout > 0 {
		a.errorf("timeout of %s reached", timeout)
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 && res.ExitCode != 0 {
		return res.ExitCode
	}

	sel, err := selector.Select(reg, selector.Context{Command: key, Variant: sf.variant, OS: sf.os, Args: cmdArgs, Input: res.Stdout})
	if err != nil {
		code := a.exitFor(err)
		if res.ExitCode != 0 {
			return res.ExitCode
		}
		return code
	}
	data, err := engine.Parse(sel.Entry.Def, res.Stdout, engine.Options{Raw: of.raw, MaxInputSize: maxOutput})
	if err != nil {
		code := a.exitFor(err)
		if res.ExitCode != 0 {
			return res.ExitCode
		}
		return code
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

// commandKey maps an executable path to its registry key: the base name
// without a Windows extension.
func commandKey(name string) string {
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
// Conflicting values are dropped so that no variant is favoured.
func mergedExecEnv(reg *registry.Registry, key string) []string {
	values := map[string]string{}
	conflict := map[string]bool{}
	for _, e := range reg.Variants(key) {
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
			out = append(out, fmt.Sprintf("%s=%s", k, values[k]))
		}
	}
	return out
}

func stringsToAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

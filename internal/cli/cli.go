// Package cli implements the jz command line.
//
// The default behaviour, with no subcommand, is to read text from
// standard input, identify which command produced it and write JSON.
// Everything else (running the command for you, listing parsers, the
// version) is a subcommand. Data goes to stdout and only ever as JSON;
// diagnostics go to stderr with a "jz:" prefix, so a consumer reading
// stdout sees valid JSON or nothing at all.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/engine"
	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/selector"
)

// Exit codes. They are a stable contract documented in the README.
const (
	ExitOK       = 0 // success
	ExitError    = 1 // unexpected failure (I/O, internal)
	ExitUsage    = 2 // bad command line
	ExitParse    = 3 // the input did not match the chosen definition
	ExitSelect   = 4 // the format could not be identified, or was ambiguous
	ExitRegistry = 5 // a registry could not be loaded
)

const flagHelp = "--help"

// MaxInputSize bounds the text jz reads, from a pipe, a file or a
// command's stdout. It is a safety limit rather than a preference, so it
// is not a command line option: a runaway producer must not be able to
// make jz allocate without bound.
const MaxInputSize = 64 << 20

// Env is the process environment the CLI runs in, injected for tests.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Getenv reads environment variables (os.Getenv by default).
	Getenv func(string) string
	// UserConfigDir locates the user registry.
	UserConfigDir func() (string, error)
	// StdinIsTerminal reports whether standard input is a terminal. It is
	// consulted for exactly one thing: `jz` with no arguments at all
	// prints help instead of blocking on a terminal. It never changes how
	// input is read or parsed.
	StdinIsTerminal func() bool
	// GOOS is the running operating system.
	GOOS string
	// Signals receives interrupts to forward to child processes (may be nil).
	Signals <-chan os.Signal
	// Context bounds long operations.
	Context context.Context
	// Embedded is the built-in registry.
	Embedded registry.Source
}

type app struct {
	env Env
}

type command struct {
	name    string
	summary string
	run     func(a *app, args []string) int
}

func commands() []command {
	return []command{
		{"run", "run a command and convert its stdout to JSON", (*app).cmdRun},
		{"list", "list supported parsers, or inspect one", (*app).cmdList},
		{"version", "print the version", (*app).cmdVersion},
	}
}

// Main runs jz with the given arguments and returns the exit code.
func Main(args []string, env Env) int {
	a := &app{env: env}
	if a.env.Context == nil {
		a.env.Context = context.Background()
	}
	if a.env.Getenv == nil {
		a.env.Getenv = os.Getenv
	}
	if a.env.UserConfigDir == nil {
		a.env.UserConfigDir = os.UserConfigDir
	}
	if a.env.StdinIsTerminal == nil {
		a.env.StdinIsTerminal = func() bool { return false }
	}
	if len(args) > 0 {
		switch args[0] {
		case "-h", flagHelp, "help":
			if len(args) > 1 {
				if c := lookup(args[1]); c != nil {
					return c.run(a, []string{flagHelp})
				}
			}
			a.usage(a.env.Stdout)
			return ExitOK
		case "-v", "--version":
			return a.cmdVersion(nil)
		}
		if c := lookup(args[0]); c != nil {
			return c.run(a, args[1:])
		}
	}
	if len(args) == 0 && a.env.StdinIsTerminal() {
		// Nothing to read and nothing asked for: help beats hanging on a
		// terminal. Piped and redirected input never take this path.
		a.usage(a.env.Stdout)
		return ExitOK
	}
	return a.cmdConvert(args)
}

func lookup(name string) *command {
	for _, c := range commands() {
		if c.name == name {
			return &c
		}
	}
	return nil
}

func (a *app) usage(w io.Writer) {
	fmt.Fprintln(w, "jsonize - Turn command output into JSON.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  COMMAND | jz [options]")
	fmt.Fprintln(w, "  jz [options] < FILE")
	fmt.Fprintln(w, "  jz run [options] COMMAND [args...]")
	fmt.Fprintln(w, "  jz list [COMMAND [VARIANT]]")
	fmt.Fprintln(w, "  jz version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	a.printOptions(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  df -h | jz")
	fmt.Fprintln(w, "  jz run df -h")
	fmt.Fprintln(w, "  jz --file captured.txt")
	fmt.Fprintln(w, "  df -h | jz --parser df")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 error, 2 usage, 3 parse failure, 4 unidentified or")
	fmt.Fprintln(w, "ambiguous input, 5 registry problem; `jz run` mirrors the command's own status.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `jz run --help` or `jz list --help` for a subcommand's own options.")
	fmt.Fprintln(w)
	printLinks(w)
}

// printLinks closes the help with where to read more, where to report a
// problem and where to support the work.
func printLinks(w io.Writer) {
	fmt.Fprintln(w, "Documentation:   https://nao1215.github.io/jsonize/")
	fmt.Fprintln(w, "Report an issue: https://github.com/nao1215/jsonize/issues")
	fmt.Fprintln(w, "GitHub Sponsors: https://github.com/sponsors/nao1215")
}

// printOptions lists the options of the default (conversion) mode, which
// are the ones the root help is about.
func (a *app) printOptions(w io.Writer) {
	o := newOptions("jz")
	var co convertOptions
	co.bind(o)
	o.print(w)
}

// errorf prints a diagnostic to stderr. Multi-line messages keep the
// prefix on the first line only.
func (a *app) errorf(format string, args ...any) {
	fmt.Fprintf(a.env.Stderr, "jz: "+format+"\n", args...)
}

// exitFor maps an error to an exit code and prints it.
func (a *app) exitFor(err error) int {
	a.errorf("%v", err)
	var pe *engine.ParseError
	if errors.As(err, &pe) {
		return ExitParse
	}
	var (
		up *selector.UnknownParserError
		uv *selector.UnknownVariantError
		nm *selector.NoMatchError
		am *selector.AmbiguousError
		me *selector.MismatchError
	)
	if errors.As(err, &up) || errors.As(err, &uv) || errors.As(err, &nm) || errors.As(err, &am) || errors.As(err, &me) {
		return ExitSelect
	}
	var vp *selector.VariantWithoutParserError
	if errors.As(err, &vp) {
		return ExitUsage
	}
	var (
		le *registry.LoadError
		ve *definition.ValidationError
		fe *definition.FormatError
	)
	if errors.As(err, &le) || errors.As(err, &ve) || errors.As(err, &fe) {
		return ExitRegistry
	}
	return ExitError
}

// stringList is a repeatable flag.
// narrow applies --extract or --exclude to a result. The options are
// checked when they are parsed, so the only failure left here is a key
// the format does not produce.
func (a *app) narrow(v any, out *outputOptions) (any, int) {
	f, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitUsage
	}
	narrowed, err := f.apply(v)
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitUsage
	}
	return narrowed, ExitOK
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

// Set appends a value; it implements flag.Value.
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringsToAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

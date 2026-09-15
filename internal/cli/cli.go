// Package cli implements the jz command line.
//
// The default behaviour, with no subcommand, is to read text from
// standard input, identify which command produced it and write JSON.
// Everything else (running the command for you, listing parsers,
// checking a registry of definitions, the version) is a subcommand. Data goes to stdout and only ever as JSON;
// diagnostics go to stderr with a "jz:" prefix, so a consumer reading
// stdout sees valid JSON or nothing at all.
package cli

import (
	"context"
	"errors"
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

// Exit codes. They are a stable contract documented in the README.
const (
	ExitOK       = 0 // success
	ExitError    = 1 // unexpected failure (I/O, internal)
	ExitUsage    = 2 // bad command line
	ExitParse    = 3 // the input did not match the chosen definition
	ExitSelect   = 4 // the format could not be identified, or was ambiguous
	ExitRegistry = 5 // a registry could not be loaded
	// ExitOutputClosed is 128+SIGPIPE: standard output was closed before
	// the document was complete, as it is when the reader is `head` and
	// has seen enough. There is nobody left to tell, so jz says nothing,
	// stops the command it started and ends the way any filter does.
	ExitOutputClosed = 141
)

const flagHelp = "--help"

// MaxInputSize bounds the text jz reads, from a pipe, a file or a
// command's stdout. It is a safety limit rather than a preference, so it
// is not a command line option: a runaway producer must not be able to
// make jz allocate without bound. It is the engine's own default rather
// than a second number that happens to agree with it.
const MaxInputSize = engine.DefaultMaxInputSize

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
	// Context bounds long operations.
	Context context.Context
	// Embedded is the built-in registry.
	Embedded registry.Source
}

type app struct {
	env Env
	// wrapperHint is what `jz run` adds to a refusal when the command it
	// was given looks like a wrapper around one jz has a parser for: the
	// command line that names that parser.
	wrapperHint string
	// named and renamed are the definition --variant named and its copy
	// with the columns --columns gave, which is read in its place.
	named, renamed *definition.Definition
}

type command struct {
	name    string
	summary string
	run     func(a *app, args []string) int
}

func commands() []command {
	return []command{
		{modeRun, "run a command and convert its stdout to JSON", (*app).cmdRun},
		{modeNew, "make a JSON object or array from arguments", (*app).cmdNew},
		{modeList, "list supported parsers, or inspect one", (*app).cmdList},
		{modeTest, "check parser definitions and their fixtures", (*app).cmdTest},
		{"completion", "print a shell completion script (bash, zsh)", (*app).cmdCompletion},
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
		case completeCommand:
			return a.cmdComplete(args[1:])
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
	fmt.Fprintln(w, "jsonize - Turn command output and data files into JSON.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  COMMAND | jz [options]")
	fmt.Fprintln(w, "  jz [options] < FILE")
	fmt.Fprintln(w, "  jz [options] --file DATA.csv|.tsv|.ltsv|.jsonl|.json|.yaml")
	fmt.Fprintln(w, "  jz run [options] COMMAND [args...]")
	fmt.Fprintln(w, "  jz new [options] KEY=VALUE KEY:=JSON ...")
	fmt.Fprintln(w, "  jz list [COMMAND [VARIANT]]")
	fmt.Fprintln(w, "  jz test [DIR...]")
	fmt.Fprintln(w, "  jz completion bash|zsh")
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
	fmt.Fprintln(w, "  vmstat 1 | jz --stream")
	fmt.Fprintln(w, "  df -h | jz --yaml")
	fmt.Fprintln(w, "  jz --file captured.txt")
	fmt.Fprintln(w, "  jz --file users.csv")
	fmt.Fprintln(w, "  cat events.log | jz --format ltsv --stream")
	fmt.Fprintln(w, "  jz new name=api replicas:=3 tags[]=web")
	fmt.Fprintln(w, "  df -h | jz --parser df")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 error, 2 usage, 3 parse failure, 4 unidentified or")
	fmt.Fprintln(w, "ambiguous input, 5 registry problem; `jz run` mirrors the command's own status.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `jz run --help`, `jz new --help`, `jz list --help`, `jz test --help` or")
	fmt.Fprintln(w, "`jz completion --help`")
	fmt.Fprintln(w, "for a subcommand's own options.")
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

// writeFailed answers a failure to write standard output. A reader that
// has gone away is not an error to report: the message would go nowhere
// useful, and the shell already knows. Anything else is unexpected.
func (a *app) writeFailed(err error) int {
	if outputClosed(err) {
		return ExitOutputClosed
	}
	a.errorf("writing output: %v", err)
	return ExitError
}

// exitFor maps an error to an exit code and prints it.
func (a *app) exitFor(err error) int {
	if outputClosed(err) {
		return ExitOutputClosed
	}
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
		if a.wrapperHint != "" && (up != nil || nm != nil) {
			fmt.Fprintln(a.env.Stderr, a.wrapperHint)
		}
		return ExitSelect
	}
	var vp *selector.VariantWithoutParserError
	if errors.As(err, &vp) {
		return ExitUsage
	}
	var ce *datafile.CompressionError
	if errors.As(err, &ce) {
		return ExitParse
	}
	var (
		le *registry.LoadError
		ve *definition.ValidationError
		fe *definition.FormatError
		ue *definition.UnknownKeyError
	)
	if errors.As(err, &le) || errors.As(err, &ve) || errors.As(err, &fe) || errors.As(err, &ue) {
		return ExitRegistry
	}
	return ExitError
}

// stringList is a repeatable flag.
// narrow applies --extract or --exclude to a result read with def. The
// options are checked when they are parsed, so the only failure left
// here is a key the format does not produce.
func (a *app) narrow(v any, out *outputOptions, def *definition.Definition) (any, int) {
	f, err := out.filter()
	if err != nil {
		a.errorf("%v", err)
		return nil, ExitUsage
	}
	if err := f.know(def); err != nil {
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

func stringsToAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

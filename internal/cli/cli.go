// Package cli implements the jz command line. Every subcommand writes
// machine-readable data to stdout only; diagnostics go to stderr with a
// "jz:" prefix so that JSON consumers never see them mixed in.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	ExitParse    = 3 // input did not match the selected definition
	ExitSelect   = 4 // no definition, no matching variant, or ambiguous
	ExitRegistry = 5 // a registry could not be loaded, validated or updated
)

// Env is the process environment the CLI runs in, injected for tests.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Getenv reads environment variables (os.Getenv by default).
	Getenv func(string) string
	// UserConfigDir / UserCacheDir locate the user registries.
	UserConfigDir func() (string, error)
	UserCacheDir  func() (string, error)
	// GOOS is the running operating system.
	GOOS string
	// Signals receives interrupts to forward to child processes (may be nil).
	Signals <-chan os.Signal
	// Context bounds long operations.
	Context context.Context
	// Embedded is the built-in registry.
	Embedded registry.Source
	// HTTPClient is used for registry updates (nil = a default client
	// with timeouts).
	HTTPClient *http.Client
}

type command struct {
	name    string
	summary string
	run     func(a *app, args []string) int
}

type app struct {
	env Env
}

// Main runs jz with the given arguments and returns the exit code.
func Main(args []string, env Env) int {
	a := &app{env: env}
	if env.Context == nil {
		a.env.Context = context.Background()
	}
	if env.Getenv == nil {
		a.env.Getenv = os.Getenv
	}
	if env.UserConfigDir == nil {
		a.env.UserConfigDir = os.UserConfigDir
	}
	if env.UserCacheDir == nil {
		a.env.UserCacheDir = os.UserCacheDir
	}
	if len(args) == 0 {
		a.usage(a.env.Stderr)
		return ExitUsage
	}
	name := args[0]
	switch name {
	case "-h", flagHelp, "help":
		if len(args) > 1 {
			if c := a.lookup(args[1]); c != nil {
				return c.run(a, []string{flagHelp})
			}
		}
		a.usage(a.env.Stdout)
		return ExitOK
	case "-v", "-version", "--version":
		name = "version"
	}
	c := a.lookup(name)
	if c == nil {
		a.errorf("unknown command %q", name)
		a.usage(a.env.Stderr)
		return ExitUsage
	}
	return c.run(a, args[1:])
}

func (a *app) commands() []command {
	return []command{
		{"run", "run a command and convert its output to JSON", (*app).cmdRun},
		{"parse", "convert output read from stdin or a file to JSON", (*app).cmdParse},
		{"list", "list supported commands and variants", (*app).cmdList},
		{"show", "show the definition of a command or variant", (*app).cmdShow},
		{"validate", "validate definitions and run their golden tests", (*app).cmdValidate},
		{"registry", "manage registries (update, paths)", (*app).cmdRegistry},
		{"version", "print the version", (*app).cmdVersion},
	}
}

func (a *app) lookup(name string) *command {
	for _, c := range a.commands() {
		if c.name == name {
			c := c
			return &c
		}
	}
	return nil
}

func (a *app) usage(w io.Writer) {
	fmt.Fprintln(w, "jsonize - Turn command output into JSON.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  jz run   [flags] <command> [args...]   run a command and parse its stdout")
	fmt.Fprintln(w, "  jz parse [flags] <command>             parse output from stdin or --file")
	fmt.Fprintln(w, "  jz <subcommand> --help                 show subcommand flags")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	for _, c := range a.commands() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 error, 2 usage, 3 parse failure, 4 no/ambiguous variant,")
	fmt.Fprintln(w, "5 registry problem; `jz run` mirrors a failing command's own exit status.")
}

// errorf prints a diagnostic to stderr.
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
	var uc *selector.UnknownCommandError
	var uv *selector.UnknownVariantError
	var nm *selector.NoMatchError
	var am *selector.AmbiguousError
	if errors.As(err, &uc) || errors.As(err, &uv) || errors.As(err, &nm) || errors.As(err, &am) {
		return ExitSelect
	}
	var le *registry.LoadError
	var ve *definition.ValidationError
	var fe *definition.FormatError
	if errors.As(err, &le) || errors.As(err, &ve) || errors.As(err, &fe) {
		return ExitRegistry
	}
	return ExitError
}

// stringList is a repeatable flag.
type stringList []string

const flagHelp = "--help"

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

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// registryFlags are shared by every mode that loads definitions.
type registryFlags struct {
	dirs         stringList
	embeddedOnly bool
}

func (f *registryFlags) bind(fs *flag.FlagSet) {
	fs.Var(&f.dirs, "registry", "additional registry `dir` (repeatable; earlier wins)")
	fs.BoolVar(&f.embeddedOnly, "embedded-only", false, "ignore the user registry and JSONIZE_REGISTRY_PATH")
}

// outputFlags control JSON emission.
type outputFlags struct {
	pretty bool
	raw    bool
	meta   bool
}

func (f *outputFlags) bind(fs *flag.FlagSet) {
	fs.BoolVar(&f.pretty, "pretty", false, "indent the JSON output")
	fs.BoolVar(&f.pretty, "p", false, "shorthand for --pretty")
	fs.BoolVar(&f.raw, "raw", false, "skip type conversion; every value stays a string")
	fs.BoolVar(&f.raw, "r", false, "shorthand for --raw")
	fs.BoolVar(&f.meta, "meta", false, "wrap the result with parser, variant and source metadata")
}

// selectFlags narrow or pin the automatic detection.
type selectFlags struct {
	parser  string
	variant string
	force   bool
}

// bind registers the flags shared by both modes. withParser is false for
// `jz run`, where the command name already names the parser.
func (f *selectFlags) bind(fs *flag.FlagSet, withParser bool) {
	if withParser {
		fs.StringVar(&f.parser, "parser", "", "only consider this parser's variants (a command `name`)")
	}
	fs.StringVar(&f.variant, "variant", "", "use this `variant` of the parser")
	fs.BoolVar(&f.force, "force", false, "parse with the named variant even if its signature does not match the input")
}

// newFlagSet builds a FlagSet whose usage text is printed by the caller.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args and prints usage on --help or error. The bool
// result is true when the caller should return the given exit code.
func (a *app) parseFlags(fs *flag.FlagSet, args []string, usage string) (int, bool) {
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.env.Stdout, usage)
		fs.SetOutput(a.env.Stdout)
		fs.PrintDefaults()
		return ExitOK, true
	}
	if err != nil {
		a.errorf("%v", err)
		fmt.Fprint(a.env.Stderr, usage)
		fs.SetOutput(a.env.Stderr)
		fs.PrintDefaults()
		return ExitUsage, true
	}
	return 0, false
}

// splitEnvFlag validates NAME=value entries.
func splitEnvFlag(entries []string) ([]string, error) {
	for _, e := range entries {
		k, _, ok := strings.Cut(e, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--env expects NAME=value, got %q", e)
		}
	}
	return entries, nil
}

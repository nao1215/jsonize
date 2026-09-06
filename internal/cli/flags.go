package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// registryFlags are shared by every subcommand that loads definitions.
type registryFlags struct {
	dirs         stringList
	embeddedOnly bool
}

func (f *registryFlags) bind(fs *flag.FlagSet) {
	fs.Var(&f.dirs, "registry", "additional registry `dir` (repeatable; earlier wins)")
	fs.BoolVar(&f.embeddedOnly, "embedded-only", false, "ignore user, cached and JSONIZE_REGISTRY_PATH registries")
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
	fs.BoolVar(&f.meta, "meta", false, "wrap the result with command, variant and source metadata")
}

// selectFlags choose a variant.
type selectFlags struct {
	variant string
	os      string
}

func (f *selectFlags) bind(fs *flag.FlagSet, defaultOS string) {
	fs.StringVar(&f.variant, "variant", "", "use this variant instead of auto-detection")
	fs.StringVar(&f.os, "os", defaultOS, "operating system that produced the output (linux, darwin, ...); empty = unknown")
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

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nao1215/jsonize/internal/conformance"
	"github.com/nao1215/jsonize/internal/registry"
)

const validateUsage = `Usage: jz validate [flags] [dir...]

Validates every parser definition in the given registry directories and
runs their golden tests (testdata/<case>.txt against <case>.json). Without
arguments the user registry and the cached official registry are checked.
Use --update to (re)write the golden JSON from the current output, then
review the diff.

Flags:
`

func (a *app) cmdValidate(args []string) int {
	fs := newFlagSet("validate")
	var update, quiet bool
	fs.BoolVar(&update, "update", false, "write testdata/<case>.json files from the current output")
	fs.BoolVar(&quiet, "quiet", false, "only print failures")
	if code, done := a.parseFlags(fs, args, validateUsage); done {
		return code
	}
	dirs := fs.Args()
	if len(dirs) == 0 {
		for _, f := range []func() (string, error){a.userRegistryDir, a.cacheRegistryDir} {
			if d, err := f(); err == nil {
				if _, err := os.Stat(d); err == nil {
					dirs = append(dirs, d)
				}
			}
		}
		if len(dirs) == 0 {
			a.errorf("nothing to validate: no user or cached registry exists and no directory was given")
			return ExitUsage
		}
	}
	failed := 0
	for _, dir := range dirs {
		n, err := a.validateDir(dir, update, quiet)
		if err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		failed += n
	}
	if failed > 0 {
		a.errorf("%d problem(s) found", failed)
		return ExitRegistry
	}
	return ExitOK
}

// validateDir validates one registry directory and returns the number of
// problems. An error is returned only for I/O failures.
func (a *app) validateDir(dir string, update, quiet bool) (int, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(filepath.Join(abs, registry.ParsersDir)); err != nil {
		a.errorf("%s: not a registry directory (no %s/ subdirectory)", dir, registry.ParsersDir)
		return 1, nil //nolint:nilerr // reported as a problem; other directories continue
	}
	fsys := os.DirFS(abs)
	reg, err := registry.Load(registry.Source{Name: abs, FS: fsys})
	if err != nil {
		a.errorf("%v", err)
		return 1, nil
	}
	failed := 0
	for _, p := range reg.Problems {
		fmt.Fprintf(a.env.Stdout, "FAIL %v\n", p)
		failed++
	}
	results := conformance.Run(reg, fsys, abs, conformance.Options{Update: update})
	for _, r := range results {
		name := r.Definition
		if r.Case != "" {
			name += " [" + r.Case + "]"
		}
		switch {
		case r.Err != nil:
			failed++
			fmt.Fprintf(a.env.Stdout, "FAIL %s: %s\n", name, indent(r.Err.Error()))
		case update && r.Actual != nil:
			golden := filepath.Join(abs, strings.TrimSuffix(filepath.FromSlash(r.Path), ".txt")+".json")
			if err := os.WriteFile(golden, r.Actual, 0o644); err != nil {
				return failed, err
			}
			fmt.Fprintf(a.env.Stdout, "WROTE %s\n", golden)
		case !quiet:
			fmt.Fprintf(a.env.Stdout, "ok   %s\n", name)
		}
	}
	passed, _ := conformance.Summary(results)
	if !quiet {
		fmt.Fprintf(a.env.Stdout, "%s: %d definitions, %d cases passed\n", dir, reg.Len(), passed)
	}
	return failed, nil
}

func indent(s string) string {
	return strings.ReplaceAll(s, "\n", "\n     ")
}

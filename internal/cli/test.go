package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/internal/conformance"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/registry"
)

const testUsage = `Usage: jz test [options] [DIR...]

Checks parser definitions the way the official ones are checked. Each
fixture is parsed with its own definition and compared with the JSON
beside it; it must give the same answer with CRLF line endings, a byte
order mark, or a blank line before or after it, which leave the text as
it was; each is changed (a foreign line added, the text doubled) and
must be refused or show the change, a doubled list being the records of
one copy twice, which proves the definition reads all of its input; and
every definition is then named explicitly on every other definition's
fixtures and must refuse them.

The official fixtures are built into jz, so a definition of your own is
checked against them without a copy of the repository: a signature wide
enough to read df output is a failure here rather than a surprise later.

With no directory, the registries jz would use are checked, except the
built-in one. With directories, they are layered above the built-in
registry and only their definitions are checked.

  jz test                              check your own registries
  jz test ./registry                   check a directory
  jz test --update ./registry          write testdata/<case>.json
  jz test --decoys ./decoys ./registry also refuse every file in a directory

Nothing is written to standard output. Exit status is 0 when everything
passed, 1 when something failed, 2 for a usage error and 5 when a
registry could not be read.

Options:
`

// testOptions are the options of jz test.
type testOptions struct {
	update bool
	decoys string
}

func (t *testOptions) bind(o *optionSet) {
	o.boolOpt(&t.update, "update", "", "rewrite testdata/<case>.json from the current output")
	o.stringOpt(&t.decoys, "decoys", "", "DIR", "", "require every file under DIR to be refused by every parser")
	o.helpDoc()
}

func (a *app) cmdTest(args []string) int {
	o := newOptions(modeTest)
	var to testOptions
	to.bind(o)
	if code, done := a.parse(o, args, testUsage); done {
		return code
	}
	update, decoys := to.update, to.decoys
	srcs, targets, code := a.testSources(o.fs.Args())
	if code != ExitOK {
		return code
	}
	reg, err := registry.Load(srcs...)
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	opts := conformance.Options{
		Update: update,
		Engine: engine.Options{MaxInputSize: MaxInputSize},
	}
	if decoys != "" {
		if _, err := os.Stat(decoys); errors.Is(err, fs.ErrNotExist) {
			a.errorf("--decoys %s does not exist", decoys)
			return ExitUsage
		}
		list, err := conformance.ReadDecoys(decoys)
		if err != nil {
			a.errorf("%v", err)
			return ExitUsage
		}
		opts.Decoys = list
	}
	for _, w := range reg.Warnings {
		a.errorf("warning: %v", w)
	}
	underTest := map[string]bool{}
	for _, t := range targets {
		underTest[t] = true
	}
	if !a.hasDefinitions(reg, underTest) {
		a.errorf("no definitions to check in %s; name a registry directory to check one",
			strings.Join(targets, ", "))
		return ExitUsage
	}
	results := conformance.Check(reg, srcs, targets, opts)
	// A definition that did not load is a failure of the registry under
	// test, not a warning to scroll past.
	var problems []error
	for _, p := range reg.Problems {
		var le *registry.LoadError
		if !errors.As(p, &le) || underTest[le.Source] {
			problems = append(problems, p)
		}
	}
	return a.reportTest(results, problems, update)
}

// testSources builds the layering to check and names the sources under
// test inside it.
func (a *app) testSources(dirs []string) ([]registry.Source, []string, int) {
	embedded := a.env.Embedded
	if embedded.Name == "" {
		embedded.Name = SourceEmbedded
	}
	if len(dirs) == 0 {
		srcs, err := a.sources()
		if err != nil {
			a.errorf("%v", err)
			return nil, nil, ExitRegistry
		}
		var targets []string
		for _, s := range srcs {
			if s.Name != embedded.Name {
				targets = append(targets, s.Name)
			}
		}
		return srcs, targets, ExitOK
	}
	var (
		srcs    []registry.Source
		targets []string
	)
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			a.errorf("%v", err)
			return nil, nil, ExitRegistry
		}
		if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) {
			a.errorf("%s does not exist", abs)
			return nil, nil, ExitRegistry
		}
		if _, err := os.Stat(filepath.Join(abs, registry.ParsersDir)); err != nil {
			a.errorf("%s is not a registry directory: it has no %s/ inside it", abs, registry.ParsersDir)
			return nil, nil, ExitRegistry
		}
		srcs = append(srcs, registry.Source{Name: abs, FS: os.DirFS(abs)})
		targets = append(targets, abs)
	}
	return append(srcs, embedded), targets, ExitOK
}

// hasDefinitions reports whether any definition under test survived the
// layering. Checking nothing and reporting success would be the wrong
// answer to "check my registry".
func (a *app) hasDefinitions(reg *registry.Registry, underTest map[string]bool) bool {
	for _, e := range reg.Entries() {
		if underTest[e.Source] {
			return true
		}
	}
	return len(reg.Problems) > 0
}

// reportTest writes one entry per failure to stderr and returns the exit
// code. Standard output stays empty: `jz test` reports, it does not
// convert.
func (a *app) reportTest(results []conformance.Result, problems []error, update bool) int {
	passed, failed := conformance.Summary(results)
	for _, p := range problems {
		fmt.Fprintf(a.env.Stderr, "%s\n", indentAfterFirst(p.Error()))
		failed++
	}
	written := map[string]bool{}
	for _, r := range results {
		if r.Err != nil {
			name := r.Definition
			if r.Case != "" {
				name += "/" + r.Case
			}
			fmt.Fprintf(a.env.Stderr, "%s: %s\n", name, indentAfterFirst(r.Err.Error()))
			continue
		}
		if !update || r.Actual == nil {
			continue
		}
		dir, ok := a.sourceDir(r.Source)
		if !ok {
			continue
		}
		golden := filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(r.Path, ".txt")+".json"))
		if err := os.WriteFile(golden, r.Actual, 0o600); err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		written[dir] = true
	}
	if update && len(written) > 0 {
		dirs := make([]string, 0, len(written))
		for d := range written {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		fmt.Fprintf(a.env.Stderr, "golden files written under %s\n", strings.Join(dirs, ", "))
	}
	fmt.Fprintf(a.env.Stderr, "%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return ExitError
	}
	return ExitOK
}

// sourceDir maps a source name back to the directory it was read from.
// The embedded registry has none, which is why --update cannot target it.
func (a *app) sourceDir(name string) (string, bool) {
	switch name {
	case SourceUser:
		dir, err := a.userRegistryDir()
		return dir, err == nil
	case SourceEmbedded, a.env.Embedded.Name:
		return "", false
	default:
		return name, filepath.IsAbs(name)
	}
}

// indentAfterFirst keeps a multi-line message (a golden diff) attached to
// the failure it belongs to.
func indentAfterFirst(s string) string {
	return strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}

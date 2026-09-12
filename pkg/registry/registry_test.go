package registry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/definition"
)

func def(cmd, variant string) string {
	return "format: 1\ncommand: " + cmd + "\nvariant: " + variant + "\nparse: {type: kv}\n"
}

func TestLoadMergesSourcesWithPrecedence(t *testing.T) {
	t.Parallel()
	high := fstest.MapFS{
		"registry.yaml":                      {Data: []byte("format: 1\nname: user\n")},
		"parsers/df/gnu/parser.yaml":         {Data: []byte(def("df", "gnu"))},
		"parsers/custom/default/parser.yaml": {Data: []byte(def("custom", "default"))},
	}
	low := fstest.MapFS{
		"parsers/df/gnu/parser.yaml":              {Data: []byte(def("df", "gnu"))},
		"parsers/df/bsd/parser.yaml":              {Data: []byte(def("df", "bsd"))},
		"parsers/df/bsd/testdata/a.txt":           {Data: []byte("x")},
		"parsers/df/bsd/testdata/a.json":          {Data: []byte("[]")},
		"parsers/df/bsd/testdata/notes.md":        {Data: []byte("ignored")},
		"parsers/uptime/linux/parser.yaml":        {Data: []byte(def("uptime", "linux"))},
		"parsers/uptime/linux/testdata/fail.txt":  {Data: []byte("bad")},
		"parsers/uptime/linux/testdata/fail.yaml": {Data: []byte("expect_error: does not match\nargs: [-a]\nos: linux\n")},
		"parsers/uptime/linux/testdata/ok.txt":    {Data: []byte("x")},
		"parsers/uptime/linux/testdata/ok.json":   {Data: []byte("{}")},
	}
	reg, err := Load(Source{Name: "user", FS: high}, Source{Name: "embedded", FS: low}, Source{Name: "missing", FS: os.DirFS(filepath.Join(t.TempDir(), "absent")), Optional: true})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Len() != 4 {
		t.Errorf("Len = %d, want 4", reg.Len())
	}
	e, ok := reg.Lookup("df", "gnu")
	if !ok || e.Source != "user" || len(e.Shadowed) != 1 || e.Shadowed[0] != "embedded" {
		t.Errorf("df/gnu = %+v", e)
	}
	if e.Def.Origin != "user" {
		t.Errorf("Origin = %s", e.Def.Origin)
	}
	if got := reg.Commands(); strings.Join(got, ",") != "custom,df,uptime" {
		t.Errorf("Commands = %v", got)
	}
	vs := reg.Variants("df")
	if len(vs) != 2 || vs[0].Def.Variant != "bsd" || vs[1].Def.Variant != "gnu" {
		t.Errorf("Variants = %v", vs)
	}
	if len(reg.Variants("nope")) != 0 {
		t.Error("unknown command should have no variants")
	}
	if ents := reg.Entries(); len(ents) != 4 || ents[0].Def.ID() != "custom/default" {
		t.Errorf("Entries = %v", ents)
	}
	if len(reg.Problems) != 0 {
		t.Errorf("Problems = %v", reg.Problems)
	}
	// The slice Variants hands out is the caller's: changing it changes
	// nothing in the registry.
	vs[0], vs[1] = vs[1], vs[0]
	if again := reg.Variants("df"); again[0].Def.Variant != "bsd" {
		t.Errorf("Variants after the caller reordered its copy = %v", again)
	}
}

func TestLoadProblems(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"parsers/df/gnu/parser.yaml":     {Data: []byte(def("df", "bsd"))}, // wrong dir
		"parsers/bad/x/parser.yaml":      {Data: []byte("format: 1\n")},
		"parsers/ok/default/parser.yaml": {Data: []byte(def("ok", "default"))},
		"parsers/ok/default/README":      {Data: []byte("not a definition")},
	}
	reg, err := Load(Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Len() != 1 {
		t.Errorf("Len = %d", reg.Len())
	}
	if len(reg.Problems) != 2 {
		t.Fatalf("Problems = %v", reg.Problems)
	}
	var le *LoadError
	if !errors.As(reg.Problems[0], &le) || le.Source != "s" || le.Unwrap() == nil {
		t.Errorf("problem type: %v", reg.Problems[0])
	}
	msgs := reg.Problems[0].Error() + reg.Problems[1].Error()
	if !strings.Contains(msgs, "must live at parsers/df/bsd/parser.yaml") || !strings.Contains(msgs, "command: is required") {
		t.Errorf("messages: %s", msgs)
	}
}

func TestLoadFatalErrors(t *testing.T) {
	t.Parallel()
	if _, err := Load(Source{Name: "nil"}); err == nil {
		t.Error("nil FS should fail")
	}
	if _, err := Load(Source{Name: "missing", FS: os.DirFS(filepath.Join(t.TempDir(), "absent"))}); err == nil {
		t.Error("missing non-optional source should fail")
	}
	_, err := Load(Source{Name: "m", FS: fstest.MapFS{"registry.yaml": {Data: []byte("format: [")}}})
	if err == nil || !strings.Contains(err.Error(), "invalid manifest") {
		t.Errorf("bad manifest: %v", err)
	}
	_, err = Load(Source{Name: "m", FS: fstest.MapFS{"registry.yaml": {Data: []byte("format: 2\n")}}})
	var fe *definition.FormatError
	if !errors.As(err, &fe) {
		t.Errorf("format mismatch: %v", err)
	}
	reg, err := Load(Source{Name: "m", FS: fstest.MapFS{"registry.yaml": {Data: []byte("format: 1\nname: empty\n")}}})
	if err != nil || reg.Len() != 0 {
		t.Errorf("manifest only: %v %v", reg, err)
	}
}

// A manifest's disable list switches off definitions of the registries
// below it. A user who hits a misdetecting official definition can take
// it out instead of rewriting it.
func TestDisableRemovesDefinitionsOfLowerSources(t *testing.T) {
	t.Parallel()
	high := fstest.MapFS{
		"registry.yaml":                    {Data: []byte("format: 1\nname: user\ndisable:\n  - file/posix\n  - du\n  - nothing-here\n")},
		"parsers/mine/default/parser.yaml": {Data: []byte(def("mine", "default"))},
	}
	low := fstest.MapFS{
		"parsers/file/posix/parser.yaml":   {Data: []byte(def("file", "posix"))},
		"parsers/file/bsd/parser.yaml":     {Data: []byte(def("file", "bsd"))},
		"parsers/du/posix/parser.yaml":     {Data: []byte(def("du", "posix"))},
		"parsers/du/gnu-human/parser.yaml": {Data: []byte(def("du", "gnu-human"))},
		"parsers/uptime/linux/parser.yaml": {Data: []byte(def("uptime", "linux"))},
	}
	reg, err := Load(Source{Name: "user", FS: high}, Source{Name: "embedded", FS: low})
	if err != nil {
		t.Fatal(err)
	}
	// A disabled definition is gone from every way of reaching it, not
	// hidden from some of them.
	for _, id := range []string{"file/posix", "du/posix", "du/gnu-human"} {
		command, variant, _ := strings.Cut(id, "/")
		if _, ok := reg.Lookup(command, variant); ok {
			t.Errorf("%s is still reachable by Lookup", id)
		}
	}
	if got := len(reg.Variants("du")); got != 0 {
		t.Errorf("du still has %d variants", got)
	}
	for _, c := range reg.Commands() {
		if c == "du" {
			t.Error("du is still a command")
		}
	}
	if _, ok := reg.Lookup("file", "bsd"); !ok {
		t.Error("file/bsd was disabled although only file/posix was named")
	}
	if got := reg.Disabled("embedded"); got != 3 {
		t.Errorf("Disabled(embedded) = %d, want 3", got)
	}
	// An entry that named nothing is worth saying, not worth failing on.
	if len(reg.Warnings) != 1 || !strings.Contains(reg.Warnings[0].Error(), `disable "nothing-here"`) {
		t.Errorf("warnings = %v", reg.Warnings)
	}
}

// A registry never disables itself, and a registry below cannot disable
// one above it.
func TestDisableOnlyReachesDownwards(t *testing.T) {
	t.Parallel()
	high := fstest.MapFS{
		"parsers/df/gnu/parser.yaml": {Data: []byte(def("df", "gnu"))},
	}
	low := fstest.MapFS{
		"registry.yaml":                   {Data: []byte("format: 1\nname: low\ndisable: [df, own]\n")},
		"parsers/own/default/parser.yaml": {Data: []byte(def("own", "default"))},
	}
	reg, err := Load(Source{Name: "user", FS: high}, Source{Name: "embedded", FS: low})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Lookup("df", "gnu"); !ok {
		t.Error("a lower registry disabled a definition above it")
	}
	if _, ok := reg.Lookup("own", "default"); !ok {
		t.Error("a registry disabled its own definition")
	}
}

func TestDisableSyntaxIsChecked(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"Bad", "a/b/c", "", "df/", "df gnu"} {
		fsys := fstest.MapFS{
			"registry.yaml":              {Data: []byte("format: 1\nname: x\ndisable: [\"" + entry + "\"]\n")},
			"parsers/df/gnu/parser.yaml": {Data: []byte(def("df", "gnu"))},
		}
		_, err := Load(Source{Name: "s", FS: fsys})
		if err == nil {
			t.Errorf("disable %q was accepted", entry)
			continue
		}
		if !strings.Contains(err.Error(), "must be a command or a command/variant") {
			t.Errorf("disable %q: %v", entry, err)
		}
	}
}

// failingFS refuses one path with the error it is given and answers for
// everything else, which is what a directory jz has no permission on
// looks like from here.
type failingFS struct {
	fs.FS
	path string
	err  error
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return f.FS.Open(name)
}

func (f failingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return fs.ReadDir(f.FS, name)
}

// A definition jz cannot read is that definition's problem, the same as
// one that does not parse or one over the size bound: the registry it
// sits in, and the registries below it, still load.
func TestUnreadableDefinitionIsOneDefinitionsProblem(t *testing.T) {
	t.Parallel()
	base := fstest.MapFS{
		"registry.yaml":                   {Data: []byte("format: 1\nname: s\n")},
		"parsers/ok/default/parser.yaml":  {Data: []byte(def("ok", "default"))},
		"parsers/bad/one/parser.yaml":     {Data: []byte(def("bad", "one"))},
		"parsers/bad/one/testdata/a.txt":  {Data: []byte("x")},
		"parsers/bad/one/testdata/a.json": {Data: []byte("[]")},
	}
	for _, tt := range []struct {
		name string
		path string
	}{
		{"file", "parsers/bad/one/parser.yaml"},
		{"directory", "parsers/bad"},
		{"the parsers directory itself", "parsers"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, err := Load(Source{Name: "s", FS: failingFS{FS: base, path: tt.path, err: fs.ErrPermission}})
			if err != nil {
				t.Fatalf("Load = %v, want the other definitions to load", err)
			}
			if len(reg.Problems) != 1 {
				t.Fatalf("Problems = %v, want the one file that could not be read", reg.Problems)
			}
			if msg := reg.Problems[0].Error(); !strings.Contains(msg, tt.path) || !strings.Contains(msg, "permission denied") {
				t.Errorf("problem = %s, want it to name %s", msg, tt.path)
			}
			want := 1
			if tt.path == "parsers" {
				want = 0
			}
			if reg.Len() != want {
				t.Errorf("Len = %d, want %d", reg.Len(), want)
			}
			if _, ok := reg.Lookup("bad", "one"); ok {
				t.Error("a definition that could not be read must not be in the registry")
			}
		})
	}
}

// A registry whose parsers is a file rather than a directory says so.
// Loading it as empty leaves the author with a registry that changes
// nothing and no reason why.
func TestParsersMustBeADirectory(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"registry.yaml": {Data: []byte("format: 1\nname: s\n")},
		"parsers":       {Data: []byte("not a directory\n")},
	}
	reg, err := Load(Source{Name: "s", FS: fsys})
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(reg.Problems) != 1 {
		t.Fatalf("Problems = %v, want one naming parsers", reg.Problems)
	}
	if msg := reg.Problems[0].Error(); !strings.Contains(msg, "parsers") || !strings.Contains(msg, "not a directory") {
		t.Errorf("problem = %s", msg)
	}
}

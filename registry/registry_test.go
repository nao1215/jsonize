package registry

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/conformance"
	"github.com/nao1215/jsonize/internal/schema"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
)

// This test is a thin wrapper around the same check `jz test` runs, so
// the official registry is held to the contract third parties are told
// to hold theirs to. `make registry-test` runs it and
// `make registry-update-golden` regenerates the expected JSON.
var update = flag.Bool("update", false, "rewrite testdata/<case>.json golden files from the current output")

func TestRegistry(t *testing.T) {
	const name = "embedded"
	fsys := FS()
	src := registry.Source{Name: name, FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Problems {
		t.Errorf("definition failed to load: %v", p)
	}
	if reg.Len() == 0 {
		t.Fatalf("%s contains no definitions", name)
	}
	opts := conformance.Options{Update: *update}
	if testing.Short() {
		// The cross product of every definition with every fixture is the
		// expensive half; -short keeps the golden cases.
		opts.SkipExclusivity = true
	} else {
		opts.Decoys = readDecoys(t)
	}
	results := conformance.Check(reg, []registry.Source{src}, []string{name}, opts)
	for _, r := range results {
		caseName := r.Definition
		if r.Case != "" {
			caseName += "/" + r.Case
		}
		t.Run(caseName, func(t *testing.T) {
			if r.Err != nil {
				t.Error(r.Err)
				return
			}
			if *update && r.Actual != nil {
				golden := filepath.FromSlash(strings.TrimSuffix(r.Path, ".txt") + ".json")
				if err := os.WriteFile(golden, r.Actual, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// decoyDir holds text that belongs to no format the registry describes:
// prose, build logs, configuration files, a document quoting a report's
// header line. Every one of them was accepted by some definition once, so
// they are kept as a corpus rather than as a memory. A definition that
// reads one has a signature that says less than it thinks.
//
// To add a decoy, drop the text in the directory. Nothing else is needed:
// the check applies every definition to every file. `jz test --decoys DIR`
// runs the same check for a registry outside this repository.
const decoyDir = "testdata/decoys"

func readDecoys(t *testing.T) []conformance.Decoy {
	t.Helper()
	decoys, err := conformance.ReadDecoys(decoyDir)
	if err != nil {
		t.Fatalf("reading %s: %v", decoyDir, err)
	}
	return decoys
}

var (
	updateSchema = flag.Bool("update-schema", false, "rewrite schemas/<command>/<variant>.json from the definitions; a breaking change needs -break")
	breakSchema  = flag.String("break", "", "comma-separated command/variant list whose breaking schema change is meant; their contract version goes up by one")
	schemaBase   = flag.String("schema-base", "", "a schemas directory published by the base branch, to check this one against")
)

// TestSchemas holds the published output contracts to the definitions.
// Every definition has a schema in schemas/, derived from the definition
// and equal to what the generator makes of it today; a change that would
// break the programs reading the output is refused unless it comes with a
// new contract version. `make registry-update-schema` rewrites the files,
// and BREAKING=command/variant is how a break is meant.
func TestSchemas(t *testing.T) {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: FS()})
	if err != nil {
		t.Fatal(err)
	}
	root := os.DirFS(".")
	meant := map[string]bool{}
	for _, id := range strings.Split(*breakSchema, ",") {
		if id = strings.TrimSpace(id); id != "" {
			meant[id] = true
		}
	}
	retired, err := schema.Retired(root)
	if err != nil {
		t.Fatal(err)
	}
	published, err := schema.All(root)
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, e := range reg.Entries() {
		id := e.Def.ID()
		defined[id] = true
		t.Run(id, func(t *testing.T) {
			prev := published[id]
			next, _, err := schema.Next(e.Def, prev, meant[id])
			if err != nil {
				t.Fatal(err)
			}
			want, err := schema.Encode(next)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.FromSlash(schema.Path(e.Def))
			if *updateSchema {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, want, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v; run make registry-update-schema", err)
			}
			// A checkout that converts line endings is not a change to
			// the contract.
			if strings.ReplaceAll(string(got), "\r\n", "\n") != string(want) {
				t.Errorf("%s does not state what the definition produces; run make registry-update-schema and review the diff", file)
			}
		})
	}
	for id := range published {
		if !defined[id] && !*updateSchema {
			t.Errorf("schemas/%s.json describes a definition that does not exist; delete it and list %s in %s", id, id, schema.RetiredFile)
		}
		if !defined[id] && *updateSchema {
			if err := os.Remove(filepath.FromSlash(schema.Dir + "/" + id + ".json")); err != nil {
				t.Error(err)
			}
		}
	}
	for id := range retired {
		if defined[id] {
			t.Errorf("%s lists %s as retired, and the definition exists", schema.RetiredFile, id)
		}
	}
}

// TestSchemasAgainstBase checks the published schemas against the ones a
// base branch published, which is what the CI job passes with
// -schema-base. A schema edited by hand to match a breaking change, with
// the version left alone, passes TestSchemas and fails here.
func TestSchemasAgainstBase(t *testing.T) {
	if *schemaBase == "" {
		t.Skip("no -schema-base given; the CI job for pull requests passes the base branch's schemas")
	}
	problems, err := schema.AgainstBase(os.DirFS(*schemaBase), os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// TestLsblkTreeDrawing reads trees deeper than a fixture can show. lsblk
// draws a level below a partition only for a stack such as LUKS or LVM,
// which the machine the fixtures come from does not have, so the text is
// written here the way util-linux draws it: two characters per level
// above a device and two in front of it.
func TestLsblkTreeDrawing(t *testing.T) {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: FS()})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := reg.Lookup("lsblk", "linux")
	if !ok {
		t.Fatal("lsblk/linux is not in the registry")
	}
	want := []string{"sda", "sda1", "cryptroot", "vg-root", "vg-home", "sdb", "sdb1", "md0", "-odd", "sdb2"}
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "C locale",
			input: "NAME            MAJ:MIN RM  SIZE RO TYPE  MOUNTPOINTS\n" +
				"sda               8:0    0  100G  0 disk  \n" +
				"`-sda1            8:1    0  100G  0 part  \n" +
				"  `-cryptroot   252:0    0  100G  0 crypt \n" +
				"    |-vg-root   252:1    0   50G  0 lvm   /\n" +
				"    `-vg-home   252:2    0   50G  0 lvm   /home\n" +
				"sdb               8:16   0   10G  0 disk  \n" +
				"|-sdb1            8:17   0    5G  0 part  \n" +
				"| |-md0           9:0    0    5G  0 raid1 /srv\n" +
				"| `--odd        252:3    0    1G  0 lvm   \n" +
				"`-sdb2            8:18   0    5G  0 part  [SWAP]\n",
		},
		{
			name: "UTF-8",
			input: "NAME            MAJ:MIN RM  SIZE RO TYPE  MOUNTPOINTS\n" +
				"sda               8:0    0  100G  0 disk  \n" +
				"└─sda1            8:1    0  100G  0 part  \n" +
				"  └─cryptroot   252:0    0  100G  0 crypt \n" +
				"    ├─vg-root   252:1    0   50G  0 lvm   /\n" +
				"    └─vg-home   252:2    0   50G  0 lvm   /home\n" +
				"sdb               8:16   0   10G  0 disk  \n" +
				"├─sdb1            8:17   0    5G  0 part  \n" +
				"│ ├─md0           9:0    0    5G  0 raid1 /srv\n" +
				"│ └─-odd        252:3    0    1G  0 lvm   \n" +
				"└─sdb2            8:18   0    5G  0 part  [SWAP]\n",
		},
		{
			name: "no tree",
			input: "NAME      MAJ:MIN RM  SIZE RO TYPE  MOUNTPOINTS\n" +
				"sda         8:0    0  100G  0 disk  \n" +
				"sda1        8:1    0  100G  0 part  \n" +
				"cryptroot 252:0    0  100G  0 crypt \n" +
				"vg-root   252:1    0   50G  0 lvm   /\n" +
				"vg-home   252:2    0   50G  0 lvm   /home\n" +
				"sdb         8:16   0   10G  0 disk  \n" +
				"sdb1        8:17   0    5G  0 part  \n" +
				"md0         9:0    0    5G  0 raid1 /srv\n" +
				"-odd      252:3    0    1G  0 lvm   \n" +
				"sdb2        8:18   0    5G  0 part  [SWAP]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := engine.Parse(e.Def, []byte(tt.input), engine.Options{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := jsonutil.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &rows); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.Name)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("names = %q, want %q", got, want)
			}
		})
	}
}

// parsersPage is the documentation page that lists every definition and
// links each to its published schema.
const parsersPage = "../website/content/parsers.md"

var parsersRow = regexp.MustCompile("^\\| `([^`]+)` \\| (.*) \\|$")
var parsersCell = regexp.MustCompile("^\\[`([^`]+)`\\]\\(\\.\\./schemas/([^)]+)\\.json\\)$")

// TestParsersPageListsEveryDefinition keeps the page that documents the
// registry in step with it: every command with the variants it carries,
// each linked to the schema published for it, and nothing else.
func TestParsersPageListsEveryDefinition(t *testing.T) {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: FS()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	for _, e := range reg.Entries() {
		want[e.Def.Command] = append(want[e.Def.Command], e.Def.Variant)
	}
	data, err := os.ReadFile(parsersPage)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	// A Windows checkout writes the page with CRLF, which $ does not
	// match past.
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		m := parsersRow.FindStringSubmatch(line)
		if m == nil || m[1] == "Command" {
			continue
		}
		for _, cell := range strings.Split(m[2], ", ") {
			c := parsersCell.FindStringSubmatch(cell)
			if c == nil {
				t.Errorf("%s: %q is not a variant linked to its schema", m[1], cell)
				continue
			}
			if c[2] != m[1]+"/"+c[1] {
				t.Errorf("%s/%s links to the schema of %s", m[1], c[1], c[2])
			}
			if _, err := os.Stat(filepath.FromSlash(schema.Dir + "/" + c[2] + ".json")); err != nil {
				t.Errorf("%s/%s links to a schema that is not published: %v", m[1], c[1], err)
			}
			got[m[1]] = append(got[m[1]], c[1])
		}
	}
	for command, variants := range want {
		sort.Strings(variants)
		if strings.Join(got[command], ",") != strings.Join(variants, ",") {
			t.Errorf("%s lists %s: %v, the registry has %v", parsersPage, command, got[command], variants)
		}
	}
	for command := range got {
		if _, ok := want[command]; !ok {
			t.Errorf("%s lists %s, which the registry does not have", parsersPage, command)
		}
	}
}

// TestEveryFixtureRecordsItsSource checks that each fixture says where
// its text came from. A fixture is the contract of a definition, and a
// reader has to be able to tell output a real command wrote from text
// somebody assembled to exercise a rule: the first says the definition
// describes what the command does, the second says only that the
// definition does what it says. What belongs in source is the command
// line, the implementation and its version, the operating system, the
// locale, and anything replaced afterwards; where the text was not run
// but quoted from a document, the document.
func TestEveryFixtureRecordsItsSource(t *testing.T) {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: FS()})
	if err != nil {
		t.Fatal(err)
	}
	fixtures, problems := conformance.Fixtures(reg, []registry.Source{{Name: "embedded", FS: FS()}})
	for _, p := range problems {
		t.Errorf("%s: %v", p.Definition, p.Err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures were found")
	}
	// Twenty characters is not a judgement of what a source says; it is
	// the length below which it cannot be saying any of it.
	const least = 20
	for _, f := range fixtures {
		if src := strings.TrimSpace(f.Case.Meta.Source); len(src) < least {
			t.Errorf("%s: %s records no source; say what produced the text, on what and with which locale",
				f.Entry.Def.ID(), f.Path())
		}
	}
}

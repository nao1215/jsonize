package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

// explainRegistry replaces the built-in registry with a small one whose
// every answer is known, so an explanation can be compared whole rather
// than searched for a line. Each definition is there for one outcome.
func explainRegistry(t *testing.T, h *harness) string {
	t.Helper()
	def := func(id, rest string) string {
		command, variant, _ := strings.Cut(id, "/")
		return "format: 1\ncommand: " + command + "\nvariant: " + variant + "\n" + rest
	}
	h.env.Embedded = registry.Source{Name: SourceEmbedded, FS: fstest.MapFS{
		// greet/hello is the only definition that fits "hello bob"; greet/hi
		// gets past nothing, and greet/loud gets past its first expression
		// and is the near miss.
		"parsers/greet/hello/parser.yaml": {Data: []byte(def("greet/hello", "detect: {signature: {all: ['^hello \\S+$']}}\ninput: {ignore: ['^#']}\nparse: {type: regex, pattern: '^hello (?P<name>\\S+)$'}\n"))},
		"parsers/greet/hi/parser.yaml":    {Data: []byte(def("greet/hi", "detect: {signature: {all: ['^hi ']}}\nparse: {type: regex, pattern: '^hi (?P<name>\\S+)$'}\n"))},
		"parsers/greet/loud/parser.yaml":  {Data: []byte(def("greet/loud", "detect: {signature: {all: ['^hello ', '!$']}}\nparse: {type: regex, pattern: '^hello (?P<name>\\S+)!$'}\n"))},
		// Only used when named: its signature fits any two tab-separated
		// words.
		"parsers/pair/tab/parser.yaml": {Data: []byte(def("pair/tab", "detect: {auto_detect: false, signature: {all: ['^\\S+\\t\\S+$']}}\nparse: {type: table, split: delimiter, delimiter: \"\\t\", header: {none: true, columns: [a, b]}}\n"))},
		// Two variants of one command with the same priority fit "dup x".
		"parsers/dup/a/parser.yaml": {Data: []byte(def("dup/a", "detect: {signature: {all: ['^dup ']}}\nparse: {type: regex, pattern: '^dup (?P<v>\\S+)$'}\n"))},
		"parsers/dup/b/parser.yaml": {Data: []byte(def("dup/b", "detect: {signature: {all: ['^dup ']}}\nparse: {type: regex, pattern: '^dup (?P<v>\\S+)$'}\n"))},
		// Two variants that priority settles.
		"parsers/pri/low/parser.yaml":  {Data: []byte(def("pri/low", "detect: {signature: {all: ['^pri ']}}\nparse: {type: regex, pattern: '^pri (?P<v>\\S+)$'}\n"))},
		"parsers/pri/high/parser.yaml": {Data: []byte(def("pri/high", "detect: {priority: 5, signature: {all: ['^pri ']}}\nparse: {type: regex, pattern: '^pri (?P<v>\\S+)$'}\n"))},
	}}
	// A registry layered above the built-in one, holding a definition
	// that fits what pri/* fit and wins by the layering.
	dir := filepath.Join(h.home, "layer")
	writeRegistry(t, dir, map[string]string{
		"parsers/prio/mine/parser.yaml": def("prio/mine", "detect: {signature: {all: ['^pri x$']}}\nparse: {type: regex, pattern: '^pri (?P<v>\\S+)$'}\n"),
	})
	h.registryPath = dir
	return dir
}

func TestExplainGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		args  []string
		code  int
		want  []string
	}{
		{
			name:  "chosen on its signature, with the lines it read and left out",
			input: "# greeting\nhello bob\n\n",
			code:  ExitOK,
			want: []string{
				"chose greet/hello from embedded",
				"scope: every definition in the registry, by its signature alone",
				"matched: signature.all[0] /^hello \\S+$/",
				"rejected: greet/loud: signature.all[1] /!$/ did not match",
				"not considered: 1 definition only used when named",
				"read: 3 lines: 1 read, 1 blank, 1 left out by input.ignore[0] /^#/",
			},
		},
		{
			name:  "nothing fits, and one definition that would is held back",
			input: "a\tb\n",
			code:  ExitSelect,
			want: []string{
				"unidentified: no definition fits the text",
				"scope: every definition in the registry, by its signature alone",
				"held back: pair/tab: its signature fits, but it is only used when named (--parser pair)",
			},
		},
		{
			name:  "naming the held back one",
			input: "a\tb\n",
			args:  []string{"--parser", "pair"},
			code:  ExitOK,
			want: []string{
				"chose pair/tab from embedded",
				"scope: the variants of pair, named by --parser",
				"matched: signature.all[0] /^\\S+\\t\\S+$/",
				"read: 1 line: 1 read",
			},
		},
		{
			name:  "two variants fit and nothing settles it",
			input: "dup x\n",
			code:  ExitSelect,
			want: []string{
				"ambiguous: dup/a, dup/b all fit the text, and no detect.priority among them is strictly highest",
				"scope: every definition in the registry, by its signature alone",
				"not considered: 1 definition only used when named",
			},
		},
		{
			name:  "two variants fit and priority settles it",
			input: "pri y\n",
			code:  ExitOK,
			want: []string{
				"chose pri/high from embedded",
				"scope: every definition in the registry, by its signature alone",
				"matched: signature.all[0] /^pri /",
				"settled by detect.priority over 1 other definition that fit as well",
				"outranked: pri/low: fits as well, with detect.priority 0 against 5",
				"not considered: 1 definition only used when named",
				"read: 1 line: 1 read",
			},
		},
		{
			name:  "three fit and the layering settles it",
			input: "pri x\n",
			code:  ExitOK,
			want: []string{
				"chose prio/mine from {layer}",
				"scope: every definition in the registry, by its signature alone",
				"matched: signature.all[0] /^pri x$/",
				"settled by the registry layering over 2 other definitions that fit as well",
				"outranked: pri/high: fits as well, from embedded, which comes after {layer} in the layering",
				"outranked: pri/low: fits as well, from embedded, which comes after {layer} in the layering",
				"not considered: 1 definition only used when named",
				"read: 1 line: 1 read",
			},
		},
		{
			name:  "a named variant that does not fit",
			input: "hello bob\n",
			args:  []string{"--parser", "greet", "--variant", "hi"},
			code:  ExitSelect,
			want: []string{
				"mismatch: greet/hi was named and does not fit: signature.all[0] /^hi / did not match",
				"scope: greet/hi, named by --parser and --variant",
			},
		},
		{
			name:  "chosen and then unable to read everything",
			input: "hello bob\nhello bob again\n",
			args:  []string{"--parser", "greet", "--variant", "hello"},
			code:  ExitParse,
			want: []string{
				"chose greet/hello from embedded",
				"scope: greet/hello, named by --parser and --variant",
				"matched: signature.all[0] /^hello \\S+$/",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			layer := explainRegistry(t, h)
			for i := range tt.want {
				tt.want[i] = strings.ReplaceAll(tt.want[i], "{layer}", layer)
			}
			args := append([]string{"--explain"}, tt.args...)
			if code := h.pipe(tt.input, args...); code != tt.code {
				t.Fatalf("code=%d, want %d\n%s", code, tt.code, h.stderr.String())
			}
			var got []string
			for _, l := range strings.Split(strings.TrimRight(h.stderr.String(), "\n"), "\n") {
				if rest, ok := strings.CutPrefix(l, "jz: explain: "); ok {
					got = append(got, rest)
				}
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("explanation:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			// The same run explains itself the same way, and explaining
			// changes neither standard output nor the status.
			first, out := h.stderr.String(), h.stdout.String()
			if code := h.pipe(tt.input, args...); code != tt.code || h.stderr.String() != first || h.stdout.String() != out {
				t.Errorf("a second run differs:\n%s\n%s", first, h.stderr.String())
			}
			if code := h.pipe(tt.input, tt.args...); code != tt.code || h.stdout.String() != out || strings.Contains(h.stderr.String(), "explain:") {
				t.Errorf("without --explain: code=%d stdout=%q stderr=%q", code, h.stdout.String(), h.stderr.String())
			}
		})
	}
}

// --explain=json writes the same facts as one document on one line, with
// every key present whatever the outcome, so a script reads every
// explanation the same way.
func TestExplainJSON(t *testing.T) {
	h := newHarness(t)
	explainRegistry(t, h)
	type rejection struct {
		Definition string `json:"definition"`
		Reason     string `json:"reason"`
	}
	type doc struct {
		Outcome string `json:"outcome"`
		Scope   struct {
			From        *string  `json:"from"`
			Parser      *string  `json:"parser"`
			Variant     *string  `json:"variant"`
			OS          *string  `json:"os"`
			Args        []string `json:"args"`
			Path        *string  `json:"path"`
			PathDropped *string  `json:"path_dropped"`
		} `json:"scope"`
		Chosen *struct {
			Definition string      `json:"definition"`
			Registry   string      `json:"registry"`
			Matched    []string    `json:"matched"`
			SettledBy  *string     `json:"settled_by"`
			Outranked  []rejection `json:"outranked"`
		} `json:"chosen"`
		Candidates    []string    `json:"candidates"`
		Rejected      []rejection `json:"rejected"`
		HeldBack      []string    `json:"held_back"`
		NotConsidered int         `json:"not_considered"`
		Read          *struct {
			Lines   int `json:"lines"`
			Read    int `json:"read"`
			Folded  int `json:"folded"`
			Blank   int `json:"blank"`
			Ignored []struct {
				Rule       string `json:"rule"`
				Expression string `json:"expression"`
				Lines      int    `json:"lines"`
			} `json:"ignored"`
		} `json:"read"`
		Command *struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
			Exit int      `json:"exit"`
		} `json:"command"`
		Error *struct {
			Message string `json:"message"`
			Exit    int    `json:"exit"`
		} `json:"error"`
	}
	read := func(t *testing.T) (doc, map[string]any) {
		t.Helper()
		var lines []string
		for _, l := range strings.Split(strings.TrimRight(h.stderr.String(), "\n"), "\n") {
			if rest, ok := strings.CutPrefix(l, "jz: explain: "); ok {
				lines = append(lines, rest)
			}
		}
		if len(lines) != 1 {
			t.Fatalf("want one explanation line, got %d:\n%s", len(lines), h.stderr.String())
		}
		var d doc
		var raw map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &d); err != nil {
			t.Fatalf("%v: %s", err, lines[0])
		}
		if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
			t.Fatal(err)
		}
		return d, raw
	}
	keys := []string{"outcome", "scope", "chosen", "candidates", "rejected", "held_back", "not_considered", "read", "command", "error"}

	if code := h.pipe("# greeting\nhello bob\n", "--explain=json"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	d, raw := read(t)
	for _, k := range keys {
		if _, ok := raw[k]; !ok {
			t.Errorf("no key %q in %v", k, raw)
		}
	}
	if d.Outcome != "chosen" || d.Chosen == nil || d.Chosen.Definition != "greet/hello" || d.Chosen.Registry != "embedded" ||
		d.Chosen.SettledBy != nil || len(d.Chosen.Outranked) != 0 || d.Scope.From != nil || d.Scope.Args != nil ||
		len(d.Rejected) != 1 || d.Rejected[0].Definition != "greet/loud" || d.NotConsidered != 1 ||
		d.Read == nil || d.Read.Lines != 2 || d.Read.Read != 1 || len(d.Read.Ignored) != 1 || d.Read.Ignored[0].Rule != "input.ignore[0]" ||
		d.Command != nil || d.Error != nil {
		t.Errorf("chosen: %+v", d)
	}
	first := h.stderr.String()
	if h.pipe("# greeting\nhello bob\n", "--explain=json"); h.stderr.String() != first {
		t.Errorf("a second run differs:\n%s\n%s", first, h.stderr.String())
	}

	if code := h.pipe("dup x\n", "--explain=json"); code != ExitSelect {
		t.Fatalf("code=%d", code)
	}
	d, _ = read(t)
	if d.Outcome != "ambiguous" || d.Chosen != nil || strings.Join(d.Candidates, ",") != "dup/a,dup/b" || d.Error == nil || d.Error.Exit != ExitSelect || d.Read != nil {
		t.Errorf("ambiguous: %+v", d)
	}
	// The failure is still the ordinary diagnostic as well.
	if !strings.Contains(h.stderr.String(), "jz: input matches multiple dup variants") {
		t.Errorf("the ordinary message is gone: %s", h.stderr.String())
	}

	if code := h.pipe("a\tb\n", "--explain=json"); code != ExitSelect {
		t.Fatalf("code=%d", code)
	}
	d, _ = read(t)
	if d.Outcome != "unidentified" || strings.Join(d.HeldBack, ",") != "pair/tab" || d.NotConsidered != 0 {
		t.Errorf("unidentified: %+v", d)
	}

	if code := h.pipe("pri x\n", "--explain=json"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	d, _ = read(t)
	if d.Chosen == nil || d.Chosen.SettledBy == nil || *d.Chosen.SettledBy != "registry" || len(d.Chosen.Outranked) != 2 {
		t.Errorf("settled by the layering: %+v", d.Chosen)
	}

	if code := h.pipe("hello bob\n", "--explain=yaml"); code != ExitUsage {
		t.Errorf("--explain=yaml: code=%d", code)
	}
}

// A path that names a definition the text does not fit is dropped, and
// the explanation says so rather than leaving the scope to be guessed.
func TestExplainDroppedPath(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(t.TempDir(), "etc")
	path := filepath.Join(dir, "fstab")
	writeRegistry(t, dir, map[string]string{"fstab": gnuDF})
	if code := h.run("--explain", "--file", path); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	got := h.stderr.String()
	for _, want := range []string{
		"jz: explain: chose df/gnu from embedded\n",
		"jz: explain: scope: every definition in the registry, by its signature alone\n",
		"jz: explain: scope: the file path " + path + " named a definition that did not fit (etc/fstab does not describe this input: ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q:\n%s", want, got)
		}
	}
}

// With --stream the explanation is written when the choice is made,
// before the first record, and it has no read counts to give.
func TestExplainStream(t *testing.T) {
	h := newHarness(t)
	explainRegistry(t, h)
	if code := h.pipe("hello bob\nhello ann\n", "--stream", "--explain"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	if got := h.stdout.String(); got != "{\"name\":\"bob\"}\n{\"name\":\"ann\"}\n" {
		t.Errorf("stdout = %q", got)
	}
	got := h.stderr.String()
	if !strings.HasPrefix(got, "jz: explain: chose greet/hello from embedded\n") || strings.Contains(got, "explain: read:") {
		t.Errorf("stderr = %s", got)
	}
	if code := h.pipe("hello bob\n", "--stream", "--explain=json"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	if n := strings.Count(h.stderr.String(), "jz: explain: {"); n != 1 || !strings.Contains(h.stderr.String(), `"read":null`) {
		t.Errorf("stderr = %s", h.stderr.String())
	}
}

// jz list --schema prints the contract of one definition's output: the
// published schema for a definition that has one, derived on the spot
// for one that does not, with the version its registry published.
func TestListSchema(t *testing.T) {
	h := newHarness(t)
	if code := h.run("list", "--schema", "df", "gnu"); code != ExitOK {
		t.Fatalf("code=%d %s", code, h.stderr.String())
	}
	var s struct {
		ID      string `json:"$id"`
		Title   string `json:"title"`
		Jsonize struct {
			Definition string `json:"definition"`
			Version    int    `json:"version"`
		} `json:"x-jsonize"` //nolint:tagliatelle // the annotation's name in the schema
		Type  string `json:"type"`
		Items struct {
			Required []string `json:"required"`
		} `json:"items"`
	}
	h.json(&s)
	if s.ID != "https://nao1215.github.io/jsonize/schemas/df/gnu.json" || s.Title != "df/gnu" || s.Jsonize.Version != 1 ||
		s.Type != "array" || len(s.Items.Required) == 0 || s.Items.Required[0] != "filesystem" {
		t.Errorf("schema = %+v", s)
	}
	// What is printed is what is published.
	published, err := os.ReadFile(filepath.Join("..", "..", "registry", "schemas", "df", "gnu.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(published), "\r\n", "\n") != h.stdout.String() {
		t.Errorf("jz list --schema differs from registry/schemas/df/gnu.json")
	}
	// A definition of the user's own has no published schema and gets
	// the first version of the one derived from it.
	explainRegistry(t, h)
	if code := h.run("list", "--schema", "greet", "hello"); code != ExitOK || !strings.Contains(h.stdout.String(), `"version": 1`) {
		t.Errorf("code=%d %s", code, h.stdout.String())
	}
	for _, args := range [][]string{{"list", "--schema"}, {"list", "--schema", "df"}, {"list", "--schema", "--json", "df", "gnu"}} {
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%v: code=%d", args, code)
		}
	}
	if code := h.run("list", "--schema", "greet", "nope"); code != ExitSelect {
		t.Errorf("unknown variant: code=%d", code)
	}
}

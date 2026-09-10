package schema

import (
	"testing"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// Whatever the text, a reading that succeeds produces a document that
// fits the schema derived from the definition. The fixtures check that
// for the text they hold; this checks it for text nobody thought of.
func FuzzOutputFitsSchema(f *testing.F) {
	defs := []string{
		"parse: {type: table, split: aligned}\nfields: {size: {type: size}, n: {type: int, null_if: ['-']}}\n",
		"parse: {type: table, min_fields: 1, header: {none: true, columns: [a, b, c]}}\nfields: {b: {type: int}, c: {when_missing: omit}}\n",
		"parse: {type: regex, patterns: ['^(?P<k>up|down) (?P<n>\\d+)(?: (?P<x>\\S+))?$', '^(?P<k>gone)$']}\nfields: {n: {type: int}, x: {when_missing: omit}}\n",
		"parse: {type: kv, as: map}\nfields: {n: {type: float}}\n",
		"parse: {type: kv}\nfields: {n: {type: bool}}\n",
		"parse: {type: csv}\nfields: {n: {type: int}}\n",
		"parse: {type: ini}\n",
		"parse: {type: table, split: box}\n",
		"parse: {type: tree, indent: '  ', node: {parse: {type: regex, patterns: ['^(?P<k>\\S+): (?P<v>.*)$', '^(?P<x>.+)$']}}}\n",
		"parse:\n  type: records\n  start: '^# '\n  parts:\n    - {name: head, select: {limit: 1}, parse: {type: regex, each: input, pattern: '^# (?P<name>\\S+)$'}}\n    - {name: rows, select: {skip: 1}, parse: {type: kv}}\n",
		"parse: {type: regex, pattern: '^(?P<ids>.*)$'}\nfields: {ids: {type: array, split: ',', items: {type: int, null_if: ['?']}}}\n",
	}
	for _, seed := range []string{
		"NAME  SIZE  N\na     1K    -\nb           2\n",
		"x 1 y\nz\n",
		"up 1\ndown 2 why\ngone\n",
		"n=1.5\nm=x\n",
		"n=yes\nx=y\n",
		"a,n\n1,2\n",
		"[s]\nk = 1\n",
		"+---+---+\n| a | b |\n+---+---+\n| 1 |   |\n+---+---+\n",
		"a: 1\n  b\n    c: 3\n",
		"# one\na=1\n# two\n",
		"1,?,3\n",
	} {
		for i := range defs {
			f.Add(i, []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, which int, input []byte) {
		if which < 0 {
			which = -which
		}
		def, err := definition.Load([]byte("format: 1\ncommand: t\nvariant: v\n"+defs[which%len(defs)]), "fuzz")
		if err != nil {
			t.Fatal(err)
		}
		v, err := engine.Parse(def, input, engine.Options{MaxInputSize: 1 << 20})
		if err != nil {
			return
		}
		doc, err := jsonutil.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if errs := Validate(Generate(def, 1), doc); len(errs) > 0 {
			t.Fatalf("%s does not fit its schema: %v", doc, errs)
		}
	})
}

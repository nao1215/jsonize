package conformance

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

// Tampering has to catch what the engine's accounting cannot: text a
// definition declares ignorable when it is not, and a pattern whose match
// runs over text it never reports. Both read their own fixture correctly,
// so the golden cases pass; only a changed fixture shows the difference.
func TestTampering(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		// Reads every line it is given: the foreign line fails the
		// pattern, and two copies are two lists of rows.
		"parsers/good/v/parser.yaml":     {Data: []byte("format: 1\ncommand: good\nvariant: v\ndetect: {signature: {all: ['^n=']}}\nparse: {type: regex, pattern: '^n=(?P<n>\\d+)$'}\n")},
		"parsers/good/v/testdata/a.txt":  {Data: []byte("n=1\nn=2\n")},
		"parsers/good/v/testdata/a.json": {Data: []byte(`[{"n":"1"},{"n":"2"}]`)},
		// Declares every line without "=" ignorable, so the foreign line
		// disappears without an error.
		"parsers/catchall/v/parser.yaml":     {Data: []byte("format: 1\ncommand: catchall\nvariant: v\ndetect: {signature: {all: ['^c=']}}\ninput: {ignore: ['^[^=]*$']}\nparse: {type: kv, as: map}\n")},
		"parsers/catchall/v/testdata/a.txt":  {Data: []byte("c=1\nd=2\n")},
		"parsers/catchall/v/testdata/a.json": {Data: []byte(`{"c":"1","d":"2"}`)},
		// A match that runs to the end of the text covers everything and
		// reports the first word, so it reads anything after it, a second
		// copy included, and keeps none of it.
		"parsers/swallow/v/parser.yaml":     {Data: []byte("format: 1\ncommand: swallow\nvariant: v\ndetect: {signature: {all: ['^s ']}}\nparse: {type: regex, each: input, pattern: '(?s)\\As (?P<v>\\w+).*'}\n")},
		"parsers/swallow/v/testdata/a.txt":  {Data: []byte("s one\nmore\n")},
		"parsers/swallow/v/testdata/a.json": {Data: []byte(`{"v":"one"}`)},
		// Everything in this fixture is left out on purpose, so a second
		// copy of it has nothing to lose.
		"parsers/header/v/parser.yaml":     {Data: []byte("format: 1\ncommand: header\nvariant: v\ndetect: {signature: {all: ['^NAME +SIZE$']}}\ninput: {ignore: ['^NAME +SIZE$']}\nparse: {type: table, header: {none: true, columns: [name, size]}}\n")},
		"parsers/header/v/testdata/a.txt":  {Data: []byte("NAME SIZE\n")},
		"parsers/header/v/testdata/a.json": {Data: []byte(`[]`)},
	}
	src := registry.Source{Name: "s", FS: fsys}
	reg, err := registry.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, problems := Fixtures(reg, []registry.Source{src})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	got := map[string][]string{}
	for _, r := range Tampering(reg, fixtures, func(string) bool { return true }, Options{}) {
		got[r.Definition] = append(got[r.Definition], r.Err.Error())
	}
	want := map[string][]string{
		"catchall/v": {
			"a foreign line at the end: the definition read the text and the line is nowhere in the result",
			"a foreign line after record 1 of 2: the definition read the text and the line is nowhere in the result",
		},
		"swallow/v": {
			"a foreign line at the end: the definition read the text and the line is nowhere in the result",
			"a foreign line after record 1 of 2: the definition read the text and the line is nowhere in the result",
			"the fixture twice: the definition read two copies and returned what it returns for one",
		},
	}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	for def, msgs := range want {
		if strings.Join(got[def], "\n") != strings.Join(msgs, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", def, got[def], msgs)
		}
	}
	// A source that is not under test is not checked.
	if rs := Tampering(reg, fixtures, func(string) bool { return false }, Options{}); len(rs) != 0 {
		t.Errorf("checked a source not under test: %v", rs)
	}
}

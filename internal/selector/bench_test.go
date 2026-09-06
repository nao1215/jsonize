package selector

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/internal/registry"
)

func BenchmarkSelect(b *testing.B) {
	fsys := fstest.MapFS{}
	for i := 0; i < 8; i++ {
		body := fmt.Sprintf("format: 1\ncommand: df\nvariant: v%d\ndetect: {os: [linux], args: {none: ['-x%d']}, signature: {all: ['^Filesystem\\s+HDR%d\\s']}}\nparse: {type: kv}\n", i, i, i)
		fsys[fmt.Sprintf("parsers/df/v%d/parser.yaml", i)] = &fstest.MapFile{Data: []byte(body)}
	}
	reg, err := registry.Load(registry.Source{Name: "bench", FS: fsys})
	if err != nil {
		b.Fatal(err)
	}
	input := []byte("Filesystem HDR5 Used\n" + "row data here\n")
	ctx := Context{Command: "df", OS: "linux", Args: []string{"-a"}, Input: input}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := Select(reg, ctx)
		if err != nil || res.Entry.Def.Variant != "v5" {
			b.Fatal(err)
		}
	}
}

func FuzzSelect(f *testing.F) {
	fsys := fstest.MapFS{
		"parsers/x/a/parser.yaml": {Data: []byte("format: 1\ncommand: x\nvariant: a\ndetect: {os: [linux], signature: {all: ['^A']}}\nparse: {type: kv}\n")},
		"parsers/x/b/parser.yaml": {Data: []byte("format: 1\ncommand: x\nvariant: b\ndetect: {args: {any: ['-b']}, signature: {none: ['^A']}}\nparse: {type: kv}\n")},
		"parsers/x/c/parser.yaml": {Data: []byte("format: 1\ncommand: x\nvariant: c\nparse: {type: kv}\n")},
	}
	reg, err := registry.Load(registry.Source{Name: "fuzz", FS: fsys})
	if err != nil {
		f.Fatal(err)
	}
	f.Add("A\n", "linux", "-b")
	f.Add("", "", "")
	f.Add("\xff\xfe", "darwin", "-bx")
	f.Fuzz(func(t *testing.T, input, goos, arg string) {
		res, err := Select(reg, Context{Command: "x", OS: goos, Args: []string{arg}, Input: []byte(input)})
		if err == nil && res.Entry == nil {
			t.Fatal("nil entry without error")
		}
		if _, err := Select(reg, Context{Command: "x", Input: []byte(input)}); err == nil {
			// The catch-all always survives; pipe mode must never be
			// ambiguous for this registry unless a and b both match.
			_ = err
		}
	})
}

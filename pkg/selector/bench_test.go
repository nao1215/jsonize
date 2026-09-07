package selector

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

// syntheticRegistry builds n definitions spread over n/4 commands, each
// with a signature that only its own header matches. It is the stand-in
// for a registry an order of magnitude larger than the official one.
func syntheticRegistry(tb testing.TB, n int) (*registry.Registry, []byte) {
	tb.Helper()
	fsys := fstest.MapFS{"registry.yaml": &fstest.MapFile{Data: []byte("format: 1\nname: bench\n")}}
	for i := range n {
		cmd := fmt.Sprintf("cmd%d", i/4)
		variant := fmt.Sprintf("v%d", i%4)
		body := fmt.Sprintf("format: 1\ncommand: %s\nvariant: %s\n"+
			"detect:\n  os: [linux]\n  args: {none: ['-x%d']}\n  signature: {all: ['^HEADER-%d\\s+COLUMN\\s+COLUMN$']}\n"+
			"parse:\n  type: table\n  header: {columns: [a, b, c]}\nfields: {b: {type: int}, c: {type: size}}\n", cmd, variant, i, i)
		fsys[fmt.Sprintf("parsers/%s/%s/parser.yaml", cmd, variant)] = &fstest.MapFile{Data: []byte(body)}
	}
	reg, err := registry.Load(registry.Source{Name: "bench", FS: fsys})
	if err != nil {
		tb.Fatal(err)
	}
	if reg.Len() != n {
		tb.Fatalf("registry has %d definitions, want %d", reg.Len(), n)
	}
	// Input for the definition in the middle of the registry, so a scan
	// cannot look fast by matching the first candidate.
	mid := n / 2
	//nolint:dupword // a table header repeats its column labels
	input := fmt.Sprintf("HEADER-%d COLUMN COLUMN\nrow %d 10K\nrow %d 20K\n", mid, mid, mid)
	return reg, []byte(input)
}

// BenchmarkDetect measures `COMMAND | jz`: every signature in the
// registry is evaluated because no parser was named.
func BenchmarkDetect(b *testing.B) {
	for _, n := range []int{26, 500, 1000} {
		reg, input := syntheticRegistry(b, n)
		b.Run(fmt.Sprintf("definitions=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := Select(reg, Context{Input: input}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDetectWithParser measures `jz --parser NAME`, where only one
// command's variants are evaluated.
func BenchmarkDetectWithParser(b *testing.B) {
	reg, input := syntheticRegistry(b, 1000)
	parser := fmt.Sprintf("cmd%d", 500/4)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Select(reg, Context{Parser: parser, Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDetectWithVariant measures a fully pinned selection, which
// still verifies the signature.
func BenchmarkDetectWithVariant(b *testing.B) {
	reg, input := syntheticRegistry(b, 1000)
	parser := fmt.Sprintf("cmd%d", 500/4)
	variant := fmt.Sprintf("v%d", 500%4)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Select(reg, Context{Parser: parser, Variant: variant, Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDetectNoMatch is the worst case: every candidate is evaluated
// and rejected.
func BenchmarkDetectNoMatch(b *testing.B) {
	reg, _ := syntheticRegistry(b, 1000)
	input := []byte("nothing here resembles a known header\nsecond line\n")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Select(reg, Context{Input: input}); err == nil {
			b.Fatal("expected no match")
		}
	}
}

// BenchmarkDetectLargeInput shows that detection cost does not grow with
// the input: only the signature window is examined.
func BenchmarkDetectLargeInput(b *testing.B) {
	reg, header := syntheticRegistry(b, 1000)
	input := make([]byte, 0, len(header)+10*100000)
	input = append(input, header...)
	input = append(input, strings.Repeat("row 1 10K\n", 100000)...)
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Select(reg, Context{Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzSelect(f *testing.F) {
	fsys := fstest.MapFS{
		"parsers/x/a/parser.yaml": {Data: []byte("format: 1\ncommand: x\nvariant: a\ndetect: {os: [linux], signature: {all: ['^A']}}\nparse: {type: kv}\n")},
		"parsers/x/b/parser.yaml": {Data: []byte("format: 1\ncommand: x\nvariant: b\ndetect: {args: {any: ['-b']}, signature: {none: ['^A'], all: ['B']}}\nparse: {type: kv}\n")},
		"parsers/y/c/parser.yaml": {Data: []byte("format: 1\ncommand: y\nvariant: c\nparse: {type: kv}\n")},
	}
	reg, err := registry.Load(registry.Source{Name: "fuzz", FS: fsys})
	if err != nil {
		f.Fatal(err)
	}
	f.Add("A\n", "linux", "-b", "x", "a")
	f.Add("B\n", "", "", "", "")
	f.Add("\xff\xfe", "darwin", "-bx", "y", "c")
	f.Add("", "", "", "zzz", "")
	f.Fuzz(func(t *testing.T, input, goos, arg, parser, variant string) {
		ctx := Context{Parser: parser, Variant: variant, OS: goos, Args: []string{arg}, Input: []byte(input)}
		res, err := Select(reg, ctx)
		switch {
		case err != nil && res != nil:
			t.Fatal("both a result and an error")
		case err == nil && res.Entry == nil:
			t.Fatal("nil entry without an error")
		case err == nil && parser != "" && res.Entry.Def.Command != parser:
			t.Fatalf("--parser %q returned %s", parser, res.Entry.Def.ID())
		case err == nil && variant != "" && res.Entry.Def.Variant != variant:
			t.Fatalf("--variant %q returned %s", variant, res.Entry.Def.ID())
		}
		// Detection without hints must never pick a definition that has
		// no signature: it cannot be identified from text.
		if res, err := Select(reg, Context{Input: []byte(input)}); err == nil && res.Entry.Def.Detect.Signature.IsZero() {
			t.Fatalf("selected the signature-less %s from text alone", res.Entry.Def.ID())
		}
	})
}

package registry

import (
	"fmt"
	"testing"
	"testing/fstest"
)

// BenchmarkLoadRegistry measures the cost of indexing a registry, which
// every jz invocation pays before anything else. The sizes bracket the
// official registry (26) and a registry an order of magnitude larger.
func BenchmarkLoadRegistry(b *testing.B) {
	for _, n := range []int{26, 500, 1000} {
		fsys := fstest.MapFS{"registry.yaml": {Data: []byte("format: 1\nname: bench\n")}}
		for i := 0; i < n; i++ {
			cmd := fmt.Sprintf("cmd%d", i/3)
			variant := fmt.Sprintf("v%d", i%3)
			body := fmt.Sprintf("format: 1\ncommand: %s\nvariant: %s\ndetect: {os: [linux], signature: {all: ['^HEADER%d']}}\nparse: {type: table}\nfields: {a: {type: int}, b: {type: int}}\n", cmd, variant, i)
			fsys[fmt.Sprintf("parsers/%s/%s/parser.yaml", cmd, variant)] = &fstest.MapFile{Data: []byte(body)}
		}
		b.Run(fmt.Sprintf("definitions=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				reg, err := Load(Source{Name: "bench", FS: fsys})
				if err != nil || reg.Len() != n {
					b.Fatal(err, reg.Len())
				}
			}
		})
	}
}

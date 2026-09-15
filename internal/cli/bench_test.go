package cli

import (
	"strconv"
	"testing"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// records is n objects of six keys each, the size of a row of ps or df.
func benchRecords(n int) []any {
	out := make([]any, n)
	for i := range out {
		o := jsonutil.NewObject()
		o.Set("pid", i)
		o.Set("user", "root")
		o.Set("cpu", "0.0")
		o.Set("mem", "0.1")
		o.Set("state", "S")
		o.Set("command", "/usr/bin/daemon --flag")
		out[i] = o
	}
	return out
}

// BenchmarkKeyFilter measures a whole document narrowed with no key
// option, with --extract and with --exclude.
func BenchmarkKeyFilter(b *testing.B) {
	doc := benchRecords(10000)
	for _, tt := range []struct {
		name string
		out  outputOptions
	}{
		{"none", outputOptions{}},
		{"extract", outputOptions{extract: stringList{"pid", "command"}}},
		{"exclude", outputOptions{exclude: stringList{"mem"}}},
	} {
		b.Run(tt.name+"/records="+strconv.Itoa(len(doc)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				f, err := tt.out.filter()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := f.apply(doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

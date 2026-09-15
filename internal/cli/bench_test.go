package cli

import (
	"context"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
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

// BenchmarkConvert measures one whole run of jz on a short df report:
// the registry is loaded, the format is chosen from the text and two
// records are written. It is what a pipeline pays for each `| jz`, where
// the loading of several hundred definitions is most of the cost and the
// reading of three lines is almost none of it.
func BenchmarkConvert(b *testing.B) {
	home := b.TempDir()
	env := Env{
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		Getenv:          func(string) string { return "" },
		UserConfigDir:   func() (string, error) { return filepath.Join(home, "config"), nil },
		StdinIsTerminal: func() bool { return false },
		GOOS:            runtime.GOOS,
		Context:         context.Background(),
		Embedded:        registry.Source{Name: SourceEmbedded, FS: official.FS()},
	}
	b.ReportAllocs()
	for b.Loop() {
		env.Stdin = strings.NewReader(gnuDF)
		if code := Main(nil, env); code != ExitOK {
			b.Fatalf("code=%d", code)
		}
	}
}

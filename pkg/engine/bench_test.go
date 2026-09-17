package engine

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// Benchmarks use synthetic but format-faithful df/mount/env inputs so that
// results are reproducible on any machine. They are for profiling one
// function; whole runs of jz are measured by bench/himorime.yaml.

func benchDef(b *testing.B, src string) *definition.Definition {
	b.Helper()
	d, err := definition.Load([]byte(src), "bench")
	if err != nil {
		b.Fatal(err)
	}
	return d
}

// dfInputRows renders rows aligned under the header exactly as df does, so
// both whitespace and aligned splitting can consume the same input.
func dfInputRows(n int) []byte {
	var buf bytes.Buffer
	buf.WriteString("Filesystem     1K-blocks    Used Available Use% Mounted on\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "%-14s %9d %7d %9d %3d%% /mnt/volume-%d\n", fmt.Sprintf("/dev/sd%c%d", 'a'+rune(i%26), i%1000), 20000000+i, 5000000+i, 15000000-i, i%100, i)
	}
	return buf.Bytes()
}

func mountInputRows(n int) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "/dev/sd%c%d on /mnt/volume-%d type ext4 (rw,relatime,errors=remount-ro,data=ordered)\n", 'a'+rune(i%26), i, i)
	}
	return buf.Bytes()
}

func envInputRows(n int) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "VAR_%d=value number %d with some text\n", i, i)
	}
	return buf.Bytes()
}

const mountDef = `
format: 1
command: mount
variant: linux
parse:
  type: regex
  pattern: '^(?P<filesystem>.+?) on (?P<mount_point>.+?) type (?P<type>\S+) \((?P<options>[^)]*)\)$'
fields:
  options: {type: array, split: ","}
`

const envDef = "format: 1\ncommand: env\nvariant: posix\nparse: {type: kv}\n"

const alignedDef = `
format: 1
command: df
variant: aligned
parse:
  type: table
  split: aligned
  header:
    columns: [filesystem, 1k_blocks, used, available, use_percent, mounted_on]
fields:
  1k_blocks: {type: int}
  used: {type: int}
  available: {type: int}
  use_percent: {type: int, trim_suffix: "%"}
`

func benchParse(b *testing.B, def *definition.Definition, input []byte) {
	b.Helper()
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(def, input, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTableWhitespaceSmall(b *testing.B) {
	benchParse(b, benchDef(b, dfDef), dfInputRows(10))
}

func BenchmarkParseTableWhitespaceLarge(b *testing.B) {
	benchParse(b, benchDef(b, dfDef), dfInputRows(100000))
}

// psInputRows renders rows the way ps aux prints them: eleven columns,
// more than the few a df row has, and a process time in each row.
func psInputRows(n int) []byte {
	var buf bytes.Buffer
	buf.WriteString("USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "user%-7d %4d %4.1f %4.1f %6d %5d ?        Ss   09:14   %d:%02d /usr/bin/daemon --id %d\n", i%100, i, float64(i%1000)/10, float64(i%100)/10, 20000+i, 1000+i, i%600, i%60, i)
	}
	return buf.Bytes()
}

const psDef = `
format: 1
command: ps
variant: bsd
parse:
  type: table
  header:
    columns: [user, pid, cpu_percent, mem_percent, vsz, rss, tty, stat, start, time, command]
fields:
  pid: {type: int}
  cpu_percent: {type: float}
  mem_percent: {type: float}
  vsz: {type: int}
  rss: {type: int}
  tty: {null_if: ["?"]}
  time: {type: duration, layout: mm:ss}
`

// BenchmarkParseTableWideLarge reads a table whose rows hold more keys
// than a df row and convert a duration in each, which is where an object
// per row and the conversions of a row show.
func BenchmarkParseTableWideLarge(b *testing.B) {
	benchParse(b, benchDef(b, psDef), psInputRows(100000))
}

func BenchmarkParseTableAlignedLarge(b *testing.B) {
	benchParse(b, benchDef(b, alignedDef), dfInputRows(100000))
}

func BenchmarkParseRegexSmall(b *testing.B) {
	benchParse(b, benchDef(b, mountDef), mountInputRows(10))
}

func BenchmarkParseRegexLarge(b *testing.B) {
	benchParse(b, benchDef(b, mountDef), mountInputRows(100000))
}

func BenchmarkParseKVLarge(b *testing.B) {
	benchParse(b, benchDef(b, envDef), envInputRows(100000))
}

func BenchmarkEncodeJSONLarge(b *testing.B) {
	def := benchDef(b, dfDef)
	v, err := Parse(def, dfInputRows(100000), Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := jsonutil.Encode(&buf, v, false); err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(buf.Len()))
	}
}

func BenchmarkEncodeJSONPrettyLarge(b *testing.B) {
	def := benchDef(b, dfDef)
	v, err := Parse(def, dfInputRows(100000), Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := jsonutil.Encode(&buf, v, true); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStreamTableLarge measures `--stream` on the input the whole
// document benchmark reads, so the two are a pair: a stream hands each
// record over and keeps none of them, and a document keeps them all.
func BenchmarkStreamTableLarge(b *testing.B) {
	def := benchDef(b, dfDef)
	input := dfInputRows(100000)
	drop := func(any) error { return nil }
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		if err := Stream(def, bytes.NewReader(input), Options{}, drop); err != nil {
			b.Fatal(err)
		}
	}
}

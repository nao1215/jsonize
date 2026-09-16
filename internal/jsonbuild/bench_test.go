package jsonbuild

import (
	"strconv"
	"strings"
	"testing"
)

// BenchmarkPlanBuild measures what `jz new` pays for one document: the
// arguments are read into a plan, and the plan is built into the value
// that is written. Both halves are here, since a caller that makes one
// document pays both and `--each` pays the first once.
func BenchmarkPlanBuild(b *testing.B) {
	args := []Arg{
		{Form: Plain, Text: "name=api"},
		{Form: Plain, Text: "version=1.4.0"},
		{Form: Plain, Text: "replicas:=3"},
		{Form: Plain, Text: "debug:=false"},
		{Form: Plain, Text: "tags[]=web"},
		{Form: Plain, Text: "tags[]=prod"},
		{Form: Plain, Text: `limits:={"memory":"512Mi","cpu":"500m"}`},
	}
	b.ReportAllocs()
	for b.Loop() {
		plan, err := Parse(args, false)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := plan.Build(Sources{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPlanBuildDeep measures a document assembled from JSON
// Pointers, where each one walks the containers the ones before it made.
func BenchmarkPlanBuildDeep(b *testing.B) {
	for _, n := range []int{8, 64} {
		args := make([]Arg, 0, n)
		for i := range n {
			args = append(args, Arg{Form: Path, Text: "/metadata/labels/key" + strconv.Itoa(i) + "=value"})
		}
		b.Run("pointers="+strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				plan, err := Parse(args, false)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := plan.Build(Sources{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFixedEach measures the part `jz new --each` does once against
// the part it does per record: the files and the literals are read into a
// Fixed, and each record is then placed into a copy of it.
func BenchmarkFixedEach(b *testing.B) {
	args := []Arg{
		{Form: String, Text: "host=web"},
		{Form: Plain, Text: "env=prod"},
		{Form: Plain, Text: "sample:=@-"},
	}
	plan, err := Parse(args, false)
	if err != nil {
		b.Fatal(err)
	}
	src := Sources{Stdin: strings.NewReader("")}
	fixed, err := plan.Fixed(src)
	if err != nil {
		b.Fatal(err)
	}
	record := map[string]any{"r": int64(1), "b": int64(0), "free": int64(5123456)}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fixed.With(record); err != nil {
			b.Fatal(err)
		}
	}
}

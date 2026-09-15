package datafile

import (
	"strconv"
	"strings"
	"testing"
)

// csvInput is a csv of rows data rows with a quoted value in each, the
// shape an export from a spreadsheet or a database has.
func csvInput(rows int) []byte {
	var b strings.Builder
	b.WriteString("id,name,note,amount\n")
	for i := range rows {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(",user")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(",\"said \"\"hi\"\", then left\",")
		b.WriteString(strconv.Itoa(i * 7))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func benchmarkReadCSV(b *testing.B, rows int) {
	b.Helper()
	data := csvInput(rows)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Read(CSV, data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadCSV(b *testing.B) {
	for _, rows := range []int{10, 100000} {
		b.Run("rows="+strconv.Itoa(rows), func(b *testing.B) { benchmarkReadCSV(b, rows) })
	}
}

// records is n objects of a few fields each, written as format writes them.
func records(format string, n int) []byte {
	var b strings.Builder
	switch format {
	case JSON:
		b.WriteByte('[')
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"id":` + strconv.Itoa(i) + `,"name":"user","tags":["a","b"],"ok":true}`)
		}
		b.WriteByte(']')
	case JSONL:
		for i := range n {
			b.WriteString(`{"id":` + strconv.Itoa(i) + `,"name":"user","tags":["a","b"],"ok":true}` + "\n")
		}
	case YAML:
		for i := range n {
			b.WriteString("- id: " + strconv.Itoa(i) + "\n  name: user\n  tags: [a, b]\n  ok: true\n")
		}
	}
	return []byte(b.String())
}

func BenchmarkRead(b *testing.B) {
	for _, format := range []string{JSON, JSONL, YAML} {
		for _, n := range []int{10, 10000} {
			data := records(format, n)
			b.Run(format+"/records="+strconv.Itoa(n), func(b *testing.B) {
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := Read(format, data); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

package cli

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes a file under the harness's home and returns its path.
func (h *harness) writeFile(name string, data []byte) string {
	h.t.Helper()
	p := filepath.Join(h.home, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// a:1<TAB>b:2 and a newline, compressed with bzip2, which the standard
// library reads and does not write.
const ltsvBzip2 = "QlpoOTFBWSZTWTFFkAIAAANJAAAwMBAwACAAIhpjUIYDnMoHi7kinChIGKLIAQA="

// A data file is read as the format its extension names, compressed or
// not, and the output is the same whichever way the format was named.
func TestDataFileByExtension(t *testing.T) {
	h := newHarness(t)
	bz, err := base64.StdEncoding.DecodeString(ltsvBzip2)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"users.csv", []byte("id,name\n1,alice\n2,\"bob, jr\"\n"), `[{"id":"1","name":"alice"},{"id":"2","name":"bob, jr"}]`},
		{"UPPER.CSV", []byte("a\n1\n"), `[{"a":"1"}]`},
		{"rows.tsv", []byte("a\tb\n1\t2\n"), `[{"a":"1","b":"2"}]`},
		{"access.ltsv", []byte("host:127.0.0.1\tstatus:200\n"), `[{"host":"127.0.0.1","status":"200"}]`},
		{"events.jsonl", []byte("{\"x\":1}\n\n{\"x\":2}\n"), `[{"x":1},{"x":2}]`},
		{"events.ndjson", []byte("1\n"), `[1]`},
		{"doc.json", []byte(`{"b":1,"a":[2.50,12345678901234567890]}`), `{"b":1,"a":[2.50,12345678901234567890]}`},
		{"conf.yaml", []byte("name: app\nreplicas: 3\ntags: [a, b]\n"), `{"name":"app","replicas":3,"tags":["a","b"]}`},
		{"conf.yml", []byte("- 1\n"), `[1]`},
		{"events.jsonl.gz", gzipBytes(t, "{\"x\":1}\n"), `[{"x":1}]`},
		{"users.csv.gz", gzipBytes(t, "a,b\n1,2\n"), `[{"a":"1","b":"2"}]`},
		{"access.ltsv.bz2", bz, `[{"a":"1","b":"2"}]`},
	}
	for _, tt := range tests {
		p := h.writeFile(tt.name, tt.data)
		if code := h.run("--file", p); code != ExitOK {
			t.Errorf("%s: exit %d: %s", tt.name, code, h.stderr.String())
			continue
		}
		if got := strings.TrimSpace(h.stdout.String()); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestDataFileFromPipe(t *testing.T) {
	h := newHarness(t)
	for _, tt := range []struct {
		format, in, want string
	}{
		{"csv", "a,b\n1,2\n", `[{"a":"1","b":"2"}]`},
		{"tsv", "a\tb\n1\t2\n", `[{"a":"1","b":"2"}]`},
		{"ltsv", "a:1\n", `[{"a":"1"}]`},
		{"jsonl", "1\n2\n", `[1,2]`},
		{"json", `{"a":true}`, `{"a":true}`},
		{"yaml", "a: ~\n", `{"a":null}`},
	} {
		if code := h.pipe(tt.in, "--format", tt.format); code != ExitOK {
			t.Errorf("%s: exit %d: %s", tt.format, code, h.stderr.String())
			continue
		}
		if got := strings.TrimSpace(h.stdout.String()); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.format, got, tt.want)
		}
	}
	// --format says how to read a file whatever its name.
	p := h.writeFile("export.txt", []byte("k:v\n"))
	if code := h.run("--format", "ltsv", "--file", p); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"k":"v"}]` {
		t.Errorf("--format over a file: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	j := h.writeFile("data.json", []byte("a:1\n"))
	if code := h.run("--format", "ltsv", "--file", j); code != ExitOK {
		t.Errorf("--format over an extension: %d %s", code, h.stderr.String())
	}
}

func TestDataFileUsageErrors(t *testing.T) {
	h := newHarness(t)
	csv := h.writeFile("u.csv", []byte("a\n1\n"))
	for _, tt := range []struct {
		name  string
		input string
		args  []string
		want  string
	}{
		{"an unknown format", "", []string{"--format", "xml"}, `unknown format "xml"; the formats are csv, tsv, ltsv, jsonl, json, yaml`},
		{"with --parser", "", []string{"--format", "csv", "--parser", "df"}, "--format and --parser/--variant/--define cannot be used together"},
		{"with --define", "", []string{"--format", "json", "--define", "parse: {type: csv}"}, "cannot be used together"},
		{"--columns for a document", "", []string{"--format", "json", "--columns", "a"}, "--columns names the columns of a csv or a tsv, and standard input is read as json"},
		{"--stream for a document", "{}", []string{"--format", "yaml", "--stream"}, "--stream writes the records of a record format (ltsv, jsonl, lines, nul), and a yaml document is one value"},
		{"a key the document does not have", `{"a":1}`, []string{"--format", "json", "--extract", "b"}, `no key "b" in the output`},
		{"a key no record had, streamed", "{\"a\":1}\n", []string{"--format", "jsonl", "--stream", "--extract", "b"}, `no key "b" in the output`},
		{"--columns for a named csv file", "", []string{"--file", csv, "--columns", "x", "--parser", "df"}, "--columns names the columns of a csv read without a header line"},
		{"--stream for a document that does not exist", "", []string{"--file", filepath.Join(h.home, "missing.yaml"), "--stream"}, "a yaml document is one value"},
		{"--columns for a document that does not exist", "", []string{"--file", filepath.Join(h.home, "missing.jsonl"), "--columns", "a"}, "is read as jsonl"},
		{"--stream for a text", "x", []string{"--format", "text", "--stream"}, "and a text document is one value"},
		{"--type for a document", "{}", []string{"--format", "json", "--type", "a=int"}, "--type converts the columns of a csv or a tsv, and standard input is read as json"},
		{"--type for lines", "1\n", []string{"--format", "lines", "--type", "a=int"}, "and standard input is read as lines"},
		{"--type without a csv", "a\n", []string{"--type", "a=int"}, "--type converts the columns of a csv or a tsv: read a .csv or .tsv file"},
		{"--type with --define", "a\n", []string{"--define", "parse: {type: csv}", "--type", "a=int"}, "--define states its own"},
		{"--type with an unknown type", "a\n", []string{"--format", "csv", "--type", "a=integer"}, `--type a=integer: unknown type "integer"; the types are int, float, bool`},
		{"--type without a type", "a\n", []string{"--format", "csv", "--type", "a"}, `--type expects COLUMN=TYPE with a type of int, float, bool, got "a"`},
		{"--type given a column twice", "a\n", []string{"--format", "csv", "--type", "a=int", "--type", "a=float"}, `--type gives the column "a" twice`},
		{"--type on a column --columns does not name", "1\n", []string{"--format", "csv", "--columns", "a", "--type", "b=int"}, `--type: no column "b"; --columns names a`},
		{"--type on a column the header lacks", "a\n1\n", []string{"--format", "csv", "--type", "b=int"}, `jz: csv/comma: line 1: no column "b"; the columns are "a"`},
		{"--type on a column the header lacks, streamed", "a\n1\n", []string{"--format", "csv", "--stream", "--type", "b=int"}, `no column "b"`},
		{"--type with --raw", "a\n1\n", []string{"--format", "csv", "--raw", "--type", "a=int"}, "--type and --raw cannot be used together"},
	} {
		if code := h.pipe(tt.input, tt.args...); code != ExitUsage || !strings.Contains(h.stderr.String(), tt.want) {
			t.Errorf("%s: exit %d, stderr %q, want %q", tt.name, code, h.stderr.String(), tt.want)
		}
		if h.stdout.Len() > 0 && tt.name != "a key no record had, streamed" {
			t.Errorf("%s: wrote %s", tt.name, h.stdout.String())
		}
	}
}

func TestDataFileParseFailures(t *testing.T) {
	h := newHarness(t)
	for _, tt := range []struct {
		name string
		data []byte
		want string
		code int
	}{
		{"broken.json", []byte("{\n\"a\": 1\n\"b\": 2\n}"), "jz: json: line 3: not valid JSON", ExitParse},
		{"twice.json", []byte(`{"a":1,"a":2}`), `the key "a" is given twice`, ExitParse},
		{"bad.jsonl", []byte("1\n{\n"), "jz: jsonl: line 2: the line is not one JSON value", ExitParse},
		{"bad.ltsv", []byte("a:1\nno label\n"), "jz: ltsv: line 2: field 1 has no label", ExitParse},
		{"bad.yaml", []byte("a: .nan\n"), "jz: yaml: line 1: .nan is not a number JSON can hold", ExitParse},
		{"ragged.csv", []byte("a,b\n1,2,3\n"), "line 2", ExitParse},
		{"notgzip.json.gz", []byte("plain text"), "notgzip.json.gz: the gzip data cannot be decompressed", ExitParse},
		{"cut.jsonl.gz", gzipBytes(t, strings.Repeat("{\"a\":1}\n", 50))[:40], "the gzip data cannot be decompressed", ExitParse},
	} {
		p := h.writeFile(tt.name, tt.data)
		if code := h.run("--file", p); code != tt.code || !strings.Contains(h.stderr.String(), tt.want) {
			t.Errorf("%s: exit %d, stderr %q, want %d %q", tt.name, code, h.stderr.String(), tt.code, tt.want)
		}
		if h.stdout.Len() > 0 {
			t.Errorf("%s: wrote %s", tt.name, h.stdout.String())
		}
	}
	if code := h.run("--file", filepath.Join(h.home, "missing.json")); code != ExitError {
		t.Errorf("a missing file: %d %s", code, h.stderr.String())
	}
}

// What the extension says gives way to what the caller said, and a file
// whose extension names no data format is read the way it always was.
func TestDataFileExtensionGivesWay(t *testing.T) {
	h := newHarness(t)
	tsv := h.writeFile("really-tab.csv", []byte("a\tb\n1\t2\n"))
	if code := h.run("--file", tsv, "--parser", "csv", "--variant", "tab"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"a":"1","b":"2"}]` {
		t.Errorf("--parser over the extension: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.run("--file", tsv, "--define", "parse: {type: csv, delimiter: \"\\t\"}"); code != ExitOK {
		t.Errorf("--define over the extension: %d %s", code, h.stderr.String())
	}
	df := h.writeFile("df.txt.gz", gzipBytes(t, gnuDF))
	if code := h.run("--file", df); code != ExitOK || len(h.rows()) != 2 {
		t.Errorf("a compressed capture is detected: %d %s", code, h.stderr.String())
	}
	headerless := h.writeFile("rows.csv", []byte("1,alice\n2,bob\n"))
	if code := h.run("--file", headerless, "--columns", "id,name"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"id":"1","name":"alice"},{"id":"2","name":"bob"}]` {
		t.Errorf("--columns with a csv file: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	tsvRows := h.writeFile("rows.tsv", []byte("1\talice\n"))
	if code := h.run("--file", tsvRows, "--columns", "id,name"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"id":"1","name":"alice"}]` {
		t.Errorf("--columns with a tsv file: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
}

func TestDataFileOutputOptions(t *testing.T) {
	h := newHarness(t)
	doc := `{"id":12345678901234567890,"ratio":1e3,"name":"app","nested":{"k":[1,2]}}`
	if code := h.pipe(doc, "--format", "json", "--pretty"); code != ExitOK || !strings.Contains(h.stdout.String(), "\n  \"id\": 12345678901234567890") {
		t.Errorf("--pretty: %d %s", code, h.stdout.String())
	}
	if code := h.pipe(`[{"a":1,"b":2},{"a":3,"b":4}]`, "--format", "json", "--exclude", "b"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"a":1},{"a":3}]` {
		t.Errorf("--exclude: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("a:1\tb:2\na:3\tb:4\n", "--format", "ltsv", "--stream", "--extract", "a"); code != ExitOK || h.stdout.String() != "{\"a\":\"1\"}\n{\"a\":\"3\"}\n" {
		t.Errorf("--stream --extract: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	// A stream writes the records before the line that fails, then stops.
	if code := h.pipe("1\n2\nx\n3\n", "--format", "jsonl", "--stream"); code != ExitParse || h.stdout.String() != "1\n2\n" || !strings.Contains(h.stderr.String(), "line 3") {
		t.Errorf("a stream that fails: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	// The options about a format's fields change nothing for a document.
	if code := h.pipe("a: 1\n", "--format", "yaml", "--raw", "--assume-year", "2024"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `{"a":1}` {
		t.Errorf("--raw and --assume-year: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
}

func TestDataFileExplain(t *testing.T) {
	h := newHarness(t)
	p := h.writeFile("events.jsonl.gz", gzipBytes(t, "1\n2\n"))
	if code := h.run("--file", p, "--explain"); code != ExitOK {
		t.Fatalf("%d %s", code, h.stderr.String())
	}
	want := "jz: explain: read as jsonl: the format the extension of " + p + " names, so nothing was chosen\njz: explain: read: 2 values\n"
	if h.stderr.String() != want {
		t.Errorf("text:\n%s\nwant\n%s", h.stderr.String(), want)
	}
	if code := h.pipe("{}", "--format", "json", "--explain=json"); code != ExitOK {
		t.Fatalf("%d %s", code, h.stderr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(h.stderr.String()), "jz: explain: ")), &doc); err != nil {
		t.Fatalf("%v: %s", err, h.stderr.String())
	}
	chosen, _ := doc["chosen"].(map[string]any)
	scope, _ := doc["scope"].(map[string]any)
	read, _ := doc["read"].(map[string]any)
	if doc["outcome"] != "format" || chosen["definition"] != "json" || chosen["registry"] != "--format" || scope["from"] != "--format" || read["values"] != float64(1) || read["lines"] != nil {
		t.Errorf("json: %v", doc)
	}
	// A JSON document that is an array is still one value.
	if code := h.pipe("[1,2,3]", "--format", "json", "--explain"); code != ExitOK || !strings.Contains(h.stderr.String(), "read: 1 value\n") {
		t.Errorf("a json array: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("x\n", "--format", "jsonl", "--explain"); code != ExitParse || !strings.Contains(h.stderr.String(), "jz: explain: read as jsonl: the format was given with --format") || strings.Contains(h.stderr.String(), "read: ") {
		t.Errorf("a failure: %d %s", code, h.stderr.String())
	}
	csv := h.writeFile("u.csv", []byte("a\n1\n"))
	if code := h.run("--file", csv, "--explain"); code != ExitOK || !strings.Contains(h.stderr.String(), "jz: explain: scope: csv/comma, the reading of the format the extension of "+csv+" names") {
		t.Errorf("a csv: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("a,b\n", "--format", "csv", "--explain"); code != ExitOK || !strings.Contains(h.stderr.String(), "scope: csv/comma, the reading of the format given with --format") {
		t.Errorf("a csv from a pipe: %d %s", code, h.stderr.String())
	}
	if code := h.pipe("a:1\n", "--format", "ltsv", "--stream", "--explain"); code != ExitOK || !strings.HasPrefix(h.stderr.String(), "jz: explain: read as ltsv") {
		t.Errorf("a stream: %d %s", code, h.stderr.String())
	}
}

// text is the input as one string; lines and nul are lists of strings,
// written record by record with --stream. Nothing is trimmed, and the
// options that say where the input comes from apply as they do to any
// data format.
func TestTextFormatsOnTheCommandLine(t *testing.T) {
	h := newHarness(t)
	for _, tt := range []struct {
		input string
		args  []string
		want  string
	}{
		{" two words \r\n\n", []string{"--format", "text"}, `" two words \r\n\n"` + "\n"},
		{"", []string{"--format", "text"}, `""` + "\n"},
		{"a b\r\n\nlast", []string{"--format", "lines"}, `["a b","","last"]` + "\n"},
		{"", []string{"--format", "lines"}, "[]\n"},
		{"one\ntwo\n", []string{"--format", "lines", "--stream"}, "\"one\"\n\"two\"\n"},
		{"x y\x00\x00z\n\x00", []string{"--format", "nul"}, `["x y","","z\n"]` + "\n"},
		{"x\x00y\x00", []string{"--format", "nul", "--stream"}, "\"x\"\n\"y\"\n"},
		{"a\nb\n", []string{"--format", "lines", "--pretty"}, "[\n  \"a\",\n  \"b\"\n]\n"},
	} {
		if code := h.pipe(tt.input, tt.args...); code != ExitOK || h.stdout.String() != tt.want {
			t.Errorf("%q %v: exit %d, stdout %q, stderr %q, want %q", tt.input, tt.args, code, h.stdout.String(), h.stderr.String(), tt.want)
		}
	}
	notes := h.writeFile("notes.txt", []byte("keep\n"))
	if code := h.run("--format", "text", "--file", notes); code != ExitOK || h.stdout.String() != `"keep\n"`+"\n" {
		t.Errorf("--format text --file: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("a\nb\n", "--format", "lines", "--explain=json"); code != ExitOK || !strings.Contains(h.stderr.String(), `"values":2`) {
		t.Errorf("--explain=json counts the records: %d %s", code, h.stderr.String())
	}
	// A record that is not UTF-8 is exit 3; a stream keeps what it wrote.
	if code := h.pipe("ok\ncaf\xe9\n", "--format", "lines"); code != ExitParse || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "jz: lines: line 2: the text is not valid UTF-8") {
		t.Errorf("lines not UTF-8: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("ok\x00caf\xe9\x00", "--format", "nul", "--stream"); code != ExitParse || h.stdout.String() != "\"ok\"\n" || !strings.Contains(h.stderr.String(), "jz: nul: record 2: the text is not valid UTF-8") {
		t.Errorf("nul not UTF-8, streamed: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
}

// --type converts the columns it names with a definition's field rules:
// the rest stay strings, an empty value is null, and a value that is not
// the type is exit 3 naming the line and the column.
func TestColumnTypes(t *testing.T) {
	h := newHarness(t)
	rows := h.writeFile("rows.csv", []byte("id,count,enabled,ratio\n007,3,yes,1.50\n8,,false,\n"))
	want := `[{"id":"007","count":3,"enabled":true,"ratio":1.5},{"id":"8","count":null,"enabled":false,"ratio":null}]` + "\n"
	if code := h.run("--file", rows, "--type", "count=int", "--type", "enabled=bool", "--type", "ratio=float"); code != ExitOK || h.stdout.String() != want {
		t.Errorf("typed csv: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("id\tcount\n1\t2\n", "--format", "tsv", "--type", "count=int", "--stream"); code != ExitOK || h.stdout.String() != `{"id":"1","count":2}`+"\n" {
		t.Errorf("typed tsv stream: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("1,x\n", "--format", "csv", "--columns", "id,count", "--type", "id=int"); code != ExitOK || h.stdout.String() != `[{"id":1,"count":"x"}]`+"\n" {
		t.Errorf("typed with --columns: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("a,1\n", "--parser", "csv", "--variant", "comma-no-header", "--type", "column_2=int"); code != ExitOK || h.stdout.String() != `[{"column_1":"a","column_2":1}]`+"\n" {
		t.Errorf("typed numbered columns: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	bad := "id,count\n1,2\n2,many\n3,4\n"
	if code := h.pipe(bad, "--format", "csv", "--type", "count=int"); code != ExitParse || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), `jz: csv/comma: line 3: field "count": cannot convert "many" to int`) {
		t.Errorf("a value that is not an int: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe(bad, "--format", "csv", "--type", "count=int", "--stream"); code != ExitParse || h.stdout.String() != `{"id":"1","count":2}`+"\n"+`{"id":"3","count":4}`+"\n" {
		t.Errorf("a value that is not an int, streamed: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("id\n1\n", "--format", "csv", "--stream", "--type", "count=int"); code != ExitUsage || h.stdout.Len() != 0 {
		t.Errorf("a missing column ends a stream before a record: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	// A definition's own field naming a column the header lacks does
	// nothing, as it always has; only a column the caller named is checked.
	if code := h.pipe("id\n1\n", "--define", "parse: {type: csv}\nfields: {count: {type: int}}"); code != ExitOK || h.stdout.String() != `[{"id":"1"}]`+"\n" {
		t.Errorf("a definition's field for a missing column: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
}

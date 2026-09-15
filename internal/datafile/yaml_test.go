package datafile

import (
	"errors"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/engine"
)

func TestReadYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"a mapping keeps its order", "z: 1\na: 2\n", `{"z":1,"a":2}`},
		{"the core schema's scalars", "n1: null\nn2: ~\nn3:\nt: True\nf: FALSE\ni: -12\no: 0o17\nx: 0xFF\nd: 1.5\ne: 2e3\ndot: .5\n",
			`{"n1":null,"n2":null,"n3":null,"t":true,"f":false,"i":-12,"o":15,"x":255,"d":1.5,"e":2000,"dot":0.5}`},
		{"quoted scalars are strings", "a: \"1\"\nb: 'true'\nc: \"null\"\n", `{"a":"1","b":"true","c":"null"}`},
		{"a block scalar is a string", "a: |\n  12\n", `{"a":"12\n"}`},
		{"words are strings", "a: yes\nb: 1.2.3\nc: 0x\nd: +\n", `{"a":"yes","b":"1.2.3","c":"0x","d":"+"}`},
		{"a leading plus and zeros", "a: +7\nb: 007\n", `{"a":7,"b":7}`},
		{"an integer past 64 bits keeps its digits", "a: 123456789012345678901234\nb: +99999999999999999999\nc: -0099999999999999999999\n", `{"a":123456789012345678901234,"b":99999999999999999999,"c":-99999999999999999999}`},
		{"a hexadecimal integer past int64", "a: 0xFFFFFFFFFFFFFFFF\n", `{"a":18446744073709551615}`},
		{"sequences and flow collections", "- [1, a]\n- {k: v}\n- - x\n", `[[1,"a"],{"k":"v"},["x"]]`},
		{"a scalar document", "hello\n", `"hello"`},
		{"a byte order mark", "\xEF\xBB\xBFa: 1\n", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := Read(YAML, []byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, v); got != tt.want {
				t.Errorf("got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestReadYAMLRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, in string
		line     int
		want     string
	}{
		{"infinity", "a: 1\nb: .inf\n", 2, "not a number JSON can hold"},
		{"not a number", "- -.Inf\n- .NaN\n", 1, "not a number JSON can hold"},
		{"a float out of range", "a: 1e999\n", 1, "out of the range"},
		{"a hexadecimal integer past 64 bits", "a: 0x1FFFFFFFFFFFFFFFF\n", 1, "does not fit in 64 bits"},
		{"a key given twice", "a: 1\na: 2\n", 2, "duplicate key"},
		{"an anchor", "a: &x 1\nb: *x\n", 1, ""},
		{"not UTF-8", "a: 1\nb: \xff\n", 2, "not valid UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(YAML, []byte(tt.in))
			parseError(t, err, tt.line, tt.want)
		})
	}
	for _, empty := range []string{"", "# only a comment\n"} {
		_, err := Read(YAML, []byte(empty))
		var pe *engine.ParseError
		if !errors.As(err, &pe) || !strings.Contains(pe.Msg, "no YAML value") {
			t.Errorf("%q: %v", empty, err)
		}
	}
}

// No text makes the reader panic, and whatever it reads is JSON the
// JSON reader reads back the same.
func FuzzReadYAML(f *testing.F) {
	for _, s := range []string{"a: 1\n", "- [x, {y: .5}]\n", "a: |\n  b\n", "a: 0x1F\nb: 1e999\n", "k:\n", "\"q\": 'r'\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Read(YAML, data)
		if err != nil {
			var pe *engine.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("a failure that is not a parse error: %v", err)
			}
			return
		}
		out := encode(t, v)
		again, err := Read(JSON, []byte(out))
		if err != nil {
			t.Fatalf("the JSON written does not read back: %v\n%s", err, out)
		}
		if encode(t, again) != out {
			t.Fatalf("a second reading differs:\n%s\n%s", out, encode(t, again))
		}
	})
}

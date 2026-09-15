package datafile

import (
	"strings"
	"testing"
)

func TestLTSV(t *testing.T) {
	t.Parallel()
	in := "host:127.0.0.1\tident:-\ttime:[10/Oct/2000:13:55:36 -0700]\treq:GET / HTTP/1.0\r\n" +
		"\n" +
		"a.b_c-d:\tEmpty:\n"
	v, err := Read(LTSV, []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"host":"127.0.0.1","ident":"-","time":"[10/Oct/2000:13:55:36 -0700]","req":"GET / HTTP/1.0"},{"a.b_c-d":"","Empty":""}]`
	if got := encode(t, v); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// A value keeps its spaces: LTSV has no quoting to lose them to.
	v, err = Read(LTSV, []byte("k: padded \n"))
	if err != nil || encode(t, v) != `[{"k":" padded "}]` {
		t.Errorf("spaces in a value: %v %v", v, err)
	}
}

func TestLTSVRefuses(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 70)
	for _, tt := range []struct {
		name, in string
		line     int
		want     string
	}{
		{"a field without a label", "a:1\n2\n", 2, "field 1 has no label"},
		{"an empty field", "a:1\t\tb:2\n", 1, "field 2 has no label"},
		{"a trailing tab", "a:1\t\n", 1, "field 2 has no label"},
		{"an empty label", ":v\n", 1, `"" is not an LTSV label`},
		{"a label with a space", "my key:v\n", 1, `"my key" is not an LTSV label`},
		{"a label given twice", "a:1\ta:2\n", 1, `the label "a" is given twice`},
		{"a long field is cut in the message", long + "\n", 1, "..."},
		{"not UTF-8", "a:\xff\n", 1, "not valid UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(LTSV, []byte(tt.in))
			parseError(t, err, tt.line, tt.want)
		})
	}
}

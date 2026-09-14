package conformance

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
)

// settleRegistry holds a format decided on its first line, one whose
// header may come on any line, and one read record by record with NUL.
func settleRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Load(registry.Source{Name: "test", FS: fstest.MapFS{
		"parsers/first/v/parser.yaml": {Data: []byte("format: 1\ncommand: first\nvariant: v\ndetect: {signature: {all: ['\\A# first']}}\nparse: {type: regex, pattern: '^(?P<line>.*)$'}\n")},
		"parsers/late/v/parser.yaml":  {Data: []byte("format: 1\ncommand: late\nvariant: v\ndetect: {signature: {all: ['^# late$']}}\nparse: {type: regex, pattern: '^(?P<line>.*)$'}\n")},
		"parsers/zero/v/parser.yaml":  {Data: []byte("format: 1\ncommand: zero\nvariant: v\ninput: {record_separator: nul}\nparse: {type: regex, pattern: '^(?P<name>.*)$'}\n")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// A stream that settles gives the answer the whole text gives, on every
// input shape the head of a stream can have.
func TestSettlesAsWhole(t *testing.T) {
	t.Parallel()
	reg := settleRegistry(t)
	for _, tc := range []struct {
		name  string
		ctx   selector.Context
		input string
	}{
		{"settled on the first line", selector.Context{}, "# first\na\nb\n"},
		{"blank lines before the text", selector.Context{}, "\n\n# first\na\n"},
		{"a header that may still come keeps it open to the end", selector.Context{}, "x\ny\n# late\n"},
		{"the last line has no line break", selector.Context{Parser: "first"}, "# first\na"},
		{"the input ends on the first line", selector.Context{Parser: "first"}, "# first\n"},
		{"named, settled at once", selector.Context{Parser: "first", Variant: "v"}, "# first\nmore\n"},
		{"records ended with NUL", selector.Context{Parser: "zero", Variant: "v"}, "a\x00b\x00c\x00"},
		{"an error both ways", selector.Context{Parser: "first"}, "nothing\nat all\n"},
	} {
		if err := settlesAsWhole(reg, tc.ctx, []byte(tc.input)); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestSameChoice(t *testing.T) {
	t.Parallel()
	reg := settleRegistry(t)
	first, _ := reg.Lookup("first", "v")
	late, _ := reg.Lookup("late", "v")
	a, b := &selector.Result{Entry: first}, &selector.Result{Entry: late}
	failed := errors.New("no parser matches\nand more lines")
	for _, tc := range []struct {
		name       string
		got, want  *selector.Result
		early, all error
		message    string
	}{
		{"the same definition", a, a, nil, nil, ""},
		{"a failure both times", nil, nil, failed, failed, ""},
		{"another definition", a, b, nil, nil, "a stream settles on first/v after 3 lines, the whole text chooses late/v"},
		{"a choice the whole text refuses", a, nil, nil, failed, `a stream settles after 3 lines with first/v, the whole text gives the error "no parser matches"`},
		{"a refusal the whole text reads", nil, b, failed, nil, `with the error "no parser matches", the whole text gives late/v`},
	} {
		err := sameChoice(3, tc.got, tc.early, tc.want, tc.all)
		switch {
		case tc.message == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.message != "" && (err == nil || !strings.Contains(err.Error(), tc.message)):
			t.Errorf("%s: %v, want %q", tc.name, err, tc.message)
		}
	}
}

func TestScopeName(t *testing.T) {
	t.Parallel()
	for ctx, want := range map[*selector.Context]string{
		{}:                               "automatic detection",
		{Parser: "df"}:                   "--parser df",
		{Parser: "df", Variant: "gnu"}:   "--parser df --variant gnu",
		{Parser: "df", Args: []string{}}: "df run with the recorded system and arguments",
		{Parser: "df", OS: "linux"}:      "df run with the recorded system and arguments",
	} {
		if got := scopeName(*ctx); got != want {
			t.Errorf("scopeName(%+v) = %q, want %q", *ctx, got, want)
		}
	}
}

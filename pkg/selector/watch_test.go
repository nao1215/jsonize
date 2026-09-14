package selector

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/registry"
)

// streamRegistry holds the shapes Watch has to tell apart: a signature
// anchored to its first lines, one that looks for a header anywhere, one
// with a none further down, one that looks at the end of the text, two
// variants of one command ranked by priority, a definition only used when
// named and a shape with no signature beside a variant that has one.
func streamRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	return buildRegistry(t, map[string]string{
		// Decided by its first two lines.
		"vm/plain":  def("vm", "plain", "detect:\n  signature:\n    all: ['\\Aprocs ', '\\A[^\\n]*\\n r b swpd free buff']\n"),
		"vm/active": def("vm", "active", "detect:\n  signature:\n    all: ['\\Aprocs ', '\\A[^\\n]*\\n r b swpd free inact']\n"),
		// A header that may come on any line of the window.
		"hdr/any": def("hdr", "any", "detect:\n  signature:\n    all: ['^Header here$']\n"),
		// Fits from its first line, and a line further down rules it out.
		"log/plain": def("log", "plain", "detect:\n  signature:\n    all: ['\\A\\d+ ']\n    none: ['^ERROR ']\n    window: 5\n"),
		// The whole window has to be one shape.
		"num/only": def("num", "only", "detect:\n  signature:\n    all: ['\\A(?:\\d+\\n)*\\d+\\z']\n"),
		// Ranked: the default wins a tie with the other.
		"rank/default": def("rank", "default", "detect:\n  priority: 1\n  signature:\n    all: ['\\A= ']\n"),
		"rank/other":   def("rank", "other", "detect:\n  signature:\n    all: ['\\A= ', '^extra$']\n"),
		// Only for linux, and only with -x.
		"os/linux": def("os", "linux", "detect:\n  os: [linux]\n  args: {any: ['-x']}\n  signature:\n    all: ['\\A@ ']\n"),
		// Used only when named, with a header it may show anywhere.
		"quiet/named": def("quiet", "named", "detect:\n  auto_detect: false\n  signature:\n    all: ['^Quiet$']\n"),
		// A shape beside a variant that claims its text.
		"shape/claims": def("shape", "claims", "detect:\n  signature:\n    all: ['\\A# shape']\n"),
		"shape/any":    def("shape", "any", ""),
	})
}

func TestWatch(t *testing.T) {
	t.Parallel()
	reg := streamRegistry(t)
	tests := []struct {
		name  string
		ctx   Context
		input string
		want  bool
	}{
		{"two lines decide both vm variants", Context{Parser: "vm"}, "procs -----\n r b swpd free buff cache\n", true},
		{"one line leaves the second line open", Context{Parser: "vm"}, "procs -----\n", false},
		{"a named variant decided by its own lines", Context{Parser: "vm", Variant: "plain"}, "procs -----\n r b swpd free buff cache\n", true},
		{"a named variant refused by its own lines is decided too", Context{Parser: "vm", Variant: "plain"}, "nothing\n", true},
		// hdr/any has not matched and could still: its header may come.
		{"a header that may still come keeps a search open", Context{}, "procs -----\n r b swpd free buff cache\n", false},
		{"a none that may still match keeps a fit open", Context{Parser: "log"}, "1 started\n2 running\n", false},
		{"a none that matched rules out at once", Context{Parser: "log"}, "1 started\nERROR boom\n", true},
		{"a full window is decided whole", Context{Parser: "log"}, "1 a\n2 b\n3 c\n4 d\n5 e\n", true},
		{"the end of the text is never final before it comes", Context{Parser: "num"}, "1\n2\n", false},
		// Every line has to be a number, so a first line that is not one
		// leaves no way through the expression, however many lines follow.
		{"an anchored expression the first line leaves no way through", Context{Parser: "num"}, "x\n", true},
		{"an anchored expression the lines so far keep open", Context{Parser: "num"}, "7\n8\n", false},
		{"a higher priority that fits does not wait for a lower one", Context{Parser: "rank"}, "= a\n", true},
		{"the system rules a definition out before any line", Context{Parser: "os", OS: "darwin"}, "", true},
		{"a missing argument rules a definition out", Context{Parser: "os", OS: "linux", Args: []string{"-y"}}, "", true},
		{"with the argument, the line decides", Context{Parser: "os", OS: "linux", Args: []string{"-x"}}, "@ one\n", true},
		{"no line yet decides nothing", Context{Parser: "os", OS: "linux", Args: []string{"-x"}}, "", false},
		{"blank lines before the text decide nothing", Context{Parser: "vm", Variant: "plain"}, "\n\n", false},
		// A definition only used when named is never the answer of a
		// search of the whole registry, so its header is not waited for.
		{"a definition used only when named is not waited for", Context{Parser: "os", OS: "linux", Args: []string{"-x"}}, "@ one\nQuiet\n", true},
		{"named, it is waited for like any other", Context{Parser: "quiet"}, "something\n", false},
		// The name alone does not reach a shape beside a variant that
		// claims its text, so the shape is not waited for either.
		{"a shape the name does not reach is not waited for", Context{Parser: "shape"}, "# shape\n", true},
		{"a shape named by its variant fits at once", Context{Parser: "shape", Variant: "any"}, "anything\n", true},
		{"a variant with no parser is an error at once", Context{Variant: "plain"}, "", true},
		{"a parser nothing defines is an error at once", Context{Parser: "nothing"}, "", true},
		{"a variant the parser does not have is an error at once", Context{Parser: "vm", Variant: "missing"}, "", true},
		// A NUL byte rules every format read line by line out.
		{"a NUL rules out the formats read by line", Context{Parser: "vm"}, "procs\x00\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Watch(reg, tt.ctx)([]byte(tt.input)); got != tt.want {
				t.Errorf("Watch = %v, want %v", got, tt.want)
			}
		})
	}
}

// The promise Watch makes: once it says the lines so far settle the
// choice, no lines that follow change what Select answers. Every prefix
// of every input is paired with every continuation, including ones built
// to trip a none expression, a late header and a second variant.
func TestWatchChoiceSurvivesAnyContinuation(t *testing.T) {
	t.Parallel()
	reg := streamRegistry(t)
	inputs := []string{
		"procs -----\n r b swpd free buff cache\n 1 0 0 100 2 3\n",
		"procs -----\n r b swpd free inact active\n 1 0 0 100 2 3\n",
		"1 started\n2 running\n3 done\n",
		"= a\n= b\n",
		"@ one\n@ two\n",
		"7\n8\n9\n",
		"# shape\nrest\n",
		"\n\nprocs -----\r\n r b swpd free buff cache\r\n",
	}
	tails := []string{
		"", "ERROR boom\n", "Header here\n", "extra\n", "x\n", "10\n", "Quiet\n",
		strings.Repeat("filler\n", 25) + "Header here\n",
		strings.Repeat("1 more\n", 3) + "ERROR late\n",
	}
	scopes := []Context{
		{}, {Parser: "vm"}, {Parser: "log"}, {Parser: "num"}, {Parser: "rank"}, {Parser: "quiet"}, {Parser: "shape"},
		{Parser: "vm", Variant: "active"}, {Parser: "os", OS: "linux", Args: []string{"-x"}},
	}
	checked := 0
	for _, in := range inputs {
		lines := strings.SplitAfter(in, "\n")
		for k := 1; k <= len(lines); k++ {
			head := strings.Join(lines[:k], "")
			if !strings.HasSuffix(head, "\n") {
				continue
			}
			for _, scope := range scopes {
				if !settledAt(reg, scope, lines[:k]) {
					continue
				}
				ctx := scope
				ctx.Input = []byte(head)
				first, firstErr := Select(reg, ctx)
				for _, tail := range tails {
					ctx.Input = []byte(head + tail)
					later, laterErr := Select(reg, ctx)
					checked++
					switch {
					case (firstErr == nil) != (laterErr == nil):
						t.Errorf("scope %+v, %q settled with err=%v, then %q gave err=%v", scope, head, firstErr, tail, laterErr)
					case firstErr == nil && first.Entry != later.Entry:
						t.Errorf("scope %+v, %q settled on %s, then %q chose %s", scope, head, first.Entry.Def.ID(), tail, later.Entry.Def.ID())
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no prefix settled, so nothing was checked")
	}
}

// settledAt feeds one watch the lines one at a time, the way a stream
// does, and reports what it says after the last of them.
func settledAt(reg *registry.Registry, scope Context, lines []string) bool {
	settled := Watch(reg, scope)
	got := false
	for i := range lines {
		got = settled([]byte(strings.Join(lines[:i+1], "")))
	}
	return got
}

func TestViable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		expr, prefix string
		want         bool
	}{
		{`\Aprocs `, "procs ---\n", true},
		{`\Aprocs `, "disk- ---\n", false},
		// \s crosses a line break, so the header may still come on the
		// next line; [ \t] does not.
		{`\AFilesystem\s+Size`, "Filesystem\n", true},
		{`\AFilesystem[ \t]+Size`, "Filesystem\n", false},
		{`\A[^\n]*\n r b swpd free inact`, "procs\n r b swpd free buff\n", false},
		{`\A[^\n]*\n r b swpd free inact`, "procs\n", true},
		{`\A(?:\d+\n)*\d+\z`, "7\n8\n", true},
		{`\A(?:\d+\n)*\d+\z`, "7\nx\n", false},
		// An assertion where the prefix ends may hold for what comes next.
		{`\Afoo\b`, "foo", true},
		{`\Afoo$`, "foo\n", true},
		// A word boundary inside the prefix is judged on what is there.
		{`\Afoo\bx`, "foox\n", false},
		{`\A(?:a|b)c`, "bc\n", true},
	}
	for _, tt := range tests {
		re := regexp.MustCompile("(?m)" + tt.expr)
		s := shapeOf(re)
		if s.prog == nil {
			t.Errorf("%s: not taken for anchored", tt.expr)
			continue
		}
		if got := viable(s.prog, tt.prefix); got != tt.want {
			t.Errorf("viable(%s, %q) = %v, want %v", tt.expr, tt.prefix, got, tt.want)
		}
	}
	for _, expr := range []string{`^Header`, `foo|\Abar`, `(?:\A)?x`} {
		if shapeOf(regexp.MustCompile("(?m)"+expr)).prog != nil {
			t.Errorf("%s was taken for anchored", expr)
		}
	}
	for _, expr := range []string{`\A\d+\z`, `(?-m:\d$)`} {
		if !shapeOf(regexp.MustCompile("(?m)" + expr)).endsText {
			t.Errorf("%s: the end of the text was not seen", expr)
		}
	}
}

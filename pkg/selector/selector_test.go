package selector

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/pkg/registry"
)

func buildRegistry(t *testing.T, defs map[string]string) *registry.Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for id, body := range defs {
		fsys["parsers/"+id+"/parser.yaml"] = &fstest.MapFile{Data: []byte(body)}
	}
	reg, err := registry.Load(registry.Source{Name: "test", FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Problems) > 0 {
		t.Fatal(reg.Problems)
	}
	return reg
}

func def(command, variant, detect string) string {
	return "format: 1\ncommand: " + command + "\nvariant: " + variant + "\n" + detect + "parse: {type: kv}\n"
}

// The fixture registry mixes variants of one command with definitions of
// other commands, which is what makes cross-command detection testable.
func testRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	return buildRegistry(t, map[string]string{
		"df/gnu":       def("df", "gnu", "detect:\n  os: [linux]\n  args: {none: ['-h']}\n  signature: {all: ['^Filesystem\\s+1K-blocks']}\n"),
		"df/gnu-human": def("df", "gnu-human", "detect:\n  os: [linux]\n  args: {any: ['-h']}\n  signature: {all: ['^Filesystem\\s+Size\\s+Used\\s+Avail\\s']}\n"),
		"df/bsd":       def("df", "bsd", "detect:\n  os: [darwin]\n  signature: {all: ['^Filesystem\\s+512-blocks']}\n"),
		"mount/linux":  def("mount", "linux", "detect:\n  os: [linux]\n  signature: {all: [' on .* type \\S+ \\(']}\n"),
		"env/posix":    def("env", "posix", ""),
	})
}

const (
	gnuDF   = "Filesystem     1K-blocks    Used Available Use% Mounted on\ntmpfs              1000       0      1000   0% /run\n"
	humanDF = "Filesystem      Size  Used Avail Use% Mounted on\ntmpfs           1.0G     0  1.0G   0% /run\n"
	bsdDF   = "Filesystem   512-blocks  Used Available Capacity iused ifree %iused Mounted on\n/dev/disk1s1  100 10 90 10% 1 2 0% /\n"
	mounted = "tmpfs on /run type tmpfs (rw,nosuid)\n"
)

func TestSelectAutomatic(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"one variant of one command", gnuDF, "df/gnu"},
		{"another variant of the same command", humanDF, "df/gnu-human"},
		{"a variant of a different OS", bsdDF, "df/bsd"},
		{"a different command entirely", mounted, "mount/linux"},
		{"CRLF and a BOM do not hide the header", "\xEF\xBB\xBFFilesystem     1K-blocks Used\r\ntmpfs 1 2\r\n", "df/gnu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Select(reg, Context{Input: []byte(tt.input)})
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if res.Entry.Def.ID() != tt.want {
				t.Errorf("selected %s, want %s", res.Entry.Def.ID(), tt.want)
			}
			if res.Scanned != 5 {
				t.Errorf("Scanned = %d, want 5", res.Scanned)
			}
		})
	}
}

func TestSelectNoMatch(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	for _, input := range []string{"", "   \n\t\n", "this is not a filesystem table\n", "\xff\xfe\x00binary\n", "Filesystem\n"} {
		_, err := Select(reg, Context{Input: []byte(input)})
		var nm *NoMatchError
		if !errors.As(err, &nm) {
			t.Fatalf("input %q: expected NoMatchError, got %v", input, err)
		}
		msg := err.Error()
		for _, want := range []string{"unable to identify the input format", "--parser"} {
			if !strings.Contains(msg, want) {
				t.Errorf("input %q: message missing %q:\n%s", input, want, msg)
			}
		}
		// The whole-registry message must stay short instead of dumping
		// one rejection per definition.
		if strings.Count(msg, "\n") > 6 {
			t.Errorf("message too long:\n%s", msg)
		}
		// Naming a parser that has a signature would be refused the same
		// way, so the way forward it offers is a definition that describes
		// a shape (env here, the one without a signature) or --define.
		if strings.Contains(msg, "--parser df") || !strings.Contains(msg, "--parser env") || !strings.Contains(msg, "--define") {
			t.Errorf("input %q: the message offers a way forward that does not lead anywhere:\n%s", input, msg)
		}
	}
}

// A command run with -z or --zero ends its records with NUL. Read line by
// line that is one line holding every record, which a signature matching
// the first one would let through; a format read line by line is out.
func TestTextWithNULIsNotReadLineByLine(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"sum/lines": def("sum", "lines", "detect: {signature: {all: ['^[0-9a-f]{4}  \\S']}}\n"),
	})
	in := []byte("abcd  a.txt\x00abcd  b.txt\x00")
	for name, ctx := range map[string]Context{
		"automatic": {Input: in},
		"scoped":    {Parser: "sum", Input: in},
		"named":     {Parser: "sum", Variant: "lines", Input: in},
	} {
		if _, err := Select(reg, ctx); err == nil || !strings.Contains(err.Error(), "NUL") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Select(reg, Context{Input: []byte("abcd  a.txt\nabcd  b.txt\n")}); err != nil {
		t.Errorf("the same records on lines: %v", err)
	}
}

func TestSelectAmbiguous(t *testing.T) {
	t.Parallel()
	// Two commands whose signatures both match the same text.
	reg := buildRegistry(t, map[string]string{
		"alpha/default": def("alpha", "default", "detect: {signature: {all: ['^HEADER']}}\n"),
		"beta/default":  def("beta", "default", "detect: {signature: {all: ['HEADER']}}\n"),
	})
	_, err := Select(reg, Context{Input: []byte("HEADER x\n")})
	var am *AmbiguousError
	if !errors.As(err, &am) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"input matches multiple parsers", "alpha/default", "beta/default", "--parser alpha"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	// Two variants of the same command: the message talks about variants.
	reg = buildRegistry(t, map[string]string{
		"df/a": def("df", "a", "detect: {signature: {all: ['^HEADER']}}\n"),
		"df/b": def("df", "b", "detect: {signature: {all: ['HEADER']}}\n"),
	})
	_, err = Select(reg, Context{Input: []byte("HEADER x\n")})
	if !errors.As(err, &am) || !strings.Contains(err.Error(), "multiple df variants") || !strings.Contains(err.Error(), "--variant a") {
		t.Errorf("same-command ambiguity: %v", err)
	}
	// Naming the parser does not help when both variants match; naming
	// the variant does.
	if _, err := Select(reg, Context{Parser: "df", Input: []byte("HEADER x\n")}); !errors.As(err, &am) {
		t.Errorf("--parser alone should stay ambiguous: %v", err)
	}
	res, err := Select(reg, Context{Parser: "df", Variant: "b", Input: []byte("HEADER x\n")})
	if err != nil || res.Entry.Def.Variant != "b" {
		t.Errorf("explicit variant: %v %v", res, err)
	}
}

func TestPriorityBreaksTiesWithinOneCommandOnly(t *testing.T) {
	t.Parallel()
	same := buildRegistry(t, map[string]string{
		"df/general":  def("df", "general", "detect: {signature: {all: ['HEADER']}}\n"),
		"df/specific": def("df", "specific", "detect: {priority: 10, signature: {all: ['HEADER']}}\n"),
	})
	res, err := Select(same, Context{Input: []byte("HEADER\n")})
	if err != nil || res.Entry.Def.Variant != "specific" {
		t.Errorf("priority within a command: %v %v", res, err)
	}
	// Equal priorities stay ambiguous.
	equal := buildRegistry(t, map[string]string{
		"df/a": def("df", "a", "detect: {priority: 5, signature: {all: ['HEADER']}}\n"),
		"df/b": def("df", "b", "detect: {priority: 5, signature: {all: ['HEADER']}}\n"),
	})
	if _, err := Select(equal, Context{Input: []byte("HEADER\n")}); err == nil {
		t.Error("equal priorities must stay ambiguous")
	}
	// Priority never ranks different commands against each other.
	cross := buildRegistry(t, map[string]string{
		"alpha/x": def("alpha", "x", "detect: {priority: 10, signature: {all: ['HEADER']}}\n"),
		"beta/y":  def("beta", "y", "detect: {signature: {all: ['HEADER']}}\n"),
	})
	var am *AmbiguousError
	if _, err := Select(cross, Context{Input: []byte("HEADER\n")}); !errors.As(err, &am) {
		t.Errorf("cross-command priority must not decide: %v", err)
	}
}

func TestExplicitOnlyDefinitions(t *testing.T) {
	t.Parallel()
	// A format too generic to claim: three numbers and a path also
	// describe `git diff --numstat`, so du is only used when named.
	reg := buildRegistry(t, map[string]string{
		"du/posix": def("du", "posix", "detect:\n  auto_detect: false\n  signature: {all: ['\\A\\s*\\d+\\t']}\n"),
		"df/gnu":   def("df", "gnu", "detect: {signature: {all: ['^Filesystem\\s+1K-blocks']}}\n"),
	})
	_, err := Select(reg, Context{Input: []byte("10\t2\tmain.go\n")})
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("expected NoMatchError, got %v", err)
	}
	if len(nm.Hints) != 1 || nm.Hints[0].Def.Command != "du" {
		t.Errorf("hints = %v", nm.Hints)
	}
	msg := err.Error()
	for _, want := range []string{"could be `du` output", "too generic", "jz --parser du"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	// Naming the parser makes it usable, and the signature is still checked.
	res, err := Select(reg, Context{Parser: "du", Input: []byte("10\tmain.go\n")})
	if err != nil || res.Entry.Def.ID() != "du/posix" {
		t.Fatalf("--parser du: %v %v", res, err)
	}
	if _, err := Select(reg, Context{Parser: "du", Input: []byte("not du output\n")}); err == nil ||
		!strings.Contains(err.Error(), "signature.all[0]") {
		t.Errorf("signature is still verified: %v", err)
	}
	// jz run knows the command, which counts as naming the parser.
	if _, err := Select(reg, Context{Parser: "du", Args: []string{"-s", "."}, Input: []byte("10\tmain.go\n")}); err != nil {
		t.Errorf("run mode: %v", err)
	}
	// A definition that is auto-detectable is unaffected.
	if _, err := Select(reg, Context{Input: []byte("Filesystem     1K-blocks Used\n")}); err != nil {
		t.Errorf("auto-detectable definition: %v", err)
	}
}

func TestDefinitionWithoutSignature(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	// env/posix has no signature: it is invisible to automatic detection.
	_, err := Select(reg, Context{Input: []byte("HOME=/root\nSHELL=/bin/sh\n")})
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("expected NoMatchError, got %v", err)
	}
	// Naming the parser makes it usable, and the rejection reason says so.
	res, err := Select(reg, Context{Parser: "env", Input: []byte("HOME=/root\n")})
	if err != nil || res.Entry.Def.ID() != "env/posix" {
		t.Fatalf("--parser env: %v %v", res, err)
	}
	// Within a parser scope a signature-less variant is a candidate, so it
	// becomes ambiguous as soon as a sibling also matches. That is why
	// every variant of a multi-variant command should carry a signature.
	reg2 := buildRegistry(t, map[string]string{
		"env/posix": def("env", "posix", ""),
		"env/other": def("env", "other", "detect: {signature: {all: ['^NOPE']}}\n"),
	})
	res, err = Select(reg2, Context{Parser: "env", Input: []byte("x\n")})
	if err != nil || res.Entry.Def.Variant != "posix" {
		t.Errorf("the only candidate wins: %v %v", res, err)
	}
	var am *AmbiguousError
	if _, err := Select(reg2, Context{Parser: "env", Input: []byte("NOPE\n")}); !errors.As(err, &am) {
		t.Errorf("a matching sibling makes it ambiguous: %v", err)
	}
	// Per-variant rejection reasons are listed when the scope is one parser.
	reg3 := buildRegistry(t, map[string]string{
		"env/one": def("env", "one", "detect: {signature: {all: ['^NOPE']}}\n"),
		"env/two": def("env", "two", "detect: {signature: {all: ['^ALSO NOPE']}}\n"),
	})
	_, err = Select(reg3, Context{Parser: "env", Input: []byte("x\n")})
	if err == nil || !strings.Contains(err.Error(), "signature.all[0]") || !strings.Contains(err.Error(), "no env variant matches") {
		t.Errorf("per-variant rejections: %v", err)
	}
}

// A signature that says every line has one shape (\z is the end of the
// window) held a blank line at the end of the input against the text,
// though the parser skips it: `cat /proc/meminfo; echo` was unidentified.
// A blank line inside the text is still part of it.
func TestTrailingBlankLinesAreNotPartOfTheText(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"n/digits": def("n", "digits", "detect:\n  signature: {all: ['\\A(?:\\d+\\n)*\\d+\\z']}\n"),
	})
	for _, in := range []string{"1\n2\n", "1\n2", "1\n2\n\n", "1\n2\n  \n\n", "1\r\n2\r\n\r\n"} {
		if res, err := Select(reg, Context{Input: []byte(in)}); err != nil || res.Entry.Def.ID() != "n/digits" {
			t.Errorf("%q: %v", in, err)
		}
	}
	for _, in := range []string{"1\n\n2\n", "\n1\n2\n"} {
		if _, err := Select(reg, Context{Input: []byte(in)}); err == nil {
			t.Errorf("%q: a blank line inside the text was dropped", in)
		}
	}
}

// The candidates for a command that printed nothing are the ones its
// system and arguments admit, with no text to look at.
func TestCandidates(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	ids := func(ctx Context) string {
		entries := Candidates(reg, ctx)
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Def.ID())
		}
		return strings.Join(out, " ")
	}
	for _, tt := range []struct {
		ctx  Context
		want string
	}{
		{Context{Parser: "df"}, "df/bsd df/gnu df/gnu-human"},
		{Context{Parser: "df", OS: "linux", Args: []string{}}, "df/gnu"},
		{Context{Parser: "df", OS: "linux", Args: []string{"-h"}}, "df/gnu-human"},
		{Context{Parser: "df", OS: "darwin", Args: []string{"-h"}}, "df/bsd"},
		{Context{Parser: "df", Variant: "gnu-human", OS: "linux", Args: []string{}}, ""},
		{Context{Parser: "df", Variant: "nope"}, ""},
		{Context{Parser: "nope"}, ""},
	} {
		if got := ids(tt.ctx); got != tt.want {
			t.Errorf("Candidates(%+v) = %q, want %q", tt.ctx, got, tt.want)
		}
	}
}

func TestSelectWithParserScope(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	res, err := Select(reg, Context{Parser: "df", Input: []byte(gnuDF)})
	if err != nil || res.Entry.Def.ID() != "df/gnu" || res.Scanned != 3 {
		t.Errorf("scoped select: %v %v", res, err)
	}
	// A parser scope does not excuse a signature mismatch, and naming a
	// variant would not either: a named variant is checked the same way,
	// so the message must not send the reader there.
	_, err = Select(reg, Context{Parser: "df", Input: []byte(mounted)})
	var nm *NoMatchError
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "no df variant matches") {
		t.Errorf("scoped mismatch: %v", err)
	}
	if strings.Contains(err.Error(), "--variant") || !strings.Contains(err.Error(), "jz list df") {
		t.Errorf("scoped hint for text no variant fits: %v", err)
	}
	// A variant whose signature fits and that only lacked an argument it
	// asks for is the one to name: a pipe carries no arguments.
	_, err = Select(reg, Context{Parser: "df", OS: "linux", Args: []string{}, Input: []byte(humanDF)})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "--variant gnu-human") || strings.Contains(err.Error(), "--variant bsd") {
		t.Errorf("scoped hint for a variant the arguments ruled out: %v", err)
	}
	// A variant that lists the argument as one whose output it does not
	// read has said so; offering it would send the reader back to it, and
	// saying no signature fits would be untrue.
	_, err = Select(reg, Context{Parser: "df", OS: "linux", Args: []string{"-hT"}, Input: []byte(gnuDF)})
	if !errors.As(err, &nm) || strings.Contains(err.Error(), "--variant") || !strings.Contains(err.Error(), "does not read the output of") {
		t.Errorf("scoped hint for a variant that excludes the argument: %v", err)
	}
}

func TestExplicitVariantIsAlwaysVerified(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	// Naming a variant is a claim about the text, not permission to
	// ignore it: there is no way to parse with a definition whose
	// signature says the input is something else.
	_, err := Select(reg, Context{Parser: "df", Variant: "bsd", Input: []byte(gnuDF)})
	var me *MismatchError
	if !errors.As(err, &me) {
		t.Fatalf("expected MismatchError, got %v", err)
	}
	if !strings.Contains(err.Error(), "df/bsd does not describe this input") {
		t.Errorf("message: %v", err)
	}
	// A variant whose OS does not match is a mismatch too.
	_, err = Select(reg, Context{Parser: "df", Variant: "bsd", OS: "linux", Input: []byte(bsdDF)})
	if !errors.As(err, &me) || !strings.Contains(err.Error(), "written for darwin, not linux") {
		t.Errorf("os mismatch: %v", err)
	}
	// The matching variant is accepted.
	res, err := Select(reg, Context{Parser: "df", Variant: "gnu", Input: []byte(gnuDF)})
	if err != nil || res.Entry.Def.ID() != "df/gnu" {
		t.Errorf("matching variant: %v %v", res, err)
	}
}

func TestSelectErrorsForUnknownNames(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	var up *UnknownParserError
	_, err := Select(reg, Context{Parser: "dg", Input: []byte(gnuDF)})
	if !errors.As(err, &up) || !strings.Contains(err.Error(), "did you mean df") {
		t.Errorf("unknown parser: %v", err)
	}
	_, err = Select(reg, Context{Parser: "zzzzzz", Input: nil})
	if !errors.As(err, &up) || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("no suggestion expected: %v", err)
	}
	var uv *UnknownVariantError
	_, err = Select(reg, Context{Parser: "df", Variant: "solaris", Input: []byte(gnuDF)})
	if !errors.As(err, &uv) || !strings.Contains(err.Error(), "available: bsd, gnu, gnu-human") {
		t.Errorf("unknown variant: %v", err)
	}
	var vp *VariantWithoutParserError
	_, err = Select(reg, Context{Variant: "gnu", Input: []byte(gnuDF)})
	if !errors.As(err, &vp) || !strings.Contains(err.Error(), "needs --parser") {
		t.Errorf("variant without parser: %v", err)
	}
}

func TestOSAndArgsAreHardFilters(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	// The OS hint removes a candidate that would otherwise match.
	_, err := Select(reg, Context{OS: "darwin", Input: []byte(gnuDF)})
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Errorf("os filter: %v", err)
	}
	res, err := Select(reg, Context{OS: "linux", Input: []byte(gnuDF)})
	if err != nil || res.Entry.Def.ID() != "df/gnu" {
		t.Errorf("matching os: %v %v", res, err)
	}
	// Arguments are known only when jz ran the command itself.
	_, err = Select(reg, Context{Parser: "df", OS: "linux", Args: []string{"-hT"}, Input: []byte(gnuDF)})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "excluded by the argument -h") {
		t.Errorf("args none: %v", err)
	}
	res, err = Select(reg, Context{Parser: "df", OS: "linux", Args: []string{"-hP"}, Input: []byte(humanDF)})
	if err != nil || res.Entry.Def.Variant != "gnu-human" {
		t.Errorf("bundled flag: %v %v", res, err)
	}
	_, err = Select(reg, Context{Parser: "df", OS: "linux", Args: []string{}, Input: []byte(humanDF)})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "needs one of the arguments [-h]") {
		t.Errorf("args any: %v", err)
	}
	all := buildRegistry(t, map[string]string{
		"z/a": def("z", "a", "detect: {args: {all: ['-a', '-b']}, signature: {all: ['ok']}}\n"),
	})
	_, err = Select(all, Context{Parser: "z", Args: []string{"-a"}, Input: []byte("ok\n")})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "needs the argument -b") {
		t.Errorf("args all: %v", err)
	}
}

func TestSignatureAnyNoneAndWindow(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"z/a": def("z", "a", "detect: {signature: {any: ['^A', '^B'], none: ['FORBIDDEN'], window: 2}}\n"),
	})
	if _, err := Select(reg, Context{Input: []byte("A\n")}); err != nil {
		t.Errorf("any: %v", err)
	}
	if _, err := Select(reg, Context{Input: []byte("C\n")}); err == nil || !strings.Contains(err.Error(), "unable to identify") {
		t.Errorf("any mismatch: %v", err)
	}
	if _, err := Select(reg, Context{Parser: "z", Input: []byte("C\n")}); err == nil || !strings.Contains(err.Error(), "no signature.any[] expression matched") {
		t.Errorf("any mismatch reason: %v", err)
	}
	if _, err := Select(reg, Context{Parser: "z", Input: []byte("A\nFORBIDDEN\n")}); err == nil || !strings.Contains(err.Error(), "none[0]") {
		t.Errorf("none: %v", err)
	}
	// The window bounds how far a signature can look.
	if _, err := Select(reg, Context{Input: []byte("A\nx\nFORBIDDEN\n")}); err != nil {
		t.Errorf("window: %v", err)
	}
}

func TestLeadingNoiseAndHeaderPosition(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	// A warning line before the header does not hide it.
	noisy := "df: /run/user/1000/gvfs: Permission denied\n" + gnuDF
	res, err := Select(reg, Context{Input: []byte(noisy)})
	if err != nil || res.Entry.Def.ID() != "df/gnu" {
		t.Errorf("leading warning: %v %v", res, err)
	}
	// A header further down is still inside the default window.
	buried := strings.Repeat("warning\n", 10) + gnuDF
	if _, err := Select(reg, Context{Input: []byte(buried)}); err != nil {
		t.Errorf("buried header: %v", err)
	}
	// Beyond the window it is not, and that is a refusal rather than a guess.
	tooFar := strings.Repeat("warning\n", 60) + gnuDF
	if _, err := Select(reg, Context{Input: []byte(tooFar)}); err == nil {
		t.Error("a header beyond the window must not be found")
	}
	// Case matters: signatures are literal about the header text.
	if _, err := Select(reg, Context{Input: []byte(strings.ToUpper(gnuDF))}); err == nil {
		t.Error("upper-cased header must not match")
	}
}

// layered builds a registry from several sources, most preferred first.
// Each map is one registry: id -> definition body.
func layered(t *testing.T, sources ...map[string]string) *registry.Registry {
	t.Helper()
	srcs := make([]registry.Source, 0, len(sources))
	for i, defs := range sources {
		fsys := fstest.MapFS{}
		for id, body := range defs {
			fsys["parsers/"+id+"/parser.yaml"] = &fstest.MapFile{Data: []byte(body)}
		}
		srcs = append(srcs, registry.Source{Name: fmt.Sprintf("source%d", i), FS: fsys})
	}
	reg, err := registry.Load(srcs...)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Problems) > 0 {
		t.Fatal(reg.Problems)
	}
	return reg
}

// A definition someone adds locally must not be able to turn an official
// parser into an ambiguity. The layering already says whose definitions
// apply, so it decides here too.
func TestSourcePrecedenceSettlesCollisions(t *testing.T) {
	t.Parallel()
	const (
		wide     = "detect: {signature: {all: ['^Filesystem']}}\n"
		narrow   = "detect: {signature: {all: ['^Filesystem\\s+1K-blocks']}}\n"
		ranked   = "detect: {priority: 5, signature: {all: ['^Filesystem']}}\n"
		unranked = "detect: {signature: {all: ['^Filesystem']}}\n"
	)
	tests := []struct {
		name    string
		sources []map[string]string
		want    string // selected id, empty when the selection must fail
	}{
		{
			name: "a user definition wins over an embedded one",
			sources: []map[string]string{
				{"greet/x": def("greet", "x", wide)},
				{"df/gnu": def("df", "gnu", narrow)},
			},
			want: "greet/x",
		},
		{
			name: "the embedded definition applies when nothing shadows it",
			sources: []map[string]string{
				{"greet/x": def("greet", "x", "detect: {signature: {all: ['^NEVER']}}\n")},
				{"df/gnu": def("df", "gnu", narrow)},
			},
			want: "df/gnu",
		},
		{
			name: "two definitions of one registry stay ambiguous",
			sources: []map[string]string{
				{"greet/x": def("greet", "x", wide), "hail/y": def("hail", "y", wide)},
				{"df/gnu": def("df", "gnu", narrow)},
			},
		},
		{
			name: "two embedded definitions stay ambiguous",
			sources: []map[string]string{
				{"df/gnu": def("df", "gnu", narrow), "df/other": def("df", "other", wide)},
			},
		},
		{
			name: "priority still decides inside the preferred registry",
			sources: []map[string]string{
				{"df/general": def("df", "general", unranked), "df/specific": def("df", "specific", ranked)},
				{"df/gnu": def("df", "gnu", narrow)},
			},
			want: "df/specific",
		},
		{
			name: "priority does not reach across registries",
			sources: []map[string]string{
				{"greet/x": def("greet", "x", unranked)},
				{"df/gnu": def("df", "gnu", ranked)},
			},
			want: "greet/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := layered(t, tt.sources...)
			res, err := Select(reg, Context{Input: []byte(gnuDF)})
			if tt.want == "" {
				var am *AmbiguousError
				if !errors.As(err, &am) {
					t.Fatalf("expected AmbiguousError, got %v (%v)", err, res)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := res.Entry.Def.ID(); got != tt.want {
				t.Errorf("selected %s, want %s", got, tt.want)
			}
		})
	}
	// Naming the parser still reaches the definition the layering hid.
	reg := layered(t,
		map[string]string{"greet/x": def("greet", "x", wide)},
		map[string]string{"df/gnu": def("df", "gnu", narrow)},
	)
	res, err := Select(reg, Context{Parser: "df", Input: []byte(gnuDF)})
	if err != nil || res.Entry.Def.ID() != "df/gnu" {
		t.Errorf("--parser df: %v %v", res, err)
	}
}

func TestShadowingChangesTheCandidateSet(t *testing.T) {
	t.Parallel()
	lower := fstest.MapFS{
		"parsers/df/gnu/parser.yaml": &fstest.MapFile{Data: []byte(def("df", "gnu", "detect: {signature: {all: ['^Filesystem\\s+1K-blocks']}}\n"))},
	}
	upper := fstest.MapFS{
		"parsers/df/gnu/parser.yaml": &fstest.MapFile{Data: []byte(def("df", "gnu", "detect: {signature: {all: ['^NEVER MATCHES']}}\n"))},
	}
	reg, err := registry.Load(registry.Source{Name: "user", FS: upper}, registry.Source{Name: "embedded", FS: lower})
	if err != nil {
		t.Fatal(err)
	}
	// The shadowing definition decides detection, so the input no longer
	// matches even though the embedded one would have accepted it.
	if _, err := Select(reg, Context{Input: []byte(gnuDF)}); err == nil {
		t.Error("the shadowing definition must decide")
	}
	// The shadowing definition is the one a name resolves to, and it is
	// checked like any other.
	if _, err := Select(reg, Context{Parser: "df", Variant: "gnu", Input: []byte(gnuDF)}); err == nil {
		t.Error("the shadowing definition decides, and it does not match")
	}
	res, err := Select(reg, Context{Parser: "df", Variant: "gnu", Input: []byte("NEVER MATCHES\n")})
	if err != nil || res.Entry.Source != "user" {
		t.Errorf("shadowed entry: %v %v", res, err)
	}
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()
	if levenshtein("kitten", "sitting") != 3 || levenshtein("", "abc") != 3 || levenshtein("same", "same") != 0 {
		t.Error("levenshtein")
	}
	// Four names are one edit away, and only three are offered.
	if s := suggest("dx", []string{"da", "db", "dc", "dd", "zzz"}); len(s) != 3 {
		t.Errorf("suggest cap: %v", s)
	}
}

// TestSuggestOffersOnlyTheClosest pins that a wrong name is answered with
// the commands nearest to it and no others. Every command within two
// edits used to be offered, which in a registry of a few hundred means
// the one the caller meant arrives in a crowd, and the alphabetical cap
// of three could drop it entirely.
func TestSuggestOffersOnlyTheClosest(t *testing.T) {
	t.Parallel()
	known := []string{"ar", "df", "dig", "du", "git", "ls", "lscpu", "nm"}
	for _, tt := range []struct {
		name string
		want []string
	}{
		// ar is two edits away and df, dig and du are one, so ar is not
		// offered even though it would fit under the old bound.
		{"dg", []string{"df", "dig", "du"}},
		// A name that extends a command counts as closer than any edit.
		{"dff", []string{"df"}},
		{"lscpuu", []string{"lscpu"}},
		// Nothing near enough is no suggestion rather than a bad one.
		{"kubectl", nil},
	} {
		got := suggest(tt.name, known)
		if len(got) != len(tt.want) {
			t.Errorf("suggest(%q) = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("suggest(%q) = %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

// A selection that settles between several definitions says which rule
// did it and what each loser fit with, which is what --explain reports;
// one that cannot settle says why not.
func TestSelectionRecordsHowItWasSettled(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"df/general":  def("df", "general", "detect: {signature: {all: ['HEADER']}}\n"),
		"df/specific": def("df", "specific", "detect: {priority: 10, signature: {all: ['HEADER']}}\n"),
		"du/posix":    def("du", "posix", "detect: {auto_detect: false, signature: {all: ['HEADER']}}\n"),
		"env/posix":   def("env", "posix", ""),
	})
	res, err := Select(reg, Context{Input: []byte("HEADER\n")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Settled != SettledByPriority || len(res.Outranked) != 1 || res.Outranked[0].Entry.Def.ID() != "df/general" ||
		res.Outranked[0].Reason != "fits as well, with detect.priority 0 against 10" {
		t.Errorf("settled %q, outranked %+v", res.Settled, res.Outranked)
	}
	// du fits and is held back; env has no signature and says nothing.
	if res.ExplicitOnly != 2 {
		t.Errorf("explicit only = %d", res.ExplicitOnly)
	}
	held := 0
	for _, r := range res.Rejections {
		if r.ExplicitOnly {
			held++
			if r.Entry.Def.ID() != "du/posix" || !r.Close {
				t.Errorf("held back: %+v", r)
			}
		}
	}
	if held != 1 {
		t.Errorf("reported %d held back, want the one whose signature fits", held)
	}
	// Only one fit: nothing was settled.
	res, err = Select(reg, Context{Parser: "df", Variant: "general", Input: []byte("HEADER\n")})
	if err != nil || res.Settled != "" || res.Outranked != nil {
		t.Errorf("named: %+v %v", res, err)
	}

	cross := buildRegistry(t, map[string]string{
		"alpha/x": def("alpha", "x", "detect: {signature: {all: ['HEADER']}}\n"),
		"beta/y":  def("beta", "y", "detect: {signature: {all: ['HEADER']}}\n"),
	})
	_, err = Select(cross, Context{Input: []byte("HEADER\n")})
	var am *AmbiguousError
	if !errors.As(err, &am) || am.Scanned != 2 || am.Unsettled != "they are different commands, and detect.priority only ranks variants of one" {
		t.Errorf("ambiguous: %+v", am)
	}
	same := buildRegistry(t, map[string]string{
		"df/a": def("df", "a", "detect: {signature: {all: ['HEADER']}}\n"),
		"df/b": def("df", "b", "detect: {signature: {all: ['HEADER']}}\n"),
	})
	if _, err := Select(same, Context{Input: []byte("HEADER\n")}); !errors.As(err, &am) || am.Unsettled != "no detect.priority among them is strictly highest" {
		t.Errorf("same command: %v", err)
	}
}

// The argument filter a definition met is named, so an explanation says
// which of its arguments decided.
func TestMatchedNamesTheArgumentFilter(t *testing.T) {
	t.Parallel()
	res, err := Select(testRegistry(t), Context{Parser: "df", OS: "linux", Args: []string{"-hT"}, Input: []byte(humanDF)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Matched, "; "); !strings.HasSuffix(got, "detect.os linux; detect.args any [-h]") {
		t.Errorf("matched = %s", got)
	}
}

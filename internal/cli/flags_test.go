package cli

import (
	"io"
	"strings"
	"testing"
)

// untouchedInput fails the test that reads it: an option pair refused
// before anything is read must not consume the input first.
type untouchedInput struct{ t *testing.T }

func (u untouchedInput) Read([]byte) (int, error) {
	u.t.Error("the input was read")
	return 0, io.EOF
}

// --yaml was an output option. jz writes JSON only, so a command line
// that still asks for YAML is refused with what to do instead, before
// the input is read or a command is started, in every mode that had it.
func TestYAMLOutputIsRemoved(t *testing.T) {
	for _, args := range [][]string{
		{"--yaml"},
		{"--yaml=true"},
		{"-yaml", "--parser", "df"},
		{"--stream", "--yaml"},
		{"run", "--yaml", "jz-no-such-command"},
		{"new", "--yaml", "a=1"},
	} {
		h := newHarness(t)
		h.env.Stdin = untouchedInput{t}
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%v: code=%d stderr=%s", args, code, h.stderr.String())
		}
		if h.stdout.Len() != 0 {
			t.Errorf("%v: stdout=%q", args, h.stdout.String())
		}
		if first, _, _ := strings.Cut(h.stderr.String(), "\n"); first != "jz: --yaml was removed: jz writes JSON only; pipe the JSON to a YAML tool to get YAML (for example: jz ... | yq -P)" {
			t.Errorf("%v: stderr=%q", args, h.stderr.String())
		}
	}
	// An option that never existed keeps the ordinary message.
	h := newHarness(t)
	if code := h.run("--yml"); code != ExitUsage || !strings.Contains(h.stderr.String(), "jz: unknown option --yml\n") {
		t.Errorf("--yml: code=%d stderr=%s", code, h.stderr.String())
	}
}

// YAML is still read: a data file, piped YAML and a definition written in
// YAML all turn into JSON.
func TestYAMLInputStays(t *testing.T) {
	h := newHarness(t)
	if code := h.pipe("name: api\nreplicas: 3\n", "--format", "yaml"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `{"name":"api","replicas":3}` {
		t.Errorf("--format yaml: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	spec := h.writeFile("spec.yml", []byte("a: [1, \"2\"]\n"))
	if code := h.run("new", "spec:=@"+spec); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `{"spec":{"a":[1,"2"]}}` {
		t.Errorf("new spec:=@spec.yml: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.pipe("a=1\n", "--define", "parse: {type: kv, separator: \"=\"}"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != `[{"name":"a","value":"1"}]` {
		t.Errorf("--define: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
}

// An option is refused in the form jz documents it, with two dashes for a
// long name and one for a letter, and a value it cannot take says what it
// takes. A duration is not negative: no limit is 0.
func TestOptionErrorsNameTheOption(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--nope"}, "jz: unknown option --nope\n"},
		{[]string{"-nope"}, "jz: unknown option --nope\n"},
		{[]string{"-x"}, "jz: unknown option -x\n"},
		{[]string{"--nope=1"}, "jz: unknown option --nope\n"},
		{[]string{"--pretty=maybe"}, `jz: --pretty takes no value, got "maybe"` + "\n"},
		{[]string{"-p=maybe"}, `jz: -p takes no value, got "maybe"` + "\n"},
		{[]string{"--explain=yaml"}, `jz: --explain takes no value, or =json for one JSON document; got "yaml"` + "\n"},
		{[]string{"--parser"}, "jz: --parser needs a value\n"},
		{[]string{"run", "--timeout", "soon", "df"}, `jz: --timeout expects a duration such as 30s or 2m, got "soon"` + "\n"},
		{[]string{"run", "--timeout", "-1s", "df"}, `jz: --timeout expects a duration such as 30s or 2m, got "-1s"; 0 is no limit` + "\n"},
		{[]string{"new", "--nope", "a=1"}, "jz: unknown option --nope\n"},
		{[]string{"list", "--os", "linux"}, "jz: unknown option --os\n"},
	} {
		h := newHarness(t)
		h.env.Stdin = untouchedInput{t}
		if code := h.run(tt.args...); code != ExitUsage {
			t.Errorf("%v: code=%d stderr=%s", tt.args, code, h.stderr.String())
		}
		if first, _, _ := strings.Cut(h.stderr.String(), "\n"); first+"\n" != tt.want {
			t.Errorf("%v: stderr starts %q, want %q", tt.args, first, tt.want)
		}
	}
}

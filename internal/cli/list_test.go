package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// jz list describes every definition it has, as text and as JSON, and
// hands out every schema: whatever parse type, field rule or detection
// criterion a definition uses, the listing has words for it. The test runs
// over the whole embedded registry, which is where those combinations are,
// and loads it once rather than once for every definition.
func TestListDescribesEveryDefinition(t *testing.T) {
	h := newHarness(t)
	a := &app{env: h.env}
	reg, code := a.loadRegistry()
	if code != ExitOK {
		t.Fatalf("loading the registry: %d %s", code, h.stderr.String())
	}
	if code := a.listCommands(reg, true); code != ExitOK || !json.Valid(h.stdout.Bytes()) {
		t.Fatalf("list --json: %d %s", code, h.stderr.String())
	}
	for _, command := range reg.Commands() {
		h.stdout.Reset()
		if code := a.listVariants(reg, command, false); code != ExitOK || !strings.Contains(h.stdout.String(), command) {
			t.Errorf("list %s: %d %s", command, code, h.stderr.String())
		}
		h.stdout.Reset()
		if code := a.listVariants(reg, command, true); code != ExitOK || !json.Valid(h.stdout.Bytes()) {
			t.Errorf("list --json %s: %d %s", command, code, h.stderr.String())
		}
	}
	for _, e := range reg.Entries() {
		d := e.Def
		h.stdout.Reset()
		if code := a.listDefinition(reg, d.Command, d.Variant, false); code != ExitOK || !strings.Contains(h.stdout.String(), "  parse:        ") {
			t.Errorf("list %s: %d\n%s%s", d.ID(), code, h.stdout.String(), h.stderr.String())
		}
		h.stdout.Reset()
		if code := a.listDefinition(reg, d.Command, d.Variant, true); code != ExitOK || !json.Valid(h.stdout.Bytes()) {
			t.Errorf("list --json %s: %d %s", d.ID(), code, h.stderr.String())
		}
		h.stdout.Reset()
		if code := a.listSchema(reg, d.Command, d.Variant); code != ExitOK || !json.Valid(h.stdout.Bytes()) {
			t.Errorf("list --schema %s: %d %s", d.ID(), code, h.stderr.String())
		}
	}
}

// A command jz has no parser for is named in the refusal, a variant it
// does not have lists the ones it does, and the name a program is run
// under is taken the way jz run takes it.
func TestListNamesWhatItHasNoParserFor(t *testing.T) {
	h := newHarness(t)
	if code := h.run("list", "no-such-command", "v"); code != ExitSelect || !strings.Contains(h.stderr.String(), `no parser for "no-such-command"`) {
		t.Errorf("unknown command: %d %s", code, h.stderr.String())
	}
	if code := h.run("list", "df", "no-such-variant"); code != ExitSelect || !strings.Contains(h.stderr.String(), "available: ") {
		t.Errorf("unknown variant: %d %s", code, h.stderr.String())
	}
	if code := h.run("list", "/usr/bin/df", "gnu"); code != ExitOK || !strings.Contains(h.stdout.String(), "df/gnu") {
		t.Errorf("a path: %d %s", code, h.stdout.String())
	}
	// A misspelt name gets the suggestion jz run and --parser give it.
	for _, args := range [][]string{{"list", "dff"}, {"list", "dff", "gnu"}} {
		if code := h.run(args...); code != ExitSelect || !strings.Contains(h.stderr.String(), "did you mean df?") {
			t.Errorf("%v: %d %s", args, code, h.stderr.String())
		}
	}
}

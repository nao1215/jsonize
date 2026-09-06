package selector

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nao1215/jsonize/internal/registry"
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

func dfReg(t *testing.T) *registry.Registry {
	t.Helper()
	return buildRegistry(t, map[string]string{
		"df/gnu":           "format: 1\ncommand: df\nvariant: gnu\ndetect:\n  os: [linux]\n  args: {none: ['-h']}\n  signature: {all: ['^Filesystem\\s+1K-blocks']}\nparse: {type: kv}\n",
		"df/gnu-human":     "format: 1\ncommand: df\nvariant: gnu-human\ndetect:\n  os: [linux]\n  args: {any: ['-h', '--human-readable']}\n  signature: {all: ['^Filesystem\\s+Size\\s+Used\\s+Avail\\s']}\nparse: {type: kv}\n",
		"df/bsd":           "format: 1\ncommand: df\nvariant: bsd\ndetect:\n  os: [darwin, freebsd]\n  signature: {all: ['^Filesystem\\s+512-blocks']}\nparse: {type: kv}\n",
		"df/busybox-human": "format: 1\ncommand: df\nvariant: busybox-human\ndetect:\n  os: [linux]\n  args: {any: ['-h']}\n  signature: {all: ['^Filesystem\\s+Size\\s+Used\\s+Available\\s']}\nparse: {type: kv}\n",
	})
}

func TestSelectBySignatureInPipeMode(t *testing.T) {
	t.Parallel()
	reg := dfReg(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"gnu", "Filesystem     1K-blocks Used Available Use% Mounted on\n", "gnu"},
		{"gnu -h", "Filesystem  Size  Used Avail Use% Mounted on\n", "gnu-human"},
		{"bsd", "Filesystem  512-blocks Used Available Capacity iused ifree %iused Mounted on\n", "bsd"},
		{"busybox -h", "Filesystem                Size      Used Available Use% Mounted on\n", "busybox-human"},
		{"crlf+bom", "\xEF\xBB\xBFFilesystem     1K-blocks Used\r\n", "gnu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Select(reg, Context{Command: "df", Input: []byte(tt.input)})
			if err != nil {
				t.Fatal(err)
			}
			if res.Entry.Def.Variant != tt.want {
				t.Errorf("selected %s, want %s", res.Entry.Def.Variant, tt.want)
			}
			if len(res.Evaluations) != 4 {
				t.Errorf("evaluations = %d", len(res.Evaluations))
			}
		})
	}
}

func TestSelectRejections(t *testing.T) {
	t.Parallel()
	reg := dfReg(t)
	// OS contradicts the signature: gnu needs linux.
	_, err := Select(reg, Context{Command: "df", OS: "darwin", Input: []byte("Filesystem 1K-blocks Used\n")})
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("expected NoMatchError, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"gnu: os darwin is not one of [linux]", "bsd: os darwin matched; signature all[0]", "--variant"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	// exec mode: args exclude gnu when -h is given even if signature matched.
	_, err = Select(reg, Context{Command: "df", OS: "linux", Args: []string{"-hT"}, Input: []byte("Filesystem 1K-blocks Used\n")})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "argument -h excludes this variant") {
		t.Errorf("args none: %v", err)
	}
	// exec mode: gnu-human requires -h.
	_, err = Select(reg, Context{Command: "df", OS: "linux", Args: []string{}, Input: []byte("Filesystem Size Used Avail Use% Mounted on\n")})
	if !errors.As(err, &nm) || !strings.Contains(err.Error(), "none of the arguments [-h, --human-readable] were given") {
		t.Errorf("args any: %v", err)
	}
	// exec mode success with bundled flag.
	res, err := Select(reg, Context{Command: "df", OS: "linux", Args: []string{"-hP"}, Input: []byte("Filesystem Size Used Avail Use% Mounted on\n")})
	if err != nil || res.Entry.Def.Variant != "gnu-human" {
		t.Errorf("bundled flag: %v %v", res, err)
	}
	// empty input: signatures fail.
	if _, err := Select(reg, Context{Command: "df"}); !errors.As(err, &nm) {
		t.Errorf("empty input: %v", err)
	}
}

func TestSelectUnknownCommandAndVariant(t *testing.T) {
	t.Parallel()
	reg := dfReg(t)
	_, err := Select(reg, Context{Command: "dg"})
	var uc *UnknownCommandError
	if !errors.As(err, &uc) || !strings.Contains(err.Error(), "did you mean df") {
		t.Errorf("unknown command: %v", err)
	}
	_, err = Select(reg, Context{Command: "zzzzzz"})
	if !errors.As(err, &uc) || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("no suggestion expected: %v", err)
	}
	_, err = Select(reg, Context{Command: "df", Variant: "solaris"})
	var uv *UnknownVariantError
	if !errors.As(err, &uv) || !strings.Contains(err.Error(), "available: bsd, busybox-human, gnu, gnu-human") {
		t.Errorf("unknown variant: %v", err)
	}
	res, err := Select(reg, Context{Command: "df", Variant: "bsd"})
	if err != nil || res.Entry.Def.Variant != "bsd" || res.Evaluations[0].Reasons[0] != "selected with --variant" {
		t.Errorf("explicit variant: %v %v", res, err)
	}
}

func TestSelectSpecificityPriorityAmbiguity(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"x/catch-all": "format: 1\ncommand: x\nvariant: catch-all\nparse: {type: kv}\n",
		"x/sig":       "format: 1\ncommand: x\nvariant: sig\ndetect: {signature: {any: ['^HEADER']}}\nparse: {type: kv}\n",
		"x/sig-os":    "format: 1\ncommand: x\nvariant: sig-os\ndetect: {os: [linux], signature: {any: ['^HEADER']}}\nparse: {type: kv}\n",
	})
	// Pipe mode, unknown OS: sig (1) beats catch-all (0); sig-os also 1 -> tie -> ambiguous.
	_, err := Select(reg, Context{Command: "x", Input: []byte("HEADER\n")})
	var amb *AmbiguousError
	if !errors.As(err, &amb) || !strings.Contains(err.Error(), "variants sig, sig-os") {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	// With OS known, sig-os (2) wins.
	res, err := Select(reg, Context{Command: "x", OS: "linux", Input: []byte("HEADER\n")})
	if err != nil || res.Entry.Def.Variant != "sig-os" {
		t.Errorf("os tie-break: %v %v", res, err)
	}
	// Non-matching input: only the catch-all survives.
	res, err = Select(reg, Context{Command: "x", Input: []byte("other\n")})
	if err != nil || res.Entry.Def.Variant != "catch-all" {
		t.Errorf("catch-all: %v %v", res, err)
	}
	// Priority breaks a tie.
	reg2 := buildRegistry(t, map[string]string{
		"y/a": "format: 1\ncommand: y\nvariant: a\ndetect: {priority: 5, signature: {any: ['^H']}}\nparse: {type: kv}\n",
		"y/b": "format: 1\ncommand: y\nvariant: b\ndetect: {signature: {any: ['^H']}}\nparse: {type: kv}\n",
	})
	res, err = Select(reg2, Context{Command: "y", Input: []byte("H\n")})
	if err != nil || res.Entry.Def.Variant != "a" {
		t.Errorf("priority: %v %v", res, err)
	}
}

func TestSignatureNoneAndAllArgs(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(t, map[string]string{
		"z/a": "format: 1\ncommand: z\nvariant: a\ndetect: {args: {all: ['-a', '-b']}, signature: {none: ['FORBIDDEN'], window: 2}}\nparse: {type: kv}\n",
	})
	if _, err := Select(reg, Context{Command: "z", Args: []string{"-a"}, Input: []byte("ok\n")}); err == nil || !strings.Contains(err.Error(), "argument -b was not given") {
		t.Errorf("args all: %v", err)
	}
	if _, err := Select(reg, Context{Command: "z", Args: []string{"-ab"}, Input: []byte("x\nFORBIDDEN\n")}); err == nil || !strings.Contains(err.Error(), "none[0]") {
		t.Errorf("signature none: %v", err)
	}
	// Window of 2 lines: FORBIDDEN on line 3 is not seen.
	res, err := Select(reg, Context{Command: "z", Args: []string{"-ab"}, Input: []byte("x\ny\nFORBIDDEN\n")})
	if err != nil || res.Entry.Def.Variant != "a" {
		t.Errorf("window: %v %v", res, err)
	}
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()
	if levenshtein("kitten", "sitting") != 3 || levenshtein("", "abc") != 3 || levenshtein("same", "same") != 0 {
		t.Error("levenshtein")
	}
	if s := suggest("d", []string{"df", "du", "date", "dig", "zzz"}); len(s) != 3 {
		t.Errorf("suggest cap: %v", s)
	}
}

package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/internal/jsonbuild"
	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
)

// The documentation is checked against the command it documents: every
// jz command line it shows parses the way jz parses it, every option list
// it prints is the one jz prints, every exit status it names exists, every
// cookbook recipe is run by the end-to-end suite, and every page link and
// demo recording it points at is there. A change to the command line that
// leaves a page behind fails here rather than on the site.

// repoRoot is the repository root, two directories above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// documents are the pages a reader sees: the README and the site.
func documents(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	pages, err := filepath.Glob(filepath.Join(root, "website", "content", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return append([]string{filepath.Join(root, "README.md")}, pages...)
}

// readText returns a file with LF line endings, since a Windows checkout
// may give the documentation CRLF ones.
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// shellWords splits a command line the way a POSIX shell splits words,
// with quotes and backslashes, and keeps the operators that end a command
// (| ; && || > <) as words of their own.
func shellWords(line string) ([]string, error) {
	var (
		words []string
		cur   strings.Builder
		has   bool
		quote rune
	)
	flush := func() {
		if has {
			words = append(words, cur.String())
			cur.Reset()
			has = false
		}
	}
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]):
				i++
				cur.WriteRune(runes[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == '\\' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
			has = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		case r == '#' && !has:
			// A comment runs to the end of the line.
			flush()
			return words, nil
		case strings.ContainsRune("|;&<>", r):
			flush()
			op := string(r)
			if i+1 < len(runes) && (runes[i+1] == r || (r == '>' && runes[i+1] == '&')) {
				i++
				op += string(runes[i])
			}
			words = append(words, op)
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote in %q", line)
	}
	flush()
	return words, nil
}

// jzInvocations returns the argument lists of every jz command in a shell
// line: `df -h | jz --yaml` holds one, `jz new a=1 | curl -d @-` holds one.
// A redirection's target after > or < is not a command.
func jzInvocations(line string) ([][]string, error) {
	words, err := shellWords(line)
	if err != nil {
		return nil, err
	}
	var out [][]string
	start := true
	for i := 0; i < len(words); i++ {
		w := words[i]
		// A file descriptor in front of a redirection ("2>&1") is part of it.
		if i+1 < len(words) && strings.HasPrefix(words[i+1], ">") && strings.Trim(w, "0123456789") == "" {
			continue
		}
		switch w {
		case "|", "||", "&&", ";", "&":
			start = true
			continue
		case ">", ">>", "<", ">&":
			i++
			continue
		}
		if !start {
			continue
		}
		start = false
		if w != "jz" {
			continue
		}
		var args []string
		for i+1 < len(words) && !strings.ContainsAny(words[i+1][:1], "|;&<>") {
			if i+2 < len(words) && strings.HasPrefix(words[i+2], ">") && strings.Trim(words[i+1], "0123456789") == "" {
				break
			}
			i++
			args = append(args, words[i])
		}
		out = append(out, args)
	}
	return out, nil
}

// errUnexpected is a positional argument where a mode takes none.
var errUnexpected = errors.New("unexpected positional argument")

// parseCommandLine parses args the way Main does, without running
// anything: the options against the mode's option set, and what is left
// against what the mode takes.
func parseCommandLine(args []string) error {
	mode := ""
	if len(args) > 0 && lookup(args[0]) != nil {
		mode, args = args[0], args[1:]
	}
	if mode == "" && len(args) == 1 && (args[0] == "-h" || args[0] == flagHelp || args[0] == "-v" || args[0] == "--version") {
		return nil
	}
	o := newOptions("jz")
	switch mode {
	case modeRun:
		var r runOptions
		r.bind(o)
	case modeList:
		var l listOptions
		l.bind(o)
	case modeTest:
		var tt testOptions
		tt.bind(o)
	case modeNew:
		var n newCmdOptions
		n.bind(o)
	case "completion", "version":
	default:
		var co convertOptions
		co.bind(o)
	}
	if err := o.fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	if name := o.repeated(); name != "" {
		return fmt.Errorf("%s given twice", name)
	}
	rest := o.fs.Args()
	switch mode {
	case "":
		if len(rest) > 0 {
			return fmt.Errorf("%w %q", errUnexpected, rest[0])
		}
	case modeRun:
		if len(rest) == 0 {
			return errors.New("jz run with no command")
		}
	case modeList:
		if len(rest) > 2 {
			return fmt.Errorf("%w %q", errUnexpected, rest[2])
		}
	case "completion":
		if len(rest) != 1 || (rest[0] != "bash" && rest[0] != "zsh") {
			return fmt.Errorf("completion takes bash or zsh, got %q", rest)
		}
	case modeNew:
		// The arguments are checked for what they say, with files that are
		// never read: a documented argument that is not KEY=VALUE fails.
		src := jsonbuild.Sources{ReadFile: func(string) ([]byte, error) { return []byte("{}"), nil }, Stdin: strings.NewReader("{}"), MaxSize: 1 << 10}
		var err error
		if slices.Contains(args, "--array") {
			_, err = jsonbuild.Array(rest, src)
		} else {
			_, err = jsonbuild.Object(rest, src)
		}
		var ue *jsonbuild.UsageError
		if errors.As(err, &ue) {
			return err
		}
	}
	return nil
}

// consoleCommands returns the command lines shown after a "$ " prompt in
// the fenced console and shell blocks of a Markdown document, with the
// line number each is on.
func consoleCommands(text string) map[int]string {
	out := map[int]string{}
	fence := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if fence == "" {
				fence = strings.TrimPrefix(trimmed, "```")
				if fence == "" {
					fence = "text"
				}
			} else {
				fence = ""
			}
			continue
		}
		if fence != "console" && fence != "sh" && fence != "shell" && fence != "bash" {
			continue
		}
		cmd := trimmed
		if fence == "console" {
			if !strings.HasPrefix(trimmed, "$ ") {
				continue
			}
			cmd = strings.TrimPrefix(trimmed, "$ ")
		}
		// A quote left open continues on the next lines of the block.
		start := n
		for {
			if _, err := shellWords(cmd); err == nil || !sc.Scan() {
				break
			}
			n++
			cmd += "\n" + sc.Text()
		}
		if strings.Contains(" "+cmd, " jz ") || strings.HasPrefix(cmd, "jz ") || cmd == "jz" || strings.Contains(cmd, "| jz") {
			out[start] = cmd
		}
	}
	return out
}

// Every jz command line a page shows parses. A page that shows an option
// jz no longer has, a value where an option takes none, or a jz new
// argument that is not an argument, fails with the page and line.
func TestDocumentedCommandsParse(t *testing.T) {
	t.Parallel()
	checked := 0
	for _, doc := range documents(t) {
		for n, line := range consoleCommands(readText(t, doc)) {
			invocations, err := jzInvocations(line)
			if err != nil {
				t.Errorf("%s:%d: %v", doc, n, err)
				continue
			}
			for _, args := range invocations {
				checked++
				if err := parseCommandLine(args); err != nil {
					t.Errorf("%s:%d: `%s`: %v", filepath.Base(doc), n, line, err)
				}
			}
		}
	}
	if checked < 50 {
		t.Errorf("only %d documented jz command lines were found; the scan is not finding them", checked)
	}
}

// The parser of a documented command line is itself held to refusing what
// jz refuses, so the test above cannot pass by accepting everything.
func TestParseCommandLineRefuses(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"jz --no-such-option",
		"jz captured.txt",
		"jz new name",
		"jz new a=1 a=2",
		"jz list a b c",
		"jz run",
		"jz completion fish",
		"jz --parser df --parser ls",
	} {
		inv, err := jzInvocations(line)
		if err != nil || len(inv) != 1 {
			t.Fatalf("%s: %v %v", line, inv, err)
		}
		if err := parseCommandLine(inv[0]); err == nil {
			t.Errorf("%s was accepted", line)
		}
	}
	inv, err := jzInvocations(`df -h | jz --define 'parse: {type: table}' > out.json && jz new x=1 | curl -d @- https://example.com`)
	if err != nil || len(inv) != 2 || inv[0][1] != "parse: {type: table}" || inv[1][0] != "new" {
		t.Errorf("invocations: %q %v", inv, err)
	}
}

// optionsBlock returns the lines of the fenced block that follows the
// first "Options:" heading or line in a document.
func optionsBlock(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "\n  -f, --file PATH")
	if i < 0 {
		return ""
	}
	end := strings.Index(text[i:], "\n```")
	if end < 0 {
		t.Fatal("the options block has no end")
	}
	return strings.TrimPrefix(text[i:i+end], "\n") + "\n"
}

// The option list the README and the usage page print is the one
// `jz --help` prints.
func TestDocumentedOptionsMatchHelp(t *testing.T) {
	t.Parallel()
	o := newOptions("jz")
	var co convertOptions
	co.bind(o)
	var want strings.Builder
	o.print(&want)
	root := repoRoot(t)
	for _, doc := range []string{filepath.Join(root, "README.md"), filepath.Join(root, "website", "content", "usage.md")} {
		got := optionsBlock(t, readText(t, doc))
		if got != want.String() {
			t.Errorf("%s: the options block differs from jz --help:\n--- page\n%s--- help\n%s", filepath.Base(doc), got, want.String())
		}
	}
}

var exitRow = regexp.MustCompile(`(?m)^\|\s*(\d+)\s*\|`)

// Every exit status a table on a page names is one jz has, and the usage
// page names every one of them.
func TestDocumentedExitCodes(t *testing.T) {
	t.Parallel()
	known := map[int]bool{ExitOK: true, ExitError: true, ExitUsage: true, ExitParse: true, ExitSelect: true, ExitRegistry: true, ExitOutputClosed: true}
	for _, doc := range documents(t) {
		text := readText(t, doc)
		for _, section := range strings.Split(text, "\n## ") {
			if !strings.Contains(strings.SplitN(section, "\n", 2)[0], "xit") && !strings.Contains(section, "| Exit |") && !strings.Contains(section, "| Code |") {
				continue
			}
			for _, m := range exitRow.FindAllStringSubmatch(section, -1) {
				code, _ := strconv.Atoi(m[1])
				if !known[code] {
					t.Errorf("%s names exit status %d, which jz does not have", filepath.Base(doc), code)
				}
			}
		}
	}
	usage := readText(t, filepath.Join(repoRoot(t), "website", "content", "usage.md"))
	i := strings.Index(usage, "\n## Exit codes")
	if i < 0 {
		t.Fatal("usage.md has no Exit codes section")
	}
	named := map[int]bool{}
	for _, m := range exitRow.FindAllStringSubmatch(strings.SplitN(usage[i+1:], "\n## ", 2)[0], -1) {
		code, _ := strconv.Atoi(m[1])
		named[code] = true
	}
	for code := range known {
		if !named[code] {
			t.Errorf("usage.md does not name exit status %d", code)
		}
	}
}

var (
	cookbookSection  = regexp.MustCompile(`(?m)^## (.+)$`)
	cookbookScenario = regexp.MustCompile(`(?m)^  - name: "(.+?): `)
	anchorLink       = regexp.MustCompile(`\]\(#([a-z0-9-]+)\)`)
)

// headingID is the id Hugo gives a heading: lower case, spaces as
// hyphens, punctuation other than hyphens dropped.
func headingID(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Every cookbook recipe is run by e2e/atago/cookbook.atago.yaml in a
// scenario named after it, every scenario there is a recipe on the page,
// and the index at the top of the page links to recipes that exist.
func TestCookbookRecipesAreRun(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	page := readText(t, filepath.Join(root, "website", "content", "cookbook.md"))
	spec := readText(t, filepath.Join(root, "e2e", "atago", "cookbook.atago.yaml"))
	sections := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range cookbookSection.FindAllStringSubmatch(page, -1) {
		sections[m[1]] = true
		ids[headingID(m[1])] = true
	}
	scenarios := map[string]bool{}
	for _, m := range cookbookScenario.FindAllStringSubmatch(spec, -1) {
		scenarios[m[1]] = true
	}
	if len(sections) < 10 {
		t.Fatalf("only %d cookbook sections found", len(sections))
	}
	for s := range sections {
		if !scenarios[s] {
			t.Errorf("the cookbook recipe %q has no scenario named %q in cookbook.atago.yaml", s, s+": ...")
		}
	}
	for s := range scenarios {
		if !sections[s] {
			t.Errorf("cookbook.atago.yaml runs %q, which is not a section of the cookbook", s)
		}
	}
	for _, m := range anchorLink.FindAllStringSubmatch(page, -1) {
		if !ids[m[1]] {
			t.Errorf("the cookbook links to #%s, which no section has", m[1])
		}
	}
}

var pageLink = regexp.MustCompile(`\]\((\.\./)?([a-z0-9-]+)/(#[a-z0-9-]+)?\)`)

// A link from one page of the site to another names a page that exists.
func TestSiteLinksResolve(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	content := filepath.Join(root, "website", "content")
	for _, doc := range documents(t) {
		if filepath.Base(doc) == "README.md" {
			continue
		}
		for _, m := range pageLink.FindAllStringSubmatch(readText(t, doc), -1) {
			name := m[2]
			if name == "schemas" || name == "demo" {
				continue
			}
			if _, err := os.Stat(filepath.Join(content, name+".md")); errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s links to %s/, which is not a page", filepath.Base(doc), name)
			}
		}
	}
}

var (
	tapeType   = regexp.MustCompile(`(?m)^Type "((?:[^"\\]|\\.)*)"`)
	tapeOutput = regexp.MustCompile(`(?m)^Output (\S+)$`)
	gifRef     = regexp.MustCompile(`\(((?:\.\./)?demo/[a-z0-9-]+\.gif)\)`)
)

// Every demo tape types jz command lines that parse and records into a
// GIF that is committed, and every GIF a page shows has a tape.
func TestDemoTapes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tapes, err := filepath.Glob(filepath.Join(root, "demo", "*.tape"))
	if err != nil || len(tapes) == 0 {
		t.Fatalf("no tapes: %v", err)
	}
	recorded := map[string]bool{}
	for _, tape := range tapes {
		text := readText(t, tape)
		out := tapeOutput.FindStringSubmatch(text)
		if out == nil {
			t.Errorf("%s has no Output", filepath.Base(tape))
			continue
		}
		recorded[out[1]] = true
		if _, err := os.Stat(filepath.Join(root, out[1])); err != nil {
			t.Errorf("%s records %s, which is not committed", filepath.Base(tape), out[1])
		}
		// A line a tape types to show a refusal says so in a comment right
		// above it, and must then be refused.
		refusal := false
		for _, raw := range strings.Split(text, "\n") {
			if strings.HasPrefix(raw, "#") {
				refusal = strings.Contains(raw, "refused on purpose")
				continue
			}
			m := tapeType.FindStringSubmatch(raw)
			if m == nil {
				continue
			}
			line := strings.ReplaceAll(m[1], `\"`, `"`)
			invocations, err := jzInvocations(line)
			if err != nil {
				t.Errorf("%s: %v", filepath.Base(tape), err)
				continue
			}
			for _, args := range invocations {
				err := parseCommandLine(args)
				switch {
				case refusal && err == nil:
					t.Errorf("%s types `%s` to show a refusal, and it parses", filepath.Base(tape), line)
				case !refusal && err != nil:
					t.Errorf("%s types `%s`: %v", filepath.Base(tape), line, err)
				}
			}
			refusal = false
		}
	}
	for _, doc := range documents(t) {
		for _, m := range gifRef.FindAllStringSubmatch(readText(t, doc), -1) {
			gif := strings.TrimPrefix(m[1], "../")
			if !recorded[gif] {
				t.Errorf("%s shows %s, which no tape records", filepath.Base(doc), gif)
			}
		}
	}
}

var registryCounts = regexp.MustCompile(`(\d+) commands through (\d+) definitions`)

// The number of commands and definitions the landing page names is the
// number the built-in registry holds.
func TestLandingPageCounts(t *testing.T) {
	t.Parallel()
	reg, err := registry.Load(registry.Source{Name: SourceEmbedded, FS: official.FS()})
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]bool{}
	for _, e := range reg.Entries() {
		commands[e.Def.Command] = true
	}
	page := readText(t, filepath.Join(repoRoot(t), "website", "content", "_index.md"))
	m := registryCounts.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the landing page names no counts")
	}
	if want := fmt.Sprintf("%d commands through %d definitions", len(commands), len(reg.Entries())); m[0] != want {
		t.Errorf("the landing page says %q; the registry holds %q", m[0], want)
	}
}

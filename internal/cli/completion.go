package cli

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/pkg/registry"
)

const completionUsage = `Usage: jz completion bash|zsh

Prints a completion script for the shell. It completes the subcommands,
the options, the parsers of every registry jz reads (the built-in one,
your own and those JSONIZE_REGISTRY_PATH names), the variants of the
parser named on the line, the formats --format takes, and file paths
where a path is expected.
Completing reads the registries and nothing else: it runs no command and
touches no network.

  bash:  eval "$(jz completion bash)"        # add to ~/.bashrc
  zsh:   source <(jz completion zsh)         # add to ~/.zshrc, after compinit
`

//go:embed completion/jz.bash
var bashCompletion string

//go:embed completion/jz.zsh
var zshCompletion string

// The subcommands whose words completion tells apart.
const (
	modeRun  = "run"
	modeList = "list"
	modeTest = "test"
)

// completeCommand is the entry the scripts call. It is not listed in the
// help: it is how the scripts ask jz what to offer, not a command for
// people.
const completeCommand = "__complete"

func (a *app) cmdCompletion(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == flagHelp) {
		fmt.Fprint(a.env.Stdout, completionUsage)
		return ExitOK
	}
	if len(args) != 1 {
		a.errorf("completion takes one shell, bash or zsh")
		fmt.Fprint(a.env.Stderr, completionUsage)
		return ExitUsage
	}
	// A Windows checkout gives the embedded scripts CRLF endings, and a
	// shell reads the carriage return as part of each command.
	switch args[0] {
	case "bash":
		fmt.Fprint(a.env.Stdout, strings.ReplaceAll(bashCompletion, "\r\n", "\n"))
	case "zsh":
		fmt.Fprint(a.env.Stdout, strings.ReplaceAll(zshCompletion, "\r\n", "\n"))
	default:
		a.errorf("completion: no script for %q; jz has bash and zsh", args[0])
		return ExitUsage
	}
	return ExitOK
}

// The kinds of answer a completion is. The first line jz writes names
// one; for words, the candidates follow it one per line.
const (
	completeWords = "words"
	completeFiles = "files"
	completeDirs  = "dirs"
	completeNone  = "none"
)

// cmdComplete answers the scripts: args are the words after jz up to and
// including the one being completed, which may be empty. It never fails
// loudly, since whatever it wrote to standard error would land in the
// middle of the command line being typed.
func (a *app) cmdComplete(args []string) int {
	kind, words := a.complete(args)
	fmt.Fprintln(a.env.Stdout, kind)
	for _, w := range words {
		fmt.Fprintln(a.env.Stdout, w)
	}
	return ExitOK
}

// completer holds what a completion needs to know about the registries,
// loaded once and only when a parser or a variant is to be offered.
type completer struct {
	a   *app
	reg *registry.Registry
}

func (c *completer) registry() *registry.Registry {
	if c.reg != nil {
		return c.reg
	}
	srcs, err := c.a.sources()
	if err == nil {
		c.reg, err = registry.Load(srcs...)
	}
	if err != nil || c.reg == nil {
		// A registry that does not load offers what the built-in one
		// has, rather than nothing.
		emb := c.a.env.Embedded
		if emb.Name == "" {
			emb.Name = SourceEmbedded
		}
		c.reg, _ = registry.Load(emb)
	}
	if c.reg == nil {
		c.reg = &registry.Registry{}
	}
	return c.reg
}

// parsers are the names --parser and jz run take: every command and
// every alias.
func (c *completer) parsers() []string {
	reg := c.registry()
	cmds := reg.Commands()
	out := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		out = append(out, cmd)
		out = append(out, reg.Aliases(cmd)...)
	}
	sort.Strings(out)
	return dedupeSorted(out)
}

// dedupeSorted drops the repeats of a sorted list: two definitions may
// answer to the same alias.
func dedupeSorted(list []string) []string {
	out := list[:0]
	for i, s := range list {
		if i == 0 || s != list[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func (c *completer) variants(parser string) []string {
	if parser == "" {
		return nil
	}
	return variantNames(c.registry().Variants(parser))
}

// optionInfo is what completion needs to know about the options of one
// mode: the names, and which of them take a value.
type optionInfo struct {
	names  []string
	valued map[string]bool
}

func infoOf(o *optionSet) optionInfo {
	info := optionInfo{valued: map[string]bool{}}
	for _, d := range o.docs {
		long := "--" + d.long
		info.names = append(info.names, long)
		if d.short != "" {
			info.names = append(info.names, "-"+d.short)
		}
		if d.arg != "" {
			info.valued[long] = true
			if d.short != "" {
				info.valued["-"+d.short] = true
			}
		}
		if d.optional != "" {
			info.names = append(info.names, long+"="+d.optional)
		}
	}
	sort.Strings(info.names)
	return info
}

func modeOptions(mode string) optionInfo {
	o := newOptions(mode)
	switch mode {
	case modeRun:
		var r runOptions
		r.bind(o)
	case modeList:
		var l listOptions
		l.bind(o)
	case modeTest:
		var t testOptions
		t.bind(o)
	default:
		var co convertOptions
		co.bind(o)
		o.doc("v", "version", "", "print the version")
	}
	return infoOf(o)
}

// complete works out what the last of words can be.
func (a *app) complete(words []string) (string, []string) {
	c := &completer{a: a}
	if len(words) == 0 {
		words = []string{""}
	}
	cur := words[len(words)-1]
	before := words[:len(words)-1]
	mode := ""
	if len(before) > 0 {
		if lookup(before[0]) != nil || before[0] == "help" {
			mode, before = before[0], before[1:]
		}
	} else if !strings.HasPrefix(cur, "-") {
		return completeWords, filter(append(commandNames(), "help"), cur)
	}
	switch mode {
	case "completion":
		if len(before) == 0 {
			return completeWords, filter([]string{"bash", "zsh"}, cur)
		}
		return completeNone, nil
	case "help":
		if len(before) == 0 {
			return completeWords, filter(commandNames(), cur)
		}
		return completeNone, nil
	case "version":
		return completeNone, nil
	}
	info := modeOptions(mode)
	st := scanWords(before, info, mode == modeRun)
	// A value after "--opt=" in one word, as zsh passes it, keeps the
	// option in front of what is offered; a value in a word of its own,
	// as bash splits it at "=", is offered bare.
	if !st.afterDashes && strings.HasPrefix(cur, "--") {
		if name, val, ok := strings.Cut(cur, "="); ok {
			kind, vals := c.value(mode, name, st, val)
			if kind != completeWords {
				return kind, nil
			}
			out := make([]string, len(vals))
			for i, v := range vals {
				out[i] = name + "=" + v
			}
			return completeWords, out
		}
	}
	if st.pending != "" {
		return c.value(mode, st.pending, st, cur)
	}
	if strings.HasPrefix(cur, "-") && !st.afterDashes && (mode != modeRun || st.command == "") {
		return completeWords, filter(info.names, cur)
	}
	switch mode {
	case modeRun:
		if st.command == "" {
			return completeWords, filter(c.parsers(), cur)
		}
		return completeFiles, nil
	case "list":
		switch len(st.positional) {
		case 0:
			return completeWords, filter(c.parsers(), cur)
		case 1:
			return completeWords, filter(c.variants(st.positional[0]), cur)
		}
		return completeNone, nil
	case modeTest:
		return completeDirs, nil
	}
	return completeNone, nil
}

// wordState is what the words before the one being completed say.
type wordState struct {
	// pending is an option still waiting for its value.
	pending string
	// parser is the value --parser was given.
	parser string
	// command is the command jz run runs, once it has been typed.
	command    string
	positional []string
	// afterDashes records a bare -- before the current word.
	afterDashes bool
}

// scanWords reads the words before the one being completed. For jz run
// the first word that is not an option is the command, and everything
// after it belongs to the command.
func scanWords(words []string, info optionInfo, run bool) wordState {
	var st wordState
	for _, w := range words {
		switch {
		case st.pending != "":
			if w == "=" {
				// bash splits --opt=value into three words.
				continue
			}
			if st.pending == "--parser" {
				st.parser = w
			}
			st.pending = ""
		case run && (st.command != "" || st.afterDashes):
			st.afterDashes = true
			if st.command == "" {
				st.command = w
			}
			st.positional = append(st.positional, w)
		case w == "--" && !st.afterDashes:
			st.afterDashes = true
		case st.afterDashes:
			st.positional = append(st.positional, w)
		case strings.HasPrefix(w, "-") && w != "-":
			name, val, hasVal := strings.Cut(w, "=")
			if info.valued[name] {
				if hasVal {
					if name == "--parser" {
						st.parser = val
					}
				} else {
					st.pending = name
				}
			}
		default:
			if st.command == "" {
				st.command = w
			}
			st.positional = append(st.positional, w)
		}
	}
	return st
}

// value completes the value of an option.
func (c *completer) value(mode, name string, st wordState, cur string) (string, []string) {
	switch name {
	case "--parser":
		return completeWords, filter(c.parsers(), cur)
	case "--variant":
		parser := st.parser
		if parser == "" && mode == modeRun {
			parser = parserKey(st.command)
		}
		return completeWords, filter(c.variants(parser), cur)
	case "-f", "--file":
		return completeFiles, nil
	case "--decoys":
		return completeDirs, nil
	case "--explain":
		return completeWords, filter([]string{"json"}, cur)
	case "--format":
		return completeWords, filter(datafile.Names(), cur)
	}
	return completeNone, nil
}

func commandNames() []string {
	cmds := commands()
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.name)
	}
	return out
}

// filter keeps the candidates that start with prefix.
func filter(list []string, prefix string) []string {
	var out []string
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

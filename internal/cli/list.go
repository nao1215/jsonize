package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/registry"
)

const listUsage = `Usage: jz list [COMMAND [VARIANT]]

  jz list                 the commands jz can convert
  jz list df              the variants of one command
  jz list df gnu-human    everything about one definition
  jz list --sources       the registries in use, in precedence order

Flags:
`

func (a *app) cmdList(args []string) int {
	fs := newFlagSet("list")
	var (
		rf      registryFlags
		asJSON  bool
		sources bool
	)
	rf.bind(fs)
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON instead of a table")
	fs.BoolVar(&sources, "sources", false, "list the registries in precedence order")
	if code, done := a.parseFlags(fs, args, listUsage); done {
		return code
	}
	if fs.NArg() > 2 {
		a.errorf("list: expected at most COMMAND and VARIANT, got %d arguments", fs.NArg())
		return ExitUsage
	}
	if sources {
		if fs.NArg() > 0 {
			a.errorf("list: --sources takes no arguments")
			return ExitUsage
		}
		return a.listSources(&rf, asJSON)
	}
	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	switch fs.NArg() {
	case 0:
		return a.listCommands(reg, asJSON)
	case 1:
		return a.listVariants(reg, fs.Arg(0), asJSON)
	default:
		return a.listDefinition(reg, fs.Arg(0), fs.Arg(1), asJSON)
	}
}

func (a *app) listCommands(reg *registry.Registry, asJSON bool) int {
	commands := reg.Commands()
	if asJSON {
		list := make([]any, 0, len(commands))
		for _, c := range commands {
			o := jsonutil.NewObject()
			o.Set("command", c)
			o.Set("variants", stringsToAny(variantNames(reg.Variants(c))))
			list = append(list, o)
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMAND\tVARIANTS")
	for _, c := range commands {
		fmt.Fprintf(tw, "%s\t%s\n", c, strings.Join(variantNames(reg.Variants(c)), ", "))
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	fmt.Fprintf(a.env.Stdout, "\n%d commands, %d definitions. `jz list COMMAND` shows the variants.\n", len(commands), reg.Len())
	return ExitOK
}

func (a *app) listVariants(reg *registry.Registry, command string, asJSON bool) int {
	entries := reg.Variants(parserKey(command))
	if len(entries) == 0 {
		a.errorf("no parser for %q; run `jz list` to see the supported commands", command)
		return ExitSelect
	}
	if asJSON {
		list := make([]any, 0, len(entries))
		for _, e := range entries {
			o := jsonutil.NewObject()
			o.Set("command", e.Def.Command)
			o.Set("variant", e.Def.Variant)
			o.Set("description", e.Def.Description)
			o.Set("os", stringsToAny(e.Def.Detect.OS))
			o.Set("source", e.Source)
			list = append(list, o)
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "VARIANT\tOS\tSOURCE\tDESCRIPTION")
	for _, e := range entries {
		goos := strings.Join(e.Def.Detect.OS, ",")
		if goos == "" {
			goos = "any"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Def.Variant, goos, e.Source, e.Def.Description)
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	fmt.Fprintf(a.env.Stdout, "\n`jz list %s %s` shows how one definition detects and parses.\n", entries[0].Def.Command, entries[0].Def.Variant)
	return ExitOK
}

func (a *app) listDefinition(reg *registry.Registry, command, variant string, asJSON bool) int {
	e, ok := reg.Lookup(parserKey(command), variant)
	if !ok {
		if len(reg.Variants(parserKey(command))) == 0 {
			a.errorf("no parser for %q; run `jz list` to see the supported commands", command)
		} else {
			a.errorf("parser %q has no variant %q (available: %s)", command, variant,
				strings.Join(variantNames(reg.Variants(parserKey(command))), ", "))
		}
		return ExitSelect
	}
	if asJSON {
		return finish(jsonutil.Encode(a.env.Stdout, describe(e), true), a)
	}
	d := e.Def
	w := a.env.Stdout
	fmt.Fprintf(w, "%s\n", d.ID())
	fmt.Fprintf(w, "  description:  %s\n", d.Description)
	fmt.Fprintf(w, "  source:       %s (%s)\n", e.Source, e.Path)
	if len(e.Shadowed) > 0 {
		fmt.Fprintf(w, "  shadows:      the same definition in %s\n", strings.Join(e.Shadowed, ", "))
	}
	fmt.Fprintf(w, "  format:       %d\n", d.Format)
	if d.MinJsonize != "" {
		fmt.Fprintf(w, "  min_jsonize:  %s\n", d.MinJsonize)
	}
	if len(d.Metadata.Compatible) > 0 {
		fmt.Fprintf(w, "  compatible:   %s\n", strings.Join(d.Metadata.Compatible, ", "))
	}
	if len(d.Metadata.Tags) > 0 {
		fmt.Fprintf(w, "  tags:         %s\n", strings.Join(d.Metadata.Tags, ", "))
	}
	for _, ref := range d.Metadata.References {
		fmt.Fprintf(w, "  reference:    %s\n", ref)
	}
	fmt.Fprintf(w, "  detection:    %s\n", describeDetect(&d.Detect))
	for _, line := range signatureLines(&d.Detect.Signature) {
		fmt.Fprintf(w, "                %s\n", line)
	}
	fmt.Fprintf(w, "  parse:        %s\n", describeParse(&d.Parse))
	printFields(w, "  ", d.Fields)
	// A composite parser keeps its fields inside the parts, so they are
	// listed where they belong rather than being lost here.
	for i := range d.Parse.Parts {
		part := &d.Parse.Parts[i]
		fmt.Fprintf(w, "  part %s: %s\n", part.Name, describeParse(&part.Parse))
		printFields(w, "    ", part.Fields)
	}
	return ExitOK
}

func printFields(w io.Writer, indent string, fields map[string]*definition.Field) {
	if len(fields) == 0 {
		return
	}
	fmt.Fprintf(w, "%sfields:\n", indent)
	for _, name := range sortedKeys(fields) {
		fmt.Fprintf(w, "%s  %-20s %s\n", indent, name, describeField(fields[name]))
	}
}

func (a *app) listSources(rf *registryFlags, asJSON bool) int {
	srcs, err := a.sources(rf)
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	reg, code := a.loadRegistry(rf)
	if code != 0 {
		return code
	}
	counts := map[string]int{}
	for _, e := range reg.Entries() {
		counts[e.Source]++
	}
	if asJSON {
		list := make([]any, 0, len(srcs))
		for i, s := range srcs {
			o := jsonutil.NewObject()
			o.Set("precedence", int64(i+1))
			o.Set("name", s.Name)
			o.Set("location", a.sourceLocation(s))
			o.Set("present", a.sourceExists(s))
			o.Set("definitions", int64(counts[s.Name]))
			list = append(list, o)
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tREGISTRY\tSTATE\tDEFINITIONS\tLOCATION")
	for i, s := range srcs {
		state := "absent"
		if a.sourceExists(s) {
			state = "present"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\n", i+1, s.Name, state, counts[s.Name], a.sourceLocation(s))
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	fmt.Fprintf(a.env.Stdout, "\nThe first registry that defines a command/variant wins.\nAdd your own with --registry DIR or %s.\n", EnvRegistryPath)
	return ExitOK
}

func (a *app) sourceLocation(s registry.Source) string {
	switch s.Name {
	case a.env.Embedded.Name, SourceEmbedded:
		return "(built into jz)"
	case SourceUser:
		dir, err := a.userRegistryDir()
		if err != nil {
			return "(unavailable)"
		}
		return dir
	default:
		return s.Name
	}
}

func (a *app) sourceExists(s registry.Source) bool {
	loc := a.sourceLocation(s)
	if !filepath.IsAbs(loc) {
		return true // the embedded registry is always there
	}
	_, err := os.Stat(filepath.Join(loc, registry.ParsersDir))
	return err == nil
}

func finish(err error, a *app) int {
	if err != nil {
		a.errorf("%v", err)
		return ExitError
	}
	return ExitOK
}

func describe(e *registry.Entry) *jsonutil.Object {
	d := e.Def
	o := jsonutil.NewObject()
	o.Set("command", d.Command)
	o.Set("variant", d.Variant)
	o.Set("description", d.Description)
	o.Set("format", int64(d.Format))
	if d.MinJsonize != "" {
		o.Set("min_jsonize", d.MinJsonize)
	}
	o.Set("source", e.Source)
	o.Set("path", e.Path)
	o.Set("shadowed", stringsToAny(e.Shadowed))
	o.Set("compatible", stringsToAny(d.Metadata.Compatible))
	o.Set("tags", stringsToAny(d.Metadata.Tags))
	o.Set("references", stringsToAny(d.Metadata.References))
	det := jsonutil.NewObject()
	det.Set("os", stringsToAny(d.Detect.OS))
	det.Set("args_any", stringsToAny(d.Detect.Args.Any))
	det.Set("args_all", stringsToAny(d.Detect.Args.All))
	det.Set("args_none", stringsToAny(d.Detect.Args.None))
	det.Set("signature_all", stringsToAny(d.Detect.Signature.All))
	det.Set("signature_any", stringsToAny(d.Detect.Signature.Any))
	det.Set("signature_none", stringsToAny(d.Detect.Signature.None))
	det.Set("priority", int64(d.Detect.Priority))
	det.Set("auto_detectable", !d.Detect.Signature.IsZero())
	o.Set("detect", det)
	o.Set("parse", describeParse(&d.Parse))
	o.Set("fields", fieldsObject(d.Fields))
	if len(d.Parse.Parts) > 0 {
		parts := make([]any, 0, len(d.Parse.Parts))
		for i := range d.Parse.Parts {
			part := &d.Parse.Parts[i]
			po := jsonutil.NewObject()
			po.Set("name", part.Name)
			po.Set("parse", describeParse(&part.Parse))
			po.Set("fields", fieldsObject(part.Fields))
			parts = append(parts, po)
		}
		o.Set("parts", parts)
	}
	return o
}

func fieldsObject(fields map[string]*definition.Field) *jsonutil.Object {
	o := jsonutil.NewObject()
	for _, name := range sortedKeys(fields) {
		o.Set(name, describeField(fields[name]))
	}
	return o
}

func describeDetect(d *definition.Detect) string {
	var parts []string
	if d.Signature.IsZero() {
		parts = append(parts, "no signature (needs --parser "+"or jz run)")
	} else {
		n := len(d.Signature.All) + len(d.Signature.Any) + len(d.Signature.None)
		parts = append(parts, fmt.Sprintf("signature (%d expressions)", n))
	}
	if len(d.OS) > 0 {
		parts = append(parts, "os="+strings.Join(d.OS, ","))
	}
	if len(d.Args.Any) > 0 {
		parts = append(parts, "args any="+strings.Join(d.Args.Any, ","))
	}
	if len(d.Args.All) > 0 {
		parts = append(parts, "args all="+strings.Join(d.Args.All, ","))
	}
	if len(d.Args.None) > 0 {
		parts = append(parts, "args none="+strings.Join(d.Args.None, ","))
	}
	if d.Priority != 0 {
		parts = append(parts, fmt.Sprintf("priority=%d", d.Priority))
	}
	return strings.Join(parts, "; ")
}

func signatureLines(s *definition.Signature) []string {
	out := make([]string, 0, len(s.All)+len(s.Any)+len(s.None))
	for _, e := range s.All {
		out = append(out, "all:  "+e)
	}
	for _, e := range s.Any {
		out = append(out, "any:  "+e)
	}
	for _, e := range s.None {
		out = append(out, "none: "+e)
	}
	return out
}

func describeParse(p *definition.Parse) string {
	switch p.Type {
	case definition.TypeTable:
		split := p.Split
		if split == "" {
			split = definition.SplitWhitespace
		}
		s := "table split=" + split
		switch {
		case len(p.Header.Columns) > 0:
			s += " columns=" + strings.Join(p.Header.Columns, ",")
		case p.Header.None:
			s += " (no header)"
		default:
			s += " columns=from header"
		}
		return s
	case definition.TypeRegex:
		each := p.Each
		if each == "" {
			each = definition.EachLine
		}
		return fmt.Sprintf("regex each=%s groups=%s", each, strings.Join(p.Groups(), ","))
	case definition.TypeKV:
		sep := p.Separator
		if sep == "" {
			sep = "="
		}
		as := p.As
		if as == "" {
			as = definition.AsList
		}
		return fmt.Sprintf("kv separator=%q as=%s", sep, as)
	case definition.TypeComposite:
		names := make([]string, len(p.Parts))
		for i, part := range p.Parts {
			names[i] = part.Name + ":" + part.Parse.Type
		}
		return "composite parts=" + strings.Join(names, ",")
	}
	return p.Type
}

func describeField(f *definition.Field) string {
	if f == nil {
		return definition.FieldString
	}
	s := f.EffectiveType()
	if f.Unit != "" {
		s += " unit=" + f.Unit
	}
	if len(f.NullIf) > 0 {
		s += fmt.Sprintf(" null_if=%q", f.NullIf)
	}
	if f.TrimSuffix != "" {
		s += fmt.Sprintf(" trim_suffix=%q", f.TrimSuffix)
	}
	if f.TrimPrefix != "" {
		s += fmt.Sprintf(" trim_prefix=%q", f.TrimPrefix)
	}
	if f.Required {
		s += " required"
	}
	if f.WhenMissing == definition.MissingOmit {
		s += " omit-when-missing"
	}
	if f.EffectiveType() == definition.FieldArray {
		if f.Items != nil {
			s += " of " + describeField(f.Items)
		} else {
			s += " of string"
		}
	}
	if f.EffectiveType() == definition.FieldObject {
		s += " {" + strings.Join(f.Groups(), ",") + "}"
	}
	return s
}

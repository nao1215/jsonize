package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nao1215/jsonize/internal/schema"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
)

const listUsage = `Usage: jz list [COMMAND [VARIANT]]

  jz list                        the commands jz can convert
  jz list df                     the variants of one command
  jz list df gnu-human           everything about one definition
  jz list --schema df gnu-human  the JSON Schema of what it produces
  jz list --sources              the registries in use, in precedence order

Options:
`

// listOptions are the options of jz list.
type listOptions struct {
	asJSON, sources, asSchema bool
}

func (l *listOptions) bind(o *optionSet) {
	o.boolOpt(&l.asJSON, "json", "", "print machine-readable JSON instead of a table")
	o.boolOpt(&l.asSchema, "schema", "", "print the JSON Schema of one definition's output")
	o.boolOpt(&l.sources, "sources", "", "list the registries in precedence order")
	o.helpDoc()
}

func (a *app) cmdList(args []string) int {
	o := newOptions(modeList)
	var lo listOptions
	lo.bind(o)
	if code, done := a.parse(o, args, listUsage); done {
		return code
	}
	asJSON, sources, asSchema := lo.asJSON, lo.sources, lo.asSchema
	if o.fs.NArg() > 2 {
		a.errorf("list: expected at most COMMAND and VARIANT, got %d arguments", o.fs.NArg())
		return ExitUsage
	}
	if asSchema && (o.fs.NArg() != 2 || sources || asJSON) {
		a.errorf("list: --schema takes COMMAND VARIANT and nothing else: a schema describes one definition's output")
		return ExitUsage
	}
	if sources {
		if o.fs.NArg() > 0 {
			a.errorf("list: --sources takes no arguments")
			return ExitUsage
		}
		return a.listSources(asJSON)
	}
	reg, code := a.loadRegistry()
	if code != 0 {
		return code
	}
	// A definition that did not load is missing from everything printed
	// below, so the listing says how many rather than leaving the
	// warnings above to be counted by hand.
	defer a.reportSkipped(reg)
	switch o.fs.NArg() {
	case 0:
		return a.listCommands(reg, asJSON)
	case 1:
		return a.listVariants(reg, o.fs.Arg(0), asJSON)
	default:
		if asSchema {
			return a.listSchema(reg, o.fs.Arg(0), o.fs.Arg(1))
		}
		return a.listDefinition(reg, o.fs.Arg(0), o.fs.Arg(1), asJSON)
	}
}

// listSchema prints the JSON Schema of what one definition produces. It is
// derived from the definition, so it is the contract of the definition jz
// would use, whichever registry that comes from; the contract version is
// the one that registry published, and 1 for a registry that publishes
// none.
func (a *app) listSchema(reg *registry.Registry, command, variant string) int {
	e, ok := reg.Lookup(parserKey(command), variant)
	if !ok {
		return a.listDefinition(reg, command, variant, false)
	}
	srcs, err := a.sources()
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	var published *schema.Schema
	for _, s := range srcs {
		if s.Name == e.Source && s.FS != nil {
			if published, _, err = schema.Published(s.FS, e.Def); err != nil {
				a.errorf("%v", err)
				return ExitRegistry
			}
		}
	}
	out, err := schema.Encode(schema.Generate(e.Def, schema.Version(published)))
	if err != nil {
		a.errorf("%v", err)
		return ExitError
	}
	if _, err := a.env.Stdout.Write(out); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// reportSkipped closes a listing with how many definitions were left out
// because they failed to load.
func (a *app) reportSkipped(reg *registry.Registry) {
	switch n := len(reg.Problems); n {
	case 0:
	case 1:
		a.errorf("1 definition failed to load and is not listed")
	default:
		a.errorf("%d definitions failed to load and are not listed", n)
	}
}

func (a *app) listCommands(reg *registry.Registry, asJSON bool) int {
	commands := reg.Commands()
	if asJSON {
		list := make([]any, 0, len(commands))
		for _, c := range commands {
			o := jsonutil.NewObject()
			o.Set("command", c)
			o.Set("variants", stringsToAny(variantNames(ownVariants(reg, c))))
			if names := reg.Aliases(c); len(names) > 0 {
				o.Set("aliases", stringsToAny(names))
			}
			list = append(list, o)
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMAND\tVARIANTS")
	for _, c := range commands {
		fmt.Fprintf(tw, "%s\t%s\n", c, strings.Join(variantNames(ownVariants(reg, c)), ", "))
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	// A command that prints what another one prints is read by the same
	// definitions, and is listed here rather than as a command of its
	// own, because it adds no format.
	var also []string
	for _, c := range commands {
		for _, name := range reg.Aliases(c) {
			also = append(also, fmt.Sprintf("%s (as %s)", name, c))
		}
	}
	if len(also) > 0 {
		fmt.Fprintf(a.env.Stdout, "\nAlso read: %s.\n", strings.Join(also, ", "))
	}
	fmt.Fprintf(a.env.Stdout, "\n%d commands, %d definitions. `jz list COMMAND` shows the variants.\n", len(commands), reg.Len())
	return ExitOK
}

// ownVariants returns the definitions a command carries itself, leaving
// out the ones it only answers to as an alias of another command.
func ownVariants(reg *registry.Registry, command string) []*registry.Entry {
	all := reg.Variants(command)
	out := make([]*registry.Entry, 0, len(all))
	for _, e := range all {
		if e.Def.Command == command {
			out = append(out, e)
		}
	}
	return out
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
			if names := e.Def.AliasNames(); len(names) > 0 {
				o.Set("aliases", stringsToAny(names))
			}
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
		// A definition reached under another name says whose it is, so
		// that the variant name can be looked up where it lives.
		name := e.Def.Variant
		if key := parserKey(command); key != e.Def.Command {
			name = e.Def.ID()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, goos, e.Source, e.Def.Description)
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	// Definitions of another command appear here when that command prints
	// what this one prints; say so rather than leave the identifier to be
	// puzzled over.
	var borrowed []string
	for _, e := range entries {
		if e.Def.Command != parserKey(command) {
			borrowed = append(borrowed, e.Def.ID())
		}
	}
	if len(borrowed) > 0 {
		fmt.Fprintf(a.env.Stdout, "\n%s prints what another command prints, and %s reads it.\n",
			parserKey(command), strings.Join(borrowed, ", "))
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
	if names := d.AliasNames(); len(names) > 0 {
		fmt.Fprintf(w, "  also reads:   %s\n", strings.Join(names, ", "))
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
	fmt.Fprintf(w, "  detection:    %s\n", describeDetect(d))
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

func (a *app) listSources(asJSON bool) int {
	srcs, err := a.sources()
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	reg, code := a.loadRegistry()
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
			o.Set("disabled", int64(reg.Disabled(s.Name)))
			list = append(list, o)
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tREGISTRY\tSTATE\tDEFINITIONS\tDISABLED\tLOCATION")
	for i, s := range srcs {
		state := "absent"
		if a.sourceExists(s) {
			state = "present"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\t%s\n", i+1, s.Name, state, counts[s.Name], reg.Disabled(s.Name), a.sourceLocation(s))
	}
	if err := tw.Flush(); err != nil {
		return finish(err, a)
	}
	fmt.Fprintf(a.env.Stdout, "\nThe first registry that defines a command/variant wins, and DISABLED counts\nthe definitions a registry above it switched off.\nAdd your own by pointing %s at a directory.\n", EnvRegistryPath)
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
		return a.writeFailed(err)
	}
	return ExitOK
}

func describe(e *registry.Entry) *jsonutil.Object {
	d := e.Def
	o := jsonutil.NewObject()
	o.Set("command", d.Command)
	o.Set("variant", d.Variant)
	o.Set("aliases", stringsToAny(d.AliasNames()))
	o.Set("description", d.Description)
	o.Set("format", int64(d.Format))
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

func describeDetect(def *definition.Definition) string {
	d := &def.Detect
	var parts []string
	if d.Signature.IsZero() {
		parts = append(parts, fmt.Sprintf("no signature (read when named: --parser %s --variant %s, or jz run)", def.Command, def.Variant))
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
	case definition.TypeComposite, definition.TypeRecords:
		names := make([]string, len(p.Parts))
		for i, part := range p.Parts {
			names[i] = part.Name + ":" + part.Parse.Type
		}
		return p.Type + " parts=" + strings.Join(names, ",")
	case definition.TypeCSV:
		s := fmt.Sprintf("csv delimiter=%q", csvDelimiterOf(p))
		switch {
		case len(p.Header.Columns) > 0:
			s += " columns=" + strings.Join(p.Header.Columns, ",")
		case p.Header.None:
			s += " (no header)"
		default:
			s += " columns=from header"
		}
		return s
	case definition.TypeINI:
		sep := p.Separator
		if sep == "" {
			sep = "="
		}
		return fmt.Sprintf("ini separator=%q", sep)
	case definition.TypeTree:
		return fmt.Sprintf("tree indent=%q node=%s", p.Indent, describeParse(&p.Node.Parse))
	}
	return p.Type
}

// csvDelimiterOf reports the delimiter a csv parser cuts on, defaulted.
func csvDelimiterOf(p *definition.Parse) string {
	if p.Delimiter != "" {
		return p.Delimiter
	}
	return ","
}

func describeField(f *definition.Field) string {
	if f == nil {
		return definition.FieldString
	}
	s := f.EffectiveType()
	if f.Layout != "" {
		s += fmt.Sprintf(" layout=%q", f.Layout)
	}
	if f.Location != "" {
		s += " location=" + f.Location
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

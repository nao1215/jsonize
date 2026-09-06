package cli

import (
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/registry"
)

const showUsage = `Usage: jz show [flags] <command> [variant]

Describes the variants of a command, or one variant in detail: how it is
detected, how its output is parsed and which fields are typed.

Flags:
`

func (a *app) cmdShow(args []string) int {
	fs := newFlagSet("show")
	var rf registryFlags
	var asJSON bool
	rf.bind(fs)
	fs.BoolVar(&asJSON, "json", false, "print as JSON")
	if code, done := a.parseFlags(fs, args, showUsage); done {
		return code
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		a.errorf("show: expected <command> [variant]")
		return ExitUsage
	}
	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	key := commandKey(fs.Arg(0))
	entries := reg.Variants(key)
	if len(entries) == 0 {
		a.errorf("no parser definition for command %q; run `jz list`", key)
		return ExitSelect
	}
	if fs.NArg() == 2 {
		e, ok := reg.Lookup(key, fs.Arg(1))
		if !ok {
			a.errorf("command %q has no variant %q", key, fs.Arg(1))
			return ExitSelect
		}
		entries = []*registry.Entry{e}
	}
	if asJSON {
		var list []any
		for _, e := range entries {
			list = append(list, describe(e))
		}
		return finish(jsonutil.Encode(a.env.Stdout, list, true), a)
	}
	w := a.env.Stdout
	for i, e := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		d := e.Def
		fmt.Fprintf(w, "%s/%s\n", d.Command, d.Variant)
		fmt.Fprintf(w, "  description: %s\n", d.Description)
		fmt.Fprintf(w, "  source:      %s (%s)\n", e.Source, e.Path)
		if len(d.Metadata.Compatible) > 0 {
			fmt.Fprintf(w, "  compatible:  %s\n", strings.Join(d.Metadata.Compatible, ", "))
		}
		fmt.Fprintf(w, "  detect:      %s\n", describeDetect(&d.Detect))
		fmt.Fprintf(w, "  parse:       %s\n", describeParse(&d.Parse))
		if len(d.Fields) > 0 {
			fmt.Fprintln(w, "  fields:")
			for _, name := range sortedKeys(d.Fields) {
				fmt.Fprintf(w, "    %-20s %s\n", name, describeField(d.Fields[name]))
			}
		}
		if len(e.Shadowed) > 0 {
			fmt.Fprintf(w, "  shadows:     %s\n", strings.Join(e.Shadowed, ", "))
		}
	}
	return ExitOK
}

func describe(e *registry.Entry) *jsonutil.Object {
	d := e.Def
	o := jsonutil.NewObject()
	o.Set("command", d.Command)
	o.Set("variant", d.Variant)
	o.Set("description", d.Description)
	o.Set("source", e.Source)
	o.Set("path", e.Path)
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
	o.Set("detect", det)
	o.Set("parse", describeParse(&d.Parse))
	fields := jsonutil.NewObject()
	for _, name := range sortedKeys(d.Fields) {
		fields.Set(name, describeField(d.Fields[name]))
	}
	o.Set("fields", fields)
	o.Set("shadowed", stringsToAny(e.Shadowed))
	return o
}

func describeDetect(d *definition.Detect) string {
	var parts []string
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
	n := len(d.Signature.All) + len(d.Signature.Any) + len(d.Signature.None)
	if n > 0 {
		parts = append(parts, fmt.Sprintf("signature(%d expr)", n))
	}
	if d.Priority != 0 {
		parts = append(parts, fmt.Sprintf("priority=%d", d.Priority))
	}
	if len(parts) == 0 {
		return "none (catch-all)"
	}
	return strings.Join(parts, "; ")
}

func describeParse(p *definition.Parse) string {
	switch p.Type {
	case definition.TypeTable:
		split := p.Split
		if split == "" {
			split = definition.SplitWhitespace
		}
		s := "table split=" + split
		if len(p.Header.Columns) > 0 {
			s += " columns=" + strings.Join(p.Header.Columns, ",")
		} else if p.Header.None {
			s += " (no header)"
		} else {
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

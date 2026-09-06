package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/nao1215/jsonize/internal/jsonutil"
)

const listUsage = `Usage: jz list [flags]

Lists every command and variant known to the layered registries.

Flags:
`

func (a *app) cmdList(args []string) int {
	fs := newFlagSet("list")
	var rf registryFlags
	var asJSON bool
	rf.bind(fs)
	fs.BoolVar(&asJSON, "json", false, "print the list as JSON")
	if code, done := a.parseFlags(fs, args, listUsage); done {
		return code
	}
	reg, code := a.loadRegistry(&rf)
	if code != 0 {
		return code
	}
	if asJSON {
		var list []any
		for _, e := range reg.Entries() {
			o := jsonutil.NewObject()
			o.Set("command", e.Def.Command)
			o.Set("variant", e.Def.Variant)
			o.Set("description", e.Def.Description)
			o.Set("source", e.Source)
			o.Set("os", stringsToAny(e.Def.Detect.OS))
			o.Set("tags", stringsToAny(e.Def.Metadata.Tags))
			o.Set("shadowed", stringsToAny(e.Shadowed))
			list = append(list, o)
		}
		if list == nil {
			list = []any{}
		}
		if err := jsonutil.Encode(a.env.Stdout, list, true); err != nil {
			a.errorf("%v", err)
			return ExitError
		}
		return ExitOK
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMAND\tVARIANT\tOS\tSOURCE\tDESCRIPTION")
	for _, e := range reg.Entries() {
		os := strings.Join(e.Def.Detect.OS, ",")
		if os == "" {
			os = "any"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Def.Command, e.Def.Variant, os, e.Source, e.Def.Description)
	}
	return finish(tw.Flush(), a)
}

func finish(err error, a *app) int {
	if err != nil {
		a.errorf("%v", err)
		return ExitError
	}
	return ExitOK
}

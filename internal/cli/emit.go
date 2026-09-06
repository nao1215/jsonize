package cli

import (
	"io"

	"github.com/nao1215/jsonize/internal/jsonutil"
	"github.com/nao1215/jsonize/internal/registry"
)

// emit writes the parse result, optionally wrapped in a metadata envelope.
func emit(w io.Writer, data any, e *registry.Entry, of *outputFlags, extra map[string]any) error {
	if !of.meta {
		return jsonutil.Encode(w, data, of.pretty)
	}
	env := jsonutil.NewObject()
	env.Set("command", e.Def.Command)
	env.Set("variant", e.Def.Variant)
	env.Set("source", e.Source)
	for _, k := range sortedKeys(extra) {
		env.Set(k, extra[k])
	}
	env.Set("data", data)
	return jsonutil.Encode(w, env, of.pretty)
}

package cli

import (
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/internal/schema"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// keyFilter narrows the objects of a result to the keys the caller named.
//
// Naming a key that the format never produces is an error rather than an
// empty answer: the caller believed the key was there, and a JSON
// document quietly missing what was asked for is the kind of wrong
// answer this tool exists to avoid. What the format produces is what its
// definition says, so a key some rows omit, or any key of an empty
// listing, is valid even when this input holds no row with it. Only the
// keys a definition takes from its input (a table naming its columns
// from the header, a key/value map) are looked for in the result, since
// nothing else can say whether they exist.
type keyFilter struct {
	keys map[string]bool
	keep bool // true for --extract, false for --exclude
	// known are the keys a record of the chosen definition can have, and
	// open says it may have others that come from the input.
	known map[string]bool
	open  bool
	// seen and present collect, over every record narrowed, the named
	// keys that appeared and every key there was.
	seen    map[string]bool
	present map[string]bool
}

func newKeyFilter(keys []string, keep bool) *keyFilter {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return &keyFilter{keys: set, keep: keep, open: true, seen: map[string]bool{}, present: map[string]bool{}}
}

// know takes the keys a record can have from the definition's schema and
// refuses, before anything is read, a named key it cannot produce.
func (f *keyFilter) know(def *definition.Definition) error {
	if len(f.keys) == 0 {
		return nil
	}
	f.known, f.open = recordKeys(def)
	if f.open {
		return nil
	}
	var unknown []string
	for k := range f.keys {
		if !f.known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return &unknownKeyError{keys: sortStrings(unknown), present: sortedKeys(f.known)}
	}
	return nil
}

// recordKeys reads the keys of one record, an element of the list or the
// one object, from the schema of what the definition produces.
func recordKeys(def *definition.Definition) (map[string]bool, bool) {
	root := schema.Generate(def, 0)
	rec := root
	if root.Items != nil {
		rec = root.Items
	}
	if rec.Ref != "" {
		rec = root.Def(strings.TrimPrefix(rec.Ref, "#/$defs/"))
	}
	if rec == nil {
		return nil, true
	}
	known := map[string]bool{}
	for _, p := range rec.Properties {
		known[p.Name] = true
	}
	return known, rec.AdditionalProperties != nil
}

// apply narrows v, and for a whole document reports a key it cannot have.
func (f *keyFilter) apply(v any) (any, error) {
	out := f.walk(v)
	return out, f.unseen()
}

// narrowRecord narrows one record of a stream. A key the input decides
// may turn up in a later record, so it is only judged once the stream
// has ended (unseen).
func (f *keyFilter) narrowRecord(v any) any {
	return f.walk(v)
}

// unseen reports a named key the definition does not declare and that no
// record narrowed so far had.
func (f *keyFilter) unseen() error {
	var missing []string
	for k := range f.keys {
		if !f.seen[k] && !f.known[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	present := map[string]bool{}
	for k := range f.known {
		present[k] = true
	}
	for k := range f.present {
		present[k] = true
	}
	return &unknownKeyError{keys: sortStrings(missing), present: sortedKeys(present)}
}

// walk narrows the objects it finds at the top level of the result. A
// value nested inside an object is left alone: the keys a caller names
// are the ones they can see in the document jz would have printed.
func (f *keyFilter) walk(v any) any {
	switch t := v.(type) {
	case *jsonutil.Object:
		return f.narrow(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			out = append(out, f.walk(e))
		}
		return out
	default:
		return v
	}
}

func (f *keyFilter) narrow(o *jsonutil.Object) *jsonutil.Object {
	out := jsonutil.NewObject()
	for _, m := range o.Members() {
		f.present[m.Key] = true
		named := f.keys[m.Key]
		if named {
			f.seen[m.Key] = true
		}
		if named == f.keep {
			out.Set(m.Key, m.Value)
		}
	}
	return out
}

func sortStrings(s []string) []string {
	set := make(map[string]bool, len(s))
	for _, v := range s {
		set[v] = true
	}
	return sortedKeys(set)
}

// unknownKeyError reports keys the result does not have.
type unknownKeyError struct {
	keys    []string
	present []string
}

func (e *unknownKeyError) Error() string {
	var b strings.Builder
	if len(e.keys) == 1 {
		fmt.Fprintf(&b, "no key %q in the output", e.keys[0])
	} else {
		fmt.Fprintf(&b, "no keys %s in the output", quoteList(e.keys))
	}
	if len(e.present) > 0 {
		fmt.Fprintf(&b, "\nthe keys it has are %s", quoteList(e.present))
	}
	return b.String()
}

func quoteList(keys []string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(quoted, ", ")
}

package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nao1215/jsonize/internal/schema"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// maxShownKeys bounds the keys a filter remembers to name in a refusal.
// Whether a named key turned up does not depend on it, only how many of
// the other keys the refusal lists.
const maxShownKeys = 64

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
	// seen collects, over every record narrowed, the named keys that
	// appeared, which decides whether one is unknown. present collects
	// the other keys there were, up to maxShownKeys, only to name them in
	// the refusal; cut says there were more.
	seen    map[string]bool
	present map[string]bool
	cut     bool
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
	if f == nil || len(f.keys) == 0 {
		return nil
	}
	f.known, f.open = recordKeys(def)
	if f.open {
		return nil
	}
	return f.refuse(f.known)
}

// knowBefore refuses, before anything is read or run, a named key that
// none of defs can produce: the definition that reads the input will be
// one of them, so the refusal cannot depend on the input. A key one of
// them may have, or any key when one of them takes its keys from the
// input, is left to know once the definition is chosen.
func (f *keyFilter) knowBefore(defs []*definition.Definition) error {
	if f == nil || len(f.keys) == 0 || len(defs) == 0 {
		return nil
	}
	known := map[string]bool{}
	for _, d := range defs {
		keys, open := recordKeys(d)
		if open {
			return nil
		}
		maps.Copy(known, keys)
	}
	return f.refuse(known)
}

// refuse reports the named keys that known, every key a record can have,
// does not hold.
func (f *keyFilter) refuse(known map[string]bool) error {
	var unknown []string
	for k := range f.keys {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return &unknownKeyError{keys: sortStrings(unknown), present: slices.Sorted(maps.Keys(known))}
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
	if f == nil {
		return v, nil
	}
	out := f.walk(v)
	return out, f.unseen()
}

// narrowRecord narrows one record of a stream. A key the input decides
// may turn up in a later record, so it is only judged once the stream
// has ended (unseen).
func (f *keyFilter) narrowRecord(v any) any {
	if f == nil {
		return v
	}
	return f.walk(v)
}

// streamEmit returns what writes one document of a stream through write.
// A composite is streamed as {"part", "value"} documents, and the keys a
// caller names for it are its parts, the keys of the object the whole
// document would have been: a document for a part left out is not written
// at all, and one for a part kept is written whole.
func (f *keyFilter) streamEmit(write func(any) error, def *definition.Definition) func(any) error {
	if f == nil {
		return write
	}
	if def.Parse.Type != definition.TypeComposite {
		return func(v any) error {
			return write(f.narrowRecord(v))
		}
	}
	return func(v any) error {
		if doc, ok := v.(*jsonutil.Object); ok && len(f.keys) > 0 {
			name, _ := doc.Get("part")
			part, _ := name.(string)
			named := f.keys[part]
			f.saw(part, named)
			if named != f.keep {
				return nil
			}
		}
		return write(v)
	}
}

// unseen reports a named key the definition does not declare and that no
// record narrowed so far had.
func (f *keyFilter) unseen() error {
	if f == nil {
		return nil
	}
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
	return &unknownKeyError{keys: sortStrings(missing), present: slices.Sorted(maps.Keys(present)), cut: f.cut}
}

// saw records a key a record had: in seen when it is named, and in
// present while there is room.
func (f *keyFilter) saw(key string, named bool) {
	if named {
		f.seen[key] = true
	}
	if f.present[key] {
		return
	}
	if len(f.present) >= maxShownKeys {
		f.cut = true
		return
	}
	f.present[key] = true
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
		named := f.keys[m.Key]
		f.saw(m.Key, named)
		if named == f.keep {
			out.Set(m.Key, m.Value)
		}
	}
	return out
}

func sortStrings(s []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(s)))
}

// unknownKeyError reports keys the result does not have.
type unknownKeyError struct {
	keys    []string
	present []string
	// cut says present is some of the keys, not all of them.
	cut bool
}

func (e *unknownKeyError) Error() string {
	var b strings.Builder
	if len(e.keys) == 1 {
		fmt.Fprintf(&b, "no key %q in the output", e.keys[0])
	} else {
		fmt.Fprintf(&b, "no keys %s in the output", quoteList(e.keys))
	}
	switch {
	case len(e.present) > 0 && e.cut:
		fmt.Fprintf(&b, "\nthe keys it has include %s", quoteList(e.present))
	case len(e.present) > 0:
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

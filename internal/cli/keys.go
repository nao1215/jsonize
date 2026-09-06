package cli

import (
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/internal/jsonutil"
)

// keyFilter narrows the objects of a result to the keys the caller named.
//
// Naming a key that the format never produces is an error rather than an
// empty answer: the caller believed the key was there, and a JSON
// document quietly missing what was asked for is the kind of wrong
// answer this tool exists to avoid. The check is against the whole
// result, so a key that some rows omit is still valid as long as one row
// has it.
type keyFilter struct {
	keys map[string]bool
	keep bool // true for --extract, false for --exclude
}

func newKeyFilter(keys []string, keep bool) *keyFilter {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return &keyFilter{keys: set, keep: keep}
}

// apply rewrites v and reports a key that appeared nowhere in it.
func (f *keyFilter) apply(v any) (any, error) {
	seen := map[string]bool{}
	out := f.walk(v, seen)
	var missing []string
	for k := range f.keys {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, &unknownKeyError{keys: sortStrings(missing), present: presentKeys(v)}
	}
	return out, nil
}

// walk narrows the objects it finds at the top level of the result. A
// value nested inside an object is left alone: the keys a caller names
// are the ones they can see in the document jz would have printed.
func (f *keyFilter) walk(v any, seen map[string]bool) any {
	switch t := v.(type) {
	case *jsonutil.Object:
		return f.narrow(t, seen)
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			out = append(out, f.walk(e, seen))
		}
		return out
	default:
		return v
	}
}

func (f *keyFilter) narrow(o *jsonutil.Object, seen map[string]bool) *jsonutil.Object {
	out := jsonutil.NewObject()
	for _, m := range o.Members() {
		named := f.keys[m.Key]
		if named {
			seen[m.Key] = true
		}
		if named == f.keep {
			out.Set(m.Key, m.Value)
		}
	}
	return out
}

// presentKeys lists the keys the result actually has, so the message can
// show the caller what they could have asked for.
func presentKeys(v any) []string {
	set := map[string]bool{}
	var collect func(any)
	collect = func(v any) {
		switch t := v.(type) {
		case *jsonutil.Object:
			for _, m := range t.Members() {
				set[m.Key] = true
			}
		case []any:
			for _, e := range t {
				collect(e)
			}
		}
	}
	collect(v)
	return sortedKeys(set)
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

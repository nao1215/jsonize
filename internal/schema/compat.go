package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Change is one difference between two versions of a schema.
type Change struct {
	// Path locates the value that changed, in the notation Validate uses
	// ("$[].answers[].ttl"; [] stands for every element of an array).
	Path string
	// Kind names the rule the change falls under.
	Kind string
	// Detail says what it was and what it is.
	Detail string
	// Breaking is true for a change a program reading the output, or a
	// document the old version produced, can be broken by.
	Breaking bool
}

func (c Change) String() string {
	return fmt.Sprintf("%s: %s: %s", c.Path, c.Kind, c.Detail)
}

// The kinds of change Compare reports.
const (
	// KindRemoved is a key the old version produced and the new one does
	// not. A program that reads it finds nothing.
	KindRemoved = "removed"
	// KindType is a change to the types a value can have, which includes
	// becoming nullable or no longer being nullable, and an object
	// becoming an array. A program that handles the old types meets one it
	// does not, or a document the old version wrote no longer fits.
	KindType = "type"
	// KindRequired is a key that became required, including a new key
	// that always appears: a document the old version wrote lacks it.
	KindRequired = "required"
	// KindOptional is a key that is no longer always there: a program
	// that relied on it finds it missing.
	KindOptional = "optional"
	// KindEnum is a change to the set of values a key can take. A value
	// that appears is one a program has no case for; a value that is
	// gone is a state that is no longer reported.
	KindEnum = "enum"
	// KindKeys is a change to whether an object carries keys the input
	// names (a table that takes its columns from its header).
	KindKeys = "keys"
	// KindAdded is a key that may appear and did not before. It is the
	// one change a program that ignores keys it does not know cannot be
	// broken by, and is not breaking.
	KindAdded = "added"
)

// Compare reports how next differs from prev. The contract version and
// the annotations (title, description, $id) are not compared: the
// version is what a breaking change is acknowledged with, and the rest
// describes the schema rather than the output.
//
// Compatibility is judged in both directions, because a schema has two
// kinds of reader. A program that consumes the output must not meet a key
// removed, a type it does not handle, a key it relied on gone missing, or
// a value it has no case for. A document the old version produced, kept
// and validated later against the new schema, must still fit: a key that
// became required, a type or a value that was narrowed away, fails it.
// The one change neither reader notices is a key that may now appear and
// never has to.
func Compare(prev, next *Schema) []Change {
	c := &comparer{prevRoot: prev, nextRoot: next, seen: map[[2]string]bool{}}
	c.compare(prev, next, "$")
	sort.SliceStable(c.out, func(i, j int) bool { return c.out[i].Path < c.out[j].Path })
	return c.out
}

// Breaking returns the breaking changes among changes.
func Breaking(changes []Change) []Change {
	var out []Change
	for _, c := range changes {
		if c.Breaking {
			out = append(out, c)
		}
	}
	return out
}

type comparer struct {
	prevRoot, nextRoot *Schema
	out                []Change
	// seen stops a comparison that follows a pair of $refs from following
	// the same pair again, which is what a tree's children would otherwise
	// do forever. It is the pair and not one name: a tree that moved into
	// a part is described under another name, and its old and new nodes
	// still refer to themselves.
	seen map[[2]string]bool
}

func (c *comparer) add(path, kind, detail string, breaking bool) {
	c.out = append(c.out, Change{Path: path, Kind: kind, Detail: detail, Breaking: breaking})
}

func (c *comparer) resolve(root, s *Schema) (*Schema, string) {
	if s == nil || s.Ref == "" {
		return s, ""
	}
	name, _ := strings.CutPrefix(s.Ref, "#/$defs/")
	if d := root.Def(name); d != nil {
		return d, s.Ref
	}
	return s, s.Ref
}

func (c *comparer) compare(prev, next *Schema, path string) {
	prev, pref := c.resolve(c.prevRoot, prev)
	next, nref := c.resolve(c.nextRoot, next)
	if pref != "" && nref != "" {
		pair := [2]string{pref, nref}
		if c.seen[pair] {
			return
		}
		c.seen[pair] = true
	}
	if !sameTypes(prev.Type, next.Type) {
		detail := fmt.Sprintf("was %s, is %s", typeList(prev.Type), typeList(next.Type))
		switch {
		case prev.Has(TypeObject) && !next.Has(TypeObject) && next.Has(TypeArray):
			detail = "an object became an array"
		case prev.Has(TypeArray) && !prev.Has(TypeObject) && next.Has(TypeObject) && !next.Has(TypeArray):
			detail = "an array became an object"
		}
		c.add(path, KindType, detail, true)
		// Two objects, or two arrays, are still compared inside; anything
		// else has nothing left in common to compare.
		bothObjects := prev.Has(TypeObject) && next.Has(TypeObject)
		bothArrays := prev.Has(TypeArray) && next.Has(TypeArray)
		if !bothObjects && !bothArrays {
			return
		}
	}
	c.compareEnum(prev, next, path)
	if prev.Items != nil || next.Items != nil {
		switch {
		case prev.Items == nil:
			c.add(path+"[]", KindType, "the elements are now described", true)
		case next.Items == nil:
			c.add(path+"[]", KindType, "the elements are no longer described", true)
		default:
			c.compare(prev.Items, next.Items, path+"[]")
		}
	}
	c.compareObject(prev, next, path)
}

func (c *comparer) compareEnum(prev, next *Schema, path string) {
	switch {
	case len(prev.Enum) == 0 && len(next.Enum) == 0:
		return
	case len(prev.Enum) == 0:
		c.add(path, KindEnum, fmt.Sprintf("the values are now limited to %q", next.Enum), true)
		return
	case len(next.Enum) == 0:
		c.add(path, KindEnum, fmt.Sprintf("the values are no longer limited to %q", prev.Enum), true)
		return
	}
	if gone := minus(prev.Enum, next.Enum); len(gone) > 0 {
		c.add(path, KindEnum, fmt.Sprintf("%q can no longer appear", gone), true)
	}
	if added := minus(next.Enum, prev.Enum); len(added) > 0 {
		c.add(path, KindEnum, fmt.Sprintf("%q can now appear", added), true)
	}
}

func (c *comparer) compareObject(prev, next *Schema, path string) {
	for _, p := range prev.Properties {
		sub := next.Prop(p.Name)
		child := path + "." + p.Name
		if sub == nil && next.AdditionalProperties != nil {
			// The key is still produced when the input names it, now as one
			// of the keys the schema does not name: what changed is its type
			// and whether it has to be there.
			if prev.IsRequired(p.Name) {
				c.add(child, KindOptional, "the key was always there and may now be missing", true)
			}
			c.compare(p.Schema, next.AdditionalProperties, child)
			continue
		}
		if sub == nil {
			c.add(child, KindRemoved, "the key is no longer produced", true)
			continue
		}
		switch {
		case prev.IsRequired(p.Name) && !next.IsRequired(p.Name):
			c.add(child, KindOptional, "the key was always there and may now be missing", true)
		case !prev.IsRequired(p.Name) && next.IsRequired(p.Name):
			c.add(child, KindRequired, "the key may have been missing and is now always there", true)
		}
		c.compare(p.Schema, sub, child)
	}
	for _, p := range next.Properties {
		if prev.Prop(p.Name) != nil {
			continue
		}
		child := path + "." + p.Name
		if prev.AdditionalProperties != nil {
			// The key could already appear when the input named it, as one
			// the schema did not name; naming it now is only a change when
			// its type or its presence is.
			if next.IsRequired(p.Name) {
				c.add(child, KindRequired, "the key may have been missing and is now always there", true)
			}
			c.compare(prev.AdditionalProperties, p.Schema, child)
			continue
		}
		if next.IsRequired(p.Name) {
			c.add(child, KindRequired, "a new key that is always there, which a document of the previous version lacks", true)
			continue
		}
		c.add(child, KindAdded, "a new key that may appear", false)
	}
	switch {
	case prev.AdditionalProperties == nil && next.AdditionalProperties == nil:
	case prev.AdditionalProperties == nil:
		c.add(path, KindKeys, "the object now carries keys the input names", true)
	case next.AdditionalProperties == nil:
		c.add(path, KindKeys, "the object no longer carries keys the input names", true)
	default:
		c.compare(prev.AdditionalProperties, next.AdditionalProperties, path+".*")
	}
}

func sameTypes(a, b []string) bool {
	a, b = normalizeTypes(a), normalizeTypes(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// minus returns the elements of a that b lacks.
func minus(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if !set[x] {
			out = append(out, x)
		}
	}
	return out
}

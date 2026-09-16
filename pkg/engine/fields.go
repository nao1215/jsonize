package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nao1215/jsonize/pkg/convert"
	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// raw is what a parser read for one field: its text, and whether it read
// anything, since an empty column of an aligned table and a group that
// took no part in a match read nothing rather than an empty string.
type raw struct {
	text string
	has  bool
}

// some is a raw that holds text.
func some(text string) raw { return raw{text: text, has: true} }

// setField converts what was read with the field rules and stores it in
// obj, honouring when_missing: omit.
func (r *run) setField(obj *jsonutil.Object, name string, v raw, f *definition.Field, ln int) error {
	return r.setMatched(obj, name, v, f, ln, nil)
}

// setMatched is setField for a value a pattern read. present holds the
// groups that matched some text, which is what an unescape rule's when
// is decided by.
func (r *run) setMatched(obj *jsonutil.Object, name string, v raw, f *definition.Field, ln int, present map[string]bool) error {
	converted, omit, err := r.convertMatched(name, v, f, ln, present)
	if err != nil {
		return err
	}
	if omit {
		return nil
	}
	obj.Set(name, converted)
	return r.countValue(ln)
}

// convert applies a field rule. The bool result is true when the value
// should be omitted from its parent object.
func (r *run) convert(name, text string, f *definition.Field, ln int) (any, bool, error) {
	return r.convertMatched(name, some(text), f, ln, nil)
}

func (r *run) convertMatched(name string, v raw, f *definition.Field, ln int, present map[string]bool) (any, bool, error) {
	if r.opts.Raw {
		// The field rules are the whole of what raw mode leaves out, and
		// they are all applied from here, so this is the one place that
		// has to know about it. when_missing and required are field rules
		// too: a shape that changed with the values in it would defeat
		// the point of looking at what was extracted.
		if !v.has {
			return nil, false, nil
		}
		return v.text, false, nil
	}
	if !v.has {
		if f != nil && f.Required {
			return nil, false, r.errorf(ln, name, "required value is missing")
		}
		return nil, f != nil && f.WhenMissing == definition.MissingOmit, nil
	}
	s := v.text
	if f == nil {
		return s, false, nil
	}
	// Only an explicit trim_prefix/trim_suffix rewrites the text. A value
	// that a parser kept verbatim (a key/value line with trim: false)
	// reaches JSON with its spacing intact; the numeric conversions trim
	// on their own where whitespace is never meaningful.
	if f.TrimPrefix != "" || f.TrimSuffix != "" {
		s = convert.Strip(s, f.TrimPrefix, f.TrimSuffix)
	}
	if f.EffectiveType() == definition.FieldString {
		kept, ok, err := r.stringRules(name, s, f, ln, present)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return r.convertMatched(name, raw{}, f, ln, present)
		}
		s = kept
	}
	trimmed := strings.TrimSpace(s)
	// A value null_if names was printed: it is the command's own word for
	// no value ("N/A", "None"), so a required field that holds one is
	// null rather than missing.
	for _, n := range f.NullIf {
		if trimmed == n {
			return nil, f.WhenMissing == definition.MissingOmit, nil
		}
	}
	if f.Required && trimmed == "" {
		return nil, false, r.errorf(ln, name, "required value is empty")
	}
	converted, err := r.convertScalar(name, s, f, ln)
	if err != nil {
		return nil, false, err
	}
	return converted, false, nil
}

// stringRules applies what a string field says about its text: the
// regex whose one group is the value, then the escapes to undo. The bool
// result is false when the group did not take part, which leaves the
// value missing.
func (r *run) stringRules(name, s string, f *definition.Field, ln int, present map[string]bool) (string, bool, error) {
	if re := f.CompiledRegex(); re != nil {
		m := re.FindStringSubmatchIndex(s)
		if m == nil {
			return "", false, r.errorf(ln, name, "value %q does not have the shape %s", truncate(s, 80), shortPattern(f.Regex))
		}
		i := re.SubexpIndex(f.Group())
		if m[2*i] < 0 {
			return "", false, nil
		}
		s = s[m[2*i]:m[2*i+1]]
	}
	if u := f.Unescape; u != nil && (u.When == "" || present[u.When]) {
		decoded, ok := u.Decode(s)
		if !ok {
			return "", false, r.errorf(ln, name, "value %q holds an escape the format does not write", truncate(s, 80))
		}
		s = decoded
	}
	return s, true, nil
}

func (r *run) convertScalar(name, s string, f *definition.Field, ln int) (any, error) {
	wrap := func(err error) error {
		return &ParseError{Definition: r.def.ID(), Line: ln, Field: name, Cause: err}
	}
	switch f.EffectiveType() {
	case definition.FieldString:
		return s, nil
	case definition.FieldInt:
		v, err := convert.Int(s)
		if errors.Is(err, strconv.ErrRange) {
			if n, ok := wideInt(s); ok {
				return n, nil
			}
		}
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldFloat:
		v, err := convert.Float(s)
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldBool:
		v, err := convert.Bool(s, f.True, f.False)
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldTime:
		loc, _ := convert.Location(f.Location)
		v, _, err := convert.TimeAssuming(s, f.Layout, loc, r.opts.Assume)
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldDuration:
		v, err := convert.Duration(s, f.Layout)
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldArray:
		return r.convertArray(name, s, f, ln)
	case definition.FieldObject:
		return r.convertObject(name, s, f, ln)
	default:
		return nil, r.errorf(ln, name, "unsupported field type %q", f.Type)
	}
}

func (r *run) convertArray(name, s string, f *definition.Field, ln int) (any, error) {
	if s == "" {
		return []any{}, nil
	}
	var parts []string
	if re := f.CompiledSplit(); re != nil {
		parts = re.Split(s, -1)
	} else {
		parts = strings.Split(s, f.Split)
	}
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if f.Items == nil {
			out = append(out, p)
			continue
		}
		v, omit, err := r.convert(name, p, f.Items, ln)
		if err != nil {
			return nil, err
		}
		if omit {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *run) convertObject(name, s string, f *definition.Field, ln int) (any, error) {
	re := f.CompiledRegex()
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil, r.errorf(ln, name, "value %q does not match %s", truncate(s, 80), shortPattern(f.Regex))
	}
	// The value was read as a whole by the parser, so what the pattern
	// matches around is text that would otherwise vanish between the
	// value and the object made of it.
	if text, _, ok := outside(s, m[0], m[1]); ok {
		return nil, &ParseError{Definition: r.def.ID(), Line: ln, Field: name,
			Msg: fmt.Sprintf("the pattern %s does not reach %q in %q", shortPattern(f.Regex), text, truncate(s, 80)), Cause: ErrUnread}
	}
	obj := jsonutil.NewObject()
	// The object's own match is the context its sub-fields are read in:
	// an unescape rule's when names a group of this pattern, and it is
	// decided by whether that group took part here.
	present := presentGroups(re, m, f.Fields)
	for i, gname := range re.SubexpNames() {
		if gname == "" {
			continue
		}
		if err := r.setMatched(obj, gname, group(s, m, i), f.Fields[gname], ln, present); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// wideInt reads an integer too large for 64 bits as the JSON number its
// digits write: an exact figure a command printed (a limit of 2^64 - 4)
// is still that figure, and JSON puts no bound on the digits of one. The
// sign and the zeros in front, which JSON does not allow, are left out
// the way reading it as an int64 leaves them out.
func wideInt(s string) (json.Number, bool) {
	t := strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(t, "-"):
		neg, t = true, t[1:]
	case strings.HasPrefix(t, "+"):
		t = t[1:]
	}
	if t == "" || strings.Trim(t, "0123456789") != "" {
		return "", false
	}
	t = strings.TrimLeft(t, "0")
	if t == "" {
		return "0", true
	}
	if neg {
		t = "-" + t
	}
	return json.Number(t), true
}

package engine

import (
	"strings"

	"github.com/nao1215/jsonize/internal/convert"
	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/jsonutil"
)

// setField converts raw (a string or nil) with the field rules and stores
// it in obj, honouring when_missing: omit.
func (r *run) setField(obj *jsonutil.Object, name string, raw any, f *definition.Field, ln int) error {
	v, omit, err := r.convert(name, raw, f, ln)
	if err != nil {
		return err
	}
	if omit {
		return nil
	}
	obj.Set(name, v)
	return nil
}

// convert applies a field rule. The bool result is true when the value
// should be omitted from its parent object.
func (r *run) convert(name string, raw any, f *definition.Field, ln int) (any, bool, error) {
	if raw == nil {
		if f != nil && f.Required {
			return nil, false, r.errorf(ln, name, "required value is missing")
		}
		return nil, f != nil && f.WhenMissing == definition.MissingOmit, nil
	}
	s, _ := raw.(string)
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
	trimmed := strings.TrimSpace(s)
	for _, n := range f.NullIf {
		if trimmed == n {
			if f.Required {
				return nil, false, r.errorf(ln, name, "required value is null")
			}
			return nil, f.WhenMissing == definition.MissingOmit, nil
		}
	}
	if f.Required && trimmed == "" {
		return nil, false, r.errorf(ln, name, "required value is empty")
	}
	v, err := r.convertScalar(name, s, f, ln)
	if err != nil {
		return nil, false, err
	}
	return v, false, nil
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
	case definition.FieldSize:
		base := convert.Binary
		if f.Unit == definition.UnitDecimal {
			base = convert.Decimal
		}
		v, err := convert.Size(s, base)
		if err != nil {
			return nil, wrap(err)
		}
		return v, nil
	case definition.FieldTime:
		loc, _ := convert.Location(f.Location)
		v, err := convert.Time(s, f.Layout, loc)
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
	obj := jsonutil.NewObject()
	for i, gname := range re.SubexpNames() {
		if gname == "" {
			continue
		}
		var raw any
		if m[2*i] >= 0 {
			raw = s[m[2*i]:m[2*i+1]]
		}
		if err := r.setField(obj, gname, raw, f.Fields[gname], ln); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

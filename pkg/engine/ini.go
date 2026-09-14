package engine

import (
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// parseINI handles type: ini.
//
// A [section] heading opens an outer key and every key = value line
// under it becomes an inner one. Keys written before the first heading
// go under the empty name, which is what a file with a preamble looks
// like and what the systemd and git configurations do not have. The
// result is an object of objects, so it has no streaming form.
//
// A key written twice under one section is an error: an object keeps one
// value per key, so the other would be missing from the result, and an
// array instead would mean a key's type depended on how many times it
// appeared.
func (r *run) parseINI(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	sep := p.Separator
	if sep == "" {
		sep = "="
	}
	trim := p.TrimCells()
	root := jsonutil.NewObject()
	section := jsonutil.NewObject()
	name := ""
	root.Set(name, section)
	// A section written twice continues the first, so a key is repeated
	// when it appears twice under one name, wherever the two are.
	keys := map[string]*keyLines{name: {}}
	for _, l := range lines {
		text := strings.TrimSpace(l.text)
		if text == "" || isINIComment(text) {
			continue
		}
		if heading, ok := iniSection(text); ok {
			name = heading
			if existing, found := root.Get(name); found {
				// A section written twice continues the first one, which
				// is what a file assembled from fragments looks like.
				if obj, ok := existing.(*jsonutil.Object); ok {
					section = obj
					continue
				}
			}
			section = jsonutil.NewObject()
			root.Set(name, section)
			keys[name] = &keyLines{}
			continue
		}
		idx := strings.Index(text, sep)
		key := ""
		if idx >= 0 {
			key = text[:idx]
			if trim {
				key = strings.TrimSpace(key)
			}
		}
		if idx < 0 || key == "" {
			return nil, r.errorf(l.num, "", "expected \"key%svalue\" or a [section] heading: %q", sep, truncate(text, 80))
		}
		value := text[idx+len(sep):]
		if trim {
			value = strings.TrimSpace(value)
		}
		if p.Unquote {
			value = unquote(value)
		}
		if err := keys[name].add(r, key, l.num); err != nil {
			return nil, err
		}
		if err := r.setField(section, key, some(value), fields[key], l.num); err != nil {
			return nil, err
		}
	}
	// The nameless section is only there when the file put something in
	// it; an empty one would be a key every consumer had to skip.
	if first, _ := root.Get(""); first.(*jsonutil.Object).Len() == 0 { //nolint:errcheck,forcetypeassert // set above and never replaced
		root.Delete("")
	}
	return root, nil
}

// isINIComment reports the two comment markers every dialect agrees on.
// They are only comments at the start of a line: a value may contain
// either character, and treating one as the start of a comment would cut
// a password or a path in half.
func isINIComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, ";")
}

// iniSection reads a "[name]" heading.
func iniSection(text string) (string, bool) {
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return "", false
	}
	return strings.TrimSpace(text[1 : len(text)-1]), true
}

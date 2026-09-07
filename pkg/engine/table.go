package engine

import (
	"strings"
	"unicode"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// column is a resolved table column: its name and, for aligned tables, the
// rune offset where its header starts.
type column struct {
	name  string
	start int
}

// parseTable handles type: table.
func (r *run) parseTable(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	split := p.Split
	if split == "" {
		split = definition.SplitWhitespace
	}
	if split == definition.SplitBox {
		// A drawn table has its own idea of where a row ends, so it does
		// not go through the line-per-row loop below.
		return r.parseBox(p, fields, lines)
	}
	var (
		cols   []column
		header line
	)
	if p.Header.None {
		cols = make([]column, len(p.Header.Columns))
		for i, c := range p.Header.Columns {
			cols[i] = column{name: c}
		}
	} else {
		if len(lines) == 0 {
			return []any{}, nil
		}
		header = lines[0]
		lines = lines[1:]
		var err error
		cols, err = r.resolveHeader(p, header, split)
		if err != nil {
			return nil, err
		}
	}
	out := make([]any, 0, len(lines))
	for _, l := range lines {
		var cells []any
		var err error
		switch split {
		case definition.SplitAligned:
			cells = alignedCells(l.text, cols)
		case definition.SplitDelimiter:
			cells, err = r.delimitedCells(p, l, len(cols))
		default:
			cells, err = r.whitespaceCells(p, l, len(cols))
		}
		if err != nil {
			return nil, err
		}
		obj := jsonutil.NewObject()
		for i, c := range cols {
			var raw any
			if i < len(cells) {
				raw = cells[i]
			}
			if err := r.setField(obj, c.name, raw, fields[c.name], l.num); err != nil {
				return nil, err
			}
		}
		out = append(out, obj)
	}
	return out, nil
}

// resolveHeader derives the column list from the header line and the
// definition's header settings.
func (r *run) resolveHeader(p *definition.Parse, header line, split string) ([]column, error) {
	h := &p.Header
	var toks []token
	switch split {
	case definition.SplitDelimiter:
		for i, cell := range strings.Split(header.text, p.Delimiter) {
			toks = append(toks, token{text: strings.TrimSpace(cell), start: i})
		}
	default:
		toks = tokenize(header.text)
	}
	if h.LeadingLabel != "" {
		toks = append([]token{{text: h.LeadingLabel, start: 0}}, toks...)
	}
	if len(toks) > definition.MaxColumns {
		return nil, r.errorf(header.num, "", "header has more than %d columns", definition.MaxColumns)
	}
	cols := make([]column, 0, len(toks))
	seen := map[string]bool{}
	if len(h.Columns) > 0 {
		// Aligned tables need one header token per declared column; extra
		// trailing tokens ("Mounted on") belong to the last column, which
		// extends to the end of the line anyway.
		if split == definition.SplitAligned && len(toks) < len(h.Columns) {
			return nil, r.errorf(header.num, "", "header has %d columns but the definition declares %d: %q", len(toks), len(h.Columns), truncate(header.text, 80))
		}
		for i, name := range h.Columns {
			start := 0
			if i < len(toks) {
				start = toks[i].start
			}
			cols = append(cols, column{name: name, start: start})
		}
		return cols, nil
	}
	if len(toks) == 0 {
		return nil, r.errorf(header.num, "", "header line is empty")
	}
	for i, t := range toks {
		name := definition.NormalizeName(t.text)
		if i == 0 && h.LeadingLabel != "" {
			name = h.LeadingLabel
		}
		if renamed, ok := h.Rename[name]; ok {
			name = renamed
		}
		if name == "" {
			return nil, r.errorf(header.num, "", "header column %d (%q) normalises to an empty name", i+1, t.text)
		}
		if seen[name] {
			return nil, r.errorf(header.num, "", "header has duplicate column %q; declare header.columns explicitly", name)
		}
		seen[name] = true
		cols = append(cols, column{name: name, start: t.start})
	}
	return cols, nil
}

// token is a whitespace-delimited word with its rune offset.
type token struct {
	text  string
	start int
}

// tokenize splits s on runs of whitespace, recording rune offsets.
func tokenize(s string) []token {
	var toks []token
	start := -1
	var b strings.Builder
	pos := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				toks = append(toks, token{text: b.String(), start: start})
				b.Reset()
				start = -1
			}
		} else {
			if start < 0 {
				start = pos
			}
			b.WriteRune(r)
		}
		pos++
	}
	if start >= 0 {
		toks = append(toks, token{text: b.String(), start: start})
	}
	return toks
}

// whitespaceCells splits a row on whitespace into at most len(cols)
// (or max_fields) cells; the last cell absorbs the remainder of the line.
func (r *run) whitespaceCells(p *definition.Parse, l line, ncols int) ([]any, error) {
	limit := p.MaxFields
	if limit == 0 {
		limit = ncols
	}
	parts := splitFieldsN(l.text, limit)
	return r.checkCount(p, l, parts, ncols)
}

// delimitedCells splits a row on the delimiter.
func (r *run) delimitedCells(p *definition.Parse, l line, ncols int) ([]any, error) {
	limit := p.MaxFields
	if limit == 0 {
		limit = ncols
	}
	if limit <= 0 {
		limit = -1
	}
	raw := strings.SplitN(l.text, p.Delimiter, limit)
	parts := make([]string, len(raw))
	for i, s := range raw {
		parts[i] = strings.TrimSpace(s)
	}
	return r.checkCount(p, l, parts, ncols)
}

func (r *run) checkCount(p *definition.Parse, l line, parts []string, ncols int) ([]any, error) {
	minFields := p.MinFields
	if minFields == 0 {
		minFields = ncols
	}
	if len(parts) < minFields {
		return nil, r.errorf(l.num, "", "expected at least %d fields but found %d: %q", minFields, len(parts), truncate(l.text, 80))
	}
	if len(parts) > ncols {
		return nil, r.errorf(l.num, "", "expected at most %d fields but found %d: %q", ncols, len(parts), truncate(l.text, 80))
	}
	cells := make([]any, len(parts))
	for i, s := range parts {
		cells[i] = s
	}
	return cells, nil
}

// splitFieldsN behaves like strings.Fields but returns at most n elements,
// with the final element holding the untouched remainder (trimmed).
func splitFieldsN(s string, n int) []string {
	if n <= 0 {
		return strings.Fields(s)
	}
	var out []string
	rest := strings.TrimSpace(s)
	for len(out) < n-1 && rest != "" {
		i := strings.IndexFunc(rest, unicode.IsSpace)
		if i < 0 {
			break
		}
		out = append(out, rest[:i])
		rest = strings.TrimLeftFunc(rest[i:], unicode.IsSpace)
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// alignedCells slices a row by the header column offsets. A value that
// crosses the nominal boundary between two columns (a right-aligned number
// that is wider than its header) is assigned to the column on its right,
// mirroring how humans read such tables. Empty cells become nil.
func alignedCells(text string, cols []column) []any {
	runes := []rune(text)
	n := len(runes)
	cells := make([]any, len(cols))
	prevEnd := 0
	for i := range cols {
		start := prevEnd
		end := n
		if i+1 < len(cols) {
			end = cols[i+1].start
			if end > n {
				end = n
			}
			if end < start {
				end = start
			}
			// The next column's value may start before its header does
			// (right-aligned numbers). Walk left from the boundary to the
			// previous whitespace.
			if end > start && end < n && !unicode.IsSpace(runes[end]) && !unicode.IsSpace(runes[end-1]) {
				j := end
				for j > start && !unicode.IsSpace(runes[j-1]) {
					j--
				}
				if j > start {
					end = j
				} else {
					// No whitespace on the left: the token began in this
					// column and overflows to the right. Keep it whole.
					for end < n && !unicode.IsSpace(runes[end]) {
						end++
					}
				}
			}
		}
		cell := strings.TrimSpace(string(runes[start:end]))
		if cell == "" {
			cells[i] = nil
		} else {
			cells[i] = cell
		}
		prevEnd = end
	}
	return cells
}

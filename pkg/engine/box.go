package engine

import (
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// The characters a drawn table is made of. Unicode gives them a block of
// their own (Box Drawing, U+2500 to U+257F), so the whole block counts
// rather than a list of the ones seen so far: duf draws its frame with
// arc corners, MySQL with ASCII, and the next tool will pick something
// else again.
const (
	boxDrawingFirst = 0x2500
	boxDrawingLast  = 0x257F
)

// boxVerticals are the characters that separate two cells: the ASCII bar
// and the box-drawing ones that stand upright.
const boxVerticals = "|│┃║╎╏╽╿"

// boxRuleASCII are the characters a rule is drawn from outside the
// Unicode block.
const boxRuleASCII = "-+=~"

// isBoxVertical reports a character that separates two cells.
func isBoxVertical(r rune) bool { return strings.ContainsRune(boxVerticals, r) }

// isBoxDrawing reports a character a rule may be drawn from.
func isBoxDrawing(r rune) bool {
	return (r >= boxDrawingFirst && r <= boxDrawingLast) || strings.ContainsRune(boxRuleASCII, r)
}

// isBoxRule reports a line that draws a rule rather than carrying
// values. Whitespace is ignored, and a line with nothing else on it is
// one, so the frame around a table and the rule under its header are
// both recognised without the definition describing either.
func isBoxRule(text string) bool {
	found := false
	for _, r := range text {
		switch {
		case r == ' ' || r == '\t':
		case isBoxDrawing(r), isBoxVertical(r):
			found = true
		default:
			return false
		}
	}
	return found
}

// boxCells cuts one drawn line into its cells on the vertical bars. The
// text before the first bar and after the last is the frame when it is
// blank, and is dropped; a table drawn without an outer frame has values
// there instead, and those are kept. An empty cell in the middle is kept
// as an empty one.
func boxCells(text string) []string {
	if !strings.ContainsAny(text, boxVerticals) {
		return nil
	}
	var cells []string
	var cur strings.Builder
	for _, r := range text {
		if isBoxVertical(r) {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	if len(cells) > 1 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 1 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// boxBlocks cuts the lines of a drawn table at its rules. The rules are
// what separates the header from the body; inside a block it is the
// lines themselves that separate the rows.
func boxBlocks(lines []line) [][]line {
	var blocks [][]line
	var cur []line
	for _, l := range lines {
		if isBoxRule(l.text) {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	return blocks
}

// boxBodyRows cuts one block of the body into rows. A line is a row of
// its own, which is what MySQL, psql and duf print: they draw a rule
// around the table and under the header and nowhere else. A line whose
// first cell is empty continues the row above it, which is how a table
// that wraps a long value writes the rest of it.
//
// A row whose first column is genuinely blank cannot be told from a
// continuation, because in this format they are the same line. A table
// with such a column is one to read some other way.
func boxBodyRows(block []line) [][]line {
	var rows [][]line
	for _, l := range block {
		cells := boxCells(l.text)
		if len(rows) > 0 && (len(cells) == 0 || cells[0] == "") {
			rows[len(rows)-1] = append(rows[len(rows)-1], l)
			continue
		}
		rows = append(rows, []line{l})
	}
	return rows
}

// boxJoin merges the cells of the lines that make up one row. join is
// "\n" for a value, which is how the pieces were printed, and "_" for a
// header, whose pieces are one name broken over two lines.
func boxJoin(group []line, join string) []any {
	width := 0
	cells := make([][]string, 0, len(group))
	for _, l := range group {
		c := boxCells(l.text)
		if len(c) > width {
			width = len(c)
		}
		cells = append(cells, c)
	}
	out := make([]any, width)
	for i := range width {
		var pieces []string
		for _, c := range cells {
			if i < len(c) && c[i] != "" {
				pieces = append(pieces, c[i])
			}
		}
		if len(pieces) == 0 {
			out[i] = nil // an empty cell, which is not the empty string
			continue
		}
		out[i] = strings.Join(pieces, join)
	}
	return out
}

// parseBox reads a table drawn with rules. The rules say where the rows
// are and the vertical bars say where the cells are, so nothing has to be
// counted or aligned: unlike split: aligned, a value wider than its
// column cannot shift a boundary.
func (r *run) parseBox(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	for _, l := range lines {
		if err := r.boxLine(l); err != nil {
			return nil, err
		}
	}
	blocks := boxBlocks(lines)
	if len(blocks) == 0 {
		return []any{}, nil
	}
	cols, err := r.boxColumns(p, blocks[0])
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, block := range blocks[1:] {
		for _, row := range boxBodyRows(block) {
			obj, err := r.boxObject(cols, boxJoin(row, "\n"), fields, row[0].num)
			if err != nil {
				return nil, err
			}
			out = append(out, obj)
		}
	}
	return out, nil
}

// boxLine refuses a line that is neither a rule nor cut by a bar. Such a
// line has no cells, so as a continuation it would add nothing to the
// row above and its text would be gone.
func (r *run) boxLine(l line) error {
	if isBoxRule(l.text) || strings.ContainsAny(l.text, boxVerticals) {
		return nil
	}
	return r.errorf(l.num, "", "a line of a drawn table with no cell in it: %q", truncate(l.text, 80))
}

// boxObject builds the object for one row. A cell past the last column has
// no name to go under, so a value in one is refused rather than left out
// of the object; an empty one is only the frame.
func (r *run) boxObject(cols []column, cells []any, fields map[string]*definition.Field, ln int) (*jsonutil.Object, error) {
	for i := len(cols); i < len(cells); i++ {
		if text, ok := cells[i].(string); ok {
			return nil, r.errorf(ln, "", "cell %d has no column to go under (the header names %d): %q", i+1, len(cols), truncate(text, 80))
		}
	}
	obj := jsonutil.NewObject()
	for i, c := range cols {
		var raw any
		if i < len(cells) {
			raw = cells[i]
		}
		if err := r.setField(obj, c.name, raw, fields[c.name], ln); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// boxColumns names the columns from the header row of a drawn table.
func (r *run) boxColumns(p *definition.Parse, header []line) ([]column, error) {
	if len(p.Header.Columns) > 0 {
		cols := make([]column, len(p.Header.Columns))
		for i, c := range p.Header.Columns {
			cols[i] = column{name: c}
		}
		return cols, nil
	}
	cells := boxJoin(header, "_")
	cols := make([]column, 0, len(cells))
	seen := map[string]bool{}
	for i, c := range cells {
		text, _ := c.(string)
		name := definition.NormalizeName(text)
		if renamed, ok := p.Header.Rename[name]; ok {
			name = renamed
		}
		if name == "" {
			return nil, r.errorf(header[0].num, "", "column %d of the header has no name", i+1)
		}
		if seen[name] {
			return nil, r.errorf(header[0].num, "", "the header names %q twice", name)
		}
		seen[name] = true
		cols = append(cols, column{name: name})
	}
	if len(cols) == 0 {
		return nil, r.errorf(header[0].num, "", "the first row of the table has no cells")
	}
	return cols, nil
}

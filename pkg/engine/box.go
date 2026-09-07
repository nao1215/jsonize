package engine

import (
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// boxVerticals are the characters that separate two cells of a drawn
// table: the ASCII bar and the light, heavy and double box-drawing ones.
const boxVerticals = "|│┃║"

// boxRuleRunes are what a rule between rows is drawn from, on top of the
// verticals above: the ASCII dash, plus and equals sign, and the
// box-drawing horizontals and junctions.
const boxRuleRunes = "-+=" +
	"─━┄┅┈┉═" + // horizontals
	"┌┍┎┏┐┑┒┓" + // upper corners
	"└┕┖┗┘┙┚┛" + // lower corners
	"├┝┞┟┠┡┢┣" + // left tees
	"┤┥┦┧┨┩┪┫" + // right tees
	"┬┭┮┯┰┱┲┳" + // top tees
	"┴┵┶┷┸┹┺┻" + // bottom tees
	"┼┽┾┿╀╁╂╃" + // crosses
	"╄╅╆╇╈╉╊╋" +
	"╔╗╚╝╠╣╦╩╬" + // double
	"╒╓╕╖╘╙╛╜" +
	"╞╟╡╢╤╥╧╨╪╫"

// isBoxRule reports a line that draws a rule rather than carrying
// values. Whitespace is ignored, and a line with nothing else on it is
// one, so the frame around a table and the rule under its header are
// both recognised without the definition describing either.
func isBoxRule(text string) bool {
	found := false
	for _, r := range text {
		switch {
		case r == ' ' || r == '\t':
		case strings.ContainsRune(boxRuleRunes, r), strings.ContainsRune(boxVerticals, r):
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
		if strings.ContainsRune(boxVerticals, r) {
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

// boxRows groups the lines of a drawn table into rows. A rule closes the
// row above it, so a value wrapped over several lines is one row and its
// pieces are joined with a newline.
func boxRows(lines []line) [][]line {
	var rows [][]line
	var cur []line
	for _, l := range lines {
		if isBoxRule(l.text) {
			if len(cur) > 0 {
				rows = append(rows, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
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
	rows := boxRows(lines)
	if len(rows) == 0 {
		return []any{}, nil
	}
	cols, err := r.boxColumns(p, rows[0])
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows)-1)
	for _, group := range rows[1:] {
		obj := jsonutil.NewObject()
		cells := boxJoin(group, "\n")
		for i, c := range cols {
			var raw any
			if i < len(cells) {
				raw = cells[i]
			}
			if err := r.setField(obj, c.name, raw, fields[c.name], group[0].num); err != nil {
				return nil, err
			}
		}
		out = append(out, obj)
	}
	return out, nil
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

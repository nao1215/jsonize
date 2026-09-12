package engine

import (
	"encoding/csv"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// parseCSV handles type: csv.
//
// The lines are joined back together before they are read, because a
// quoted value may contain the line break that split them. Everything
// before this point still applies: input.ignore and input.select choose
// which lines take part, and joining what they left is what a CSV of
// those lines would be.
func (r *run) parseCSV(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	if len(lines) == 0 {
		return []any{}, nil
	}
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	rows, err := readCSV(strings.Join(texts, "\n"), p)
	if err != nil {
		return nil, r.errorf(lines[0].num+csvLine(err)-1, "", "%s", err.Error())
	}
	var (
		cols   []string
		header []string
	)
	if p.Header.None {
		cols = p.Header.Columns
		if len(cols) == 0 && len(rows) > 0 {
			cols = csvNumbered(len(rows[0]))
		}
	} else {
		if len(rows) == 0 {
			return []any{}, nil
		}
		cols, header = csvColumns(p, rows[0]), rows[0]
		rows = rows[1:]
	}
	out := make([]any, 0, len(rows))
	for i, row := range rows {
		if header != nil && slices.Equal(row, header) {
			continue // the header of a second file joined to the first
		}
		obj, err := r.csvRow(p, fields, cols, row, lines[0].num+i)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

// csvRow builds one object. A row shorter than the header leaves the
// remaining keys null rather than dropping them, so every object of a
// document carries the same keys; a row longer than the header is an
// error, because a value with no column to go under has nowhere to be
// reported.
func (r *run) csvRow(p *definition.Parse, fields map[string]*definition.Field, cols, row []string, ln int) (*jsonutil.Object, error) {
	if len(row) > len(cols) {
		switch {
		case !p.Header.None:
			return nil, r.errorf(ln, "", "row has %d fields but the header names %d", len(row), len(cols))
		case len(p.Header.Columns) == 0:
			return nil, r.errorf(ln, "", "row has %d fields but the first record, which numbers the columns, has %d", len(row), len(cols))
		default:
			return nil, r.errorf(ln, "", "row has %d fields but %d columns are named", len(row), len(cols))
		}
	}
	obj := jsonutil.NewObject()
	for i, name := range cols {
		var raw any
		if i < len(row) {
			raw = row[i]
		}
		if err := r.setField(obj, name, raw, fields[name], ln); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// csvNumbered names the columns of a csv that has no header line and
// whose definition names none: column_1, column_2 and so on, as many as
// the first record has. The first record is data like every other one;
// it is only what says how many columns there are, which is the one
// thing a stream can know before the rest has arrived.
func csvNumbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "column_" + strconv.Itoa(i+1)
	}
	return out
}

// csvColumns names the columns: the ones the definition states, or the
// first row normalised the way a table header is.
func csvColumns(p *definition.Parse, header []string) []string {
	if len(p.Header.Columns) > 0 {
		return p.Header.Columns
	}
	out := make([]string, len(header))
	used := map[string]int{}
	for i, cell := range header {
		name := definition.NormalizeName(cell)
		if renamed, ok := p.Header.Rename[name]; ok {
			name = renamed
		}
		if name == "" {
			name = "column"
		}
		// A spreadsheet exports two columns under one heading often
		// enough that refusing the file would be the wrong answer;
		// numbering the repeats keeps every value reachable.
		if n := used[name]; n > 0 {
			used[name] = n + 1
			name = name + "_" + strconv.Itoa(n+1)
		} else {
			used[name] = 1
		}
		out[i] = name
	}
	return out
}

// readCSV reads the whole text with the definition's delimiter.
func readCSV(text string, p *definition.Parse) ([][]string, error) {
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = csvDelimiter(p)
	// A CSV of command output is not required to be rectangular, and the
	// row builder is what reports a row that does not fit its header.
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false
	cr.ReuseRecord = false
	rows, err := cr.ReadAll()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return rows, nil
}

func csvDelimiter(p *definition.Parse) rune {
	if d := []rune(p.Delimiter); len(d) == 1 {
		return d[0]
	}
	return ','
}

// csvLine returns the 1-based line a csv failure names, so the message
// can point at the input rather than at the joined text.
func csvLine(err error) int {
	var pe *csv.ParseError
	if errors.As(err, &pe) && pe.Line > 0 {
		return pe.Line
	}
	return 1
}

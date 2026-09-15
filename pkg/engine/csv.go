package engine

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// parseCSV handles type: csv.
//
// A quoted value may hold the line break that split the input, so the
// lines are first grouped into records, a record being the lines from
// one that opens outside a quoted value to the one that closes it. At
// the top level that grouping has already happened, before the lines
// were folded, selected or dropped, and every line here is one record;
// a csv part of a composite reads the physical lines of its region, and
// the grouping happens here. Each record is then read on its own, so
// that a row is reported on the line it stands on.
func (r *run) parseCSV(p *definition.Parse, fields map[string]*definition.Field, lines []line) (any, error) {
	if len(lines) == 0 {
		return []any{}, nil
	}
	var (
		cols   []string
		header []string
	)
	if p.Header.None {
		cols = p.Header.Columns
	}
	out := make([]any, 0, len(lines))
	for _, rec := range csvRecords(p, lines) {
		rows, err := readCSV(rec.text, p)
		if err != nil {
			ln, msg := csvFailure(rec, err)
			return nil, r.errorf(ln, "", "%s", msg)
		}
		for _, row := range rows {
			switch {
			case cols != nil:
			case p.Header.None:
				cols = csvNumbered(len(row))
				if err := r.checkColumns(fields, cols, rec.num); err != nil {
					return nil, err
				}
			default:
				cols, header = csvColumns(p, row), row
				if err := r.checkColumns(fields, cols, rec.num); err != nil {
					return nil, err
				}
				continue
			}
			// A csv file is data: a row that holds the header's values is
			// a row, unless the definition says the header is printed
			// again, as a command that prints a report per interval does.
			if p.RepeatedHeader() && slices.Equal(row, header) {
				continue
			}
			obj, err := r.csvRow(p, fields, cols, row, rec.num)
			if err != nil {
				return nil, err
			}
			out = append(out, obj)
		}
	}
	return out, nil
}

// csvRecords groups lines into csv records: a line that ends inside a
// quoted value is joined with the ones after it up to the line that
// closes the value. A record keeps the number of the line it starts on.
// Lines that are records already pass through as they are.
func csvRecords(p *definition.Parse, lines []line) []line {
	q := newCSVQuote(csvDelimiter(p))
	out := lines[:0:0]
	var pieces []string
	num := 0
	for _, l := range lines {
		if pieces == nil {
			num = l.num
		}
		pieces = append(pieces, l.text)
		if q.feed(l.text) {
			continue
		}
		out = append(out, line{text: strings.Join(pieces, "\n"), num: num})
		pieces = nil
	}
	if pieces != nil {
		// A quoted value that never closes is a record the input did not
		// finish, and reporting it is better than dropping it.
		out = append(out, line{text: strings.Join(pieces, "\n"), num: num})
	}
	return out
}

// csvQuote follows the quoting rules of a csv text one line at a time,
// so that a reader knows whether a line ends inside a quoted value
// without reading the lines before it again. Only a quote that begins a
// value opens one, and inside it a quote written twice is a quote.
// Counting the quotes instead took one in the middle of a value, which
// opens nothing and is an error of its own line, for a value going on
// to the next line, and held every line after it.
type csvQuote struct {
	delim rune
	// quoted is set inside a quoted value; start is set where a value
	// begins, which is the one place a quote opens one.
	quoted, start bool
}

func newCSVQuote(delim rune) csvQuote {
	return csvQuote{delim: delim, start: true}
}

// feed reads one line and the line break after it, and reports whether a
// quoted value is still open at the end of it.
func (q *csvQuote) feed(text string) bool {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		switch {
		case q.quoted && r == '"':
			if i < len(text) && text[i] == '"' {
				i++ // a quote written twice
				continue
			}
			q.quoted = false
		case q.quoted:
		case q.start && r == '"':
			q.quoted, q.start = true, false
		case r == q.delim || r == '\n':
			q.start = true
		default:
			q.start = false
		}
	}
	if !q.quoted {
		q.start = true
	}
	return q.quoted
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
		var v raw
		if i < len(row) {
			v = some(row[i])
		}
		if err := r.setField(obj, name, v, fields[name], ln); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// MissingColumnError is a field rule for a column a csv does not have: a
// name its header line does not hold, or a numbered column past the ones
// its first record has. It is about the input as a whole rather than one
// record, so a stream ends with it instead of leaving a record out.
type MissingColumnError struct {
	Column  string
	Columns []string
}

func (e *MissingColumnError) Error() string {
	quoted := make([]string, len(e.Columns))
	for i, c := range e.Columns {
		quoted[i] = strconv.Quote(c)
	}
	return fmt.Sprintf("no column %q; the columns are %s", e.Column, strings.Join(quoted, ", "))
}

// checkColumns refuses a field rule for a column cols, read from the
// input, does not have. Converting a column that is not there would
// otherwise do nothing, and the caller who named it would not know.
func (r *run) checkColumns(fields map[string]*definition.Field, cols []string, ln int) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(cols, name) {
			return &ParseError{Definition: r.def.ID(), Line: ln, Cause: &MissingColumnError{Column: name, Columns: cols}}
		}
	}
	return nil
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
// first row normalised the way a table header is. Every name is unique,
// so that every value of a row has a key of its own.
func csvColumns(p *definition.Parse, header []string) []string {
	if len(p.Header.Columns) > 0 {
		return p.Header.Columns
	}
	out := make([]string, len(header))
	for i, cell := range header {
		name := definition.NormalizeName(cell)
		if renamed, ok := p.Header.Rename[name]; ok {
			name = renamed
		}
		if name == "" {
			name = "column"
		}
		out[i] = name
	}
	// A spreadsheet exports two columns under one heading often enough
	// that refusing the file would be the wrong answer; numbering the
	// repeats keeps every value reachable. A number is only given where
	// no heading already has that name, so that "x, x, x_2" does not put
	// two values under x_2.
	taken := map[string]bool{}
	for _, name := range out {
		taken[name] = true
	}
	given := map[string]bool{}
	for i, name := range out {
		if given[name] {
			for n := 2; ; n++ {
				numbered := name + "_" + strconv.Itoa(n)
				if !taken[numbered] && !given[numbered] {
					name = numbered
					break
				}
			}
			out[i] = name
		}
		given[name] = true
	}
	return out
}

// readCSV reads one record with the definition's delimiter.
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

// csvFailure places a csv reader's error in the input: the line it
// names is the line of the record it was in, and the message says what
// was wrong without the record's own line numbers, which count from the
// record rather than from the input.
func csvFailure(rec line, err error) (int, string) {
	var pe *csv.ParseError
	if errors.As(err, &pe) && pe.Line > 0 {
		return rec.num + pe.Line - 1, fmt.Sprintf("column %d: %v", pe.Column, pe.Err)
	}
	return rec.num, err.Error()
}

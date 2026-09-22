// Package datafile reads the data file formats jz converts to JSON as
// they are, rather than as the output of a command: JSON, JSON Lines,
// LTSV, YAML, and text as one string or as a list of lines or of
// NUL-separated records. CSV and TSV are read by the engine's csv parse
// with a definition fixed here (TabularDefinition), so that the command
// line and jz new read a csv file the same way.
//
// A format is chosen by the caller, with --format, or by the extension of
// the file (data.csv, events.jsonl.gz). Nothing is guessed from the text:
// an extension is a claim the person who named the file made, and a
// reader that finds text the format does not allow says where, and gives
// no JSON at all.
//
// Every reader refuses what it cannot represent faithfully rather than
// reading part of it: a key given twice, a value that is not UTF-8, text
// after a JSON document, a YAML number JSON has no spelling for.
package datafile

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/engine"
)

// The formats, by the name --format takes.
const (
	CSV   = "csv"
	TSV   = "tsv"
	LTSV  = "ltsv"
	JSONL = "jsonl"
	JSON  = "json"
	YAML  = "yaml"
	// TEXT is the whole input as one string.
	TEXT = "text"
	// LINES is a list of strings, one per line.
	LINES = "lines"
	// NUL is a list of strings, one per NUL-terminated record, as
	// find -print0 and xargs -0 write them.
	NUL = "nul"
)

// The compressions a file name may end in.
const (
	Gzip  = "gzip"
	Bzip2 = "bzip2"
)

// Names lists the formats in the order help shows them.
func Names() []string {
	return []string{CSV, TSV, LTSV, JSONL, JSON, YAML, TEXT, LINES, NUL}
}

// Known reports whether name is a format --format takes.
func Known(name string) bool {
	return slices.Contains(Names(), name)
}

// Tabular reports a format read with TabularDefinition, which the command
// line reads with the engine and Read reads the same way.
func Tabular(name string) bool {
	return name == CSV || name == TSV
}

// Streams reports a format made of records, which --stream can write as
// they are read. A JSON or YAML document and a text are one value, and
// there is nothing to write before their end.
func Streams(name string) bool {
	return name == LTSV || name == JSONL || name == LINES || name == NUL
}

var extensions = map[string]string{
	".csv":    CSV,
	".tsv":    TSV,
	".ltsv":   LTSV,
	".jsonl":  JSONL,
	".ndjson": JSONL,
	".json":   JSON,
	".yaml":   YAML,
	".yml":    YAML,
}

var compressions = map[string]string{
	".gz":  Gzip,
	".bz2": Bzip2,
}

// FromPath reads the extensions of a file name. compression is the
// compression its last extension names, and format the format the one
// before names, or the last one when there is no compression. Either is
// "" when the name says nothing about it. Case does not matter:
// EXPORT.CSV is a csv.
func FromPath(path string) (format, compression string) {
	base := strings.ToLower(filepath.Base(path))
	ext := filepath.Ext(base)
	if c, ok := compressions[ext]; ok {
		compression = c
		base = strings.TrimSuffix(base, ext)
		ext = filepath.Ext(base)
	}
	// A name that is only an extension (".json", ".csv.gz") is a hidden
	// file named after one, not a file of that format.
	if ext == base {
		return "", compression
	}
	return extensions[ext], compression
}

// CompressionError is compressed input that cannot be decompressed: a
// file named .gz that is not gzip, or one cut short or damaged.
type CompressionError struct {
	Compression string
	Err         error
}

func (e *CompressionError) Error() string {
	return fmt.Sprintf("the %s data cannot be decompressed: %v", e.Compression, e.Err)
}

func (e *CompressionError) Unwrap() error { return e.Err }

// Decompress wraps r in the reader of compression, or returns r as it is
// when compression is "". An error the decompression meets, then or
// while reading, is a CompressionError.
func Decompress(r io.Reader, compression string) (io.Reader, error) {
	switch compression {
	case "":
		return r, nil
	case Gzip:
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, &CompressionError{Compression: compression, Err: err}
		}
		return &decompressed{r: zr, compression: compression}, nil
	case Bzip2:
		return &decompressed{r: bzip2.NewReader(r), compression: compression}, nil
	}
	return nil, fmt.Errorf("unknown compression %q", compression)
}

// decompressed marks the errors of a decompressing reader, so that a
// damaged file is told from a failing disk.
type decompressed struct {
	r           io.Reader
	compression string
}

func (d *decompressed) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		var ce *CompressionError
		if !errors.As(err, &ce) {
			err = &CompressionError{Compression: d.compression, Err: err}
		}
	}
	return n, err
}

// Read reads a whole document of format. The size of data is the
// caller's to bound; the values the document yields are bounded here, by
// the same limit a definition's reading has (engine.DefaultMaxValues).
func Read(format string, data []byte) (any, error) {
	return read(format, data, engine.DefaultMaxValues)
}

func read(format string, data []byte, maxValues int) (any, error) {
	whole := newCounter(format, maxValues, true)
	switch format {
	case JSON:
		return readJSON(data, whole)
	case YAML:
		return readYAML(data, whole)
	case CSV, TSV:
		return readTabular(format, data, maxValues)
	case TEXT:
		return readText(data)
	case JSONL, LTSV, LINES, NUL:
		out := []any{}
		if err := whole.add(0); err != nil {
			return nil, err
		}
		err := streamCounted(format, bytes.NewReader(data), len(data)+1, whole, func(v any) error {
			out = append(out, v)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("datafile: %q is not read here", format)
}

// counter bounds the values a reading retains, the way
// engine.Options.MaxValues bounds a definition's: over a whole document,
// or over each record when the reading is a stream. A value is every JSON
// value the output holds, a list or an object as much as a string.
type counter struct {
	format string
	max, n int
	whole  bool
}

// startRecord starts the count over for a record of a stream.
func (c *counter) startRecord() {
	if !c.whole {
		c.n = 0
	}
}

func newCounter(format string, maxValues int, whole bool) *counter {
	if maxValues <= 0 {
		maxValues = engine.DefaultMaxValues
	}
	return &counter{format: format, max: maxValues, whole: whole}
}

// add counts one more value, on line (or, for nul, record) num, and
// refuses the one past the limit. A num of 0 leaves the place to the
// caller, which knows it better.
func (c *counter) add(num int) error {
	c.n++
	if c.n <= c.max {
		return nil
	}
	msg := fmt.Sprintf("the record yields more than %d values, more than one record holds", c.max)
	if c.whole {
		msg = fmt.Sprintf("the input yields more than %d values, more than one document holds", c.max)
		if Streams(c.format) {
			msg += "; --stream reads it one record at a time"
		}
	}
	pe := &engine.ParseError{Definition: c.format, Line: num, Msg: msg, Cause: engine.ErrTooManyValues}
	if c.format == NUL && num > 0 {
		pe.Line, pe.Msg = 0, fmt.Sprintf("record %d: %s", num, msg)
	}
	return pe
}

// TabularDefinition returns the definition a csv or a tsv is read with as
// data, with a header line or, when header is false, without one. It is
// fixed here rather than taken from a registry, so that a definition a
// user registers under csv changes what --parser csv reads and never what
// a data file holds. It is named after the registry shape it matches
// (csv/comma, csv/tab-no-header), so a failure reads the same either way.
//
// A line of spaces is a record, since in a csv it holds a value; an empty
// line holds none. The definition is read with TabularOptions.
func TabularDefinition(format string, header bool) (*definition.Definition, error) {
	body := `{input: {skip_blank: false}, parse: {type: csv}}`
	variant := "comma"
	if format == TSV {
		body = `{input: {skip_blank: false}, parse: {type: csv, delimiter: "\t"}}`
		variant = "tab"
	}
	if !header {
		body = strings.Replace(body, "type: csv", "type: csv, header: {none: true}", 1)
		variant += "-no-header"
	}
	def, err := definition.LoadInline([]byte(body), format)
	if err != nil {
		return nil, err
	}
	def.Command, def.Variant = "csv", variant
	return def, nil
}

// TabularOptions sets how a csv or a tsv is read as data: an escape
// sequence is part of the value that holds it, not colour to take off, a
// heading is the key as written rather than a name normalised for a
// definition to use, and a record is as long as the input may be, as a
// line of every other data format is, rather than bounded by the line
// limit of command output.
func TabularOptions(opts engine.Options) engine.Options {
	opts.KeepEscapes = true
	opts.KeepNames = true
	if opts.MaxLineLength <= 0 {
		opts.MaxLineLength = int(opts.MaxInputSize)
		if opts.MaxLineLength <= 0 {
			opts.MaxLineLength = engine.DefaultMaxInputSize
		}
	}
	return opts
}

// readTabular reads a csv or a tsv with the engine, with the definition
// the command line reads the same file with.
func readTabular(format string, data []byte, maxValues int) (any, error) {
	def, err := TabularDefinition(format, true)
	if err != nil {
		return nil, err
	}
	return engine.Parse(def, data, TabularOptions(engine.Options{MaxInputSize: int64(len(data)) + 1, MaxValues: maxValues}))
}

// readText reads the whole input as one string. A byte order mark in
// front is not part of the text; nothing else is trimmed.
func readText(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(data) {
		return nil, docError(TEXT, data, invalidUTF8Offset(data), "the text is not valid UTF-8")
	}
	return string(data), nil
}

// Stream reads the records of a record format from r and hands each to
// emit as soon as it has ended.
//
// The first record that cannot be read ends the stream, as it does for
// engine.Stream: the records before it stand, and the failure is the
// stream's. maxLine bounds what is held while a record waits for its end.
//
// Each record holds at most engine.DefaultMaxValues values; one that holds
// more is a record that cannot be read.
func Stream(format string, r io.Reader, maxLine int, emit func(any) error) error {
	return stream(format, r, maxLine, engine.DefaultMaxValues, emit)
}

func stream(format string, r io.Reader, maxLine, maxValues int, emit func(any) error) error {
	return streamCounted(format, r, maxLine, newCounter(format, maxValues, false), emit)
}

// streamCounted is Stream with the values counted by c: afresh for every
// record, or over all of them when c counts a whole document.
func streamCounted(format string, r io.Reader, maxLine int, c *counter, emit func(any) error) error {
	var record func([]byte, int, *counter) (any, error)
	switch format {
	case JSONL:
		record = jsonLine
	case LTSV:
		record = ltsvLine
	case LINES:
		return streamStrings(format, r, '\n', maxLine, c, emit)
	case NUL:
		return streamStrings(format, r, 0, maxLine, c, emit)
	default:
		return fmt.Errorf("datafile: %q is not a record format", format)
	}
	br := bufio.NewReader(r)
	for num := 1; ; num++ {
		line, err := readLine(br, maxLine)
		switch {
		case errors.Is(err, errLineTooLong):
			return lineError(format, num, fmt.Sprintf("the line is longer than %d bytes", maxLine))
		case err != nil && !errors.Is(err, io.EOF):
			// What was read of a line the input failed on is not the line.
			return err
		}
		if len(line) > 0 || err == nil {
			if num == 1 {
				line = bytes.TrimPrefix(line, []byte("\xEF\xBB\xBF"))
			}
			line = bytes.TrimSuffix(line, []byte("\r"))
			if !blankLine(format, line) {
				if ferr := readRecordLine(format, line, num, record, c, emit); ferr != nil {
					return ferr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// blankLine reports a line that holds no record. In JSON Lines that is a
// line of the white space JSON allows between tokens (space, tab and CR);
// in LTSV only an empty line, since a line of spaces is a field without a
// label. Other spaces, a no-break space or a form feed, are text the line
// holds.
func blankLine(format string, line []byte) bool {
	if format == LTSV {
		return len(line) == 0
	}
	return len(bytes.Trim(line, " \t\r")) == 0
}

// readRecordLine reads one line of JSON Lines or LTSV and hands its record
// to emit, or returns the failure that ends the stream.
func readRecordLine(format string, line []byte, num int, record func([]byte, int, *counter) (any, error), c *counter, emit func(any) error) error {
	if !utf8.Valid(line) {
		return lineError(format, num, "the line is not valid UTF-8")
	}
	c.startRecord()
	v, err := record(line, num, c)
	if err != nil {
		return err
	}
	return emit(v)
}

// streamStrings hands each record ended by sep to emit as a string.
//
// A record is everything up to its separator, empty records included, and
// nothing is trimmed. The separator ends a record rather than starting
// one, so input that ends with it has no empty record after it, and a
// last record without one is still a record. Lines end with LF or CRLF,
// the CR of CRLF being part of the ending, and a byte order mark in front
// of the first line is not part of it. A NUL-separated record keeps every
// byte, CR and LF included.
func streamStrings(format string, r io.Reader, sep byte, maxLine int, c *counter, emit func(any) error) error {
	br := bufio.NewReader(r)
	for num := 1; ; num++ {
		rec, err := readRecord(br, sep, maxLine)
		switch {
		case errors.Is(err, errLineTooLong):
			return recordError(format, num, fmt.Sprintf("the record is longer than %d bytes", maxLine))
		case err != nil && !errors.Is(err, io.EOF):
			return err
		}
		ended := err == nil
		if sep == '\n' && num == 1 {
			rec = bytes.TrimPrefix(rec, []byte("\xEF\xBB\xBF"))
		}
		if !ended && len(rec) == 0 {
			return nil
		}
		if sep == '\n' && ended {
			rec = bytes.TrimSuffix(rec, []byte("\r"))
		}
		var ferr error
		c.startRecord()
		if cerr := c.add(num); cerr != nil {
			ferr = cerr
		} else if utf8.Valid(rec) {
			ferr = emit(string(rec))
		} else {
			ferr = recordError(format, num, "the text is not valid UTF-8")
		}
		if ferr != nil {
			return ferr
		}
		if !ended {
			return nil
		}
	}
}

// recordError is a failure in one record: on a line for lines, and by its
// number for NUL-separated records, which have no lines to count.
func recordError(format string, num int, msg string) error {
	if format == NUL {
		return &engine.ParseError{Definition: format, Msg: fmt.Sprintf("record %d: %s", num, msg)}
	}
	return lineError(format, num, msg)
}

var errLineTooLong = errors.New("line too long")

// readLine returns the next line without its newline. At the end of the
// input it returns what is left with io.EOF.
func readLine(br *bufio.Reader, maxLine int) ([]byte, error) {
	return readRecord(br, '\n', maxLine)
}

// readRecord returns the next record without the sep that ends it. At the
// end of the input it returns what is left with io.EOF.
func readRecord(br *bufio.Reader, sep byte, maxLine int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice(sep)
		size := len(line) + len(chunk)
		if err == nil {
			size-- // the separator that ends the record is not part of it
		}
		if size > maxLine {
			return nil, errLineTooLong
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return line, err
		}
	}
}

// lineError is a parse failure on one line, which is exit 3 to the
// command line the same as a definition's.
func lineError(format string, line int, msg string) error {
	return &engine.ParseError{Definition: format, Line: line, Msg: msg}
}

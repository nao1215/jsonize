// Package definition describes parser definitions: the YAML documents that
// tell jsonize how to recognise a command's output and turn it into JSON.
//
// A definition is data, never code. It can express line selection, table,
// regular-expression and key/value extraction, typed field conversion and
// variant detection, but it cannot run commands or evaluate expressions.
// Regular expressions use Go's RE2 engine, which guarantees linear-time
// matching, so a hostile definition cannot cause catastrophic backtracking.
//
// Load is the way in. It decodes one YAML document, validates it and
// compiles its expressions; a Definition that has not been through it
// carries no compiled expressions and must not be handed to the engine.
package definition

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/yaml"
)

// CurrentFormat is the definition format version this build understands.
// The major number changes only for incompatible schema changes; a
// definition with a different format is rejected at load time.
const CurrentFormat = 1

// Parse types.
const (
	TypeTable     = "table"
	TypeRegex     = "regex"
	TypeKV        = "kv"
	TypeComposite = "composite"
	TypeRecords   = "records"
	// TypeCSV is a delimiter-separated table with RFC 4180 quoting, where
	// a value may contain the delimiter, a quote or a newline.
	TypeCSV = "csv"
	// TypeINI is a file of [section] headings and key = value lines.
	TypeINI = "ini"
	// TypeTree is a report whose depth comes from the indentation of the
	// input: lspci -vv, lsusb -v, iw dev.
	TypeTree = "tree"
)

// MaxTreeDepth bounds how deep a tree may go, so that a producer cannot
// make jz build an unbounded stack of objects. No report comes close.
const MaxTreeDepth = 32

// Table split modes.
const (
	SplitWhitespace = "whitespace"
	SplitAligned    = "aligned"
	SplitDelimiter  = "delimiter"
	// SplitBox is a table drawn with rules, as MySQL and several
	// container tools print one.
	SplitBox = "box"
)

// Regex "each" modes.
const (
	EachLine  = "line"
	EachInput = "input"
)

// Key/value output shapes.
const (
	AsList = "list"
	AsMap  = "map"
)

// Field types.
const (
	FieldString = "string"
	FieldInt    = "int"
	FieldFloat  = "float"
	FieldBool   = "bool"
	FieldTime   = "time"
	// FieldDuration is a printed length of time, reported in seconds.
	FieldDuration = "duration"
	FieldArray    = "array"
	FieldObject   = "object"
)

// Year policies for a time field whose layout carries no year.
const (
	// YearAssumed says the format prints no year and that the value is
	// to be dated only when the command line says which year to assume.
	YearAssumed = "assumed"
)

// Missing-value policies for object sub-fields.
const (
	MissingNull = "null"
	MissingOmit = "omit"
)

// Definition is one parser definition: how to parse one output format of
// one command.
type Definition struct {
	Format      int               `yaml:"format"`
	Command     string            `yaml:"command"`
	Variant     string            `yaml:"variant"`
	Aliases     []Alias           `yaml:"aliases,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Metadata    Metadata          `yaml:"metadata,omitempty"`
	Detect      Detect            `yaml:"detect,omitempty"`
	Exec        Exec              `yaml:"exec,omitempty"`
	Input       Input             `yaml:"input,omitempty"`
	Parse       Parse             `yaml:"parse"`
	Fields      map[string]*Field `yaml:"fields,omitempty"`

	// Source is where the definition was loaded from (file path or
	// "embedded:..."). It is set by the loader, not by the YAML.
	Source string `yaml:"-"`
	// Origin names the registry the definition came from (e.g. "embedded",
	// "user", a JSONIZE_REGISTRY_PATH directory). Set by the registry
	// loader.
	Origin string `yaml:"-"`
}

// ID returns "command/variant".
func (d *Definition) ID() string {
	return d.Command + "/" + d.Variant
}

// Alias is another command that prints this same format: vdir prints
// what ls -l prints, printenv what env prints, podman what docker
// prints. One definition answers for all of them, because nothing in the
// text says which of the commands wrote it.
type Alias struct {
	// Name is the other command's name, matched against argv[0] the way
	// Command is.
	Name string `yaml:"name"`
	// Args replaces detect.args while the definition is reached under
	// this name, for an alias that needs other arguments than the command
	// does: getent prints the passwd file only when asked for passwd,
	// while vdir needs no argument where ls needs -l. An alias that says
	// nothing here is filtered the way the command is, and one that says
	// an empty filter accepts any arguments.
	Args *ArgsMatch `yaml:"args,omitempty"`
}

// ArgsFor returns the argument filter that applies when the definition
// was reached under name.
func (d *Definition) ArgsFor(name string) *ArgsMatch {
	if name != "" && name != d.Command {
		for i := range d.Aliases {
			if d.Aliases[i].Name != name {
				continue
			}
			if d.Aliases[i].Args != nil {
				return d.Aliases[i].Args
			}
			break
		}
	}
	return &d.Detect.Args
}

// AliasNames returns the alias names in the order the definition lists
// them.
func (d *Definition) AliasNames() []string {
	if len(d.Aliases) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.Aliases))
	for i := range d.Aliases {
		out = append(out, d.Aliases[i].Name)
	}
	return out
}

// Metadata is descriptive information that does not affect parsing.
type Metadata struct {
	Tags       []string `yaml:"tags,omitempty"`
	References []string `yaml:"references,omitempty"`
	// Compatible lists the implementations known to produce this format,
	// e.g. ["GNU coreutils", "BusyBox"]. Informational only.
	Compatible []string `yaml:"compatible,omitempty"`
}

// Detect holds the signals used to choose among variants of a command.
type Detect struct {
	// AutoDetect can be set to false for a format whose text is too
	// unremarkable to recognise on its own (three numbers in a row, a
	// number and a path). Such a definition is only used when the user
	// names the parser, or when jz ran the command itself and therefore
	// knows what produced the text. Its signature is still verified, so
	// naming the parser confirms the format rather than bypassing the
	// check.
	AutoDetect *bool `yaml:"auto_detect,omitempty"`
	// OS restricts the variant to these GOOS values. Empty means any.
	OS []string `yaml:"os,omitempty"`
	// Args matches the arguments the command was run with (exec mode only).
	Args ArgsMatch `yaml:"args,omitempty"`
	// Signature matches the captured output itself.
	Signature Signature `yaml:"signature,omitempty"`
	// Priority breaks a tie between variants of the same command.
	Priority int `yaml:"priority,omitempty"`
}

// ArgsMatch matches command line arguments, each as one whole word:
// "--format=long" is the word "--format=long". Single-letter flags
// bundled as "-hT" are expanded, so "-h" matches "-hT", and a
// single-letter flag given more than once holds its bundle, so "-vv"
// matches "-v -v" and "-vvv". The words after a "--" are operands, which
// no filter sees.
type ArgsMatch struct {
	Any  []string `yaml:"any,omitempty"`
	All  []string `yaml:"all,omitempty"`
	None []string `yaml:"none,omitempty"`
}

// AutoDetectable reports whether the definition may be chosen without
// the parser being named. It defaults to true.
func (d *Detect) AutoDetectable() bool {
	return d.AutoDetect == nil || *d.AutoDetect
}

// IsZero reports whether no argument criteria are set.
func (a ArgsMatch) IsZero() bool {
	return len(a.Any) == 0 && len(a.All) == 0 && len(a.None) == 0
}

// Signature matches regular expressions against the first Window lines of
// the input, joined by newlines and evaluated in multi-line mode.
type Signature struct {
	All    []string `yaml:"all,omitempty"`
	Any    []string `yaml:"any,omitempty"`
	None   []string `yaml:"none,omitempty"`
	Window int      `yaml:"window,omitempty"`

	all, any, none []*regexp.Regexp
}

// DefaultSignatureWindow is the number of leading lines a signature sees.
const DefaultSignatureWindow = 20

// MaxSignatureWindow bounds the window so a definition cannot force the
// selector to scan an unbounded prefix.
const MaxSignatureWindow = 200

// IsZero reports whether no signature criteria are set.
func (s Signature) IsZero() bool {
	return len(s.All) == 0 && len(s.Any) == 0 && len(s.None) == 0
}

// Compiled returns the compiled expressions.
func (s *Signature) Compiled() (all, anyOf, none []*regexp.Regexp) {
	return s.all, s.any, s.none
}

// Exec configures how jsonize runs the command in exec mode.
type Exec struct {
	// Env is merged over the default environment (which already forces
	// LC_ALL=C). Values are literal; no expansion is performed.
	Env map[string]string `yaml:"env,omitempty"`
}

// Record separators.
const (
	RecordNewline = "newline"
	RecordNUL     = "nul"
)

// Input controls pre-processing of the captured text before parsing.
type Input struct {
	// RecordSeparator splits the input into records: "newline" (the
	// default) or "nul" for the NUL-separated output of tools such as
	// `env -0`, where a value may itself contain newlines.
	RecordSeparator string `yaml:"record_separator,omitempty"`
	// Ignore lists regular expressions; matching lines are dropped.
	Ignore []string `yaml:"ignore,omitempty"`
	// Fold names the continuation of the line above it: a matching line
	// is joined onto the previous one with a single space, which is how a
	// report that breaks a long value at the terminal width is read as
	// one value. It is applied before Ignore and SkipBlank.
	Fold string `yaml:"fold,omitempty"`
	// SkipBlank drops blank lines. Defaults to true.
	SkipBlank *bool `yaml:"skip_blank,omitempty"`
	// Select narrows the lines handed to the parser.
	Select Select `yaml:"select,omitempty"`

	ignore []*regexp.Regexp
	fold   *regexp.Regexp
}

// Separator returns the effective record separator byte.
func (in *Input) Separator() byte {
	if in.RecordSeparator == RecordNUL {
		return 0
	}
	return '\n'
}

// SkipBlankLines reports the effective skip_blank setting.
func (in *Input) SkipBlankLines() bool {
	return in.SkipBlank == nil || *in.SkipBlank
}

// IgnorePatterns returns the compiled ignore expressions.
func (in *Input) IgnorePatterns() []*regexp.Regexp { return in.ignore }

// FoldPattern reports the continuation expression, nil when the format
// has no wrapped lines.
func (in *Input) FoldPattern() *regexp.Regexp { return in.fold }

// Select picks a contiguous range of lines. The steps are applied in the
// order after, until, skip, limit.
type Select struct {
	// After drops everything up to and including the first matching line.
	After string `yaml:"after,omitempty"`
	// Until stops before the first matching line.
	Until string `yaml:"until,omitempty"`
	// Skip drops this many leading lines.
	Skip int `yaml:"skip,omitempty"`
	// Limit keeps at most this many lines (0 = unlimited).
	Limit int `yaml:"limit,omitempty"`

	after, until *regexp.Regexp
	// heading is after, required to cover the whole of the line it
	// matches. A heading the expression describes completely is a line the
	// definition has read; one it only opens with leaves the rest of the
	// line unaccounted for.
	heading *regexp.Regexp
}

// IsZero reports whether the selection keeps every line.
func (s Select) IsZero() bool {
	return s.After == "" && s.Until == "" && s.Skip == 0 && s.Limit == 0
}

// CompiledAfter returns the compiled after expression (may be nil).
func (s *Select) CompiledAfter() *regexp.Regexp { return s.after }

// Heading reports whether text is a line that select.after describes
// from end to end, surrounding whitespace aside. Such a line is the
// heading of the region the selection opens, and it counts as read by the
// parser that reads the region.
func (s *Select) Heading(text string) bool {
	return s.heading != nil && s.heading.MatchString(text)
}

// CompiledUntil returns the compiled until expression (may be nil).
func (s *Select) CompiledUntil() *regexp.Regexp { return s.until }

// Parse describes the extraction algorithm. Only the keys relevant to Type
// may be set.
type Parse struct {
	Type string `yaml:"type"`

	// table, csv
	Header Header `yaml:"header,omitempty"`
	Split  string `yaml:"split,omitempty"`
	// Delimiter is the literal separator of a table with split:
	// delimiter, and the one character a csv parser cuts on (default
	// ",").
	Delimiter string `yaml:"delimiter,omitempty"`
	MaxFields int    `yaml:"max_fields,omitempty"`
	MinFields int    `yaml:"min_fields,omitempty"`

	// regex
	Pattern string `yaml:"pattern,omitempty"`
	// Patterns are alternatives tried in order; the first one that
	// matches wins. They let a definition treat structurally different
	// lines differently (an ls symlink line and an ordinary one) without
	// a single expression having to guess.
	Patterns []Alternative `yaml:"patterns,omitempty"`
	Each     string        `yaml:"each,omitempty"`

	// kv
	Separator string `yaml:"separator,omitempty"`
	// Trim removes surrounding whitespace from both sides of the
	// separator. It defaults to true; a format whose values are
	// significant down to the space (env) sets it to false.
	Trim *bool `yaml:"trim,omitempty"`
	// Unquote removes one matching pair of surrounding quotes from the
	// value, for the shell-quoted files (/etc/os-release) whose quotes
	// are syntax rather than content.
	Unquote bool   `yaml:"unquote,omitempty"`
	As      string `yaml:"as,omitempty"`

	// composite, records
	Parts []Part `yaml:"parts,omitempty"`

	// tree
	//
	// Indent is one level of indentation as it is written: "\t" for a
	// report indented with tabs, "  " for one indented with two spaces.
	// A line's depth is how many times it opens the line.
	Indent Indent `yaml:"indent,omitempty"`
	// Node says how one line of the tree is read. Every line is a node,
	// so this applies at every depth.
	Node *Node `yaml:"node,omitempty"`
	// Root is what a line at depth zero has to match, when the format
	// says what a top-level line looks like: a device line for lspci -v,
	// a bus line for lsusb -v. The node patterns describe every depth,
	// and the one that reads a flag list's continuation reads any text,
	// so without Root a line printed after the report would be read as
	// a root of its own.
	Root string `yaml:"root,omitempty"`

	// records: a line matching Start opens a record, and everything up
	// to the next such line belongs to it. Each record is then read the
	// way composite reads a whole input, so the parts vocabulary is the
	// same one.
	Start string `yaml:"start,omitempty"`
	// Record is the other way to say how a record is read: by one parser
	// over the whole block rather than by named regions of it. A report
	// whose block is one labelled list, or one expression, has nothing to
	// name the regions of, and the record is then the object that parser
	// yields rather than an object holding it under a part's name.
	Record *Record `yaml:"record,omitempty"`

	compiled []*regexp.Regexp
	// values are the fixed values each compiled pattern adds, in the
	// same order.
	values [][]Value
	start  *regexp.Regexp
	root   *regexp.Regexp
	// groups are the named capture groups of every pattern, in order and
	// without duplicates.
	groups []string
}

// YieldsArray reports whether this parser produces a JSON array. It is
// what lets a caller that knows the format but has no text to read say
// the answer is an empty list rather than that it could not tell.
func (p *Parse) YieldsArray() bool {
	switch p.Type {
	case TypeTable, TypeRecords, TypeCSV, TypeTree:
		return true
	case TypeRegex:
		return p.Each != EachInput
	case TypeKV:
		return p.As != AsMap
	default:
		return false
	}
}

// Streams reports whether the result can be handed over as it is read:
// the records of a list one at a time, or a composite part by part.
func (p *Parse) Streams() bool {
	return p.YieldsArray() || p.Type == TypeComposite
}

// CompiledStart returns the compiled expression that opens a record.
func (p *Parse) CompiledStart() *regexp.Regexp { return p.start }

// CompiledRoot returns the compiled expression a tree's top-level lines
// have to match, or nil when the definition states none.
func (p *Parse) CompiledRoot() *regexp.Regexp { return p.root }

// CompiledPatterns returns the compiled expressions of a regex parser in
// the order they are tried.
func (p *Parse) CompiledPatterns() []*regexp.Regexp { return p.compiled }

// PatternValues returns the fixed values the i-th compiled pattern adds
// to what it reads.
func (p *Parse) PatternValues(i int) []Value {
	if i < 0 || i >= len(p.values) {
		return nil
	}
	return p.values[i]
}

// Groups returns the named capture groups of every pattern.
func (p *Parse) Groups() []string { return p.groups }

// PatternSources returns the expressions as written, for diagnostics.
func (p *Parse) PatternSources() []string {
	if p.Pattern != "" {
		return []string{p.Pattern}
	}
	if len(p.Patterns) == 0 {
		return nil
	}
	out := make([]string, len(p.Patterns))
	for i, a := range p.Patterns {
		out[i] = a.Pattern
	}
	return out
}

// Alternative is one entry of patterns: an expression, and the values a
// line it matches carries whatever its text says. The values are what
// the line is rather than what it prints (a crontab line that sets a
// variable and one that schedules a job), so a record can say which it
// is. An entry written as a plain string is an expression with none.
type Alternative struct {
	Pattern string            `yaml:"pattern"`
	Values  map[string]string `yaml:"values,omitempty"`
}

// UnmarshalYAML accepts a string or a mapping of pattern and values.
func (a *Alternative) UnmarshalYAML(n *yaml.Node) error {
	var one string
	if err := yaml.Decode(n, &one, true); err == nil {
		*a = Alternative{Pattern: one}
		return nil
	}
	type plain Alternative
	var full plain
	if err := yaml.Decode(n, &full, true); err != nil {
		return fmt.Errorf("a pattern is a string, or a mapping of pattern and values: %w", err)
	}
	*a = Alternative(full)
	return nil
}

// Value is one fixed key and value an alternative adds.
type Value struct {
	Name  string
	Value string
}

// TrimCells reports the effective kv trim setting.
func (p *Parse) TrimCells() bool {
	return p.Trim == nil || *p.Trim
}

// Header describes the header line of a table.
type Header struct {
	// Columns names the columns explicitly. When set and None is false the
	// header line is still consumed but its text is ignored.
	Columns []string `yaml:"columns,omitempty"`
	// None declares that the table has no header line; Columns is required.
	None bool `yaml:"none,omitempty"`
	// LeadingLabel names an unlabelled first column, as in `free` where the
	// header starts with whitespace and rows start with "Mem:".
	LeadingLabel string `yaml:"leading_label,omitempty"`
	// Rename maps normalised header names to field names.
	Rename map[string]string `yaml:"rename,omitempty"`
}

// RepeatedHeader reports whether a body line holding the same cells as
// the header is that header printed again rather than a row. A table's
// is, since a command that prints a report per interval prints its
// header with each; a csv file is data, so such a row is a row; and
// with no header line there is nothing to repeat.
func (p *Parse) RepeatedHeader() bool {
	if p.Header.None {
		return false
	}
	return p.Type != TypeCSV
}

// Indent is one level of a tree's indentation, or the forms one level
// may take. A report that marks a level with a branch character rather
// than with spaces writes several: `systemd-analyze critical-chain`
// indents by two characters that are either two spaces or a backtick and
// a dash, and tree(1) by four that are one of "|   ", "    ", "|-- " and
// "`-- ". A line's depth is how many of them open it, and the first that
// fits at each step is the one taken, so a definition that writes
// several states the order they are tried in.
//
// One level written as a plain string is the same as a list of one.
type Indent []string

// UnmarshalYAML accepts either a string or a list of them.
func (i *Indent) UnmarshalYAML(n *yaml.Node) error {
	// A number would decode as the digits of itself, which is a level of
	// indentation nothing prints. Saying so beats accepting "2" as two
	// characters that happen to be a two.
	var count int
	if err := yaml.Decode(n, &count, true); err == nil {
		return fmt.Errorf("indent is the indentation as it is written, not how many characters it is: %q for a tab, %q for two spaces", "\\t", "  ")
	}
	var one string
	if err := yaml.Decode(n, &one, true); err == nil {
		*i = Indent{one}
		return nil
	}
	var many []string
	if err := yaml.Decode(n, &many, true); err != nil {
		return errors.New("indent must be a string or a list of strings")
	}
	*i = many
	return nil
}

// Node describes how one line of a tree is read. It is a part without a
// name or a region: a tree has one description for every node, and what
// differs between them is only how deep they are.
type Node struct {
	Parse  Parse             `yaml:"parse"`
	Fields map[string]*Field `yaml:"fields,omitempty"`
}

// Record says how one record of a records parser is read when the block
// is one value: the parser reads the whole block and its result is the
// record. It is what parts would be with nothing to name.
type Record struct {
	Parse  Parse             `yaml:"parse"`
	Fields map[string]*Field `yaml:"fields,omitempty"`
}

// Part is one component of a composite parser.
type Part struct {
	Name   string `yaml:"name"`
	Select Select `yaml:"select,omitempty"`
	// Ignore lists regular expressions; a line of the part's region that
	// matches one is dropped. It is what a part uses to say which lines
	// of a shared region belong to a sibling: a settings line the part
	// above already read, a slave link the part below reads. Naming them
	// is the point, so that a line nobody reads is a mistake the parse
	// reports rather than something quietly lost.
	Ignore []string          `yaml:"ignore,omitempty"`
	Parse  Parse             `yaml:"parse"`
	Fields map[string]*Field `yaml:"fields,omitempty"`

	ignore []*regexp.Regexp
}

// IgnorePatterns returns the compiled expressions of a part's ignore list.
func (p *Part) IgnorePatterns() []*regexp.Regexp { return p.ignore }

// Field describes how one extracted value is converted.
type Field struct {
	Type string `yaml:"type,omitempty"`
	// NullIf lists raw values (after trimming) that become null.
	NullIf []string `yaml:"null_if,omitempty"`
	// TrimPrefix / TrimSuffix are removed before conversion.
	TrimPrefix string `yaml:"trim_prefix,omitempty"`
	TrimSuffix string `yaml:"trim_suffix,omitempty"`
	// True / False list the spellings for bool fields.
	True  []string `yaml:"true_values,omitempty"`
	False []string `yaml:"false_values,omitempty"`
	// Layout is the Go reference layout a time field is written in
	// ("2006-01-02T15:04:05-0700"). It is required for a time field, and
	// it has to carry a year unless Year says the format prints none.
	//
	// For a duration field it is one of two words rather than a layout,
	// h:mm or mm:ss, and it says what the last part of a bare two-part
	// reading is. That is the one thing the text cannot settle on its
	// own: ps prints four minutes fifty seconds as "4:50" and uptime
	// prints an hour and twenty-three minutes as "1:23".
	Layout string `yaml:"layout,omitempty"`
	// Year is "assumed" for a time field whose layout carries no year.
	// Such a value stays the string it was printed as unless the command
	// line says which year to assume, so writing it is a statement about
	// the format rather than a way to have a date invented.
	Year string `yaml:"year,omitempty"`
	// Location reads a timestamp that states no zone: "utc" (the default)
	// or "local" for the zone the running system is in.
	Location string `yaml:"location,omitempty"`
	// Required rejects null or missing values.
	Required bool `yaml:"required,omitempty"`
	// WhenMissing controls what happens when a regex group did not
	// participate in the match: "null" (default) or "omit".
	WhenMissing string `yaml:"when_missing,omitempty"`

	// array
	Split      string `yaml:"split,omitempty"`
	SplitRegex string `yaml:"split_regex,omitempty"`
	Items      *Field `yaml:"items,omitempty"`

	// Regex is how an object field is cut into keys. On a string field it
	// is the shape the whole value has to have, and its one named group
	// is the part of it that is the value: the name under the tree lsblk
	// draws in front of it.
	Regex string `yaml:"regex,omitempty"`
	// object
	Fields map[string]*Field `yaml:"fields,omitempty"`

	// Unescape undoes the escapes a command writes into a string value
	// (md5sum's "\n" for a newline in a file name), exactly the ones it
	// lists.
	Unescape *Unescape `yaml:"unescape,omitempty"`

	splitRegex *regexp.Regexp
	regex      *regexp.Regexp
	groups     []string
}

// Unescape lists the escapes one format writes and what each stands for.
// Every escape begins with the same character, and an occurrence of it
// that begins none of them is an error rather than something kept: the
// text is then not the format the definition describes.
type Unescape struct {
	// When names a group of the same pattern. The value is decoded only
	// when that group matched some text: GNU md5sum marks a line
	// whose name it escaped with a backslash in front of the checksum,
	// and a line without it holds the name as it is.
	When string `yaml:"when,omitempty"`
	// Quote is the character a format puts around a value it escaped:
	// git writes a path holding a byte it will not print in double
	// quotes. A value between two of them has the quotes removed and is
	// decoded; any other value is kept as it is. It is the other way to
	// say that the text declares the escaping, beside When.
	Quote string `yaml:"quote,omitempty"`
	// Octal reads the escape character and three octal digits as the byte
	// they name ("\346"), which is how git and getfacl write a byte they
	// will not print. The bytes a value decodes to have to be UTF-8 text.
	Octal bool `yaml:"octal,omitempty"`
	// Sequences maps each escape, as it is written, to the text it
	// stands for.
	Sequences map[string]string `yaml:"sequences"`
}

// Decode replaces each escape in s. It reports false when s holds the
// escape character where no listed escape begins, or decodes to bytes
// that are not UTF-8.
func (u *Unescape) Decode(s string) (string, bool) {
	if u.Quote != "" {
		if len(s) < 2*len(u.Quote) || !strings.HasPrefix(s, u.Quote) || !strings.HasSuffix(s, u.Quote) {
			return s, true
		}
		s = s[len(u.Quote) : len(s)-len(u.Quote)]
	}
	var esc byte
	for k := range u.Sequences {
		esc = k[0]
		break
	}
	if strings.IndexByte(s, esc) < 0 {
		return s, true
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != esc {
			b.WriteByte(s[i])
			i++
			continue
		}
		best := ""
		for k := range u.Sequences {
			if len(k) > len(best) && strings.HasPrefix(s[i:], k) {
				best = k
			}
		}
		if best != "" {
			b.WriteString(u.Sequences[best])
			i += len(best)
			continue
		}
		if u.Octal && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) && s[i+1] <= '3' {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 4
			continue
		}
		return "", false
	}
	out := b.String()
	if u.Octal && !utf8.ValidString(out) {
		return "", false
	}
	return out, true
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// EffectiveType returns the type, defaulting to string.
func (f *Field) EffectiveType() string {
	if f == nil || f.Type == "" {
		return FieldString
	}
	return f.Type
}

// CompiledSplit returns the compiled split_regex (may be nil).
func (f *Field) CompiledSplit() *regexp.Regexp { return f.splitRegex }

// CompiledRegex returns the compiled object regex (may be nil).
func (f *Field) CompiledRegex() *regexp.Regexp { return f.regex }

// Group returns the named group of a string field's regex, the part of
// the value that is kept.
func (f *Field) Group() string {
	if len(f.groups) == 0 {
		return ""
	}
	return f.groups[0]
}

// Groups returns the named groups of the object regex.
func (f *Field) Groups() []string { return f.groups }

// NormalizeName turns a header cell such as "1K-blocks", "%CPU", "Use%" or
// "Mounted on" into a JSON-friendly field name: 1k_blocks, cpu_percent,
// use_percent, mounted_on.
func NormalizeName(s string) string {
	t := strings.TrimSpace(strings.ToLower(s))
	percentPrefix := strings.HasPrefix(t, "%")
	percentSuffix := strings.HasSuffix(t, "%")
	t = strings.Trim(t, "%")
	var b strings.Builder
	lastUnderscore := false
	for _, r := range t {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		switch {
		case isAlnum:
			b.WriteRune(r)
			lastUnderscore = false
		case r == '%':
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteString("percent")
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if percentPrefix || percentSuffix {
		if out == "" {
			return "percent"
		}
		out += "_percent"
	}
	return out
}

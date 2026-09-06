// Package definition describes parser definitions: the YAML documents that
// tell jsonize how to recognise a command's output and turn it into JSON.
//
// A definition is data, never code. It can express line selection, table,
// regular-expression and key/value extraction, typed field conversion and
// variant detection, but it cannot run commands or evaluate expressions.
// Regular expressions use Go's RE2 engine, which guarantees linear-time
// matching, so a hostile definition cannot cause catastrophic backtracking.
package definition

import (
	"regexp"
	"strings"
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
)

// Table split modes.
const (
	SplitWhitespace = "whitespace"
	SplitAligned    = "aligned"
	SplitDelimiter  = "delimiter"
)

// Regex "each" modes.
const (
	EachLine  = "line"
	EachInput = "input"
)

// Mismatch policies.
const (
	MismatchError = "error"
	MismatchSkip  = "skip"
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
	FieldSize   = "size"
	FieldArray  = "array"
	FieldObject = "object"
)

// Size unit bases.
const (
	UnitBinary  = "binary"
	UnitDecimal = "decimal"
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
	Description string            `yaml:"description,omitempty"`
	MinJsonize  string            `yaml:"min_jsonize,omitempty"`
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
	// "user", a --registry directory). Set by the registry loader.
	Origin string `yaml:"-"`
}

// ID returns "command/variant".
func (d *Definition) ID() string {
	return d.Command + "/" + d.Variant
}

// Metadata is descriptive information that does not affect parsing.
type Metadata struct {
	Tags       []string `yaml:"tags,omitempty"`
	References []string `yaml:"references,omitempty"`
	Fixtures   string   `yaml:"fixtures,omitempty"`
	Authors    []string `yaml:"authors,omitempty"`
	// Compatible lists the implementations known to produce this format,
	// e.g. ["GNU coreutils", "BusyBox"]. Informational only.
	Compatible []string `yaml:"compatible,omitempty"`
}

// Detect holds the signals used to choose among variants of a command.
type Detect struct {
	// OS restricts the variant to these GOOS values. Empty means any.
	OS []string `yaml:"os,omitempty"`
	// Args matches the arguments the command was run with (exec mode only).
	Args ArgsMatch `yaml:"args,omitempty"`
	// Signature matches the captured output itself.
	Signature Signature `yaml:"signature,omitempty"`
	// Priority breaks ties between candidates of equal specificity.
	Priority int `yaml:"priority,omitempty"`
}

// ArgsMatch matches command line arguments. Single-letter flags bundled as
// "-hT" are expanded, so "-h" matches "-hT".
type ArgsMatch struct {
	Any  []string `yaml:"any,omitempty"`
	All  []string `yaml:"all,omitempty"`
	None []string `yaml:"none,omitempty"`
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

// Input controls pre-processing of the captured text before parsing.
type Input struct {
	// Ignore lists regular expressions; matching lines are dropped.
	Ignore []string `yaml:"ignore,omitempty"`
	// SkipBlank drops blank lines. Defaults to true.
	SkipBlank *bool `yaml:"skip_blank,omitempty"`
	// Select narrows the lines handed to the parser.
	Select Select `yaml:"select,omitempty"`

	ignore []*regexp.Regexp
}

// SkipBlankLines reports the effective skip_blank setting.
func (in *Input) SkipBlankLines() bool {
	return in.SkipBlank == nil || *in.SkipBlank
}

// IgnorePatterns returns the compiled ignore expressions.
func (in *Input) IgnorePatterns() []*regexp.Regexp { return in.ignore }

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
}

// IsZero reports whether the selection keeps every line.
func (s Select) IsZero() bool {
	return s.After == "" && s.Until == "" && s.Skip == 0 && s.Limit == 0
}

// CompiledAfter returns the compiled after expression (may be nil).
func (s *Select) CompiledAfter() *regexp.Regexp { return s.after }

// CompiledUntil returns the compiled until expression (may be nil).
func (s *Select) CompiledUntil() *regexp.Regexp { return s.until }

// Parse describes the extraction algorithm. Only the keys relevant to Type
// may be set.
type Parse struct {
	Type string `yaml:"type"`

	// table
	Header    Header `yaml:"header,omitempty"`
	Split     string `yaml:"split,omitempty"`
	Delimiter string `yaml:"delimiter,omitempty"`
	MaxFields int    `yaml:"max_fields,omitempty"`
	MinFields int    `yaml:"min_fields,omitempty"`

	// regex
	Pattern    string `yaml:"pattern,omitempty"`
	Each       string `yaml:"each,omitempty"`
	OnMismatch string `yaml:"on_mismatch,omitempty"`

	// kv
	Separator string `yaml:"separator,omitempty"`
	As        string `yaml:"as,omitempty"`
	KeyName   string `yaml:"key_name,omitempty"`
	ValueName string `yaml:"value_name,omitempty"`

	// composite
	Parts []Part `yaml:"parts,omitempty"`

	pattern *regexp.Regexp
	// groups are the named capture groups of pattern in order.
	groups []string
}

// CompiledPattern returns the compiled regular expression for regex parsers.
func (p *Parse) CompiledPattern() *regexp.Regexp { return p.pattern }

// Groups returns the named capture groups of the pattern in order.
func (p *Parse) Groups() []string { return p.groups }

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

// Part is one component of a composite parser.
type Part struct {
	Name   string            `yaml:"name"`
	Select Select            `yaml:"select,omitempty"`
	Parse  Parse             `yaml:"parse"`
	Fields map[string]*Field `yaml:"fields,omitempty"`
}

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
	// Unit selects binary (default) or decimal multipliers for size fields.
	Unit string `yaml:"unit,omitempty"`
	// Required rejects null or missing values.
	Required bool `yaml:"required,omitempty"`
	// WhenMissing controls what happens when a regex group did not
	// participate in the match: "null" (default) or "omit".
	WhenMissing string `yaml:"when_missing,omitempty"`

	// array
	Split      string `yaml:"split,omitempty"`
	SplitRegex string `yaml:"split_regex,omitempty"`
	Items      *Field `yaml:"items,omitempty"`

	// object
	Regex  string            `yaml:"regex,omitempty"`
	Fields map[string]*Field `yaml:"fields,omitempty"`

	splitRegex *regexp.Regexp
	regex      *regexp.Regexp
	groups     []string
}

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

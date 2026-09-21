package definition

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"regexp/syntax"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/buildinfo"
	"github.com/nao1215/jsonize/internal/yaml"
	"github.com/nao1215/jsonize/pkg/convert"
)

// Limits guarding against hostile or accidental resource use.
const (
	// MaxDefinitionSize is the largest YAML document accepted.
	MaxDefinitionSize = 256 * 1024
	// MaxRegexLength bounds any single regular expression in a definition.
	MaxRegexLength = 2048
	// MaxColumns bounds the number of table columns.
	MaxColumns = 256
	// MaxParts bounds composite parts.
	MaxParts = 32
	// MaxPatterns bounds the alternatives of a regex parser.
	MaxPatterns = 16
	// MaxNesting bounds field nesting depth (array of object of array...).
	MaxNesting = 8
)

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)
	variantRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	fieldRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:@-]*$`)
	// reservedNames cannot be used as directory names on Windows, and Go's
	// embed and module tooling refuse them, so they are rejected up front.
	reservedNames = map[string]bool{
		"con": true, "prn": true, "aux": true, "nul": true,
		"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
		"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
	}
	knownOS = map[string]bool{
		"linux": true, "darwin": true, "freebsd": true, "openbsd": true, "netbsd": true,
		"dragonfly": true, "windows": true, "aix": true, "solaris": true, "illumos": true,
	}
)

// ValidationError is one problem found in a definition. Path points at the
// offending YAML key using dotted notation (e.g. "parse.header.columns[2]").
type ValidationError struct {
	Source string
	Path   string
	Msg    string
}

func (e *ValidationError) Error() string {
	src := e.Source
	if src == "" {
		src = "definition"
	}
	if e.Path == "" {
		return fmt.Sprintf("%s: %s", src, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", src, e.Path, e.Msg)
}

// FormatError reports a definition whose format version this build cannot
// read.
type FormatError struct {
	Source string
	Got    int
}

func (e *FormatError) Error() string {
	return fmt.Sprintf("%s: format %d is not supported by this jsonize (supports format %d); update jsonize or the definition",
		e.Source, e.Got, CurrentFormat)
}

// UnknownKeyError reports a key this build does not know. It is told
// apart from other schema problems because it is the one that a newer
// jsonize may well understand: keys are added within format 1, so a
// definition written for a later release reaches an older one this way
// rather than as a format number it cannot read.
type UnknownKeyError struct {
	Source string
	Key    string
	// Line is where the key appears, 0 when the decoder did not say.
	Line int
}

func (e *UnknownKeyError) Error() string {
	where := ""
	if e.Line > 0 {
		where = fmt.Sprintf("line %d: ", e.Line)
	}
	return fmt.Sprintf("%s: %sunknown key %q; this definition may need a newer jz (this build reads definition format %d as of %s)",
		e.Source, where, e.Key, CurrentFormat, buildinfo.Get())
}

// Load decodes, validates and compiles a definition. source is used in
// error messages and stored in Definition.Source.
func Load(data []byte, source string) (*Definition, error) {
	d, err := decode(data, source)
	if err != nil {
		return nil, err
	}
	return d.finish(source)
}

// decode reads one YAML document into a definition, refusing one over
// the size limit and naming a key this build does not know.
func decode(data []byte, source string) (*Definition, error) {
	if len(data) > MaxDefinitionSize {
		return nil, &ValidationError{Source: source, Msg: fmt.Sprintf("definition exceeds %d bytes", MaxDefinitionSize)}
	}
	// A file with nothing in it decodes to a definition with every key
	// unset, and reporting that as format 0 names a version nobody wrote
	// and tells the reader to update jz, which will not help.
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &ValidationError{Source: source, Msg: "the file is empty"}
	}
	var d Definition
	if err := DecodeYAML(data, &d); err != nil {
		return nil, decodeError(err, source)
	}
	if d.Format == 0 && d.Command == "" && d.Variant == "" && d.Parse.Type == "" {
		return nil, &ValidationError{Source: source, Msg: "holds no definition"}
	}
	return &d, nil
}

// decodeError names what the decoder refused: a key this build does not
// know, a value that does not fit the key it is under, or text that is
// not YAML.
func decodeError(err error, source string) error {
	var unknown *yaml.UnknownFieldError
	if errors.As(err, &unknown) {
		return &UnknownKeyError{Source: source, Key: unknown.Key, Line: unknown.Line}
	}
	var mismatch *yaml.DecodeError
	if errors.As(err, &mismatch) {
		return &ValidationError{Source: source, Msg: mismatch.Error()}
	}
	return &ValidationError{Source: source, Msg: "invalid YAML: " + err.Error()}
}

// finish checks the format, validates the definition and compiles its
// expressions, which is what makes a decoded definition one the engine
// may be given.
func (d *Definition) finish(source string) (*Definition, error) {
	d.Source = source
	if d.Format != CurrentFormat {
		return nil, &FormatError{Source: source, Got: d.Format}
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// InlineCommand and InlineVariant name a definition given on the command
// line rather than loaded from a registry. It is not written to disk and
// is not detected, so the name says where it came from.
const (
	InlineCommand = "inline"
	InlineVariant = "inline"
)

// LoadInline reads a definition body given on the command line. It is a
// parser.yaml without the four keys that place a definition in a
// registry: format is this build's, command and variant are the inline
// pair, and detect is meaningless for a definition nothing chooses.
// Everything else (input, parse, fields, exec) is the same schema and
// goes through the same validation, so an inline definition cannot
// express anything a file cannot. The body may be written in block
// style or as one flow mapping, whichever a command line is easier
// with.
func LoadInline(body []byte, source string) (*Definition, error) {
	if len(body) > MaxDefinitionSize {
		return nil, &ValidationError{Source: source, Msg: fmt.Sprintf("definition exceeds %d bytes", MaxDefinitionSize)}
	}
	var probe struct {
		Format  int    `yaml:"format"`
		Command string `yaml:"command"`
		Variant string `yaml:"variant"`
		Detect  any    `yaml:"detect"`
	}
	// A body is decoded loosely once to name the keys that place a
	// definition, so that writing one gets an explanation rather than
	// "unknown key" from the strict pass below. It goes through the same
	// guarded decoder as everything else, so a body the library cannot
	// read is an error here too rather than a crash.
	if err := decodeYAML(body, &probe, false); err == nil {
		for _, k := range []struct {
			name string
			set  bool
		}{
			{"format", probe.Format != 0},
			{"command", probe.Command != ""},
			{"variant", probe.Variant != ""},
			{"detect", probe.Detect != nil},
		} {
			if k.set {
				return nil, &ValidationError{Source: source, Msg: k.name + ": a definition given here is not chosen and does not live in a registry, so it states no " + k.name}
			}
		}
	}
	d, err := decode(body, source)
	if err != nil {
		return nil, err
	}
	d.Format, d.Command, d.Variant = CurrentFormat, InlineCommand, InlineVariant
	return d.finish(source)
}

// DecodeYAML decodes strictly: a key the value has no field for is an
// error. A definition may come from a registry jz was pointed at, so a
// decoder panic, which none is expected to raise, is an error rather
// than the end of jz.
func DecodeYAML(data []byte, v any) error {
	return decodeYAML(data, v, true)
}

// decodeYAML is DecodeYAML with the strictness chosen: a probe that
// only looks for a few keys reads loosely, everything else refuses a key
// it does not know.
func decodeYAML(data []byte, v any, strict bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decoder failure: %v", r)
		}
	}()
	return yaml.Unmarshal(data, v, strict)
}

// validator accumulates problems for one definition.
type validator struct {
	source string
	errs   []error
}

func (v *validator) add(path, format string, args ...any) {
	v.errs = append(v.errs, &ValidationError{Source: v.source, Path: path, Msg: fmt.Sprintf(format, args...)})
}

func (v *validator) regex(path, expr string) *pattern {
	if len(expr) > MaxRegexLength {
		v.add(path, "regular expression longer than %d characters", MaxRegexLength)
		return nil
	}
	// The flags regexp.Compile parses with, so that what passes here is
	// what compiles when the pattern is first matched.
	parsed, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		var se *syntax.Error
		if errors.As(err, &se) {
			v.add(path, "invalid regular expression: %s", se.Code)
		} else {
			v.add(path, "invalid regular expression: %v", err)
		}
		return nil
	}
	return &pattern{expr: expr, names: parsed.CapNames()[1:]}
}

// validate checks the definition and compiles its regular expressions.
// All problems are reported together (errors.Join) so that authors can
// fix a file in one pass. Load is the only way in: a Definition that has
// not been through it has no compiled expressions.
func (d *Definition) validate() error {
	v := &validator{source: d.Source}
	if d.Format != CurrentFormat {
		v.add("format", "must be %d", CurrentFormat)
	}
	validateName(v, "command", d.Command, nameRe)
	validateName(v, "variant", d.Variant, variantRe)
	validateAliases(v, d)
	validateDetect(v, &d.Detect)
	for k := range d.Exec.Env {
		if k == "" || strings.ContainsAny(k, "= \t\n") {
			v.add("exec.env", "invalid variable name %q", k)
		}
	}
	validateInput(v, &d.Input)
	validateParse(v, "parse", &d.Parse, d.Fields, "fields", "")
	validateFields(v, "fields", d.Fields, 0, d.Parse.Type == TypeKV || d.Parse.Type == TypeINI)
	if len(v.errs) == 0 {
		return nil
	}
	return errors.Join(v.errs...)
}

// validateName checks the command or the variant a definition is filed
// under, which is also a directory name on every system jz runs on.
func validateName(v *validator, key, name string, re *regexp.Regexp) {
	switch {
	case name == "":
		v.add(key, "is required")
	case !re.MatchString(name):
		v.add(key, "%q must match %s", name, re)
	case reservedNames[name]:
		v.add(key, "%q is a reserved file name on Windows and cannot be used", name)
	}
}

// validateAliases checks the other names a definition answers to.
func validateAliases(v *validator, d *Definition) {
	seen := map[string]bool{}
	for i := range d.Aliases {
		ap := fmt.Sprintf("aliases[%d]", i)
		name := d.Aliases[i].Name
		switch {
		case name == "":
			v.add(ap+".name", "is required")
		case !nameRe.MatchString(name):
			v.add(ap+".name", "%q must match %s", name, nameRe)
		case name == d.Command:
			v.add(ap+".name", "%q is the command itself", name)
		case seen[name]:
			v.add(ap+".name", "duplicate alias %q", name)
		}
		seen[name] = true
		if am := d.Aliases[i].Args; am != nil {
			validateArgs(v, ap+".args", am)
		}
	}
}

// validateArgs refuses an empty word in an argument filter, which no
// command line holds and which would match nothing or everything.
func validateArgs(v *validator, path string, am *ArgsMatch) {
	for i, a := range append(append(append([]string{}, am.Any...), am.All...), am.None...) {
		if strings.TrimSpace(a) == "" {
			v.add(fmt.Sprintf("%s[%d]", path, i), "empty argument")
		}
	}
}

// validateDetect checks the systems, the arguments and the signature.
func validateDetect(v *validator, d *Detect) {
	for i, os := range d.OS {
		if !knownOS[os] {
			v.add(fmt.Sprintf("detect.os[%d]", i), "unknown operating system %q", os)
		}
	}
	validateArgs(v, "detect.args", &d.Args)
	sig := &d.Signature
	if sig.Window < 0 || sig.Window > MaxSignatureWindow {
		v.add("detect.signature.window", "must be between 0 and %d", MaxSignatureWindow)
	}
	// Signatures see several lines joined by newlines, so ^ and $ must
	// match at line boundaries.
	sig.all = compileList(v, "detect.signature.all", sig.All, "(?m)")
	sig.any = compileList(v, "detect.signature.any", sig.Any, "(?m)")
	sig.none = compileList(v, "detect.signature.none", sig.None, "(?m)")
}

// validateInput checks what happens to the lines before they are parsed.
func validateInput(v *validator, in *Input) {
	switch in.RecordSeparator {
	case "", RecordNewline, RecordNUL:
	default:
		v.add("input.record_separator", "must be newline or nul")
	}
	in.ignore = compileList(v, "input.ignore", in.Ignore, "")
	if in.Fold != "" {
		in.fold = v.regex("input.fold", in.Fold)
	}
	validateSelect(v, "input.select", &in.Select)
}

// compileList compiles each expression with flags prepended.
func compileList(v *validator, path string, exprs []string, flags string) *patternList {
	out := &patternList{items: make([]*pattern, 0, len(exprs))}
	for i, e := range exprs {
		if re := v.regex(fmt.Sprintf("%s[%d]", path, i), flags+e); re != nil {
			out.items = append(out.items, re)
		}
	}
	return out
}

func validateSelect(v *validator, path string, s *Select) {
	if s.After != "" {
		s.after = v.regex(path+".after", s.After)
		if s.after != nil {
			// The expression compiled on its own, so it compiles inside a
			// group too; the flags it may open with stay scoped to it.
			s.heading = &pattern{expr: `\A[ \t]*(?:` + s.After + `)[ \t]*\z`}
		}
	}
	if s.Until != "" {
		s.until = v.regex(path+".until", s.Until)
		if s.until != nil {
			s.end = &pattern{expr: `\A[ \t]*(?:` + s.Until + `)[ \t]*\z`}
		}
	}
	if s.Skip < 0 {
		v.add(path+".skip", "must not be negative")
	}
	if s.Limit < 0 {
		v.add(path+".limit", "must not be negative")
	}
}

// validateParse checks one parser. fields is the fields map beside it
// and fieldsPath where that map is written. parent is the type of the
// parser this one is a part of, empty at the top level. A composite part
// may be records, which is what a banner followed by repeating blocks
// needs; no other nesting is allowed, because anything deeper describes a
// tree whose shape comes from the input rather than from the definition.
func validateParse(v *validator, path string, p *Parse, fields map[string]*Field, fieldsPath, parent string) {
	if p.Type != TypeTree && (len(p.Indent) > 0 || p.Node != nil || p.Root != "") {
		v.add(path, "indent/node/root are only valid for type tree")
	}
	// A parser that reads its values through parts or nodes has no
	// values of its own to convert, so a fields map beside it would
	// apply to nothing. Refusing it is what keeps a rule written in the
	// wrong place from being silently left out.
	switch p.Type {
	case TypeComposite, TypeRecords:
		if len(fields) > 0 {
			where := "parts[].fields"
			if p.Type == TypeRecords && p.Record != nil {
				where = "record.fields"
			}
			v.add(fieldsPath, "does not apply to type %s, whose values are read by what it is made of; write the rules under %s", p.Type, where)
		}
	case TypeTree:
		if len(fields) > 0 {
			v.add(fieldsPath, "does not apply to type tree, whose values are read by its node; write the rules under node.fields")
		}
	}
	if p.Type != TypeRegex {
		for name, f := range fields {
			if f != nil && f.Unescape != nil && f.Unescape.When != "" {
				v.add("fields."+name+".unescape.when", "names a group, and only a regex parser has groups")
			}
		}
	}
	switch p.Type {
	case TypeTable:
		validateTable(v, path, p, fields)
		rejectKeys(v, path, p, "regex", "kv", "parts")
	case TypeRegex:
		validateRegexParse(v, path, p, fields)
		rejectKeys(v, path, p, "table", "kv", "parts")
	case TypeKV:
		validateKV(v, path, p)
		rejectKeys(v, path, p, "table", "regex", "parts")
	case TypeComposite:
		if parent != "" {
			v.add(path+".type", "a part cannot be composite")
			return
		}
		rejectRecordsKeys(v, path, p)
		validateComposite(v, path, p, TypeComposite)
		rejectKeys(v, path, p, "table", "regex", "kv")
	case TypeRecords:
		if parent == TypeRecords {
			v.add(path+".type", "records parts cannot be records")
			return
		}
		if p.Start == "" {
			v.add(path+".start", "is required for type records")
		} else {
			p.start = v.regex(path+".start", p.Start)
		}
		validateRecords(v, path, p)
		rejectKeys(v, path, p, "table", "regex", "kv")
	case TypeCSV:
		validateCSV(v, path, p, fields)
		rejectKeys(v, path, p, "regex", "kv", "parts")
	case TypeINI:
		validateINI(v, path, p)
		rejectKeys(v, path, p, "table", "regex", "parts")
	case TypeTree:
		validateTree(v, path, p)
		rejectKeys(v, path, p, "table", "regex", "kv", "parts")
	case "":
		v.add(path+".type", "is required (%s)", parseTypeList)
	default:
		v.add(path+".type", "unknown parse type %q (expected %s)", p.Type, parseTypeList)
	}
}

// parseTypeList names the parse types in error messages.
const parseTypeList = "table, csv, ini, tree, regex, kv, composite or records"

// validateCSV checks a csv parser. It is a table whose cells are cut by a
// delimiter that a value may itself contain, so it shares the header
// vocabulary and takes none of the whitespace-table settings.
func validateCSV(v *validator, path string, p *Parse, fields map[string]*Field) {
	if p.Split != "" {
		v.add(path+".split", "only valid for type table; a csv parser always cuts on its delimiter")
	}
	if d := []rune(p.Delimiter); len(d) > 1 {
		v.add(path+".delimiter", "must be a single character, not %q", p.Delimiter)
	} else if len(d) == 1 && !csvDelimiter(d[0]) {
		v.add(path+".delimiter", "cannot be %q: a quote, a line break, a NUL byte and a character that is not valid UTF-8 are what the quoting rules and the record separator are about", p.Delimiter)
	}
	if p.MaxFields != 0 || p.MinFields != 0 {
		v.add(path, "max_fields/min_fields are only valid for type table; a csv row states its own width")
	}
	if p.Header.LeadingLabel != "" {
		v.add(path+".header.leading_label", "only valid for type table")
	}
	validateHeader(v, path, p, fields)
}

// csvDelimiter reports whether r can cut a csv record, which is the
// rule the csv reader applies when it runs; checking it here is what
// makes a definition fail when it is loaded rather than when it is used.
func csvDelimiter(r rune) bool {
	return r != 0 && r != '"' && r != '\r' && r != '\n' && r != utf8.RuneError && utf8.ValidRune(r)
}

// validateTree checks a tree parser. The indentation unit is written out
// rather than counted, so that a report indented with tabs and one
// indented with two spaces are both stated the same way and neither is
// guessed.
func validateTree(v *validator, path string, p *Parse) {
	if len(p.Indent) == 0 {
		v.add(path+".indent", `is required for type tree: one level of indentation as it is written, "\t" or "  ", or the forms one level may take`)
	}
	for i, unit := range p.Indent {
		if unit == "" {
			v.add(fmt.Sprintf("%s.indent[%d]", path, i), "is empty, so it opens every line and no line ever ends")
		}
	}
	if p.Root != "" {
		p.root = v.regex(path+".root", p.Root)
	}
	if p.Node == nil {
		v.add(path+".node", "is required for type tree: how one line of the tree is read")
		return
	}
	switch p.Node.Parse.Type {
	case TypeRegex:
		if p.Node.Parse.Each == EachInput {
			v.add(path+".node.parse.each", "a tree node is one line, so each: input has nothing to match against")
		}
	case TypeKV:
		if p.Node.Parse.As == AsMap {
			v.add(path+".node.parse.as", "a tree node is one line, so a map of it would have one key")
		}
	case "":
		v.add(path+".node.parse.type", "is required (regex or kv)")
		return
	default:
		v.add(path+".node.parse.type", "a tree node is read with regex or kv, not %q", p.Node.Parse.Type)
		return
	}
	validateParse(v, path+".node.parse", &p.Node.Parse, p.Node.Fields, path+".node.fields", TypeTree)
	validateFields(v, path+".node.fields", p.Node.Fields, 0, p.Node.Parse.Type == TypeKV)
	// Every node carries its children under "children", so a key of
	// that name, whether a group reads it or a pattern's values state
	// it, would be set and then written over.
	for _, key := range p.Node.Parse.outputKeys() {
		if key == "children" {
			v.add(path+".node.parse", `names a key "children", which is where a node's children go; name it something else`)
			break
		}
	}
}

// outputKeys lists every key a regex parser can set: the named groups of
// its patterns and the fixed values they add.
func (p *Parse) outputKeys() []string {
	out := append([]string(nil), p.groups...)
	seen := map[string]bool{}
	for _, g := range out {
		seen[g] = true
	}
	for _, vals := range p.values {
		for _, val := range vals {
			if !seen[val.Name] {
				seen[val.Name] = true
				out = append(out, val.Name)
			}
		}
	}
	return out
}

// validateINI checks an ini parser. The sections make it an object of
// objects, so nothing about tables applies; what it shares with kv is how
// one line is cut.
func validateINI(v *validator, path string, p *Parse) {
	if p.As != "" {
		v.add(path+".as", "only valid for type kv; ini is always a map of sections")
	}
}

// rejectKeys reports keys that belong to other parse types.
func rejectKeys(v *validator, path string, p *Parse, families ...string) {
	for _, f := range families {
		switch f {
		case "table":
			if len(p.Header.Columns) > 0 || p.Header.None || p.Header.LeadingLabel != "" || len(p.Header.Rename) > 0 {
				v.add(path+".header", "only valid for type table and type csv")
			}
			if p.Split != "" || p.Delimiter != "" || p.MaxFields != 0 || p.MinFields != 0 {
				v.add(path, "split/delimiter/max_fields/min_fields are only valid for type table")
			}
		case "regex":
			if p.Pattern != "" || len(p.Patterns) > 0 || p.Each != "" {
				v.add(path, "pattern/patterns/each are only valid for type regex")
			}
		case "kv":
			if p.Separator != "" || p.As != "" || p.Trim != nil || p.Unquote {
				v.add(path, "separator/as/trim/unquote are only valid for type kv and type ini")
			}
		case "parts":
			if len(p.Parts) > 0 {
				v.add(path+".parts", "only valid for type composite or records")
			}
			rejectRecordsKeys(v, path, p)
		}
	}
}

// rejectRecordsKeys reports the keys only a records parser has, for a
// type that is not one.
func rejectRecordsKeys(v *validator, path string, p *Parse) {
	if p.Start != "" {
		v.add(path+".start", "only valid for type records")
	}
	if p.Record != nil {
		v.add(path+".record", "only valid for type records")
	}
}

func validateTable(v *validator, path string, p *Parse, fields map[string]*Field) {
	switch p.Split {
	case "", SplitWhitespace, SplitAligned, SplitBox:
		if p.Delimiter != "" {
			v.add(path+".delimiter", "only valid with split: delimiter")
		}
	case SplitDelimiter:
		if p.Delimiter == "" {
			v.add(path+".delimiter", "is required with split: delimiter")
		}
	default:
		v.add(path+".split", "unknown split mode %q (expected whitespace, aligned, delimiter or box)", p.Split)
	}
	if p.MaxFields < 0 || p.MinFields < 0 {
		v.add(path, "max_fields/min_fields must not be negative")
	}
	if p.MaxFields > 0 && p.MinFields > p.MaxFields {
		v.add(path+".min_fields", "exceeds max_fields")
	}
	if p.Split == SplitAligned && (p.MaxFields != 0 || p.MinFields != 0) {
		v.add(path, "max_fields/min_fields do not apply to split: aligned")
	}
	if p.Split == SplitBox {
		if p.MaxFields != 0 || p.MinFields != 0 {
			v.add(path, "max_fields/min_fields do not apply to split: box; the rules say where the cells are")
		}
		if p.Header.None {
			v.add(path+".header.none", "cannot be combined with split: box; a box table draws its header row")
		}
		if p.Header.LeadingLabel != "" {
			v.add(path+".header.leading_label", "cannot be combined with split: box; the bars say where the first cell is, so there is no unlabelled column to name")
		}
	}
	validateHeader(v, path, p, fields)
}

// validateHeader checks the header block of a table parser.
func validateHeader(v *validator, path string, p *Parse, fields map[string]*Field) {
	h := &p.Header
	// A csv with no header line and no names numbers its columns from the
	// width of its first record; a table cut on whitespace or alignment
	// has no such record to count, since how many cells a line holds is
	// what the columns decide there.
	if h.None && len(h.Columns) == 0 && p.Type != TypeCSV {
		v.add(path+".header.columns", "is required when header.none is true")
	}
	if h.None && h.LeadingLabel != "" {
		v.add(path+".header.leading_label", "cannot be combined with header.none")
	}
	if h.None && p.Split == SplitAligned {
		v.add(path+".split", "aligned requires a header line")
	}
	if len(h.Columns) > MaxColumns {
		v.add(path+".header.columns", "more than %d columns", MaxColumns)
	}
	seen := map[string]bool{}
	for i, c := range h.Columns {
		cp := fmt.Sprintf("%s.header.columns[%d]", path, i)
		if !fieldRe.MatchString(c) {
			v.add(cp, "invalid column name %q", c)
		}
		if seen[c] {
			v.add(cp, "duplicate column name %q", c)
		}
		seen[c] = true
	}
	if h.LeadingLabel != "" && !fieldRe.MatchString(h.LeadingLabel) {
		v.add(path+".header.leading_label", "invalid name %q", h.LeadingLabel)
	}
	if len(h.Columns) > 0 && len(h.Rename) > 0 {
		v.add(path+".header.rename", "cannot be combined with header.columns: explicit columns are the names, so a rename of a derived one would do nothing")
	}
	for from, to := range h.Rename {
		if !fieldRe.MatchString(to) {
			v.add(path+".header.rename."+from, "invalid name %q", to)
		}
	}
	// One more than the number of columns is how a definition asks for
	// the row to be counted rather than absorbed: the split then yields a
	// cell the columns have no name for, and the row is refused for
	// having more fields than the format has columns. Anything beyond
	// that says nothing further, since such a row is refused either way.
	if p.MaxFields != 0 && len(h.Columns) > 0 && p.MaxFields > len(h.Columns)+1 {
		v.add(path+".max_fields", "exceeds the number of columns (%d) by more than the one field that makes a row too wide", len(h.Columns))
	}
	if len(h.Columns) > 0 {
		known := seen
		if h.LeadingLabel != "" {
			known[h.LeadingLabel] = true
		}
		for name := range fields {
			if !known[name] {
				v.add("fields."+name, "is not one of the declared columns")
			}
		}
	}
}

func validateRegexParse(v *validator, path string, p *Parse, fields map[string]*Field) {
	alts := p.Patterns
	key := path + ".patterns"
	switch {
	case p.Pattern != "" && len(p.Patterns) > 0:
		v.add(path, "pattern and patterns are mutually exclusive")
		return
	case p.Pattern != "":
		alts = []Alternative{{Pattern: p.Pattern}}
		key = path + ".pattern"
	case len(p.Patterns) == 0:
		v.add(path+".pattern", "is required for type regex (or patterns for several alternatives)")
		return
	}
	if len(alts) > MaxPatterns {
		v.add(key, "more than %d alternatives", MaxPatterns)
		return
	}
	groupSet := map[string]bool{}
	valueSet := map[string]bool{}
	p.compiled = &patternList{items: make([]*pattern, 0, len(alts))}
	for i, alt := range alts {
		ep := key
		if len(alts) > 1 {
			ep = fmt.Sprintf("%s[%d]", key, i)
		}
		if alt.Pattern == "" {
			v.add(ep+".pattern", "is required")
			continue
		}
		re := v.regex(ep, alt.Pattern)
		if re == nil {
			continue
		}
		groups := namedGroups(re)
		vals := alternativeValues(v, ep, alt, groups)
		p.compiled.items = append(p.compiled.items, re)
		p.values = append(p.values, vals)
		if len(groups) == 0 && len(vals) == 0 {
			v.add(ep, "must contain at least one named group (?P<name>...)")
		}
		own := map[string]bool{}
		for _, g := range groups {
			if !fieldRe.MatchString(g) {
				v.add(ep, "group name %q is not a valid field name", g)
			}
			// Two groups of one pattern under one name would give the key
			// two answers, and the second would write over the first.
			// Two patterns may share a name: only one of them reads a line.
			if own[g] {
				v.add(ep, "names the group %q twice; a key holds one value, so the second would write over the first", g)
			}
			own[g] = true
			if !groupSet[g] {
				groupSet[g] = true
				p.groups = append(p.groups, g)
			}
		}
		for _, val := range vals {
			valueSet[val.Name] = true
		}
	}
	for name, f := range fields {
		if !groupSet[name] && !valueSet[name] {
			v.add("fields."+name, "is not a named group of any pattern")
		}
		if f != nil && f.Unescape != nil && f.Unescape.When != "" && !groupSet[f.Unescape.When] {
			v.add("fields."+name+".unescape.when", "%q is not a named group of any pattern", f.Unescape.When)
		}
	}
	switch p.Each {
	case "", EachLine, EachInput:
	default:
		v.add(path+".each", "must be line or input")
	}
}

// alternativeValues checks the fixed values of one alternative and returns
// them in the order of their names. A value may not share a name with a
// group of its own pattern, which would give the key two answers.
func alternativeValues(v *validator, path string, alt Alternative, groups []string) []Value {
	if len(alt.Values) == 0 {
		return nil
	}
	own := map[string]bool{}
	for _, g := range groups {
		own[g] = true
	}
	names := make([]string, 0, len(alt.Values))
	for name := range alt.Values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Value, 0, len(names))
	for _, name := range names {
		switch {
		case !fieldRe.MatchString(name):
			v.add(path+".values", "%q is not a valid field name", name)
		case own[name]:
			v.add(path+".values."+name, "is also a named group of the pattern")
		}
		out = append(out, Value{Name: name, Value: alt.Values[name]})
	}
	return out
}

func validateKV(v *validator, path string, p *Parse) {
	switch p.As {
	case "", AsList, AsMap:
	default:
		v.add(path+".as", "must be list or map")
	}
}

// validateRecords checks the two ways a record says how it is read:
// parts, the named regions of a block, or record, one parser over the
// whole of it. Exactly one of them describes a record; a block with
// nothing to name has no part names to invent, and a block with regions
// has no single parser to read all of it.
func validateRecords(v *validator, path string, p *Parse) {
	switch {
	case p.Record == nil:
		validateComposite(v, path, p, TypeRecords)
	case len(p.Parts) > 0:
		v.add(path, "record and parts both say how a record is read; write one of them")
	default:
		rp := path + ".record.parse"
		validateParse(v, rp, &p.Record.Parse, p.Record.Fields, path+".record.fields", TypeRecords)
		validateFields(v, path+".record.fields", p.Record.Fields, 0, p.Record.Parse.Type == TypeKV || p.Record.Parse.Type == TypeINI)
		// A record is one object, so the parser over it has to yield
		// one. A parser that yields a list would make the result a list
		// of lists, where what the definition meant is either a list of
		// objects or a part with a name to hold the list.
		if p.Record.Parse.Type != "" && p.Record.Parse.YieldsArray() {
			v.add(rp, "yields a list, and a record is one object; read the block with parts so the list has a name")
		}
	}
}

func validateComposite(v *validator, path string, p *Parse, kind string) {
	if len(p.Parts) == 0 {
		v.add(path+".parts", "is required for type %s", kind)
	}
	if len(p.Parts) > MaxParts {
		v.add(path+".parts", "more than %d parts", MaxParts)
	}
	seen := map[string]bool{}
	for i := range p.Parts {
		part := &p.Parts[i]
		pp := fmt.Sprintf("%s.parts[%d]", path, i)
		switch {
		case part.Name == "":
			v.add(pp+".name", "is required")
		case !fieldRe.MatchString(part.Name):
			v.add(pp+".name", "invalid name %q", part.Name)
		case seen[part.Name]:
			v.add(pp+".name", "duplicate part name %q", part.Name)
		}
		seen[part.Name] = true
		validateSelect(v, pp+".select", &part.Select)
		part.ignore = compileList(v, pp+".ignore", part.Ignore, "")
		validateParse(v, pp+".parse", &part.Parse, part.Fields, pp+".fields", kind)
		validateFields(v, pp+".fields", part.Fields, 0, part.Parse.Type == TypeKV || part.Parse.Type == TypeINI)
	}
}

// validateFields checks the entries of a fields map. rawKeys is set for a
// kv parse, whose entries are looked up by the key the command printed
// rather than by a name the definition chose: "CPU(s)" and "Thread(s) per
// core" are what lscpu prints, and a definition that cannot name them
// cannot convert their values.
func validateFields(v *validator, path string, fields map[string]*Field, depth int, rawKeys bool) {
	if depth > MaxNesting {
		v.add(path, "fields nested deeper than %d levels", MaxNesting)
		return
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := fields[name]
		fp := path + "." + name
		switch {
		case rawKeys && (name == "" || strings.ContainsAny(name, "\n\r")):
			v.add(fp, "invalid key %q", name)
		case !rawKeys && !fieldRe.MatchString(name):
			v.add(fp, "invalid field name %q", name)
		}
		if f == nil {
			continue
		}
		validateField(v, fp, f, depth)
	}
}

func validateField(v *validator, path string, f *Field, depth int) {
	if depth > MaxNesting {
		v.add(path, "fields nested deeper than %d levels", MaxNesting)
		return
	}
	switch f.EffectiveType() {
	case FieldString:
		validateStringField(v, path, f)
	case FieldInt, FieldFloat:
	case FieldBool:
		validateBoolField(v, path, f)
	case FieldTime:
		validateTimeField(v, path, f)
	case FieldDuration:
		validateDurationField(v, path, f)
	case FieldArray:
		validateArrayField(v, path, f, depth)
	case FieldObject:
		validateObjectField(v, path, f, depth)
	default:
		v.add(path+".type", "unknown type %q (expected string, int, float, bool, time, duration, array or object)", f.Type)
	}
	validateFieldKeys(v, path, f)
}

// validateStringField checks what a string field may carry besides its
// text: a regex whose one named group is the value, and the escapes to
// undo in it.
func validateStringField(v *validator, path string, f *Field) {
	if f.Regex != "" {
		// The expression describes the whole value, so it is anchored at
		// both ends whatever it says itself: a value that only begins
		// with the shape is not that shape.
		if re := v.regex(path+".regex", `\A(?:`+f.Regex+`)\z`); re != nil {
			f.regex = re
			f.groups = namedGroups(re)
			if len(f.groups) != 1 {
				v.add(path+".regex", "on a string field must have exactly one named group, the part of the value that is kept")
			}
		}
	}
	if u := f.Unescape; u != nil {
		if len(u.Sequences) == 0 {
			v.add(path+".unescape.sequences", "is required: the escapes the format writes and what each stands for")
		}
		if u.Quote != "" && utf8.RuneCountInString(u.Quote) != 1 {
			v.add(path+".unescape.quote", "is one character, the one the format puts around a value it escaped")
		}
		if u.Quote != "" && u.When != "" {
			v.add(path+".unescape", "quote and when both say when a value is decoded; write one of them")
		}
		var esc byte
		for k := range u.Sequences {
			if len(k) < 2 {
				v.add(path+".unescape.sequences", "%q is not an escape: an escape is its escape character and at least one more", k)
				continue
			}
			if esc == 0 {
				esc = k[0]
			} else if k[0] != esc {
				v.add(path+".unescape.sequences", "every escape begins with the same character, and %q does not begin with %q", k, string(esc))
			}
		}
	}
}

func validateBoolField(v *validator, path string, f *Field) {
	for i, t := range f.True {
		for _, fl := range f.False {
			if strings.EqualFold(t, fl) {
				v.add(fmt.Sprintf("%s.true_values[%d]", path, i), "%q is also listed in false_values", t)
			}
		}
	}
}

// validateTimeField checks a time field. A layout that names no year is
// only allowed alongside year: assumed, because a format that prints
// none (an `ls -l` listing of a recent file, a syslog line) can only be
// dated by being told which year it meant. Saying so is a statement
// about the format; the value stays the string it was printed as until
// the command line supplies the year.
func validateTimeField(v *validator, path string, f *Field) {
	switch {
	case f.Layout == "":
		v.add(path+".layout", "is required for type time (a Go reference layout, e.g. \"2006-01-02 15:04:05\")")
	case !strings.Contains(f.Layout, "06") && f.Year != YearAssumed:
		v.add(path+".layout", "%q states no year; write year: assumed beside it if the format prints none, and the value stays a string until --assume-year says which year to use", f.Layout)
	case strings.Contains(f.Layout, "06") && f.Year == YearAssumed:
		v.add(path+".year", "assumed contradicts a layout that already carries a year")
	}
	if _, ok := convert.Location(f.Location); !ok {
		v.add(path+".location", "must be utc or local, not %q", f.Location)
	}
}

// validateDurationField checks a duration field. The layout is one of two
// words rather than a Go layout, and it is required: a bare two-part
// reading is minutes and seconds in one format and hours and minutes in
// another, nothing in the text says which, and being wrong about it is a
// factor of sixty that nothing downstream would notice.
func validateDurationField(v *validator, path string, f *Field) {
	switch f.Layout {
	case convert.LayoutHourMinute, convert.LayoutMinuteSecond:
	case "":
		v.add(path+".layout", "is required for type duration: %s or %s, saying what the last part of a bare \"4:50\" is", convert.LayoutHourMinute, convert.LayoutMinuteSecond)
	default:
		v.add(path+".layout", "must be %s or %s for type duration, not %q", convert.LayoutHourMinute, convert.LayoutMinuteSecond, f.Layout)
	}
	if f.Location != "" {
		v.add(path+".location", "is only valid for type time")
	}
}

func validateArrayField(v *validator, path string, f *Field, depth int) {
	if f.Split == "" && f.SplitRegex == "" {
		v.add(path, "array fields need split or split_regex")
	}
	if f.Split != "" && f.SplitRegex != "" {
		v.add(path, "split and split_regex are mutually exclusive")
	}
	if f.SplitRegex != "" {
		f.splitRegex = v.regex(path+".split_regex", f.SplitRegex)
		// A separator that can be nothing splits between every character,
		// so "abc def" would read as a list of its letters.
		if f.splitRegex != nil && f.splitRegex.regexp().MatchString("") {
			v.add(path+".split_regex", "matches the empty string, which splits a value between every character; write a separator that is at least one character (`[ \\t]+`, not `[ \\t]*`)")
		}
	}
	if f.Items != nil {
		if f.Items.EffectiveType() == FieldArray {
			v.add(path+".items", "nested arrays are not supported; use an object item with an array field")
		}
		if f.Items.Unescape != nil && f.Items.Unescape.When != "" {
			v.add(path+".items.unescape.when", "names a group, and an item of an array is not read by one")
		}
		validateField(v, path+".items", f.Items, depth+1)
	}
}

func validateObjectField(v *validator, path string, f *Field, depth int) {
	if f.Regex == "" {
		v.add(path+".regex", "is required for object fields")
	} else if re := v.regex(path+".regex", f.Regex); re != nil {
		f.regex = re
		f.groups = namedGroups(re)
		if len(f.groups) == 0 {
			v.add(path+".regex", "must contain at least one named group")
		}
		groupSet := map[string]bool{}
		for _, g := range f.groups {
			groupSet[g] = true
		}
		for name, sub := range f.Fields {
			if !groupSet[name] {
				v.add(path+".fields."+name, "is not a named group of the regex")
			}
			if sub != nil && sub.Unescape != nil && sub.Unescape.When != "" && !groupSet[sub.Unescape.When] {
				v.add(path+".fields."+name+".unescape.when", "%q is not a named group of the regex", sub.Unescape.When)
			}
		}
	}
	// A nested fields map is keyed by regex group names, which are
	// identifiers by construction.
	validateFields(v, path+".fields", f.Fields, depth+1, false)
}

// validateFieldKeys rejects keys that belong to a different field type.
func validateFieldKeys(v *validator, path string, f *Field) {
	if f.EffectiveType() != FieldArray && (f.Split != "" || f.SplitRegex != "" || f.Items != nil) {
		v.add(path, "split/split_regex/items are only valid for type array")
	}
	switch f.EffectiveType() {
	case FieldObject:
	case FieldString:
		if len(f.Fields) > 0 {
			v.add(path, "fields is only valid for type object")
		}
	default:
		if f.Regex != "" || len(f.Fields) > 0 {
			v.add(path, "regex is only valid for type object and type string, fields for type object")
		}
	}
	if f.Unescape != nil && f.EffectiveType() != FieldString {
		v.add(path+".unescape", "is only valid for type string")
	}
	if f.EffectiveType() != FieldBool && (len(f.True) > 0 || len(f.False) > 0) {
		v.add(path, "true_values/false_values are only valid for type bool")
	}
	validateGroupSeparator(v, path, f)
	switch f.EffectiveType() {
	case FieldTime:
	case FieldDuration:
		if f.Year != "" {
			v.add(path+".year", "is only valid for type time")
		}
	default:
		if f.Layout != "" || f.Location != "" {
			v.add(path, "layout/location are only valid for type time and type duration")
		}
		if f.Year != "" {
			v.add(path+".year", "is only valid for type time")
		}
	}
	if f.Year != "" && f.Year != YearAssumed {
		v.add(path+".year", "must be %q, not %q", YearAssumed, f.Year)
	}
	switch f.WhenMissing {
	case "", MissingNull, MissingOmit:
	default:
		v.add(path+".when_missing", "must be null or omit")
	}
	if f.Required && f.WhenMissing == MissingOmit {
		v.add(path, "required and when_missing: omit are contradictory")
	}
}

// validateGroupSeparator checks the character a format writes between
// groups of three digits. It has to be one character that is not part of
// a number, and on a float field it may not be the decimal point: the
// same value would then be two numbers.
func validateGroupSeparator(v *validator, path string, f *Field) {
	if f.GroupSeparator == "" {
		return
	}
	switch f.EffectiveType() {
	case FieldInt, FieldFloat:
	default:
		v.add(path+".group_separator", "is only valid for type int and type float")
		return
	}
	if utf8.RuneCountInString(f.GroupSeparator) != 1 {
		v.add(path+".group_separator", "is one character, the one the format writes between groups of three digits")
		return
	}
	if strings.ContainsAny(f.GroupSeparator, "0123456789+-eE") {
		v.add(path+".group_separator", "%q is part of a number and cannot separate its digits", f.GroupSeparator)
	}
	if f.EffectiveType() == FieldFloat && f.GroupSeparator == "." {
		v.add(path+".group_separator", "\".\" is the decimal point of a float field")
	}
}

func namedGroups(re *pattern) []string {
	var out []string
	for _, n := range re.names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ColumnTypes are the types WithTypes converts a column to.
func ColumnTypes() []string { return []string{FieldInt, FieldFloat, FieldBool} }

// WithTypes returns a copy of a csv definition that converts the columns
// types names, each to int, float or bool, an empty value to null and a
// value that is not one to an error naming the line and the column. It
// is how a caller types the columns of a data file without writing a
// definition out, and the conversion is the one a definition's field of
// that type does. A column the definition converts already is refused
// rather than converted twice; a column the input does not have is
// refused when it is read (Header.Expected, engine.MissingColumnError).
func (d *Definition) WithTypes(types map[string]string) (*Definition, error) {
	if d.Parse.Type != TypeCSV {
		return nil, fmt.Errorf("%s does not read a csv, so there are no columns to convert", d.ID())
	}
	fields := make(map[string]*Field, len(d.Fields)+len(types))
	maps.Copy(fields, d.Fields)
	for _, name := range slices.Sorted(maps.Keys(types)) {
		typ := types[name]
		switch {
		case !fieldRe.MatchString(name):
			return nil, fmt.Errorf("invalid column name %q: a name is letters, digits and _ . : @ -, and starts with a letter, a digit or _", name)
		case !slices.Contains(ColumnTypes(), typ):
			return nil, fmt.Errorf("column %q: unknown type %q; the types are %s", name, typ, strings.Join(ColumnTypes(), ", "))
		case d.Fields[name] != nil:
			return nil, fmt.Errorf("%s converts the column %q already", d.ID(), name)
		}
		fields[name] = &Field{Type: typ, NullIf: []string{""}}
	}
	c := *d
	c.Fields = fields
	c.Parse.Header.Expected = append(slices.Clone(d.Parse.Header.Expected), slices.Sorted(maps.Keys(types))...)
	return &c, nil
}

// WithColumns returns a copy of a csv definition with no header line and
// no column names, naming the columns it would otherwise number
// (column_1, column_2, ...). It is how a caller names the columns of a
// file whose definition can only count them, without writing the
// definition out. The names follow the rules header.columns does, and a
// definition that converts a numbered column cannot have it renamed from
// under the conversion.
func (d *Definition) WithColumns(names []string) (*Definition, error) {
	p := &d.Parse
	switch {
	case p.Type != TypeCSV || !p.Header.None:
		return nil, fmt.Errorf("%s does not read a csv without a header line, so there are no columns to name", d.ID())
	case len(p.Header.Columns) > 0:
		return nil, fmt.Errorf("%s names its columns already: %s", d.ID(), strings.Join(p.Header.Columns, ", "))
	case len(names) == 0:
		return nil, errors.New("no column names given")
	case len(names) > MaxColumns:
		return nil, fmt.Errorf("%d column names, more than the %d a definition may name", len(names), MaxColumns)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if !fieldRe.MatchString(n) {
			return nil, fmt.Errorf("invalid column name %q: a name is letters, digits and _ . : @ -, and starts with a letter, a digit or _", n)
		}
		if seen[n] {
			return nil, fmt.Errorf("column name %q is given twice", n)
		}
		seen[n] = true
	}
	for name := range d.Fields {
		if !seen[name] {
			return nil, fmt.Errorf("%s converts the column %q, which the names given do not include", d.ID(), name)
		}
	}
	c := *d
	c.Parse.Header.Columns = append([]string(nil), names...)
	return &c, nil
}

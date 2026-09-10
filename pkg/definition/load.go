package definition

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/nao1215/jsonize/internal/buildinfo"
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

// unknownFieldRe reads the key and its position out of the strict
// decoder's message, which is the only place they are reported.
var unknownFieldRe = regexp.MustCompile(`(?:\[(\d+):\d+\] )?unknown field "([^"]+)"`)

// Load decodes, validates and compiles a definition. source is used in
// error messages and stored in Definition.Source.
func Load(data []byte, source string) (*Definition, error) {
	if len(data) > MaxDefinitionSize {
		return nil, &ValidationError{Source: source, Msg: fmt.Sprintf("definition exceeds %d bytes", MaxDefinitionSize)}
	}
	var d Definition
	if err := DecodeYAML(data, &d); err != nil {
		if m := unknownFieldRe.FindStringSubmatch(err.Error()); m != nil {
			line, _ := strconv.Atoi(m[1])
			return nil, &UnknownKeyError{Source: source, Key: m[2], Line: line}
		}
		return nil, &ValidationError{Source: source, Msg: "invalid YAML: " + err.Error()}
	}
	d.Source = source
	if d.Format != CurrentFormat {
		return nil, &FormatError{Source: source, Got: d.Format}
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
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
// Everything else — input, parse, fields — is the same schema and goes
// through the same validation, so an inline definition cannot express
// anything a file cannot.
func LoadInline(body []byte, source string) (*Definition, error) {
	var probe struct {
		Format  int    `yaml:"format"`
		Command string `yaml:"command"`
		Variant string `yaml:"variant"`
		Detect  any    `yaml:"detect"`
	}
	// A body is decoded loosely once to name the keys that place a
	// definition, so that writing one gets an explanation rather than
	// "unknown key" from the strict pass below.
	if err := yaml.Unmarshal(body, &probe); err == nil {
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
	full := fmt.Sprintf("format: %d\ncommand: %s\nvariant: %s\n", CurrentFormat, InlineCommand, InlineVariant)
	return Load(append([]byte(full), body...), source)
}

// DecodeYAML decodes strictly and turns a decoder panic into an error. A
// definition may come from an untrusted registry, and the YAML library
// has been observed to panic on some malformed tagged scalars; a broken
// file must never take jz down.
func DecodeYAML(data []byte, v any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decoder failure: %v", r)
		}
	}()
	if err := yaml.UnmarshalWithOptions(data, v, yaml.Strict()); err != nil {
		return errors.New(strings.TrimSpace(yaml.FormatError(err, false, false)))
	}
	return nil
}

// validator accumulates problems for one definition.
type validator struct {
	source string
	errs   []error
}

func (v *validator) add(path, format string, args ...any) {
	v.errs = append(v.errs, &ValidationError{Source: v.source, Path: path, Msg: fmt.Sprintf(format, args...)})
}

func (v *validator) regex(path, expr string) *regexp.Regexp {
	if len(expr) > MaxRegexLength {
		v.add(path, "regular expression longer than %d characters", MaxRegexLength)
		return nil
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		var se *syntax.Error
		if errors.As(err, &se) {
			v.add(path, "invalid regular expression: %s", se.Code)
		} else {
			v.add(path, "invalid regular expression: %v", err)
		}
		return nil
	}
	return re
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
	switch {
	case d.Command == "":
		v.add("command", "is required")
	case !nameRe.MatchString(d.Command):
		v.add("command", "%q must match %s", d.Command, nameRe)
	case reservedNames[d.Command]:
		v.add("command", "%q is a reserved file name on Windows and cannot be used", d.Command)
	}
	switch {
	case d.Variant == "":
		v.add("variant", "is required")
	case !variantRe.MatchString(d.Variant):
		v.add("variant", "%q must match %s", d.Variant, variantRe)
	case reservedNames[d.Variant]:
		v.add("variant", "%q is a reserved file name on Windows and cannot be used", d.Variant)
	}
	seenAlias := map[string]bool{}
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
		case seenAlias[name]:
			v.add(ap+".name", "duplicate alias %q", name)
		}
		seenAlias[name] = true
		if am := d.Aliases[i].Args; am != nil {
			for j, a := range append(append(append([]string{}, am.Any...), am.All...), am.None...) {
				if strings.TrimSpace(a) == "" {
					v.add(fmt.Sprintf("%s.args[%d]", ap, j), "empty argument")
				}
			}
		}
	}
	for i, os := range d.Detect.OS {
		if !knownOS[os] {
			v.add(fmt.Sprintf("detect.os[%d]", i), "unknown operating system %q", os)
		}
	}
	for i, a := range append(append(append([]string{}, d.Detect.Args.Any...), d.Detect.Args.All...), d.Detect.Args.None...) {
		if strings.TrimSpace(a) == "" {
			v.add(fmt.Sprintf("detect.args[%d]", i), "empty argument")
		}
	}
	sig := &d.Detect.Signature
	if sig.Window < 0 || sig.Window > MaxSignatureWindow {
		v.add("detect.signature.window", "must be between 0 and %d", MaxSignatureWindow)
	}
	// Signatures see several lines joined by newlines, so ^ and $ must
	// match at line boundaries.
	sig.all = compileList(v, "detect.signature.all", sig.All, "(?m)")
	sig.any = compileList(v, "detect.signature.any", sig.Any, "(?m)")
	sig.none = compileList(v, "detect.signature.none", sig.None, "(?m)")
	for k := range d.Exec.Env {
		if k == "" || strings.ContainsAny(k, "= \t\n") {
			v.add("exec.env", "invalid variable name %q", k)
		}
	}
	switch d.Input.RecordSeparator {
	case "", RecordNewline, RecordNUL:
	default:
		v.add("input.record_separator", "must be newline or nul")
	}
	d.Input.ignore = compileList(v, "input.ignore", d.Input.Ignore, "")
	if d.Input.Fold != "" {
		d.Input.fold = v.regex("input.fold", d.Input.Fold)
	}
	validateSelect(v, "input.select", &d.Input.Select)
	validateParse(v, "parse", &d.Parse, d.Fields, "")
	validateFields(v, "fields", d.Fields, 0, d.Parse.Type == TypeKV || d.Parse.Type == TypeINI)
	if len(v.errs) == 0 {
		return nil
	}
	return errors.Join(v.errs...)
}

// compileList compiles each expression with flags prepended.
func compileList(v *validator, path string, exprs []string, flags string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(exprs))
	for i, e := range exprs {
		if re := v.regex(fmt.Sprintf("%s[%d]", path, i), flags+e); re != nil {
			out = append(out, re)
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
			s.heading = regexp.MustCompile(`\A[ \t]*(?:` + s.After + `)[ \t]*\z`)
		}
	}
	if s.Until != "" {
		s.until = v.regex(path+".until", s.Until)
	}
	if s.Skip < 0 {
		v.add(path+".skip", "must not be negative")
	}
	if s.Limit < 0 {
		v.add(path+".limit", "must not be negative")
	}
}

// validateParse checks one parser. parent is the type of the parser this
// one is a part of, empty at the top level. A composite part may be
// records, which is what a banner followed by repeating blocks needs; no
// other nesting is allowed, because anything deeper describes a tree
// whose shape comes from the input rather than from the definition.
func validateParse(v *validator, path string, p *Parse, fields map[string]*Field, parent string) {
	if p.Type != TypeTree && (len(p.Indent) > 0 || p.Node != nil) {
		v.add(path, "indent/node are only valid for type tree")
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
		if p.Start != "" {
			v.add(path+".start", "only valid for type records")
		}
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
		validateComposite(v, path, p, TypeRecords)
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
	} else if len(d) == 1 && (d[0] == '"' || d[0] == '\r' || d[0] == '\n') {
		v.add(path+".delimiter", "cannot be %q: a quote and a line break are what the quoting rules are about", p.Delimiter)
	}
	if p.MaxFields != 0 || p.MinFields != 0 {
		v.add(path, "max_fields/min_fields are only valid for type table; a csv row states its own width")
	}
	if p.Header.LeadingLabel != "" {
		v.add(path+".header.leading_label", "only valid for type table")
	}
	validateHeader(v, path, p, fields)
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
	validateParse(v, path+".node.parse", &p.Node.Parse, p.Node.Fields, TypeTree)
	validateFields(v, path+".node.fields", p.Node.Fields, 0, p.Node.Parse.Type == TypeKV)
	// Every node carries its children under "children", so a group of
	// that name would be read and then written over.
	for _, g := range p.Node.Parse.Groups() {
		if g == "children" {
			v.add(path+".node.parse", `names a group "children", which is where a node's children go; name it something else`)
			break
		}
	}
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
			if p.Start != "" {
				v.add(path+".start", "only valid for type records")
			}
		}
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
	if p.MaxFields != 0 && p.MinFields > p.MaxFields {
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
	}
	validateHeader(v, path, p, fields)
}

// validateHeader checks the header block of a table parser.
func validateHeader(v *validator, path string, p *Parse, fields map[string]*Field) {
	h := &p.Header
	if h.None && len(h.Columns) == 0 {
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
	for from, to := range h.Rename {
		if !fieldRe.MatchString(to) {
			v.add(path+".header.rename."+from, "invalid name %q", to)
		}
	}
	if p.MaxFields != 0 && len(h.Columns) > 0 && p.MaxFields > len(h.Columns) {
		v.add(path+".max_fields", "exceeds the number of columns (%d)", len(h.Columns))
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
	exprs := p.Patterns
	key := path + ".patterns"
	switch {
	case p.Pattern != "" && len(p.Patterns) > 0:
		v.add(path, "pattern and patterns are mutually exclusive")
		return
	case p.Pattern != "":
		exprs = []string{p.Pattern}
		key = path + ".pattern"
	case len(p.Patterns) == 0:
		v.add(path+".pattern", "is required for type regex (or patterns for several alternatives)")
		return
	}
	if len(exprs) > MaxPatterns {
		v.add(key, "more than %d alternatives", MaxPatterns)
		return
	}
	groupSet := map[string]bool{}
	for i, expr := range exprs {
		ep := key
		if len(exprs) > 1 {
			ep = fmt.Sprintf("%s[%d]", key, i)
		}
		re := v.regex(ep, expr)
		if re == nil {
			continue
		}
		p.compiled = append(p.compiled, re)
		groups := namedGroups(re)
		if len(groups) == 0 {
			v.add(ep, "must contain at least one named group (?P<name>...)")
		}
		for _, g := range groups {
			if !fieldRe.MatchString(g) {
				v.add(ep, "group name %q is not a valid field name", g)
			}
			if !groupSet[g] {
				groupSet[g] = true
				p.groups = append(p.groups, g)
			}
		}
	}
	for name := range fields {
		if !groupSet[name] {
			v.add("fields."+name, "is not a named group of any pattern")
		}
	}
	switch p.Each {
	case "", EachLine, EachInput:
	default:
		v.add(path+".each", "must be line or input")
	}
}

func validateKV(v *validator, path string, p *Parse) {
	switch p.As {
	case "", AsList, AsMap:
	default:
		v.add(path+".as", "must be list or map")
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
		validateParse(v, pp+".parse", &part.Parse, part.Fields, kind)
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
	case FieldString, FieldInt, FieldFloat:
	case FieldBool:
		validateBoolField(v, path, f)
	case FieldSize:
		switch f.Unit {
		case "", UnitBinary, UnitDecimal:
		default:
			v.add(path+".unit", "must be binary or decimal")
		}
	case FieldTime:
		validateTimeField(v, path, f)
	case FieldDuration:
		validateDurationField(v, path, f)
	case FieldArray:
		validateArrayField(v, path, f, depth)
	case FieldObject:
		validateObjectField(v, path, f, depth)
	default:
		v.add(path+".type", "unknown type %q (expected string, int, float, bool, size, time, duration, array or object)", f.Type)
	}
	validateFieldKeys(v, path, f)
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
	}
	if f.Items != nil {
		if f.Items.EffectiveType() == FieldArray {
			v.add(path+".items", "nested arrays are not supported; use an object item with an array field")
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
		for name := range f.Fields {
			if !groupSet[name] {
				v.add(path+".fields."+name, "is not a named group of the regex")
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
	if f.EffectiveType() != FieldObject && (f.Regex != "" || len(f.Fields) > 0) {
		v.add(path, "regex/fields are only valid for type object")
	}
	if f.EffectiveType() != FieldBool && (len(f.True) > 0 || len(f.False) > 0) {
		v.add(path, "true_values/false_values are only valid for type bool")
	}
	if f.EffectiveType() != FieldSize && f.Unit != "" {
		v.add(path+".unit", "only valid for type size")
	}
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

func namedGroups(re *regexp.Regexp) []string {
	var out []string
	for _, n := range re.SubexpNames() {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

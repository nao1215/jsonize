// Package jsonbuild makes a JSON value from arguments given on a command
// line, which is what `jz new` prints.
//
// An object is made of arguments that are a key, an operator and a value:
//
//	name=api             the string "api"
//	replicas:=3          the JSON value 3; any JSON: true, null, "text", [1,2], {"a":1}
//	notes=@NOTES.txt     the text of a file, less its last line ending, as a string
//	spec:=@deploy.yaml   the value a data file holds, read as its extension names
//	tags[]=a             one more element of the array tags
//
// Three option forms place a value where a plain argument cannot:
//
//	--string KEY=TEXT        TEXT as it is, never read as JSON or a file name
//	--text-file KEY=PATH     a file's text with every line ending kept
//	--path POINTER=VALUE     any of the operators above, at a JSON Pointer
//
// The KEY of --string and --text-file is a key as above, or a JSON Pointer
// (RFC 6901) when it starts with "/".
//
// An array (--array) is made of values written the same way without the
// key: `api`, `=api`, `:=3`, `=@NOTES.txt`, `:=@deploy.yaml`; an empty KEY
// or the pointer /- appends, and a pointer into it starts with /- or the
// index of an element already given.
//
// Nothing is guessed. `=` always makes a string, so "007" and "true" stay
// strings, and `:=` always reads JSON. A path of `-` reads standard input,
// which only one argument may do.
//
// A location is given once. A key given twice without [] is refused, since
// the arguments do not say which of the two values is meant, and so is a
// key given both with and without []. A pointer creates the objects and
// arrays on its way that no argument gave yet, and never enters a value
// given whole: jz makes JSON, it does not edit it.
package jsonbuild

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// Form is how an argument was given on the command line.
type Form int

// The forms.
const (
	// Plain is KEY=VALUE, KEY:=JSON, KEY=@FILE or KEY:=@FILE, or a value
	// of --array.
	Plain Form = iota
	// String is --string KEY=TEXT.
	String
	// TextFile is --text-file KEY=PATH.
	TextFile
	// Path is --path POINTER=VALUE and its other operators.
	Path
)

// Arg is one argument and the form it was given in.
type Arg struct {
	Form Form
	Text string
}

// String is the argument as it was written, the option included.
func (a Arg) String() string {
	switch a.Form {
	case Plain:
	case String:
		return "--string " + a.Text
	case TextFile:
		return "--text-file " + a.Text
	case Path:
		return "--path " + a.Text
	}
	return a.Text
}

// UsageError is an argument that does not say what JSON to make: no
// operator, an empty key, a location given twice, JSON that is not JSON.
type UsageError struct {
	Arg string
	Msg string
}

// quotedArgBytes is how much of an argument an error quotes; a longer
// one is cut and ended with "...".
const quotedArgBytes = 64

func (e *UsageError) Error() string {
	arg := e.Arg
	if len(arg) > quotedArgBytes {
		cut := quotedArgBytes
		for cut > 0 && !utf8.RuneStart(arg[cut]) {
			cut--
		}
		arg = arg[:cut] + "..."
	}
	return fmt.Sprintf("%q: %s", arg, e.Msg)
}

// InputError is a file an argument named that cannot be read, or whose
// text is not what the argument said it is. Err is the reason, a
// *engine.ParseError when the file is not the format it was read as.
type InputError struct {
	Arg string
	Err error
}

func (e *InputError) Error() string {
	return fmt.Sprintf("%s: %v", e.Arg, e.Err)
}

func (e *InputError) Unwrap() error { return e.Err }

// ErrNotUTF8 is a file read as text that is not UTF-8.
var ErrNotUTF8 = errors.New("the text is not valid UTF-8")

// Sources are where the values an argument names with @ come from.
type Sources struct {
	// ReadFile reads a file. It bounds the size of what it returns.
	ReadFile func(path string) ([]byte, error)
	// Stdin is read, whole, by the one argument whose path is "-".
	Stdin io.Reader
	// MaxSize bounds what is read from Stdin.
	MaxSize int64
	// MaxValues bounds the values one document holds, the files it is made
	// of together (0 = engine.DefaultMaxValues).
	MaxValues int
}

func (s Sources) maxValues() int {
	if s.MaxValues <= 0 {
		return engine.DefaultMaxValues
	}
	return s.MaxValues
}

// kind is what an argument's value is made from.
type kind int

const (
	literal    kind = iota // text as it was written
	inlineJSON             // JSON written in the argument, read when it was parsed
	fileText               // a file's text, less its last line ending
	fileWhole              // a file's text as it is
	fileData               // the value a data file holds
)

// token is one step of a location: a key of an object, an index of an
// array element already given, or the end of an array to append at.
type token struct {
	name string
	// appends is a step past the end of an array: KEY[] or the pointer
	// token "-".
	appends bool
	// index is a pointer token that is a decimal number, which names an
	// element in an array and a key in an object.
	index bool
}

type placement struct {
	arg  Arg
	loc  []token
	kind kind
	// text is the literal text or the path, and value the JSON an
	// inlineJSON argument holds.
	text  string
	value any
	// bare is a Plain argument of an object, whose conflicts keep the
	// messages they have always had.
	bare bool
}

// Plan is what the arguments say to make, checked against each other and
// with nothing read yet.
type Plan struct {
	root   *node
	places []*placement
}

type nodeKind int

const (
	objectNode nodeKind = iota
	arrayNode
	valueNode
)

// node is one value of the document being planned. An object or an array
// a location made is open to more locations; a value an argument gave is
// closed.
type node struct {
	kind    nodeKind
	keys    []string
	members map[string]*node
	elems   []*node
	// by is the argument that made the node, and place the one whose value
	// it is.
	by    *placement
	place int
}

func newContainer(k nodeKind, by *placement) *node {
	return &node{kind: k, members: map[string]*node{}, by: by}
}

// Parse reads the arguments of an object, or of an array when array is
// set, and plans the document. Every refusal the arguments can earn is
// given here, before a file or standard input is read.
func Parse(args []Arg, array bool) (*Plan, error) {
	rootKind := objectNode
	if array {
		rootKind = arrayNode
	}
	p := &Plan{root: newContainer(rootKind, nil)}
	for _, a := range args {
		pl, err := parseArg(a, array)
		if err != nil {
			return nil, err
		}
		if err := p.place(pl); err != nil {
			return nil, err
		}
		p.places = append(p.places, pl)
	}
	if err := oneStdin(p.places); err != nil {
		return nil, err
	}
	return p, nil
}

func parseArg(a Arg, array bool) (*placement, error) {
	use := func(msg string) error { return &UsageError{Arg: a.String(), Msg: msg} }
	if !utf8.ValidString(a.Text) {
		return nil, use("the argument is not valid UTF-8")
	}
	pl := &placement{arg: a}
	switch a.Form {
	case Plain:
		if array {
			return pl, splitElement(pl, use)
		}
		return pl, split(pl, use)
	case String, TextFile:
		loc, value, ok := strings.Cut(a.Text, "=")
		if !ok {
			if a.Form == String {
				return nil, use("--string takes KEY=TEXT or POINTER=TEXT as one argument")
			}
			return nil, use("--text-file takes KEY=PATH or POINTER=PATH as one argument")
		}
		tokens, err := location(loc, array, a.Form)
		if err != nil {
			return nil, use(err.Error())
		}
		pl.loc, pl.kind, pl.text = tokens, literal, value
		if a.Form == TextFile {
			if value == "" {
				return nil, use("the path is empty; - is standard input")
			}
			pl.kind = fileWhole
		}
		return pl, nil
	case Path:
		i := strings.IndexByte(a.Text, '=')
		if i < 0 {
			return nil, use("--path takes POINTER=VALUE, POINTER:=JSON, POINTER=@FILE or POINTER:=@FILE")
		}
		ptr, json := a.Text[:i], false
		if strings.HasSuffix(ptr, ":") {
			ptr, json = strings.TrimSuffix(ptr, ":"), true
		}
		if !strings.HasPrefix(ptr, "/") {
			return nil, use("a JSON Pointer starts with /, as in /metadata/name=api")
		}
		tokens, err := pointer(ptr)
		if err != nil {
			return nil, use(err.Error())
		}
		pl.loc = tokens
		return pl, operand(pl, a.Text[i+1:], json, use)
	}
	return nil, use("unknown argument form")
}

// split reads one object argument.
func split(pl *placement, use func(string) error) error {
	arg := pl.arg.Text
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		msg := "an argument is KEY=VALUE, KEY:=JSON, KEY=@FILE or KEY:=@FILE"
		if strings.HasPrefix(arg, "-") {
			msg += "; options come before the arguments"
		}
		return use(msg)
	}
	key, json, appends := arg[:i], false, false
	if strings.HasSuffix(key, ":") {
		json, key = true, strings.TrimSuffix(key, ":")
	}
	if strings.HasSuffix(key, "[]") {
		appends, key = true, strings.TrimSuffix(key, "[]")
	}
	if key == "" {
		return use("the key is empty")
	}
	if strings.HasSuffix(key, "[]") {
		return use(errDoubleAppend.Error())
	}
	pl.bare = true
	pl.loc = []token{{name: key}}
	if appends {
		pl.loc = append(pl.loc, token{appends: true})
	}
	return operand(pl, arg[i+1:], json, use)
}

// splitElement reads one array argument. A value that opens with neither
// "=" nor ":=" is the string it is, whatever it holds.
func splitElement(pl *placement, use func(string) error) error {
	arg := pl.arg.Text
	pl.loc = []token{{appends: true}}
	switch {
	case strings.HasPrefix(arg, ":="):
		return operand(pl, arg[2:], true, use)
	case strings.HasPrefix(arg, "="):
		return operand(pl, arg[1:], false, use)
	}
	pl.kind, pl.text = literal, arg
	return nil
}

// operand reads what follows the operator: text, JSON, or @ and a path.
func operand(pl *placement, value string, json bool, use func(string) error) error {
	if rest, ok := strings.CutPrefix(value, "@"); ok {
		if rest == "" {
			return use("@ names no file; @- is standard input")
		}
		pl.kind, pl.text = fileText, rest
		if json {
			pl.kind = fileData
		}
		return nil
	}
	if !json {
		pl.kind, pl.text = literal, value
		return nil
	}
	v, err := readJSON(value)
	if err != nil {
		var pe *engine.ParseError
		if errors.As(err, &pe) {
			// The argument is JSON; jz refuses what it holds. That is the
			// input failing, the way the same text in a file is, so it is
			// not handed to use, which makes a usage error.
			return &InputError{Arg: pl.arg.String(), Err: err}
		}
		return use(err.Error())
	}
	pl.kind, pl.value = inlineJSON, v
	return nil
}

func readJSON(text string) (any, error) {
	v, err := datafile.Read(datafile.JSON, []byte(text))
	if err == nil {
		return v, nil
	}
	// JSON jz refuses to write (a key given twice, nesting too deep, more
	// values than it holds) is not a string written by mistake. It gets
	// the reason rather than the hint, and it keeps the parse failure it
	// is, so that the status is the one the same text read from a file
	// earns.
	var pe *engine.ParseError
	if json.Valid([]byte(text)) && errors.As(err, &pe) {
		return nil, &engine.ParseError{Msg: "the value after := cannot be written: " + pe.Msg}
	}
	return nil, errors.New("the value after := is not JSON; a string is written with = or quoted")
}

// location reads the KEY of --string and --text-file: a JSON Pointer when
// it starts with "/", otherwise a key with an optional trailing [].
// errDoubleAppend refuses a key that ends in [] once the [] that appends
// is taken off: a key holding [] at its end is what a plain argument
// cannot name, so reading one as a key named "a[]" would be a guess.
var errDoubleAppend = errors.New("a key cannot end in [], which appends; write such a key inside a := value")

func location(loc string, array bool, form Form) ([]token, error) {
	// A key or a pointer that ends in : is a := typed where = was meant,
	// or the other way round, and either way a guess would be wrong.
	if strings.HasSuffix(loc, ":") {
		name := "--string writes TEXT as it is"
		if form == TextFile {
			name = "--text-file writes a file's text as it is"
		}
		return nil, fmt.Errorf("%s, and a key ending in : looks like :=, which reads JSON; use --path for JSON", name)
	}
	if strings.HasPrefix(loc, "/") {
		return pointer(loc)
	}
	if array {
		if loc != "" {
			return nil, errors.New("an array has no keys: leave KEY empty to append, or give a pointer such as /-")
		}
		return []token{{appends: true}}, nil
	}
	key, appends := strings.CutSuffix(loc, "[]")
	if key == "" {
		return nil, errors.New("the key is empty")
	}
	if strings.HasSuffix(key, "[]") {
		return nil, errDoubleAppend
	}
	tokens := []token{{name: key}}
	if appends {
		tokens = append(tokens, token{appends: true})
	}
	return tokens, nil
}

// pointer reads a JSON Pointer. "~1" is "/" and "~0" is "~" in a token,
// "-" appends to an array, and a decimal number is an index in an array
// and a key in an object.
func pointer(ptr string) ([]token, error) {
	parts := strings.Split(ptr[1:], "/")
	out := make([]token, 0, len(parts))
	for _, part := range parts {
		name, err := unescape(part)
		if err != nil {
			return nil, err
		}
		switch {
		case name == "":
			return nil, fmt.Errorf("the pointer %s holds an empty key", ptr)
		case part == "-":
			out = append(out, token{appends: true})
		case strings.HasSuffix(name, "[]"):
			return nil, fmt.Errorf("[] appends after a plain key; in the pointer %s append with /-", ptr)
		default:
			out = append(out, token{name: name, index: isIndex(name)})
		}
	}
	return out, nil
}

func unescape(part string) (string, error) {
	if !strings.Contains(part, "~") {
		return part, nil
	}
	var b strings.Builder
	for i := 0; i < len(part); i++ {
		if part[i] != '~' {
			b.WriteByte(part[i])
			continue
		}
		if i+1 < len(part) && (part[i+1] == '0' || part[i+1] == '1') {
			b.WriteByte("~/"[part[i+1]-'0'])
			i++
			continue
		}
		return "", fmt.Errorf("~ in a pointer is ~0 (a ~) or ~1 (a /), and %q holds another", part)
	}
	return b.String(), nil
}

// isIndex reports a decimal number with no leading zero, the only way a
// pointer names an array element.
func isIndex(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// place adds a placement to the planned document, or says why its
// location cannot hold it.
func (p *Plan) place(pl *placement) error {
	cur := p.root
	for i := range pl.loc {
		var (
			next *node
			msg  string
		)
		switch cur.kind {
		case valueNode:
			if pl.bare && cur.by.bare {
				msg = fmt.Sprintf("the key %q is given both as a value and as an array with []", pl.loc[0].name)
			} else {
				msg = fmt.Sprintf("%s is a value given whole by %q, and jz does not add to a value it was given", render(pl.loc[:i]), cur.by.arg.String())
			}
		case objectNode:
			next, msg = p.intoObject(cur, pl, i)
		case arrayNode:
			next, msg = p.intoArray(cur, pl, i)
		}
		if msg != "" {
			return &UsageError{Arg: pl.arg.String(), Msg: msg}
		}
		cur = next
	}
	return nil
}

// intoObject takes the i-th step of pl inside the object cur: to the
// member the token names, made when it is not there yet.
func (p *Plan) intoObject(cur *node, pl *placement, i int) (*node, string) {
	tok, at := pl.loc[i], render(pl.loc[:i])
	if tok.appends {
		if cur == p.root {
			return nil, "the document is an object: - appends to an array; name a key"
		}
		return nil, fmt.Sprintf("%s is an object, made by %q, and - appends to an array", at, cur.by.arg.String())
	}
	if next := cur.members[tok.name]; next != nil {
		if i == len(pl.loc)-1 {
			return nil, twice(pl, next, render(pl.loc))
		}
		return next, ""
	}
	next, err := p.create(pl, i)
	if err != nil {
		return nil, err.Error()
	}
	cur.keys = append(cur.keys, tok.name)
	cur.members[tok.name] = next
	return next, ""
}

// intoArray takes the i-th step of pl inside the array cur: past its end,
// or to an element an earlier argument gave.
func (p *Plan) intoArray(cur *node, pl *placement, i int) (*node, string) {
	tok, at := pl.loc[i], render(pl.loc[:i])
	switch {
	case tok.appends:
		next, err := p.create(pl, i)
		if err != nil {
			return nil, err.Error()
		}
		cur.elems = append(cur.elems, next)
		return next, ""
	case tok.index:
		idx, err := strconv.Atoi(tok.name)
		if err != nil || idx >= len(cur.elems) {
			return nil, fmt.Sprintf("%s has %d elements, so %s names none; append with %s/-", orRoot(at), len(cur.elems), tok.name, at)
		}
		next := cur.elems[idx]
		if i == len(pl.loc)-1 {
			return nil, fmt.Sprintf("%s is given twice, first by %q", render(pl.loc), next.by.arg.String())
		}
		return next, ""
	case cur == p.root:
		return nil, "the document is an array (--array): name an element with an index or append with -"
	}
	return nil, fmt.Sprintf("%s is an array, made by %q, and %q is not an index in it; append with %s/-", at, cur.by.arg.String(), tok.name, at)
}

// create makes the node the i-th token of pl leads to: the value itself
// for the last token, or the container the next token needs.
func (p *Plan) create(pl *placement, i int) (*node, error) {
	if i == len(pl.loc)-1 {
		return &node{kind: valueNode, by: pl, place: len(p.places)}, nil
	}
	next := pl.loc[i+1]
	switch {
	case next.appends:
		return newContainer(arrayNode, pl), nil
	case next.index:
		at := render(pl.loc[:i+1])
		return nil, fmt.Errorf("%s does not exist yet, so %s cannot be an index in it; append with %s/-", at, next.name, at)
	}
	return newContainer(objectNode, pl), nil
}

// twice words a location given a second time. Two plain arguments naming
// one key keep the words they have always had.
func twice(pl *placement, existing *node, at string) string {
	if pl.bare && existing.by.bare && len(pl.loc) == 1 {
		if existing.kind == arrayNode && existing.by.loc[len(existing.by.loc)-1].appends {
			return fmt.Sprintf("the key %q is given both as a value and as an array with []", pl.loc[0].name)
		}
		return fmt.Sprintf("the key %q is given twice; add [] to make an array of the values", pl.loc[0].name)
	}
	return fmt.Sprintf("%s is given twice, first by %q", at, existing.by.arg.String())
}

// render writes a location as a JSON Pointer, for messages.
func render(loc []token) string {
	var b strings.Builder
	for _, t := range loc {
		b.WriteByte('/')
		if t.appends {
			b.WriteByte('-')
			continue
		}
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(t.name, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

func orRoot(at string) string {
	if at == "" {
		return "the array"
	}
	return at
}

// oneStdin refuses a second argument that reads standard input before
// anything is read: the first would take all of it.
func oneStdin(places []*placement) error {
	first := ""
	for _, pl := range places {
		if pl.kind < fileText || pl.text != "-" {
			continue
		}
		if first != "" {
			return &UsageError{Arg: pl.arg.String(), Msg: fmt.Sprintf("standard input is already read by %q", first)}
		}
		first = pl.arg.String()
	}
	return nil
}

// Build reads the files and standard input the plan names, in the order
// the arguments gave them, and makes the document.
func (p *Plan) Build(src Sources) (any, error) {
	values := make([]any, len(p.places))
	n := p.root.containers()
	for i, pl := range p.places {
		v, err := value(pl, src)
		if err != nil {
			return nil, err
		}
		values[i] = v
		if n += countValues(v, src.maxValues()); n > src.maxValues() {
			return nil, tooManyValues(src.maxValues())
		}
	}
	return p.root.make(values), nil
}

// StdinFormat says how the argument that reads standard input reads it,
// as the name of the data file format a record of it is: "lines" for
// KEY=@-, a string per line, "jsonl" for KEY:=@-, a JSON value per line,
// and "text" for --text-file KEY=-, which reads it whole. It is "" when no
// argument reads standard input. arg is that argument as it was written.
func (p *Plan) StdinFormat() (format, arg string) {
	for _, pl := range p.places {
		if pl.kind < fileText || pl.text != "-" {
			continue
		}
		switch pl.kind {
		case fileText:
			return datafile.LINES, pl.arg.String()
		case fileData:
			return datafile.JSONL, pl.arg.String()
		case literal, inlineJSON, fileWhole:
		}
		return datafile.TEXT, pl.arg.String()
	}
	return "", ""
}

// Fixed is a plan with every value read but the one standard input gives,
// made once per record of standard input with With.
type Fixed struct {
	plan   *Plan
	values []any
	slot   int
	// counted is the values every document holds before its record's, and
	// max the most a document may hold.
	counted, max int
}

// Fixed reads the files the plan names, in the order the arguments gave
// them, and leaves standard input unread.
func (p *Plan) Fixed(src Sources) (*Fixed, error) {
	f := &Fixed{plan: p, values: make([]any, len(p.places)), slot: -1, counted: p.root.containers(), max: src.maxValues()}
	for i, pl := range p.places {
		if pl.kind >= fileText && pl.text == "-" {
			f.slot = i
			continue
		}
		v, err := value(pl, src)
		if err != nil {
			return nil, err
		}
		f.values[i] = v
		if f.counted += countValues(v, f.max); f.counted > f.max {
			return nil, tooManyValues(f.max)
		}
	}
	return f, nil
}

// With makes the document with v as the value of the argument that reads
// standard input. The values read once are shared by every document, and
// a document they and v make past the limit is refused.
func (f *Fixed) With(v any) (any, error) {
	values := f.values
	if f.slot >= 0 {
		if f.counted+countValues(v, f.max) > f.max {
			return nil, tooManyValues(f.max)
		}
		values = slices.Clone(f.values)
		values[f.slot] = v
	}
	return f.plan.root.make(values), nil
}

// containers counts the objects and arrays the arguments' locations make
// around the values placed in them.
func (n *node) containers() int {
	switch n.kind {
	case valueNode:
		return 0
	case objectNode:
		c := 1
		for _, m := range n.members {
			c += m.containers()
		}
		return c
	case arrayNode:
		c := 1
		for _, e := range n.elems {
			c += e.containers()
		}
		return c
	}
	return 0
}

// countValues counts the JSON values v holds, itself included, and stops
// once the count is past max.
func countValues(v any, limit int) int {
	n := 1
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if n > limit {
				break
			}
			n += countValues(e, limit-n)
		}
	case *jsonutil.Object:
		for _, m := range t.Members() {
			if n > limit {
				break
			}
			n += countValues(m.Value, limit-n)
		}
	}
	return n
}

// tooManyValues refuses a document past the limit on the values it holds,
// which is a parse failure the way a data file past it is.
func tooManyValues(limit int) error {
	return &engine.ParseError{Msg: fmt.Sprintf("the document yields more than %d values, more than one document holds", limit), Cause: engine.ErrTooManyValues}
}

func (n *node) make(values []any) any {
	switch n.kind {
	case valueNode:
	case objectNode:
		obj := jsonutil.NewObject()
		for _, k := range n.keys {
			obj.Set(k, n.members[k].make(values))
		}
		return obj
	case arrayNode:
		out := make([]any, 0, len(n.elems))
		for _, e := range n.elems {
			out = append(out, e.make(values))
		}
		return out
	}
	return values[n.place]
}

func value(pl *placement, src Sources) (any, error) {
	switch pl.kind {
	case literal:
		return pl.text, nil
	case inlineJSON:
		return pl.value, nil
	case fileText, fileWhole:
		return textValue(pl, src)
	case fileData:
		return dataValue(pl, src)
	}
	return nil, fmt.Errorf("unknown value kind %d", pl.kind)
}

// textValue is a file's text as a string: UTF-8 without a byte order
// mark, whole for --text-file and less the one line ending a text file
// ends with for =@.
func textValue(pl *placement, src Sources) (any, error) {
	arg := pl.arg.String()
	data, err := read(arg, pl.text, src)
	if err != nil {
		return nil, err
	}
	// A byte order mark says how the text is encoded and is not part of
	// it, as --format text reads it.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(data) {
		return nil, &InputError{Arg: arg, Err: ErrNotUTF8}
	}
	s := string(data)
	if pl.kind == fileText {
		if t, ok := strings.CutSuffix(s, "\n"); ok {
			s = strings.TrimSuffix(t, "\r")
		}
	}
	return s, nil
}

// dataValue is the value a data file holds, read as its extension names
// and as JSON when it names nothing, standard input included.
func dataValue(pl *placement, src Sources) (any, error) {
	arg := pl.arg.String()
	data, err := read(arg, pl.text, src)
	if err != nil {
		return nil, err
	}
	format := datafile.JSON
	if pl.text != "-" {
		named, compression := datafile.FromPath(pl.text)
		if named != "" {
			format = named
		}
		if compression != "" {
			if data, err = decompress(data, compression, src.MaxSize); err != nil {
				return nil, &InputError{Arg: arg, Err: err}
			}
		}
	}
	v, err := datafile.Read(format, data)
	if err != nil {
		return nil, &InputError{Arg: arg, Err: err}
	}
	return v, nil
}

func read(arg, path string, src Sources) ([]byte, error) {
	if path != "-" {
		data, err := src.ReadFile(path)
		if err != nil {
			return nil, &InputError{Arg: arg, Err: err}
		}
		return data, nil
	}
	if src.Stdin == nil {
		return nil, &InputError{Arg: arg, Err: errors.New("there is no standard input")}
	}
	data, err := io.ReadAll(io.LimitReader(src.Stdin, src.MaxSize+1))
	if err != nil {
		return nil, &InputError{Arg: arg, Err: err}
	}
	if int64(len(data)) > src.MaxSize {
		return nil, &InputError{Arg: arg, Err: &engine.ParseError{Msg: fmt.Sprintf("standard input exceeds the %d byte limit", src.MaxSize)}}
	}
	return data, nil
}

// decompress reads a compressed file whole, bounded by max so that a
// small file cannot unpack into an unbounded one.
func decompress(data []byte, compression string, maxSize int64) ([]byte, error) {
	r, err := datafile.Decompress(bytes.NewReader(data), compression)
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > maxSize {
		return nil, &engine.ParseError{Msg: fmt.Sprintf("the decompressed text exceeds the %d byte limit", maxSize)}
	}
	return out, nil
}

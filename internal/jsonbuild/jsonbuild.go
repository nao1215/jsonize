// Package jsonbuild makes a JSON value from arguments given on a command
// line, which is what `jz new` prints.
//
// An object is made of arguments that are a key, an operator and a value:
//
//	name=api             the string "api"
//	replicas:=3          the JSON value 3; any JSON: true, null, "text", [1,2], {"a":1}
//	notes=@NOTES.txt     the text of a file, as a string
//	spec:=@deploy.yaml   the value a data file holds, read as its extension names
//	tags[]=a             one more element of the array tags
//
// An array (--array) is made of values written the same way without the
// key: `api`, `=api`, `:=3`, `=@NOTES.txt`, `:=@deploy.yaml`.
//
// Nothing is guessed. `=` always makes a string, so "007" and "true" stay
// strings, and `:=` always reads JSON. A path of `-` reads standard input,
// which only one argument may do.
//
// A key given twice without [] is refused, since the arguments do not say
// which of the two values is meant, and so is a key given both with and
// without []. The key is everything before the first "=", less the ":"
// of ":=" and a trailing "[]"; a key that itself holds "=" is written in
// a JSON document given with :=@.
package jsonbuild

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// UsageError is an argument that does not say what JSON to make: no
// operator, an empty key, a key given twice, JSON that is not JSON.
type UsageError struct {
	Arg string
	Msg string
}

func (e *UsageError) Error() string {
	return fmt.Sprintf("%q: %s", e.Arg, e.Msg)
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
}

type part struct {
	key    string
	append bool
	json   bool
	file   bool
	value  string
}

// split reads one object argument.
func split(arg string) (part, error) {
	if !utf8.ValidString(arg) {
		return part{}, &UsageError{Arg: arg, Msg: "the argument is not valid UTF-8"}
	}
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		msg := "an argument is KEY=VALUE, KEY:=JSON, KEY=@FILE or KEY:=@FILE"
		if strings.HasPrefix(arg, "-") {
			msg += "; options come before the arguments"
		}
		return part{}, &UsageError{Arg: arg, Msg: msg}
	}
	p := part{key: arg[:i], value: arg[i+1:]}
	if strings.HasSuffix(p.key, ":") {
		p.json, p.key = true, strings.TrimSuffix(p.key, ":")
	}
	if strings.HasSuffix(p.key, "[]") {
		p.append, p.key = true, strings.TrimSuffix(p.key, "[]")
	}
	if strings.HasPrefix(p.value, "@") {
		p.file, p.value = true, p.value[1:]
	}
	if p.key == "" {
		return part{}, &UsageError{Arg: arg, Msg: "the key is empty"}
	}
	if p.file && p.value == "" {
		return part{}, &UsageError{Arg: arg, Msg: "@ names no file; @- is standard input"}
	}
	return p, nil
}

// splitElement reads one array argument. A value that opens with neither
// "=" nor ":=" is the string it is, whatever it holds.
func splitElement(arg string) (part, error) {
	if !utf8.ValidString(arg) {
		return part{}, &UsageError{Arg: arg, Msg: "the argument is not valid UTF-8"}
	}
	p := part{value: arg}
	switch {
	case strings.HasPrefix(arg, ":="):
		p.json, p.value = true, arg[2:]
	case strings.HasPrefix(arg, "="):
		p.value = arg[1:]
	default:
		return p, nil
	}
	if strings.HasPrefix(p.value, "@") {
		p.file, p.value = true, p.value[1:]
		if p.value == "" {
			return part{}, &UsageError{Arg: arg, Msg: "@ names no file; @- is standard input"}
		}
	}
	return p, nil
}

type builder struct {
	src Sources
}

// oneStdin refuses a second argument that reads standard input before
// anything is read: the first would take all of it.
func oneStdin(args []string, parts []part) error {
	first := ""
	for i, p := range parts {
		if !p.file || p.value != "-" {
			continue
		}
		if first != "" {
			return &UsageError{Arg: args[i], Msg: fmt.Sprintf("standard input is already read by %q", first)}
		}
		first = args[i]
	}
	return nil
}

// Object makes an object from KEY=VALUE arguments. The keys are in the
// order they were first given.
func Object(args []string, src Sources) (*jsonutil.Object, error) {
	b := &builder{src: src}
	parts := make([]part, 0, len(args))
	kinds := map[string]bool{} // key -> appended
	for _, arg := range args {
		p, err := split(arg)
		if err != nil {
			return nil, err
		}
		if appended, seen := kinds[p.key]; seen {
			switch {
			case appended != p.append:
				return nil, &UsageError{Arg: arg, Msg: fmt.Sprintf("the key %q is given both as a value and as an array with []", p.key)}
			case !p.append:
				return nil, &UsageError{Arg: arg, Msg: fmt.Sprintf("the key %q is given twice; add [] to make an array of the values", p.key)}
			}
		}
		kinds[p.key] = p.append
		parts = append(parts, p)
	}
	if err := oneStdin(args, parts); err != nil {
		return nil, err
	}
	obj := jsonutil.NewObject()
	for i, p := range parts {
		v, err := b.value(args[i], p)
		if err != nil {
			return nil, err
		}
		if !p.append {
			obj.Set(p.key, v)
			continue
		}
		prev, ok := obj.Get(p.key)
		list, _ := prev.([]any)
		if !ok {
			list = []any{}
		}
		obj.Set(p.key, append(list, v))
	}
	return obj, nil
}

// Array makes an array from value arguments.
func Array(args []string, src Sources) ([]any, error) {
	b := &builder{src: src}
	parts := make([]part, 0, len(args))
	for _, arg := range args {
		p, err := splitElement(arg)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if err := oneStdin(args, parts); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(parts))
	for i, p := range parts {
		v, err := b.value(args[i], p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (b *builder) value(arg string, p part) (any, error) {
	if !p.file {
		if !p.json {
			return p.value, nil
		}
		v, err := datafile.Read(datafile.JSON, []byte(p.value))
		if err != nil {
			return nil, &UsageError{Arg: arg, Msg: "the value after := is not JSON; a string is written with = or quoted"}
		}
		return v, nil
	}
	data, err := b.read(arg, p.value)
	if err != nil {
		return nil, err
	}
	if !p.json {
		return b.text(arg, data)
	}
	format := datafile.JSON
	if p.value != "-" {
		named, compression := datafile.FromPath(p.value)
		if named != "" {
			format = named
		}
		if compression != "" {
			if data, err = decompress(data, compression, b.src.MaxSize); err != nil {
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

// text is a file's text as a string: UTF-8, and without the one line
// ending a text file ends with, which is not part of what it says.
func (b *builder) text(arg string, data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, &InputError{Arg: arg, Err: ErrNotUTF8}
	}
	s := string(data)
	switch {
	case strings.HasSuffix(s, "\r\n"):
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "\n"):
		s = s[:len(s)-1]
	}
	return s, nil
}

func (b *builder) read(arg, path string) ([]byte, error) {
	if path != "-" {
		data, err := b.src.ReadFile(path)
		if err != nil {
			return nil, &InputError{Arg: arg, Err: err}
		}
		return data, nil
	}
	if b.src.Stdin == nil {
		return nil, &InputError{Arg: arg, Err: errors.New("there is no standard input")}
	}
	data, err := io.ReadAll(io.LimitReader(b.src.Stdin, b.src.MaxSize+1))
	if err != nil {
		return nil, &InputError{Arg: arg, Err: err}
	}
	if int64(len(data)) > b.src.MaxSize {
		return nil, &InputError{Arg: arg, Err: fmt.Errorf("standard input exceeds the %d byte limit", b.src.MaxSize)}
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
		return nil, fmt.Errorf("the decompressed text exceeds the %d byte limit", maxSize)
	}
	return out, nil
}

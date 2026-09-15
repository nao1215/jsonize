package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nao1215/jsonize/internal/datafile"
	"github.com/nao1215/jsonize/internal/jsonbuild"
	"github.com/nao1215/jsonize/pkg/engine"
)

const modeNew = "new"

const newUsage = `Usage: jz new [options] [KEY=VALUE | KEY:=JSON | KEY=@FILE | KEY:=@FILE]...
       jz new --array [options] [VALUE | :=JSON | =@FILE | :=@FILE]...

Makes a JSON object from the arguments, or an array with --array. Nothing
is guessed: = makes a string and := reads JSON.

  jz new name=api replicas:=3 debug:=false
  jz new tags[]=web tags[]=prod                 # {"tags":["web","prod"]}
  jz new version=@VERSION                       # a file's text, less its last line ending
  jz new spec:=@deploy.yaml                     # a data file, read as its extension names
  kubectl get pod web -o json | jz new pod:=@-  # standard input
  jz new --array a :=1 :=null                   # ["a",1,null]
  jz new --string "message=$MESSAGE"            # the text as it is, even when it starts with @
  jz new --text-file body=NOTES.md              # a file's text, every line ending kept
  jz new --path /metadata/name=api --path /spec/replicas:=3

--string, --text-file and --path may be repeated, come before the plain
arguments, and are placed in the order given. KEY is a key, or a JSON
Pointer when it starts with /: a pointer makes the objects on its way,
- appends to an array, and an index names an element already given. A
location is given once, and no location enters a value given whole.

Options:
`

type newCmdOptions struct {
	output outputOptions
	array  bool
	// placed are the --string, --text-file and --path arguments in the
	// order they were given.
	placed []jsonbuild.Arg
}

// placedValue is one of the options that place a value; each value it is
// given joins the one list the three share, so their order survives.
type placedValue struct {
	form jsonbuild.Form
	list *[]jsonbuild.Arg
}

func (p placedValue) String() string { return "" }

// Set records the argument.
func (p placedValue) Set(v string) error {
	*p.list = append(*p.list, jsonbuild.Arg{Form: p.form, Text: v})
	return nil
}

func (n *newCmdOptions) bind(o *optionSet) {
	o.boolOpt(&n.array, "array", "", "make an array of the values instead of an object")
	o.fs.Var(placedValue{jsonbuild.String, &n.placed}, "string", "")
	o.doc("", "string", "KEY=TEXT", "put TEXT at KEY as a string, as it is (repeatable)")
	o.fs.Var(placedValue{jsonbuild.TextFile, &n.placed}, "text-file", "")
	o.doc("", "text-file", "KEY=PATH", "put a file's text at KEY, line endings kept; - is stdin (repeatable)")
	o.fs.Var(placedValue{jsonbuild.Path, &n.placed}, "path", "")
	o.doc("", "path", "POINTER=VALUE", "put a value at a JSON Pointer, with =, :=, =@ or :=@ (repeatable)")
	o.boolOpt(&n.output.pretty, "pretty", "p", "indent JSON output")
	o.helpDoc()
}

func (a *app) cmdNew(args []string) int {
	o := newOptions(modeNew)
	var no newCmdOptions
	no.bind(o)
	if code, done := a.parse(o, args, newUsage); done {
		return code
	}
	if err := no.output.check(); err != nil {
		a.errorf("%v", err)
		return ExitUsage
	}
	all := no.placed
	for _, text := range o.fs.Args() {
		all = append(all, jsonbuild.Arg{Form: jsonbuild.Plain, Text: text})
	}
	plan, err := jsonbuild.Parse(all, no.array)
	if err != nil {
		return a.newFailed(err)
	}
	v, err := plan.Build(jsonbuild.Sources{ReadFile: readLimited, Stdin: a.env.Stdin, MaxSize: MaxInputSize})
	if err != nil {
		return a.newFailed(err)
	}
	if err := no.output.write(a.env.Stdout, v); err != nil {
		return a.writeFailed(err)
	}
	return ExitOK
}

// newFailed reports why the arguments made no JSON: an argument that
// says nothing JSON can be is a usage error, a file that is not the
// format it was read as is a parse failure, and one that cannot be read
// at all is an error.
func (a *app) newFailed(err error) int {
	a.errorf("new: %v", err)
	var (
		ue *jsonbuild.UsageError
		pe *engine.ParseError
		ce *datafile.CompressionError
	)
	switch {
	case errors.As(err, &ue):
		return ExitUsage
	case errors.As(err, &pe), errors.As(err, &ce), errors.Is(err, jsonbuild.ErrNotUTF8):
		return ExitParse
	}
	return ExitError
}

// readLimited reads a file an argument names, up to the input limit.
func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxInputSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxInputSize {
		return nil, fmt.Errorf("the file exceeds the %d byte limit", MaxInputSize)
	}
	return data, nil
}

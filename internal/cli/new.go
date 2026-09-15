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

//nolint:dupword // the usage block repeats "jz new" on purpose
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

A key given twice needs [] to be an array. A string that opens with @ is
written as JSON: note:='"@here"'.

Options:
`

type newCmdOptions struct {
	output outputOptions
	array  bool
}

func (n *newCmdOptions) bind(o *optionSet) {
	o.boolOpt(&n.array, "array", "", "make an array of the values instead of an object")
	o.boolOpt(&n.output.pretty, "pretty", "p", "indent JSON output")
	o.boolOpt(&n.output.yaml, "yaml", "", "write YAML instead of JSON")
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
	src := jsonbuild.Sources{ReadFile: readLimited, Stdin: a.env.Stdin, MaxSize: MaxInputSize}
	var (
		v   any
		err error
	)
	if no.array {
		v, err = jsonbuild.Array(o.fs.Args(), src)
	} else {
		v, err = jsonbuild.Object(o.fs.Args(), src)
	}
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

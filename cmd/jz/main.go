// Command jz turns command output into JSON.
package main

import (
	"context"
	"os"
	"runtime"

	"github.com/nao1215/jsonize/internal/cli"
	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
)

// Interrupts are not subscribed to here: jz forwards them to the command
// it runs, and only while that command runs, so the subscription lives
// in the runner. Outside that window an interrupt ends jz, which is what
// a filter with nothing to forward to should do.
func main() {
	keepGoingOnClosedOutput()
	code := cli.Main(os.Args[1:], cli.Env{
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		GOOS:            runtime.GOOS,
		Context:         context.Background(),
		Embedded:        registry.Source{Name: cli.SourceEmbedded, FS: official.FS()},
		StdinIsTerminal: stdinIsTerminal,
	})
	os.Exit(code)
}

// stdinIsTerminal reports whether standard input is attached to a
// terminal. jz consults this for one thing only: running `jz` with no
// arguments at all in a shell prints help instead of blocking forever on
// a read. Piped and redirected input take exactly the same path either
// way.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

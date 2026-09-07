// Command jz turns command output into JSON.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/nao1215/jsonize/internal/cli"
	"github.com/nao1215/jsonize/pkg/registry"
	official "github.com/nao1215/jsonize/registry"
)

func main() {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	code := cli.Main(os.Args[1:], cli.Env{
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		GOOS:            runtime.GOOS,
		Signals:         sigs,
		Context:         context.Background(),
		Embedded:        registry.Source{Name: cli.SourceEmbedded, FS: official.FS()},
		StdinIsTerminal: stdinIsTerminal,
	})
	signal.Stop(sigs)
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

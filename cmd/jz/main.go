// Command jz turns human-oriented command output into JSON.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/nao1215/jsonize/internal/cli"
	"github.com/nao1215/jsonize/internal/registry"
	official "github.com/nao1215/jsonize/registry"
)

func main() {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	code := cli.Main(os.Args[1:], cli.Env{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		GOOS:     runtime.GOOS,
		Signals:  sigs,
		Context:  context.Background(),
		Embedded: registry.Source{Name: cli.SourceEmbedded, FS: official.FS()},
	})
	signal.Stop(sigs)
	os.Exit(code)
}

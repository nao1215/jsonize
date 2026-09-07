package cli

import (
	"fmt"
	"runtime"

	"github.com/nao1215/jsonize/internal/buildinfo"
	"github.com/nao1215/jsonize/pkg/definition"
)

func (a *app) cmdVersion(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == flagHelp) {
		fmt.Fprintln(a.env.Stdout, "Usage: jz version\n\nPrints the jsonize version, the definition format it reads and the platform.")
		return ExitOK
	}
	fmt.Fprintf(a.env.Stdout, "jz %s (definition format %d, %s/%s)\n", buildinfo.Get(), definition.CurrentFormat, runtime.GOOS, runtime.GOARCH)
	return ExitOK
}

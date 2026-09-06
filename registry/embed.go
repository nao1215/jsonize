// Package registry embeds the official parser definitions and their
// fixtures. It contains no logic: this file is the only Go code in the
// directory so the definitions can move to a separate repository later
// without touching the parser engine.
package registry

import (
	"embed"
	"io/fs"
)

//go:embed registry.yaml parsers
var files embed.FS

// FS returns the embedded registry rooted at the directory that holds
// registry.yaml.
func FS() fs.FS {
	return files
}

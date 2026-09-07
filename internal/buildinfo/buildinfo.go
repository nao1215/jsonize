// Package buildinfo exposes the jsonize version that is stamped at build
// time with -ldflags. It is a separate package so that the CLI and the
// definition loader can both name the running build without an import
// cycle.
package buildinfo

import "runtime/debug"

// Version is set by the linker (see Makefile / .goreleaser.yml). It falls
// back to the module version recorded by `go install` and finally to
// "(devel)".
var Version = ""

// Get returns the effective version string.
func Get() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "(devel)"
}

// Package buildinfo exposes the jsonize version that is stamped at build
// time with -ldflags. It is a separate package so that both the CLI and the
// registry loader (for min_jsonize checks) can read it without import cycles.
package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
)

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

// Semver is a parsed MAJOR.MINOR.PATCH version.
type Semver struct {
	Major, Minor, Patch int
}

// ParseSemver parses "v1.2.3" or "1.2.3". Pre-release and build suffixes are
// ignored for comparison purposes. Anything else is reported as an error.
func ParseSemver(s string) (Semver, error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(t, "-+"); i >= 0 {
		t = t[:i]
	}
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("invalid semantic version %q", s)
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("invalid semantic version %q", s)
		}
		out[i] = n
	}
	return Semver{Major: out[0], Minor: out[1], Patch: out[2]}, nil
}

// Less reports whether v is older than other.
func (v Semver) Less(other Semver) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// String formats the version as MAJOR.MINOR.PATCH.
func (v Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Current returns the running version as Semver and true when it is a
// release build. Development builds report false so that definition
// min_jsonize checks are skipped for them.
func Current() (Semver, bool) {
	v, err := ParseSemver(Get())
	if err != nil {
		return Semver{}, false
	}
	return v, true
}

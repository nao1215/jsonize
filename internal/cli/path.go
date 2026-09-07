package cli

import (
	"path/filepath"
	"strings"

	"github.com/nao1215/jsonize/pkg/registry"
)

// lookuper is the one thing parserFromPath needs of a registry.
type lookuper interface {
	Lookup(command, variant string) (*registry.Entry, bool)
}

// parserFromPath reads a file path as evidence about the text in it.
//
// A file has no argv, so a path is the only thing besides the text that
// says where the text came from. The rule is one sentence and holds no
// list of names: the directory the file sits in is the parser, the file
// itself is the variant, and the pair has to exist in the registry. That
// is what /etc/fstab, /etc/passwd, /proc/meminfo and /proc/stat already
// look like, because those parsers are named after the directory their
// formats live in.
//
// It is evidence and not an instruction. The definition it names still
// has to fit the text, and the caller drops the pair and reads the text
// on its own terms when it does not, so a path can only ever add an
// answer. What it adds is the formats that are too unremarkable to claim
// on sight: /etc/fstab is six whitespace-separated fields, which is why
// it declares auto_detect: false and, until the path was read, had to be
// named on the command line.
//
// Nothing here knows about /etc or /proc. A registry with a parser named
// after some other directory gets the same treatment, and a path whose
// directory names no parser is ignored.
func parserFromPath(reg lookuper, path string) (parser, variant string, ok bool) {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	dir := filepath.Base(filepath.Dir(clean))
	if base == "" || dir == "" || dir == "." || dir == string(filepath.Separator) {
		return "", "", false
	}
	// A capture saved as df.txt is a file name, not a variant name, and
	// guessing past the extension would start the list of names this rule
	// exists to avoid.
	if strings.Contains(base, ".") {
		return "", "", false
	}
	if _, found := reg.Lookup(dir, base); !found {
		return "", "", false
	}
	return dir, base, true
}

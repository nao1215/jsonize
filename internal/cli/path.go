package cli

import (
	"path/filepath"
	"strings"

	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
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
// A definition that describes a shape rather than a command is never
// named this way. Such a definition carries no signature, so nothing
// about the text can rule it out, and the directory name would be the
// whole of the evidence: a file that happened to sit at csv/comma was
// read as CSV whatever it held, which is the one failure this tool
// exists to prevent. Naming one of those stays something the caller does
// on the command line, where it is a claim they made rather than one
// their directory layout made for them.
//
// A name with a dot in it is looked up with a dash in its place, and a
// file one directory down with that directory joined to its name, since
// that is how such variants are named (etc/resolv-conf, proc/net-dev).
// Neither is a guess past an extension: a capture saved as df.txt is
// looked up as df-txt, which no definition is called.
//
// Nothing here knows about /etc or /proc. A registry with a parser named
// after some other directory gets the same treatment, and a path whose
// directory names no parser is ignored.
func parserFromPath(reg lookuper, path string) (parser, variant string, ok bool) {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	parent := filepath.Dir(clean)
	dir := filepath.Base(parent)
	if base == "" || dir == "" || dir == "." || dir == string(filepath.Separator) {
		return "", "", false
	}
	// A variant name is written with dashes where the file name has dots
	// (etc/resolv-conf for /etc/resolv.conf), and a file one directory
	// down is named with that directory in front (proc/net-dev for
	// /proc/net/dev). Each is one exact name to look up, so a capture
	// saved as df.txt names nothing: no variant is called df-txt.
	names := [][2]string{{dir, strings.ReplaceAll(base, ".", "-")}}
	if grand := filepath.Base(filepath.Dir(parent)); grand != "" && grand != "." && grand != string(filepath.Separator) {
		names = append(names, [2]string{grand, dir + "-" + strings.ReplaceAll(base, ".", "-")})
	}
	for _, n := range names {
		e, found := reg.Lookup(n[0], n[1])
		if found && !selector.ShapeOnly(e.Def) {
			return n[0], n[1], true
		}
	}
	return "", "", false
}

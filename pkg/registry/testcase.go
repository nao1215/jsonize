package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
)

// Case is one golden test case stored next to a definition: an input
// fixture, the expected JSON and optional metadata.
type Case struct {
	Name     string
	Input    []byte
	Expected []byte
	Meta     CaseMeta
	// Dir is the testdata directory path inside the source FS.
	Dir string
}

// CaseMeta is the optional <case>.yaml beside a fixture.
type CaseMeta struct {
	// Description documents where the fixture came from.
	Description string `yaml:"description,omitempty"`
	// Args simulates the arguments used in exec mode for selection tests.
	Args []string `yaml:"args,omitempty"`
	// OS simulates the operating system for selection tests.
	OS string `yaml:"os,omitempty"`
	// ExpectError, when set, means parsing must fail with a message that
	// contains this text; Expected is then not required.
	ExpectError string `yaml:"expect_error,omitempty"`
	// Source records where the captured output came from.
	Source string `yaml:"source,omitempty"`
	// AutoDetect can be set to false for a fixture that cannot be
	// identified from its text alone (a definition without a signature,
	// or output that legitimately matches several parsers). Such a case
	// is still parsed and compared; only the detection check is skipped,
	// and the reason belongs in Description.
	AutoDetect *bool `yaml:"auto_detect,omitempty"`
}

// AutoDetects reports whether the fixture must be identifiable without
// any hint. It defaults to true.
func (m CaseMeta) AutoDetects() bool {
	return m.AutoDetect == nil || *m.AutoDetect
}

// Cases loads the golden cases of an entry. Expected is nil when no
// <case>.json exists; the conformance runner decides whether that is an
// error (it is, unless golden files are being generated).
func Cases(fsys fs.FS, e *Entry) ([]Case, error) {
	dir := e.TestdataPath()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cases []Case
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".txt") {
			continue
		}
		name := strings.TrimSuffix(de.Name(), ".txt")
		c := Case{Name: name, Dir: dir}
		c.Input, err = fs.ReadFile(fsys, path.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		if meta, err := fs.ReadFile(fsys, path.Join(dir, name+".yaml")); err == nil {
			if err := definition.DecodeYAML(meta, &c.Meta); err != nil {
				return nil, fmt.Errorf("%s: invalid case metadata: %w", path.Join(dir, name+".yaml"), err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		c.Expected, err = fs.ReadFile(fsys, path.Join(dir, name+".json"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

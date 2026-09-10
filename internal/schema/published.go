package schema

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
)

// Dir is where a registry keeps the published schemas of its
// definitions, one file per definition at Dir/command/variant.json.
const Dir = "schemas"

// RetiredFile lists, one command/variant per line, the definitions whose
// schema was published once and whose removal is intended. It is how
// taking a definition out is told apart from losing one.
const RetiredFile = Dir + "/retired"

// Path returns where the published schema of def lives inside a
// registry.
func Path(def *definition.Definition) string {
	return pathOf(def.Command + "/" + def.Variant)
}

func pathOf(id string) string {
	return path.Join(Dir, id+".json")
}

// Published reads the published schema of def. The bool is false when
// none has been published yet, which is not an error.
func Published(fsys fs.FS, def *definition.Definition) (*Schema, bool, error) {
	data, err := fs.ReadFile(fsys, Path(def))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s, err := Decode(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", Path(def), err)
	}
	return s, true, nil
}

// Version returns the contract version a published schema states, or 1
// for a definition that has none yet.
func Version(published *Schema) int {
	if published == nil || published.Contract == nil || published.Contract.Version < 1 {
		return 1
	}
	return published.Contract.Version
}

// Next decides what to publish for def, given what was published before.
// A schema whose changes are all compatible keeps its version. One with
// a breaking change is refused unless breakOK says the change is meant,
// in which case the version goes up by one: that is the whole of
// acknowledging a break, and it is what the comparison against the base
// branch checks for.
func Next(def *definition.Definition, published *Schema, breakOK bool) (*Schema, []Change, error) {
	version := Version(published)
	generated := Generate(def, version)
	if published == nil {
		return generated, nil, nil
	}
	changes := Compare(published, generated)
	breaking := Breaking(changes)
	if len(breaking) == 0 {
		return generated, changes, nil
	}
	if !breakOK {
		return nil, changes, &BreakError{ID: def.ID(), Version: version, Changes: breaking}
	}
	generated.Contract.Version = version + 1
	return generated, changes, nil
}

// BreakError reports a schema change that would break a program reading
// the output and was not acknowledged with a new contract version.
type BreakError struct {
	ID      string
	Version int
	Changes []Change
}

func (e *BreakError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: the output contract (version %d) changes in a way that breaks its readers:", e.ID, e.Version)
	for _, c := range e.Changes {
		fmt.Fprintf(&b, "\n  %s", c)
	}
	fmt.Fprintf(&b, "\nif the change is meant, publish it as version %d (make registry-update-schema BREAKING=%s)", e.Version+1, e.ID)
	return b.String()
}

// Retired reads the list of definitions whose removal is intended.
func Retired(fsys fs.FS) (map[string]bool, error) {
	data, err := fs.ReadFile(fsys, RetiredFile)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, sc.Err()
}

// All reads every published schema in fsys, keyed by command/variant.
func All(fsys fs.FS) (map[string]*Schema, error) {
	out := map[string]*Schema{}
	if _, err := fs.Stat(fsys, Dir); errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	err := fs.WalkDir(fsys, Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		s, err := Decode(data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, Dir+"/"), ".json")
		out[id] = s
		return nil
	})
	return out, err
}

// AgainstBase checks the schemas a change publishes (head) against the
// ones the branch it is merged into published (base). Every breaking
// change has to come with a higher contract version, and a schema that
// disappears has to be for a definition listed as retired. It is what
// catches a published schema edited by hand past the check that
// regenerates it.
func AgainstBase(base, head fs.FS) ([]string, error) {
	before, err := All(base)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	after, err := All(head)
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	retired, err := Retired(head)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var problems []string
	for _, id := range ids {
		prev := before[id]
		next, ok := after[id]
		if !ok {
			if !retired[id] {
				problems = append(problems, fmt.Sprintf("%s: the schema was published and is gone; list %s in %s if the definition was removed on purpose", id, id, RetiredFile))
			}
			continue
		}
		breaking := Breaking(Compare(prev, next))
		if len(breaking) == 0 {
			if Version(next) < Version(prev) {
				problems = append(problems, fmt.Sprintf("%s: the contract version went down from %d to %d", id, Version(prev), Version(next)))
			}
			continue
		}
		if Version(next) <= Version(prev) {
			var b strings.Builder
			fmt.Fprintf(&b, "%s: a breaking change published without a new contract version (still %d):", id, Version(next))
			for _, c := range breaking {
				fmt.Fprintf(&b, "\n  %s", c)
			}
			problems = append(problems, b.String())
		}
	}
	return problems, nil
}

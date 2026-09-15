package cli

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/jsonutil"
)

func object(pairs ...any) *jsonutil.Object {
	o := jsonutil.NewObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// Without --extract or --exclude there is no filter: the result is handed
// on as it was read, with nothing copied and no key remembered.
func TestNoKeyFilterLeavesTheResultAlone(t *testing.T) { //nolint:paralleltest // AllocsPerRun cannot run in a parallel test
	f, err := (&outputOptions{}).filter()
	if err != nil {
		t.Fatal(err)
	}
	doc := []any{object("a", 1), object("b", 2)}
	out, err := f.apply(doc)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := out.([]any)
	if !ok || len(list) != 2 || list[0] != doc[0] || list[1] != doc[1] {
		t.Errorf("the result was rebuilt: %v", out)
	}
	rec := object("a", 1)
	if allocs := testing.AllocsPerRun(100, func() { _ = f.narrowRecord(rec) }); allocs != 0 {
		t.Errorf("narrowing a record without a filter allocates %v times", allocs)
	}
	if err := f.unseen(); err != nil {
		t.Errorf("unseen without a filter: %v", err)
	}
}

// A filter over a stream remembers the named keys it has seen, which is
// what deciding an unknown key needs, and only a bounded number of the
// other keys, which are only there to be shown in the refusal. A stream
// whose every record has keys of its own does not grow with them.
func TestKeyFilterHoldsABoundedListOfKeys(t *testing.T) {
	t.Parallel()
	for _, keep := range []bool{true, false} {
		f := newKeyFilter([]string{"late", "never"}, keep)
		for i := range 10000 {
			f.narrowRecord(object("key_"+strconv.Itoa(i), i))
		}
		f.narrowRecord(object("late", true))
		if len(f.present) > maxShownKeys {
			t.Errorf("keep=%v: %d keys held after 10001 records", keep, len(f.present))
		}
		err := f.unseen()
		var ue *unknownKeyError
		if !errors.As(err, &ue) || len(ue.keys) != 1 || ue.keys[0] != "never" {
			t.Fatalf("keep=%v: the key no record had: %v", keep, err)
		}
		if len(ue.present) > maxShownKeys || !strings.Contains(err.Error(), "the keys it has include") {
			t.Errorf("keep=%v: a list cut short says so: %d keys, %v", keep, len(ue.present), err)
		}
	}
	// A list that was not cut short is the whole list, said as before.
	f := newKeyFilter([]string{"z"}, true)
	f.narrowRecord(object("a", 1, "b", 2))
	if err := f.unseen(); err == nil || !strings.Contains(err.Error(), `the keys it has are "a", "b"`) {
		t.Errorf("a short list: %v", err)
	}
}

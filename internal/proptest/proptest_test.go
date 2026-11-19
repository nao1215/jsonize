package proptest

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// The same seed hands the property the same inputs, which is what makes a
// printed seed replay a failure; and every run gets inputs of its own.
func TestCheckReplaysTheSameInputsForASeed(t *testing.T) {
	t.Setenv("JSONIZE_PROPERTY_SEED", "42")
	t.Setenv("JSONIZE_PROPERTY_RUNS", "5")
	draw := func() []uint64 {
		var got []uint64
		Check(t, func(r *rand.Rand) string {
			got = append(got, r.Uint64())
			return ""
		})
		return got
	}
	first, second := draw(), draw()
	if len(first) != 5 {
		t.Fatalf("JSONIZE_PROPERTY_RUNS=5 ran the property %d times", len(first))
	}
	if !slices.Equal(first, second) {
		t.Fatalf("seed 42 drew %v, then %v", first, second)
	}
	if len(slices.Compact(slices.Sorted(slices.Values(first)))) != 5 {
		t.Fatalf("runs drew repeated inputs: %v", first)
	}
}

func TestCheckRunsTheDefaultNumberOfTimes(t *testing.T) {
	t.Setenv("JSONIZE_PROPERTY_SEED", "")
	t.Setenv("JSONIZE_PROPERTY_RUNS", "")
	n := 0
	Check(t, func(*rand.Rand) string {
		n++
		return ""
	})
	if n != DefaultRuns {
		t.Fatalf("ran %d times, want %d", n, DefaultRuns)
	}
}

func TestPickReturnsAnElement(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2))
	xs := []string{"a", "b", "c"}
	seen := map[string]bool{}
	for range 100 {
		seen[Pick(r, xs)] = true
	}
	if len(seen) != len(xs) {
		t.Fatalf("100 picks saw %v of %v", seen, xs)
	}
}

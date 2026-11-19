// Package proptest runs generated-input property tests with the standard
// library alone, so jsonize keeps no dependency outside it.
//
// Each run draws its inputs from a PCG generator seeded with (seed, run).
// The seed is random unless JSONIZE_PROPERTY_SEED names one, and a failure
// prints the environment that replays it. JSONIZE_PROPERTY_RUNS sets how
// many inputs a property is checked on: the default keeps go test fast,
// and the nightly job raises it.
package proptest

import (
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
)

// DefaultRuns is how many inputs a property is checked on when
// JSONIZE_PROPERTY_RUNS is not set.
const DefaultRuns = 200

// Check runs prop once per generated input. prop returns a description of
// the input and what went wrong, or "" when the property holds.
func Check(t *testing.T, prop func(r *rand.Rand) string) {
	t.Helper()
	seed, runs := settings(t)
	for i := range runs {
		r := rand.New(rand.NewPCG(seed, uint64(i))) // #nosec G404 -- test inputs replay from a printed seed; nothing drawn is secret
		if msg := prop(r); msg != "" {
			t.Fatalf("property failed on run %d: %s\nreplay with JSONIZE_PROPERTY_SEED=%d JSONIZE_PROPERTY_RUNS=%d", i, msg, seed, i+1)
		}
	}
}

func settings(t *testing.T) (uint64, int) {
	t.Helper()
	seed := rand.Uint64() // #nosec G404 -- a seed for test inputs, printed on failure; not a secret
	if s := os.Getenv("JSONIZE_PROPERTY_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("JSONIZE_PROPERTY_SEED=%q: %v", s, err)
		}
		seed = v
	}
	runs := DefaultRuns
	if s := os.Getenv("JSONIZE_PROPERTY_RUNS"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			t.Fatalf("JSONIZE_PROPERTY_RUNS=%q: want a positive integer", s)
		}
		runs = v
	}
	return seed, runs
}

// Pick returns one element of xs.
func Pick[T any](r *rand.Rand, xs []T) T {
	return xs[r.IntN(len(xs))]
}

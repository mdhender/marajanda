// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng

import (
	"math"
	"math/rand/v2"
)

// Roller is a dice-rolling front end over a single stream. It is the primitive
// every game dice expression maps onto: a "3d4" is RollN(3, 4), a "2d6" is
// RollN(2, 6), and so on; callers add, subtract, or clamp on top of the raw sum.
//
// A Roller wraps ONE math/rand/v2 generator, built once for a given address and
// drawn from repeatedly. It never re-derives a stream per die: successive rolls
// advance the same underlying PCG, so a Roller is an ordered draw source, not a
// pure function of its arguments. Two Rollers built at the same address produce
// identical sequences. A Roller is not safe for concurrent use.
//
// The rand/v2 uint64 -> bounded-int mapping (IntN and friends) is committed
// frozen by the Go team, so RollN and RollRange are stable across machines and
// Go versions given the same seeds and address — the same guarantee the golden
// vectors pin.
type Roller struct {
	rng *rand.Rand
}

// Roller returns a Roller for the stream addressed by path — one PCG per
// address, drawn from repeatedly. Build it once and reuse it; do not build a
// fresh Roller per die, which would restart the stream and correlate rolls.
//
// The first path element must be a domain tag (see tags.go); the rest identify
// the instance, exactly as for Stream.
func (s Seeds) Roller(path ...Key) *Roller {
	return &Roller{rng: rand.New(s.Stream(path...))}
}

// RollN returns the sum of n dice, each a uniform int in [1, sides], rolled left
// to right with one IntN(sides)+1 call per die. IntN may consume more than one
// source value when it rejects a value to avoid modulo bias. RollN is the
// primitive behind every game dice expression, e.g. RollN(3, 4) is 3d4. The
// result lies in [n, n*sides].
//
// It panics if n or sides is not positive, or if the maximum possible sum does
// not fit in an int.
func (r *Roller) RollN(n, sides int) int {
	if n <= 0 || sides <= 0 {
		panic("prng: RollN requires positive n and sides")
	}
	if sides > math.MaxInt/n {
		panic("prng: RollN result overflows int")
	}
	sum := 0
	for range n {
		sum += r.rng.IntN(sides) + 1
	}
	return sum
}

// RollRange returns a uniform int in [lo, hi] INCLUSIVE. It makes one bounded
// random call, which may consume more than one source value to avoid modulo
// bias.
//
// It panics if !(lo < hi); bounds are programmer-controlled constants at the
// call sites.
func (r *Roller) RollRange(lo, hi int) int {
	if !(lo < hi) {
		panic("prng: RollRange requires lo < hi")
	}

	// Unsigned subtraction gives the interval width without overflowing int.
	// A distance of MaxUint covers the entire int domain, whose size cannot be
	// represented in a uint; truncating Uint64 supplies one uniform machine word.
	distance := uint(hi) - uint(lo)
	if distance == math.MaxUint {
		return int(uint(lo) + uint(r.rng.Uint64()))
	}
	return int(uint(lo) + uint(r.rng.Uint64N(uint64(distance)+1)))
}

// Shuffle pseudo-randomizes the order of n elements using swap, a straight
// passthrough to the embedded *rand.Rand. It draws from the same stream as the
// roll methods, advancing it. Used later for the placement hex shuffle and the
// orbit shuffle.
func (r *Roller) Shuffle(n int, swap func(i, j int)) {
	r.rng.Shuffle(n, swap)
}

// Perm returns a pseudo-random permutation of [0, n), a straight passthrough to
// the embedded *rand.Rand. It draws from the same stream as the roll methods,
// advancing it.
func (r *Roller) Perm(n int) []int {
	return r.rng.Perm(n)
}

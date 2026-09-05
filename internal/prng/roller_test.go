// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mdhender/marajanda/internal/prng"
)

// TestRollNBounds: RollN(n, sides) always lands in [n, n*sides].
func TestRollNBounds(t *testing.T) {
	roller := prng.New(1, 2).Roller(prng.TagTile, 0, 0, 0, 1)
	cases := []struct{ n, sides int }{
		{1, 6}, {3, 4}, {2, 6}, {4, 10}, {10, 20},
	}
	for _, c := range cases {
		lo, hi := c.n, c.n*c.sides
		for range 1000 {
			got := roller.RollN(c.n, c.sides)
			if got < lo || got > hi {
				t.Fatalf("RollN(%d,%d) = %d, out of [%d,%d]", c.n, c.sides, got, lo, hi)
			}
		}
	}
}

// TestRollNSingleDie: RollN(1, sides) is a plain [1, sides] die and hits both
// ends over enough draws.
func TestRollNSingleDie(t *testing.T) {
	roller := prng.New(5, 6).Roller(prng.TagTile, 1, 0, -1, 1)
	const sides = 6
	seen := map[int]bool{}
	for range 2000 {
		v := roller.RollN(1, sides)
		if v < 1 || v > sides {
			t.Fatalf("RollN(1,%d) = %d out of range", sides, v)
		}
		seen[v] = true
	}
	for face := 1; face <= sides; face++ {
		if !seen[face] {
			t.Errorf("face %d never rolled in %d draws", face, 2000)
		}
	}
}

// TestRollRangeInclusive: RollRange(lo, hi) stays in [lo, hi] and reaches both
// endpoints.
func TestRollRangeInclusive(t *testing.T) {
	roller := prng.New(9, 10).Roller(prng.TagMarajanda)
	const lo, hi = -3, 3
	sawLo, sawHi := false, false
	for range 5000 {
		v := roller.RollRange(lo, hi)
		if v < lo || v > hi {
			t.Fatalf("RollRange(%d,%d) = %d out of range", lo, hi, v)
		}
		if v == lo {
			sawLo = true
		}
		if v == hi {
			sawHi = true
		}
	}
	if !sawLo || !sawHi {
		t.Errorf("RollRange(%d,%d) did not reach both endpoints (lo=%v hi=%v)", lo, hi, sawLo, sawHi)
	}
}

func TestRollRangeWideBounds(t *testing.T) {
	roller := prng.New(9, 10).Roller(prng.TagMarajanda)
	for range 100 {
		if got := roller.RollRange(math.MinInt, 0); got < math.MinInt || got > 0 {
			t.Fatalf("RollRange(MinInt,0) = %d out of range", got)
		}
	}

	// The full int domain has one more value than MaxUint can represent, so this
	// exercises RollRange's full-width special case.
	roller.RollRange(math.MinInt, math.MaxInt)
}

// TestRollRangePanics: RollRange must panic when !(lo < hi) — a programmer-error
// guard on constant bounds.
func TestRollRangePanics(t *testing.T) {
	cases := []struct{ lo, hi int }{
		{5, 5},  // equal
		{5, 4},  // inverted
		{0, -1}, // inverted across zero
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RollRange(%d,%d) did not panic", c.lo, c.hi)
				}
			}()
			prng.New(1, 1).Roller(prng.TagMarajanda).RollRange(c.lo, c.hi)
		}()
	}
}

// TestRollerReproducible: two Rollers built at the same address produce
// identical sequences — the Roller advances one stream, and address is the only
// input.
func TestRollerReproducible(t *testing.T) {
	seeds := prng.New(0xabc, 0xdef)
	a := seeds.Roller(prng.TagTile, 3, -7, 4, 1)
	b := seeds.Roller(prng.TagTile, 3, -7, 4, 1)
	for i := range 50 {
		if x, y := a.RollN(2, 6), b.RollN(2, 6); x != y {
			t.Fatalf("roll %d differs between equal-address Rollers: %d vs %d", i, x, y)
		}
	}
}

// TestRollerMatchesStream: a Roller and a fresh rand.New(Stream) at the same
// address agree call-for-call — the Roller is exactly rand.New over the stream,
// with one IntN(sides)+1 call per die.
func TestRollerMatchesStream(t *testing.T) {
	seeds := prng.New(42, 43)
	path := []prng.Key{prng.TagHex, 2, -4, 2}

	roller := seeds.Roller(path...)
	rng := rand.New(seeds.Stream(path...))

	for i := range 50 {
		const n, sides = 3, 4
		want := 0
		for range n {
			want += rng.IntN(sides) + 1
		}
		if got := roller.RollN(n, sides); got != want {
			t.Fatalf("roll %d: Roller.RollN = %d, hand-rolled Stream = %d", i, got, want)
		}
	}
}

// TestRollerDistinctAddresses checks that two representative addresses have
// different first rolls.
func TestRollerDistinctAddresses(t *testing.T) {
	seeds := prng.New(7, 11)
	// Wide range so a collision would signal shared state, not chance.
	x := seeds.Roller(prng.TagTile, 0, 0, 0, 1).RollRange(0, 1<<30)
	y := seeds.Roller(prng.TagTile, 0, 0, 0, 2).RollRange(0, 1<<30)
	if x == y {
		t.Errorf("distinct tile addresses produced identical first roll %d", x)
	}
}

func TestRollNInvalidParametersPanic(t *testing.T) {
	tests := []struct{ n, sides int }{
		{0, 6},
		{-1, 6},
		{1, 0},
		{1, -1},
		{2, math.MaxInt},
	}
	for _, tt := range tests {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RollN(%d,%d) did not panic", tt.n, tt.sides)
				}
			}()
			prng.New(1, 2).Roller(prng.TagMarajanda).RollN(tt.n, tt.sides)
		}()
	}
}

// TestShuffleDeterministic: Shuffle draws from the Roller's stream, so equal
// addresses shuffle identically.
func TestShuffleDeterministic(t *testing.T) {
	seeds := prng.New(3, 4)
	perm := func() []int {
		s := []int{0, 1, 2, 3, 4, 5, 6, 7}
		seeds.Roller(prng.TagMarajanda).Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
		return s
	}
	a, b := perm(), perm()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("Shuffle not deterministic at %d: %v vs %v", i, a, b)
		}
	}
}

// TestPermDeterministic: Perm draws from the Roller's stream, so equal addresses
// permute identically and the result is a valid permutation of [0,n).
func TestPermDeterministic(t *testing.T) {
	seeds := prng.New(3, 4)
	a := seeds.Roller(prng.TagMarajanda).Perm(10)
	b := seeds.Roller(prng.TagMarajanda).Perm(10)
	if len(a) != 10 {
		t.Fatalf("Perm(10) len = %d", len(a))
	}
	seen := make([]bool, 10)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("Perm not deterministic at %d: %v vs %v", i, a, b)
		}
		if a[i] < 0 || a[i] >= 10 || seen[a[i]] {
			t.Fatalf("Perm(10) not a valid permutation: %v", a)
		}
		seen[a[i]] = true
	}
}

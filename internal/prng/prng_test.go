// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng_test

import (
	"encoding/json"
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mdhender/marajanda/internal/prng"
)

// update regenerates testdata/golden.json from the current code. Run once when
// intentionally establishing the frozen surface:
//
//	go test ./internal/prng/ -update
//
// then eyeball the diff and commit. Never run it to "fix" a failing golden test:
// a failure means the addressing, hashing, or generator changed, which silently
// rewrites every live game.
var update = flag.Bool("update", false, "regenerate testdata/golden.json")

const goldenPath = "testdata/golden.json"

// drawsPerStream is how many uint64 each golden stream pins.
const drawsPerStream = 4

// rollsPerVector is how many rolls each golden roll vector pins.
const rollsPerVector = 8

// golden is the on-disk shape of the frozen vectors.
type golden struct {
	Streams  []streamVector  `json:"streams"`
	Derives  []deriveVector  `json:"derives"`
	Rolls    []rollVector    `json:"rolls"`
	Shuffles []shuffleVector `json:"shuffles"`
}

type streamVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	Draws []uint64   `json:"draws"`
}

type deriveVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	// child seeds are exposed only via their observable behavior; we pin the
	// first draw of the child's own default stream so the vector stays black-box.
	WantChildDraw uint64 `json:"want_child_draw"`
}

// rollVector pins a Roller's output sequence for a fixed seed + address. Kind
// selects the call: "rolln" repeats RollN(N, Sides); "rollrange" repeats
// RollRange(Lo, Hi). Pinning the whole sequence catches any change to the
// draw-to-die mapping, the draw order, or the reduction.
type rollVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	Kind  string     `json:"kind"`
	N     int        `json:"n"`
	Sides int        `json:"sides"`
	Lo    int        `json:"lo"`
	Hi    int        `json:"hi"`
	Out   []int      `json:"out"`
}

// shuffleVector pins Shuffle and Perm for a fixed seed + address.
type shuffleVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	N     int        `json:"n"`
	// Out is 0..N-1 after Shuffle; Perm is Perm(N) from a fresh Roller at the
	// same address, which must therefore start from the same stream state.
	Out  []int `json:"out"`
	Perm []int `json:"perm"`
}

// goldenInputs enumerates the addresses whose outputs we freeze. Extend by
// APPENDING; never change an existing entry's seeds or path.
func goldenInputs() golden {
	streamPaths := [][]prng.Key{
		{prng.TagMarajanda},
		{prng.TagPlayer, 1},
		{prng.TagPlayer, 2},
		{prng.TagFaction, 1},
		{prng.TagFaction, 2},
		{prng.TagHex, 0, 0, 0},
		{prng.TagHex, 3, -7, 4},
		{prng.TagTile, 3, -7, 4},    // shorter path (length is part of the address)
		{prng.TagTile, 3, -7, 4, 1}, // hex (3, -7, 4), tile type 1
		{prng.TagTile, 3, -7, 4, 2}, // sibling tile type must differ
		{prng.TagWorld, 1},          // world field 1, whole-field draw
		{prng.TagWorld, 1, 12, -5},  // world field 1, noise lattice point (12, -5)
	}
	derivePaths := [][]prng.Key{
		{prng.TagMarajanda},
		{prng.TagPlayer, 42},
		{prng.TagHex, 3, -7, 4},
		{prng.TagWorld, 1},
	}
	const s1, s2 = 0x0123456789abcdef, 0xfedcba9876543210

	var g golden
	seeds := prng.New(s1, s2)
	for _, p := range streamPaths {
		st := seeds.Stream(p...)
		draws := make([]uint64, drawsPerStream)
		for i := range draws {
			draws[i] = st.Uint64()
		}
		g.Streams = append(g.Streams, streamVector{Seed1: s1, Seed2: s2, Path: p, Draws: draws})
	}
	for _, p := range derivePaths {
		child := seeds.Derive(p...)
		g.Derives = append(g.Derives, deriveVector{
			Seed1: s1, Seed2: s2, Path: p,
			WantChildDraw: child.Stream(prng.TagMarajanda).Uint64(),
		})
	}

	// Roll sequences: pin RollN and RollRange output order for fixed addresses.
	rollNInputs := []struct {
		path     []prng.Key
		n, sides int
	}{
		{[]prng.Key{prng.TagTile, 3, -7, 4, 1}, 3, 4}, // 3d4
		{[]prng.Key{prng.TagHex, 3, -7, 4}, 2, 6},     // 2d6
	}
	for _, in := range rollNInputs {
		roller := seeds.Roller(in.path...)
		out := make([]int, rollsPerVector)
		for i := range out {
			out[i] = roller.RollN(in.n, in.sides)
		}
		g.Rolls = append(g.Rolls, rollVector{
			Seed1: s1, Seed2: s2, Path: in.path, Kind: "rolln",
			N: in.n, Sides: in.sides, Out: out,
		})
	}
	rollRangeInputs := []struct {
		path   []prng.Key
		lo, hi int
	}{
		{[]prng.Key{prng.TagMarajanda}, 1, 10},
		{[]prng.Key{prng.TagTile, 3, -7, 4, 2}, -3, 3},
	}
	for _, in := range rollRangeInputs {
		roller := seeds.Roller(in.path...)
		out := make([]int, rollsPerVector)
		for i := range out {
			out[i] = roller.RollRange(in.lo, in.hi)
		}
		g.Rolls = append(g.Rolls, rollVector{
			Seed1: s1, Seed2: s2, Path: in.path, Kind: "rollrange",
			Lo: in.lo, Hi: in.hi, Out: out,
		})
	}

	// Shuffle and Perm at two representative addresses.
	shuffleInputs := []struct {
		path []prng.Key
		n    int
	}{
		{[]prng.Key{prng.TagMarajanda}, 16},
		{[]prng.Key{prng.TagFaction, 2}, 16},
	}
	for _, in := range shuffleInputs {
		out := make([]int, in.n)
		for i := range out {
			out[i] = i
		}
		seeds.Roller(in.path...).Shuffle(in.n, func(i, j int) { out[i], out[j] = out[j], out[i] })
		g.Shuffles = append(g.Shuffles, shuffleVector{
			Seed1: s1, Seed2: s2, Path: in.path, N: in.n,
			Out: out, Perm: seeds.Roller(in.path...).Perm(in.n),
		})
	}
	return g
}

func TestGolden(t *testing.T) {
	inputs := goldenInputs()
	if *update {
		writeGolden(t, inputs)
		t.Log("wrote", goldenPath)
	}

	want := readGolden(t)
	sections := []struct {
		name      string
		got, want int
	}{
		{name: "streams", got: len(want.Streams), want: len(inputs.Streams)},
		{name: "derives", got: len(want.Derives), want: len(inputs.Derives)},
		{name: "rolls", got: len(want.Rolls), want: len(inputs.Rolls)},
		{name: "shuffles", got: len(want.Shuffles), want: len(inputs.Shuffles)},
	}
	for _, section := range sections {
		if section.got != section.want {
			t.Fatalf("golden %s has %d vectors, want %d (run with -update to recreate)", section.name, section.got, section.want)
		}
	}

	for _, v := range want.Streams {
		st := prng.New(v.Seed1, v.Seed2).Stream(v.Path...)
		for i, w := range v.Draws {
			if got := st.Uint64(); got != w {
				t.Errorf("Stream(%v) draw %d = %d, want %d (frozen surface changed?)", v.Path, i, got, w)
			}
		}
	}
	for _, v := range want.Derives {
		child := prng.New(v.Seed1, v.Seed2).Derive(v.Path...)
		if got := child.Stream(prng.TagMarajanda).Uint64(); got != v.WantChildDraw {
			t.Errorf("Derive(%v) child draw = %d, want %d (frozen surface changed?)", v.Path, got, v.WantChildDraw)
		}
	}
	for _, v := range want.Rolls {
		roller := prng.New(v.Seed1, v.Seed2).Roller(v.Path...)
		for i, w := range v.Out {
			var got int
			switch v.Kind {
			case "rolln":
				got = roller.RollN(v.N, v.Sides)
			case "rollrange":
				got = roller.RollRange(v.Lo, v.Hi)
			default:
				t.Fatalf("unknown roll kind %q", v.Kind)
			}
			if got != w {
				t.Errorf("Roller(%v).%s roll %d = %d, want %d (frozen surface changed?)", v.Path, v.Kind, i, got, w)
			}
		}
	}
	for _, v := range want.Shuffles {
		seeds := prng.New(v.Seed1, v.Seed2)
		got := make([]int, v.N)
		for i := range got {
			got[i] = i
		}
		seeds.Roller(v.Path...).Shuffle(v.N, func(i, j int) { got[i], got[j] = got[j], got[i] })
		if !slices.Equal(got, v.Out) {
			t.Errorf("Roller(%v).Shuffle(%d) = %v, want %v (frozen surface changed?)", v.Path, v.N, got, v.Out)
		}
		if perm := seeds.Roller(v.Path...).Perm(v.N); !slices.Equal(perm, v.Perm) {
			t.Errorf("Roller(%v).Perm(%d) = %v, want %v (frozen surface changed?)", v.Path, v.N, perm, v.Perm)
		}
	}
}

// TestOrderIndependence: an address's output depends only on the address, never
// on when it is computed relative to other draws.
func TestOrderIndependence(t *testing.T) {
	seeds := prng.New(1, 2)
	a := []prng.Key{prng.TagTile, 5, -3, -2, 1}
	b := []prng.Key{prng.TagTile, 8, -12, 4, 2}

	// Reference: draw A on its own.
	ref := drawN(seeds.Stream(a...), 3)

	// Draw B first, then A — A must be unchanged.
	seeds.Stream(b...).Uint64()
	got := drawN(seeds.Stream(a...), 3)

	if !equal(ref, got) {
		t.Errorf("A's draws changed with order: %v vs %v", ref, got)
	}
}

// TestDistinctAddresses checks that representative distinct tags, instances,
// and path lengths have different first outputs.
func TestDistinctAddresses(t *testing.T) {
	seeds := prng.New(7, 11)
	cases := map[string][]prng.Key{
		"marajanda":    {prng.TagMarajanda},
		"hex-0-0-0":    {prng.TagHex, 0, 0, 0},
		"hex-0-0-0-0":  {prng.TagHex, 0, 0, 0, 0}, // length is part of the address
		"hex-1-0-neg1": {prng.TagHex, 1, 0, -1},
		"tile-0-0-0-1": {prng.TagTile, 0, 0, 0, 1}, // tag separates same-coordinate domains
		"player-1":     {prng.TagPlayer, 1},
		"player-2":     {prng.TagPlayer, 2},
	}
	seen := map[uint64]string{}
	for name, path := range cases {
		first := seeds.Stream(path...).Uint64()
		if other, ok := seen[first]; ok {
			t.Errorf("address %q collides with %q (first draw %d)", name, other, first)
		}
		seen[first] = name
	}
}

func TestPathRequiresDomainKey(t *testing.T) {
	seeds := prng.New(1, 2)
	tests := []struct {
		name string
		call func()
	}{
		{name: "empty stream path", call: func() { seeds.Stream() }},
		{name: "zero stream domain", call: func() { seeds.Stream(0, 1) }},
		{name: "unknown root stream domain", call: func() { seeds.Stream(99, 1) }},
		{name: "empty derive path", call: func() { seeds.Derive() }},
		{name: "zero derive domain", call: func() { seeds.Derive(0, 1) }},
		{name: "unknown root derive domain", call: func() { seeds.Derive(99, 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("call did not panic")
				}
			}()
			tt.call()
		})
	}

	// Derived subsystems own their domain registry, but zero remains invalid.
	seeds.Derive(prng.TagMarajanda).Stream(99).Uint64()
}

// Stream must satisfy math/rand/v2.Source (so rand.New can wrap it).
var _ rand.Source = (*prng.Stream)(nil)

func TestStreamWrapsRand(t *testing.T) {
	r := rand.New(prng.New(3, 4).Stream(prng.TagPlayer, 99))
	if n := r.IntN(6); n < 0 || n >= 6 {
		t.Errorf("IntN(6) out of range: %d", n)
	}
}

func drawN(s *prng.Stream, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = s.Uint64()
	}
	return out
}

func equal(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func writeGolden(t *testing.T, g golden) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

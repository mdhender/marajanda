package hexfield

import (
	"iter"
	"math"

	"github.com/mdhender/marjanda/internal/hexgrid"
)

// Field is a hexagon-shaped height field of radius 2**Levels, addressed by
// cube coordinate.
//
// The hexagon is the natural domain for this algorithm. It is convex in the
// hex metric, so midpoints of in-bounds points are always in bounds; it has
// no preferred axis; and its coarsest lattice is exactly seven points -- the
// origin plus six at radius N -- which refines with no leftovers. Those seven
// values are the caller's control over the large-scale shape of the map.
//
//	Levels 0: radius  1 ->    7 hexes   (the seed)
//	Levels 1: radius  2 ->   19 hexes
//	Levels 2: radius  4 ->   61 hexes
//	Levels K: radius  N -> 3N^2+3N+1 hexes
type Field struct {
	Levels int // number of subdivision levels
	Radius int // 1 << Levels

	stride int
	h      []float64
}

// New returns a Field of radius 2**levels with every height unset.
func New(levels int) *Field {
	n := 1 << levels
	stride := 2*n + 1
	f := &Field{
		Levels: levels,
		Radius: n,
		stride: stride,
		h:      make([]float64, stride*stride),
	}
	// The hexagon occupies about three quarters of the enclosing rhombus.
	// NaN marks both the unused corners and any point not yet written.
	for i := range f.h {
		f.h[i] = math.NaN()
	}
	return f
}

// Contains reports whether c lies inside the hexagon.
func (f *Field) Contains(c hexgrid.Coord) bool {
	return c.Valid() && c.Length() <= f.Radius
}

func (f *Field) index(c hexgrid.Coord) int { return (c.Q+f.Radius)*f.stride + c.R + f.Radius }

// At returns the height at c, or NaN if c is out of bounds or unset.
func (f *Field) At(c hexgrid.Coord) float64 {
	if !f.Contains(c) {
		return math.NaN()
	}
	return f.h[f.index(c)]
}

// Has reports whether c is in bounds and holds a height.
func (f *Field) Has(c hexgrid.Coord) bool {
	return f.Contains(c) && !math.IsNaN(f.h[f.index(c)])
}

// Set stores a height at c. Out-of-bounds coordinates are ignored.
func (f *Field) Set(c hexgrid.Coord, v float64) {
	if f.Contains(c) {
		f.h[f.index(c)] = v
	}
}

// All iterates every in-bounds hex and its height, in no meaningful order.
func (f *Field) All() iter.Seq2[hexgrid.Coord, float64] {
	return func(yield func(hexgrid.Coord, float64) bool) {
		n := f.Radius
		for q := -n; q <= n; q++ {
			for r := max(-n, -n-q); r <= min(n, n-q); r++ {
				c := hexgrid.Coord{Q: q, R: r, S: -q - r}
				if !yield(c, f.h[f.index(c)]) {
					return
				}
			}
		}
	}
}

// Len returns the number of hexes in the field.
func (f *Field) Len() int { return hexgrid.Count(f.Radius) }

// Range returns the lowest and highest heights present.
func (f *Field) Range() (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, v := range f.All() {
		if math.IsNaN(v) {
			continue
		}
		lo, hi = min(lo, v), max(hi, v)
	}
	return lo, hi
}

// Normalize rescales every height into [0,1]. A flat field becomes all zeros.
func (f *Field) Normalize() {
	lo, hi := f.Range()
	span := hi - lo
	if span <= 0 {
		for c := range f.All() {
			f.Set(c, 0)
		}
		return
	}
	for c, v := range f.All() {
		f.Set(c, (v-lo)/span)
	}
}

// eachLattice calls fn for every in-bounds point of the level-k lattice,
// where step is 2**k. Because Q+R+S == 0, S is divisible by step whenever Q
// and R both are, so stepping Q and R is sufficient. The bound -Radius is
// itself a multiple of every step in use, so the walk stays on the lattice.
func (f *Field) eachLattice(step int, fn func(hexgrid.Coord)) {
	n := f.Radius
	for q := -n; q <= n; q += step {
		for r := -n; r <= n; r += step {
			if c := (hexgrid.Coord{Q: q, R: r, S: -q - r}); f.Contains(c) {
				fn(c)
			}
		}
	}
}

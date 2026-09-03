// Package hexfield generates fractal terrain on a hexagonal grid.
//
// Hexagons are not rep-tiles: a hexagon cannot be divided into smaller
// hexagons, so the square-grid diamond-square algorithm has no direct
// analogue here. The centres of a hex grid, however, form a triangular
// lattice, and triangular lattices do refine self-similarly -- inserting a
// point at every edge midpoint yields a triangular lattice of half the
// spacing, splitting each triangle into four. That is the recursion this
// package uses, and it is what Loop subdivision does topologically.
//
// In cube coordinates the refinement is exact. Define the level-k lattice as
// every hex whose Q, R and S are all divisible by 2**k. The midpoint of two
// adjacent level-k points lands precisely on the level-(k-1) lattice, and
// since Q+R+S == 0 the parities of a coordinate fall into exactly four
// classes: (even,even,even) is the coarse lattice that already holds values,
// and the other three are the edge midpoints along the three edge directions.
// Each refinement therefore adds exactly three points per existing point,
// covering the finer lattice with nothing missed and nothing written twice.
//
// One consequence is that this needs only a single step per level. Square
// grids need a diamond phase and a square phase because their midpoints come
// in two incompatible flavours; the triangular lattice refines uniformly.
package hexfield

// Coord is a cube coordinate on a hex grid. Every valid Coord satisfies
// Q+R+S == 0, so the three axes are symmetric and none is privileged.
//
// The coordinate system carries no orientation. Pointy-top and flat-top
// differ only in the layout matrix applied at render time, so every
// adjacency, distance and midpoint below is identical either way.
type Coord struct {
	Q, R, S int
}

// Origin is the centre of a Field.
var Origin = Coord{}

// Directions lists the six neighbour directions in cyclic order. The cyclic
// ordering is load-bearing: Directions[(i+1)%6] and Directions[(i+5)%6] are
// the two directions adjacent to Directions[i], which is how the subdivision
// step locates the apexes of the two triangles flanking an edge.
var Directions = [6]Coord{
	{Q: 1, R: -1, S: 0},
	{Q: 1, R: 0, S: -1},
	{Q: 0, R: 1, S: -1},
	{Q: -1, R: 1, S: 0},
	{Q: -1, R: 0, S: 1},
	{Q: 0, R: -1, S: 1},
}

// forward is how many directions must be walked to visit every lattice edge
// exactly once. The remaining three are negations of these, so walking all
// six would process every edge twice.
const forward = 3

// Add returns c+o.
func (c Coord) Add(o Coord) Coord { return Coord{c.Q + o.Q, c.R + o.R, c.S + o.S} }

// Scale returns c multiplied by n.
func (c Coord) Scale(n int) Coord { return Coord{c.Q * n, c.R * n, c.S * n} }

// Valid reports whether c satisfies the cube invariant Q+R+S == 0.
func (c Coord) Valid() bool { return c.Q+c.R+c.S == 0 }

// Length is the distance from the origin in hex steps.
func (c Coord) Length() int { return max(abs(c.Q), abs(c.R), abs(c.S)) }

// Lattice reports the coarsest lattice c belongs to: the largest k <= maxK
// for which every coordinate of c is divisible by 2**k. Masking works for
// negative coordinates because Go's ints are two's complement.
func (c Coord) Lattice(maxK int) int {
	for k := maxK; k > 0; k-- {
		m := 1<<k - 1
		if c.Q&m == 0 && c.R&m == 0 && c.S&m == 0 {
			return k
		}
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

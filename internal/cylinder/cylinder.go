// Copyright (c) 2026 Michael D Henderson.

// Package cylinder provides hex geometry on a world that wraps east-west.
//
// Every operation in hexg assumes an unbounded plane. On a cylinder that is
// silently wrong rather than obviously wrong: hexg.Hex.Distance across the seam
// returns the long way around, with no error and no way to tell from the
// result. This package wraps the operations that measure or walk so the seam
// stops being a special case the caller has to remember.
//
// The world wraps in q only. North and south are walls, and this package
// deliberately does not know where they are: the row bounds are a game rule
// (marajanda ends its world in impassable ice), not a property of the topology,
// and a Cylinder that clipped rows would have to be rebuilt every time that
// rule changed. Callers filter by row themselves.
//
// # Canonical form
//
// A hex is canonical when its q lies in the window [lo, lo+columns-1], where
// lo is -(columns/2). For an odd column count that window is symmetric about
// the origin and every hex has exactly one canonical name. For an even count it
// is lopsided by one, because a symmetric window would span one more integer
// than there are columns and the two endpoints would name the same hex.
//
// Normalize before using a hex as a key of anything. Marajanda derives a
// private PRNG stream from hex coordinates, so a hex reached from the east
// would otherwise seed differently than the same hex reached from the west.
//
// # What is not here
//
// Rotation and reflection are absent on purpose. Of the six cube operations
// hexg offers, only two survive the wrap - a 180 degree rotation and ReflectR,
// an east-west mirror - and both hold only about the origin with a symmetric
// row range. The other four are not cylinder isometries at all: they do not
// preserve wrapped distance, so there is no correct version to write. Exposing
// the two survivors alongside would imply the other four merely need wrapping.
package cylinder

import (
	"fmt"
	"slices"

	"github.com/maloquacious/hexg"
)

// Cylinder is a hex world of a fixed column count that wraps east-west.
//
// The zero value is not usable; build one with [New].
type Cylinder struct {
	columns int
}

// New returns a cylinder of the given total column count.
//
// columns is the whole width of the world, never a half-extent. A caller that
// thinks in half-extents - marajanda's --width is the number of columns either
// side of the origin - passes 2*half+1 and keeps the factor of two visible at
// its own call site, which is the only place it can be checked.
//
// Panics if columns is not positive. That is a programmer error rather than a
// runtime condition: a world size arrives from a validated flag, and a
// cylinder of zero columns has no meaningful behaviour to fall back on.
func New(columns int) Cylinder {
	if columns < 1 {
		panic(fmt.Sprintf("cylinder: columns must be positive, got %d", columns))
	}
	return Cylinder{columns: columns}
}

// Columns returns the total column count.
func (c Cylinder) Columns() int { return c.columns }

// low returns the westmost canonical q.
func (c Cylinder) low() int { return -(c.columns / 2) }

// wrapQ brings a q coordinate into the canonical window.
func (c Cylinder) wrapQ(q int) int {
	lo := c.low()
	return ((q-lo)%c.columns+c.columns)%c.columns + lo
}

// Normalize returns the canonical representative of h. The row is untouched.
//
// This is the hottest operation in the package - everything that keys off a
// coordinate normalizes first - which is why the canonical window is defined on
// q rather than on the offset column. Fixing it on the column names the same
// world, but costs a cube-to-offset round trip on every call instead of one
// modulo.
func (c Cylinder) Normalize(h hexg.Hex) hexg.Hex {
	return hexg.NewHex(c.wrapQ(h.Q()), h.R())
}

// IsCanonical reports whether h is already its own canonical representative.
func (c Cylinder) IsCanonical(h hexg.Hex) bool {
	return h.Q() == c.wrapQ(h.Q())
}

// representatives returns the three copies of h that can be closest to a given
// hex: the canonical one and its neighbours one lap either way. No fourth copy
// can ever win, because going around more than once is strictly further.
func (c Cylinder) representatives(h hexg.Hex) [3]hexg.Hex {
	q, r := c.wrapQ(h.Q()), h.R()
	return [3]hexg.Hex{
		hexg.NewHex(q-c.columns, r),
		hexg.NewHex(q, r),
		hexg.NewHex(q+c.columns, r),
	}
}

// Nearest returns the copy of h closest to origin, which is generally not the
// canonical one.
//
// This is what rendering and pathfinding need, and it is easy to reach for
// Normalize by mistake. A viewport drawn around a player standing next to the
// seam must place that player's eastern neighbour one hex east, not most of a
// world away; the canonical name would do the latter.
//
// The result is stable when two copies tie: the western one wins.
func (c Cylinder) Nearest(origin, h hexg.Hex) hexg.Hex {
	best, bestDistance := hexg.Hex{}, -1
	for _, candidate := range c.representatives(h) {
		if d := origin.Distance(candidate); bestDistance < 0 || d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best
}

// Distance returns the number of steps between a and b by the shorter way
// around.
func (c Cylinder) Distance(a, b hexg.Hex) int {
	return a.Distance(c.Nearest(a, b))
}

// Neighbor returns the canonical hex one step from h in the given direction.
// Direction is coerced to 0..5, as in hexg.
func (c Cylinder) Neighbor(h hexg.Hex, direction int) hexg.Hex {
	return c.Normalize(h.Neighbor(direction))
}

// Ring returns every canonical hex at exactly radius steps from center,
// ordered by [hexg.Hex.Compare].
//
// Past half the world's width a planar ring is not merely redundant on a
// cylinder, it is the wrong set: normalizing hexg.Hex.Ring at that size yields
// hexes that are not at the requested distance at all, so deduplicating it does
// not rescue it. This derives the ring from wrapped distance instead once the
// cheap path stops being exact.
//
// Panics if radius is negative, as hexg does.
func (c Cylinder) Ring(center hexg.Hex, radius int) []hexg.Hex {
	if radius < 0 {
		panic(fmt.Sprintf("cylinder: radius must be non-negative, got %d", radius))
	}
	if radius == 0 {
		return []hexg.Hex{c.Normalize(center)}
	}

	var hexes []hexg.Hex
	if radius <= (c.columns-1)/2 {
		// The planar ring is exact and duplicate-free at this size.
		hexes = make([]hexg.Hex, 0, 6*radius)
		for _, h := range center.Ring(radius) {
			hexes = append(hexes, c.Normalize(h))
		}
	} else {
		// A hex at distance radius is at most radius rows away, so this scan is
		// bounded even though it is not clever.
		lo := c.low()
		for r := center.R() - radius; r <= center.R()+radius; r++ {
			for q := lo; q < lo+c.columns; q++ {
				h := hexg.NewHex(q, r)
				if c.Distance(center, h) == radius {
					hexes = append(hexes, h)
				}
			}
		}
	}
	slices.SortFunc(hexes, hexg.Hex.Compare)
	return hexes
}

// Spiral returns every canonical hex within radius of center, ordered by
// distance and then by [hexg.Hex.Compare] within each distance.
//
// No hex can repeat: wrapped distance is a single value per hex, so the rings
// it concatenates are disjoint however far the radius reaches around.
//
// Panics if radius is negative.
func (c Cylinder) Spiral(center hexg.Hex, radius int) []hexg.Hex {
	if radius < 0 {
		panic(fmt.Sprintf("cylinder: radius must be non-negative, got %d", radius))
	}
	hexes := []hexg.Hex{c.Normalize(center)}
	for k := 1; k <= radius; k++ {
		hexes = append(hexes, c.Ring(center, k)...)
	}
	return hexes
}

// LineDraw returns the canonical hexes on a shortest line from a to b,
// beginning at a and ending at the canonical form of b.
//
// The line takes the shorter way around. When both ways are equally short the
// western one wins, following [Nearest], so the result is stable rather than
// merely correct.
func (c Cylinder) LineDraw(a, b hexg.Hex) []hexg.Hex {
	line := a.LineDraw(c.Nearest(a, b))
	hexes := make([]hexg.Hex, 0, len(line))
	for _, h := range line {
		hexes = append(hexes, c.Normalize(h))
	}
	return hexes
}

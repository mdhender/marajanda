// Copyright (c) 2026 Michael D Henderson.

// Package compass names the six directions a hex has a neighbour in.
//
// hexg numbers its directions 0 to 5 and stops there, which is correct: which
// number faces north-east is a property of how a consumer lays its map out,
// not of the hex algebra. This package is marajanda answering that question
// for its own world, the way [github.com/mdhender/marajanda/internal/cylinder]
// answers the wrapping question. See #27.
//
// # There is no north
//
// The world is drawn pointy-top, so a hex has flat edges east and west and
// vertices at the top and bottom. Its six neighbours are north-east, east,
// south-east, south-west, west and north-west. Due north and due south are
// real bearings that no hex borders, and [Parse] says so in as many words
// rather than rounding them to something near.
//
// # The order
//
// [Points] returns the six in a fixed order - NE, E, SE, SW, W, NW - which is
// clockwise on screen starting from north-east. The order is a game rule and
// not a presentation choice: rules that require visiting a hex's neighbours in
// order use this one, so it is stable and callers may rely on it.
//
// # The hexg numbers
//
// The mapping onto hexg's direction indices is an implementation detail and
// deliberately unexported. It is measured from the layout rather than assumed:
// under a pointy-top layout, hexg direction 0 steps due east and direction 1
// steps up and to the right, which the tests re-derive from
// [github.com/maloquacious/hexg.Layout] rather than restate. Nothing outside
// this package should learn the numbers, because the day the layout changes
// they change with it.
package compass

import (
	"errors"
	"fmt"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/cylinder"
)

// Point is one of the six directions a hex has a neighbour in.
//
// The zero value is not a point. An order that was never filled in is a bug,
// and it should not quietly mean north-east.
type Point int

// The six points, in the order [Points] returns them.
//
// The numbering starts at 1 so that the zero value stays invalid. It is not a
// storage or wire format: orders name a point, they do not number one.
const (
	_ Point = iota

	NE
	E
	SE
	SW
	W
	NW
)

var (
	// ErrUnknownPoint reports a value that names no compass point at all.
	ErrUnknownPoint = errors.New("not a compass point")

	// ErrNoNeighbor reports a bearing that is a real direction but not one a
	// hex has a neighbour in: due north and due south on a pointy-top grid.
	ErrNoNeighbor = errors.New("no hex lies in that direction")
)

// points is the canonical order. Everything that iterates the compass reads
// this, so there is one place the order is written down.
var points = [...]Point{NE, E, SE, SW, W, NW}

// details describes each point, indexed by the point itself. Index 0 is the
// invalid zero value and is left blank, which is what makes [Point.IsValid] a
// lookup rather than a list of cases.
var details = [...]struct {
	abbreviation string
	name         string
	// direction is the hexg direction index. See the package comment on why it
	// does not leave this file.
	direction int
}{
	NE: {"NE", "north-east", 1},
	E:  {"E", "east", 0},
	SE: {"SE", "south-east", 5},
	SW: {"SW", "south-west", 4},
	W:  {"W", "west", 3},
	NW: {"NW", "north-west", 2},
}

// Points returns the six compass points in the canonical order: NE, E, SE, SW,
// W, NW.
//
// The result is a fresh slice, so a caller that sorts or reverses it cannot
// change what the next caller sees.
func Points() []Point {
	return append([]Point(nil), points[:]...)
}

// IsValid reports whether p is one of the six compass points.
func (p Point) IsValid() bool {
	return p >= NE && p <= NW
}

// String returns the abbreviation: "NE", "E", "SE", "SW", "W", "NW".
//
// An invalid point prints as its number rather than panicking, because String
// is what a test failure and a log line call and neither is improved by
// crashing there.
func (p Point) String() string {
	if !p.IsValid() {
		return fmt.Sprintf("Point(%d)", int(p))
	}
	return details[p].abbreviation
}

// Name returns the long, hyphenated name: "north-east", "east", "south-east",
// "south-west", "west", "north-west".
func (p Point) Name() string {
	if !p.IsValid() {
		return fmt.Sprintf("Point(%d)", int(p))
	}
	return details[p].name
}

// Opposite returns the point 180 degrees away.
//
// Opposite of an invalid point is that same invalid point: the zero value
// stays the zero value rather than becoming a direction.
func (p Point) Opposite() Point {
	if !p.IsValid() {
		return p
	}
	// The order is clockwise, so the opposite of a point is three steps round
	// the compass from it.
	return points[(p.index()+3)%len(points)]
}

// index is p's position in the canonical order, 0 through 5.
func (p Point) index() int {
	return int(p) - int(NE)
}

// Vector returns the axial step from a hex to its neighbour in this direction.
//
// The step is planar, which is the one thing in this package that is. It is
// here for a caller doing its own hex arithmetic; a caller that wants the hex
// it lands on wants [Neighbor], which normalizes.
//
// Panics on an invalid point. A vector is arithmetic, and arithmetic on the
// zero value is a bug in the caller rather than something to return a zero
// step for and let travel silently stop.
func (p Point) Vector() hexg.Hex {
	if !p.IsValid() {
		panic(fmt.Sprintf("compass: %s has no vector", p))
	}
	return hexg.DirectionVector(details[p].direction)
}

// Neighbor returns the canonical hex one step from h toward p.
//
// Every function here that lands on a hex takes the cylinder and normalizes,
// because on a world that wraps the alternative is a coordinate that is right
// about where it is and wrong about what it is called. A step east from the
// meridian is one hex east; naming it a column past the meridian would seed a
// different PRNG stream for the same ground. See
// [github.com/mdhender/marajanda/internal/cylinder].
func Neighbor(c cylinder.Cylinder, h hexg.Hex, p Point) hexg.Hex {
	return c.Normalize(h.Add(p.Vector()))
}

// Neighbors returns the six canonical hexes around h, in the canonical compass
// order: NE, E, SE, SW, W, NW.
//
// This is the order a rule that visits neighbours in turn must walk them in,
// and it is why this exists rather than callers looping 0 to 5 over
// [github.com/maloquacious/hexg.Hex.Neighbor]: hexg's numbering starts due east
// and runs anticlockwise, which is neither our first point nor our direction
// of travel.
func Neighbors(c cylinder.Cylinder, h hexg.Hex) []hexg.Hex {
	around := make([]hexg.Hex, 0, len(points))
	for _, p := range points {
		around = append(around, Neighbor(c, h, p))
	}
	return around
}

// Steps returns the canonical hex reached by walking count steps from h toward
// p.
//
// A negative count walks backwards, which lands where the same count toward
// [Point.Opposite] would. A count of zero is h, normalized.
//
// Rows do not wrap, so a walk that runs off the top or bottom of the world
// returns a row outside it rather than a clamped one. Whether that is the ice
// or an error is a game rule, and this package does not hold game rules; the
// caller checks the row against the world it has.
func Steps(c cylinder.Cylinder, h hexg.Hex, p Point, count int) hexg.Hex {
	return c.Normalize(h.Add(p.Vector().Multiply(count)))
}

// names maps every accepted spelling to its point. Keys are lowercase with
// whitespace and hyphens already removed, which is the form [Parse] reduces
// its input to.
var names = map[string]Point{
	"ne": NE, "northeast": NE,
	"e": E, "east": E,
	"se": SE, "southeast": SE,
	"sw": SW, "southwest": SW,
	"w": W, "west": W,
	"nw": NW, "northwest": NW,
}

// noNeighbor holds the bearings a person will write that this grid has no
// neighbour in, with the two points that bracket each. A pointy-top hex has
// vertices at the top and bottom, so due north and due south point at a corner
// rather than through an edge.
var noNeighbor = map[string][2]Point{
	"n": {NW, NE}, "north": {NW, NE},
	"s": {SW, SE}, "south": {SW, SE},
}

// Parse reads a compass point from an order.
//
// It accepts the abbreviation, the hyphenated name and the unhyphenated name,
// in any case and with surrounding whitespace: "NE", "ne", "north-east",
// "northeast" and " North East " are all north-east.
//
// "north" and "south" fail with [ErrNoNeighbor] rather than [ErrUnknownPoint],
// and the message names the two points either side. They are the orders this
// grid most invites and least supports, and a player who wrote one deserves
// better than being told their word is not a direction.
func Parse(value string) (Point, error) {
	key := reduce(value)
	if key == "" {
		return 0, fmt.Errorf("%w: %q", ErrUnknownPoint, value)
	}
	if point, ok := names[key]; ok {
		return point, nil
	}
	if bracket, ok := noNeighbor[key]; ok {
		return 0, fmt.Errorf("%w: %q lies between %s and %s", ErrNoNeighbor, value, bracket[0].Name(), bracket[1].Name())
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownPoint, value)
}

// reduce folds a written direction to its lookup key: lowercase, with every
// space and hyphen removed.
//
// Removing rather than normalizing the separators is what makes one map serve
// "north-east", "northeast" and "north east" without three entries apiece.
func reduce(value string) string {
	var key strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch r {
		case ' ', '\t', '-', '_':
			continue
		}
		key.WriteRune(r)
	}
	return key.String()
}

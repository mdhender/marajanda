package world

import (
	"math"

	"github.com/maloquacious/hexg"
)

// Direction indexes the six neighbours of a hex. The indices are hexg's own
// direction order and the names are the compass points those indices land on
// once the grid is drawn: rows increase southward, and odd columns are pushed
// half a row south, so the same direction is a different (col,row) step
// depending on the column.
//
// TestDirectionsPointWhereTheyClaim checks each name against the pixel
// positions hexg computes, so a reordering upstream fails loudly instead of
// quietly rotating every map.
type Direction int

const (
	SE Direction = iota
	NE
	N
	NW
	SW
	S
)

// Directions is every direction in cyclic order, for callers walking all six.
var Directions = [6]Direction{SE, NE, N, NW, SW, S}

// Radius is the hex circumradius, in pixels, that donjon draws a world at:
// 200 columns of it make its maps exactly 4000 pixels wide. It is a default
// for renderers, not a property of the data.
const Radius = 40.0 / 3.0

// oddQ converts between offset and cube coordinates, where only the offset
// rule matters and the scale does not.
var oddQ = hexg.NewLayout(hexg.OddQ, hexg.Point{X: 1, Y: 1}, hexg.Point{})

// Layout is the hexg layout that draws this grid at circumradius r. The hex
// trigonometry is hexg's; what belongs here is only the frame donjon uses, so
// a render lines up hex for hex with the samples in docs/downloads: flat-top
// odd-q hexes, and an origin half a hex west so that column 0 is cut in half
// by the western edge and the image tiles east to west without a seam.
func (g Grid) Layout(r float64) hexg.Layout {
	return hexg.NewLayout(hexg.OddQ, hexg.Point{X: r, Y: r}, hexg.Point{X: r / 2, Y: 0})
}

// Len is the number of hexes in the grid, and the length of every populated
// layer.
func (g Grid) Len() int { return g.Cols * g.Rows }

// Index is the position of (col, row) in a layer. Column-major, matching
// Worldographer's own [col][row] tile array, so an export walks memory in
// order rather than striding through it.
func (g Grid) Index(col, row int) int { return col*g.Rows + row }

// ColRow is the inverse of Index.
func (g Grid) ColRow(i int) (col, row int) { return i / g.Rows, i % g.Rows }

// Contains reports whether (col, row) is a hex of this grid. It does not
// wrap: a wrapped column is a different column, and Normalize is what turns
// one into the other.
func (g Grid) Contains(col, row int) bool {
	return 0 <= col && col < g.Cols && 0 <= row && row < g.Rows
}

// Normalize brings a column that has run off the east or west edge back onto
// the grid. It reports false for a row outside the grid, since north of the
// north pole is nowhere, and for an off-grid column when the grid does not
// wrap.
func (g Grid) Normalize(col, row int) (int, int, bool) {
	if row < 0 || row >= g.Rows {
		return col, row, false
	}
	if g.WrapEastWest {
		col = ((col % g.Cols) + g.Cols) % g.Cols
	} else if col < 0 || col >= g.Cols {
		return col, row, false
	}
	return col, row, true
}

// Cube converts to the cube coordinate a Worldographer tile carries.
func (g Grid) Cube(col, row int) hexg.Hex {
	return oddQ.OffsetToCube(hexg.NewOffsetCoord(col, row))
}

// Neighbor returns the hex one step from (col, row) in the given direction.
// The step is hexg's; the wrap is this package's, because a cylinder is a
// property of the world and not of hex geometry. It reports false when the
// step leaves the map, which on a wrapping world happens only at the poles.
func (g Grid) Neighbor(col, row int, d Direction) (int, int, bool) {
	oc := oddQ.CubeToOffset(g.Cube(col, row).Neighbor(int(d)))
	return g.Normalize(oc.Col, oc.Row)
}

// pixel is the centre of a hex in the unit-radius frame, which is the frame
// the projection below is defined in.
func (g Grid) pixel(col, row int) hexg.Point {
	l := g.Layout(1)
	return l.HexToPixel(g.Cube(col, row))
}

// span is the size of the whole map in that same frame. It is measured rather
// than derived: one full turn east of column 0 is column Cols, and one full
// map south of row 0 is row Rows, so hexg's layout supplies both and this
// package does no hex trigonometry of its own. Longitude divides by the width;
// latitude uses poles instead, which is a narrower span than the height.
func (g Grid) span() (w, h float64) {
	origin := g.pixel(0, 0)
	return g.pixel(g.Cols, 0).X - origin.X, g.pixel(0, g.Rows).Y - origin.Y
}

// poles is where the two poles fall in pixel space: half a row beyond the
// northernmost and southernmost hex centres, so that every hex centre sits in
// the middle of the latitude band it covers.
//
// Measuring the southernmost centre rather than assuming it is what makes the
// result symmetric. Odd columns are pushed half a row south, so the last row's
// odd columns reach further than its even ones, and taking row Rows-1 at face
// value would leave the south pole half a band closer to the grid than the
// north -- which renders as one ice cap larger than the other.
func (g Grid) poles() (north, south float64) {
	half := (g.pixel(0, 1).Y - g.pixel(0, 0).Y) / 2
	last := 0
	if g.Cols > 1 {
		last = 1 // an odd column, and so the southern extreme of the last row
	}
	return g.pixel(0, 0).Y - half, g.pixel(last, g.Rows-1).Y + half
}

// LatLon is the hex's position on the globe in degrees: latitude +90 at the
// north pole down to -90 at the south, longitude -180 to +180 increasing
// eastward. The grid is read as an equirectangular projection of a sphere,
// which is what makes an east-west wrap and a spherical generator the same
// geometry rather than two that have to be reconciled.
//
// Latitude is measured between the poles as poles defines them, not between
// the edges of the image. A hex covers a band and sits in the middle of it, so
// the outermost hex centres are half a band short of +/-90 and the two hemi-
// spheres are mirror images. Longitude needs no such care: the columns close
// into a full circle, so the map's own width is the right divisor.
func (g Grid) LatLon(col, row int) (lat, lon float64) {
	p := g.pixel(col, row)
	w, _ := g.span()
	north, south := g.poles()
	return 90 - 180*(p.Y-north)/(south-north), 360*p.X/w - 180
}

// Unit is the hex's position as a unit vector on the sphere. A generator
// working in this space -- cutting the sphere, sampling noise over it --
// wraps east to west by construction, because there is no edge in it to wrap.
func (g Grid) Unit(col, row int) (x, y, z float64) {
	lat, lon := g.LatLon(col, row)
	rlat, rlon := lat*math.Pi/180, lon*math.Pi/180
	return math.Cos(rlat) * math.Cos(rlon), math.Cos(rlat) * math.Sin(rlon), math.Sin(rlat)
}

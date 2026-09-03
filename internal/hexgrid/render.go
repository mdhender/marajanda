package hexgrid

import (
	"image"
	"image/color"
	"math"
)

var sqrt3 = math.Sqrt(3)

// Background fills the corners of the image that fall outside the hexagon.
var Background = color.RGBA{R: 18, G: 18, B: 20, A: 255}

// ImageSize reports the pixel dimensions Render would produce for a hexagon of
// the given radius, so a caller can refuse an unreasonable request before
// allocating the buffer.
func ImageSize(radius int, size float64) (w, h int) {
	if size < 1 {
		size = 1
	}
	n := float64(radius)
	return int(math.Ceil(sqrt3 * size * (2*n + 1))), int(math.Ceil(size * (3*n + 2)))
}

// Render draws a hexagon-shaped map of the given radius as pointy-top
// hexagons, size pixels from a hex centre to a corner. at supplies the colour
// for a coordinate and reports whether that coordinate is part of the map;
// anything else gets Background.
//
// Rather than filling polygons, every pixel is converted back to a cube
// coordinate and coloured by that hex. Tile edges then land exactly where the
// hex boundaries are, with no seams or overdraw.
//
// Orientation is confined to this function. Cube coordinates carry none:
// flat-top is the same code with the layout matrix rotated by 30 degrees.
func Render(radius int, size float64, at func(Coord) (color.RGBA, bool)) *image.RGBA {
	if size < 1 {
		size = 1
	}
	w, h := ImageSize(radius, size)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2

	for y := range h {
		py := float64(y) + 0.5 - cy
		for x := range w {
			px := float64(x) + 0.5 - cx

			// Inverse of the pointy-top layout, then cube rounding.
			fq := (sqrt3/3*px - py/3) / size
			fr := (2.0 / 3.0 * py) / size

			c, ok := at(Round(fq, fr, -fq-fr))
			if !ok {
				c = Background
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// Round snaps fractional cube coordinates to the nearest hex, discarding
// whichever component rounded furthest so the invariant Q+R+S == 0 survives.
func Round(fq, fr, fs float64) Coord {
	q, r, s := math.Round(fq), math.Round(fr), math.Round(fs)
	dq, dr, ds := math.Abs(q-fq), math.Abs(r-fr), math.Abs(s-fs)
	switch {
	case dq > dr && dq > ds:
		q = -r - s
	case dr > ds:
		r = -q - s
	default:
		s = -q - r
	}
	return Coord{Q: int(q), R: int(r), S: int(s)}
}

// Center returns the pixel offset of a hex centre from the middle of the
// image, for callers that need to place something on top of the map.
func Center(c Coord, size float64) (x, y float64) {
	return size * sqrt3 * (float64(c.Q) + float64(c.R)/2), size * 1.5 * float64(c.R)
}

// Count returns the number of hexes in a hexagon of the given radius.
func Count(radius int) int { return 3*radius*radius + 3*radius + 1 }

// Hexes iterates every coordinate in a hexagon of the given radius.
func Hexes(radius int, yield func(Coord) bool) {
	for q := -radius; q <= radius; q++ {
		for r := max(-radius, -radius-q); r <= min(radius, radius-q); r++ {
			if !yield(Coord{Q: q, R: r, S: -q - r}) {
				return
			}
		}
	}
}

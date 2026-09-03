package hexfield

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/mdhender/marjanda/internal/hexgrid"
)

// Ramp is the default ASCII height ramp, lightest (lowest) first.
const Ramp = " .:-=+*#%@"

// ASCII renders the field as text, one line per row of constant R.
//
// Hexes are drawn two characters wide so that neighbours in a row abut with
// no gap, and successive rows shift by one character, which is what gives the
// staggered hex look. Passing a sea level in [0,1] draws everything at or
// below it as water.
func (f *Field) ASCII(ramp string, sea float64) string {
	if ramp == "" {
		ramp = Ramp
	}
	glyphs := []rune(ramp)
	lo, hi := f.Range()
	span := hi - lo

	n := f.Radius
	width := 4*n + 2
	var b strings.Builder

	for r := -n; r <= n; r++ {
		row := make([]rune, width)
		for i := range row {
			row[i] = ' '
		}
		for q := max(-n, -n-r); q <= min(n, n-r); q++ {
			v := f.At(hexgrid.Coord{Q: q, R: r, S: -q - r})
			if math.IsNaN(v) {
				continue
			}
			t := 0.0
			if span > 0 {
				t = (v - lo) / span
			}

			g := '~'
			if t > sea {
				i := int(float64(len(glyphs)) * (t - sea) / (1 - sea))
				g = glyphs[min(max(i, 0), len(glyphs)-1)]
			}

			// col 0 is the leftmost hex of the widest row, at q=-n, r=0.
			col := 2*q + r + 2*n
			row[col], row[col+1] = g, g
		}
		b.WriteString(strings.TrimRight(string(row), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// ImageSize reports the pixel dimensions Image would produce.
func (f *Field) ImageSize(size float64) (w, h int) {
	return hexgrid.ImageSize(f.Radius, size)
}

// Image renders the field, mapping each height through pal after normalizing
// the field's range to [0,1].
func (f *Field) Image(size float64, pal hexgrid.Palette) *image.RGBA {
	if pal == nil {
		pal = hexgrid.Grayscale
	}
	lo, hi := f.Range()
	span := hi - lo

	return hexgrid.Render(f.Radius, size, func(c hexgrid.Coord) (color.RGBA, bool) {
		v := f.At(c)
		if math.IsNaN(v) {
			return color.RGBA{}, false
		}
		t := 0.0
		if span > 0 {
			t = (v - lo) / span
		}
		return pal(t), true
	})
}

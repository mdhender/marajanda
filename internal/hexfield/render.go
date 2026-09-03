package hexfield

import (
	"image"
	"image/color"
	"math"
	"strings"
)

var sqrt3 = math.Sqrt(3)

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
			v := f.At(Coord{Q: q, R: r, S: -q - r})
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

// Palette maps a normalized height in [0,1] to a colour.
type Palette func(t float64) color.RGBA

// Grayscale is a plain black-to-white ramp.
func Grayscale(t float64) color.RGBA {
	v := uint8(math.Round(255 * clamp01(t)))
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

// Terrain returns a palette with a water ramp below sea and a land ramp
// above it, so moving sea level re-cuts the coastline without recolouring
// the land.
func Terrain(sea float64) Palette {
	water := []color.RGBA{
		{R: 12, G: 32, B: 74, A: 255},   // deep ocean
		{R: 24, G: 68, B: 126, A: 255},  // ocean
		{R: 58, G: 124, B: 178, A: 255}, // shallows
	}
	land := []color.RGBA{
		{R: 214, G: 202, B: 154, A: 255}, // beach
		{R: 122, G: 152, B: 84, A: 255},  // grass
		{R: 66, G: 112, B: 62, A: 255},   // forest
		{R: 126, G: 116, B: 82, A: 255},  // upland
		{R: 138, G: 132, B: 126, A: 255}, // rock
		{R: 246, G: 246, B: 250, A: 255}, // snow
	}
	sea = clamp01(sea)
	return func(t float64) color.RGBA {
		t = clamp01(t)
		if t <= sea {
			if sea == 0 {
				return water[0]
			}
			return rampAt(water, t/sea)
		}
		if sea == 1 {
			return land[len(land)-1]
		}
		return rampAt(land, (t-sea)/(1-sea))
	}
}

func rampAt(stops []color.RGBA, t float64) color.RGBA {
	t = clamp01(t) * float64(len(stops)-1)
	i := min(int(t), len(stops)-2)
	return lerp(stops[i], stops[i+1], t-float64(i))
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 255}
}

func clamp01(t float64) float64 { return min(max(t, 0), 1) }

// ImageSize reports the pixel dimensions Image would produce, so a caller can
// refuse an unreasonable request before allocating the buffer.
func (f *Field) ImageSize(size float64) (w, h int) {
	if size < 1 {
		size = 1
	}
	n := float64(f.Radius)
	return int(math.Ceil(sqrt3 * size * (2*n + 1))), int(math.Ceil(size * (3*n + 2)))
}

// Image renders the field as pointy-top hexagons, size pixels from a hex
// centre to a corner.
//
// Rather than filling polygons, every pixel is converted back to a cube
// coordinate and coloured by that hex. Tile edges then land exactly where the
// hex boundaries are, with no seams or overdraw. Orientation is confined to
// this function: flat-top is the same code with the layout matrix rotated by
// 30 degrees.
func (f *Field) Image(size float64, pal Palette) *image.RGBA {
	if size < 1 {
		size = 1
	}
	if pal == nil {
		pal = Grayscale
	}

	w, h := f.ImageSize(size)
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	lo, hi := f.Range()
	span := hi - lo
	bg := color.RGBA{R: 18, G: 18, B: 20, A: 255}
	cx, cy := float64(w)/2, float64(h)/2

	for y := range h {
		py := float64(y) + 0.5 - cy
		for x := range w {
			px := float64(x) + 0.5 - cx

			// Inverse of the pointy-top layout, then cube rounding.
			fq := (sqrt3/3*px - py/3) / size
			fr := (2.0 / 3.0 * py) / size
			c := cubeRound(fq, fr, -fq-fr)

			v := f.At(c)
			if math.IsNaN(v) {
				img.SetRGBA(x, y, bg)
				continue
			}
			t := 0.0
			if span > 0 {
				t = (v - lo) / span
			}
			img.SetRGBA(x, y, pal(t))
		}
	}
	return img
}

// cubeRound snaps fractional cube coordinates to the nearest hex, discarding
// whichever component rounded furthest so the invariant Q+R+S == 0 survives.
func cubeRound(fq, fr, fs float64) Coord {
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

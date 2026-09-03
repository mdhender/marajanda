package hexgrid

import (
	"image/color"
	"math"
)

// Palette maps a normalized value in [0,1] to a colour.
type Palette func(t float64) color.RGBA

// Grayscale is a plain black-to-white ramp.
func Grayscale(t float64) color.RGBA {
	v := uint8(math.Round(255 * Clamp01(t)))
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

// Terrain returns a palette with a water ramp below sea and a land ramp above
// it, so moving sea level re-cuts the coastline without recolouring the land.
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
	sea = Clamp01(sea)
	return func(t float64) color.RGBA {
		t = Clamp01(t)
		if t <= sea {
			if sea == 0 {
				return water[0]
			}
			return Ramp(water, t/sea)
		}
		if sea == 1 {
			return land[len(land)-1]
		}
		return Ramp(land, (t-sea)/(1-sea))
	}
}

// Ramp interpolates along a list of colour stops.
func Ramp(stops []color.RGBA, t float64) color.RGBA {
	t = Clamp01(t) * float64(len(stops)-1)
	i := min(int(t), len(stops)-2)
	return Lerp(stops[i], stops[i+1], t-float64(i))
}

// Lerp blends two colours.
func Lerp(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 255}
}

// Clamp01 restricts t to [0,1].
func Clamp01(t float64) float64 { return min(max(t, 0), 1) }

// HSV converts to RGB, for generators that need many visually distinct
// colours rather than a ramp. h is in turns, s and v in [0,1].
func HSV(h, s, v float64) color.RGBA {
	h = (h - math.Floor(h)) * 6
	i := math.Floor(h)
	f := h - i
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))

	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	u := func(x float64) uint8 { return uint8(math.Round(255 * Clamp01(x))) }
	return color.RGBA{R: u(r), G: u(g), B: u(b), A: 255}
}

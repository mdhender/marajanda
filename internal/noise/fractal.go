package noise

import "math"

// Shape is what an octave contributes before it is summed.
type Shape int

const (
	// FBM sums the noise as it comes: symmetric, roughly Gaussian, and the
	// only shape whose sum is unbiased. Rolling terrain.
	FBM Shape = iota

	// Ridged folds the noise at zero and turns it upside down, so the ridges
	// land on the zero set of the noise rather than on its peaks. The zero
	// set of a continuous field is a set of curves, which is why this is the
	// shape that produces mountain chains instead of blobs.
	//
	// It is also the one shape whose octaves are not independent: each is
	// scaled by the one above it, so detail only appears where there is
	// already relief. Summed flat, the folds of every octave land in
	// different places and the map comes out as speckle rather than chains --
	// which is the whole reason to reach for this shape.
	Ridged

	// Billow folds at zero without inverting, piling up rounded lumps. Dunes
	// and cloud decks.
	Billow
)

// Fractal is a sum of octaves of one noise function.
type Fractal struct {
	Kind Kind
	// Octaves is how many doublings to sum. Above about 8 the added octaves
	// are finer than a hex and only add per-hex hash.
	Octaves int
	// Frequency is the base frequency, in features per unit of input.
	Frequency float64
	// Lacunarity is the frequency ratio between successive octaves, and Gain
	// the amplitude ratio. Gain near 1/Lacunarity gives the scale-invariant
	// case; below it, smoother.
	Lacunarity float64
	Gain       float64
	Shape      Shape
	// Warp displaces the sample point by the noise itself, measured in base
	// wavelengths. It is what bends the field's features into something that
	// looks eroded rather than merely bumpy. 0 is off.
	Warp float64
}

// At evaluates the fractal at (x,y), in [-1,1] for every shape. Each octave
// contributes a bounded value and the sum is divided by the total amplitude,
// so changing the octave count or the gain re-shapes the field without
// rescaling it -- which is what stops the coastline moving when a knob that
// should only add detail is turned.
func (s *Source) At(p Fractal, x, y float64) float64 {
	octaves := min(max(p.Octaves, 1), maxOctaves)
	if p.Frequency <= 0 {
		p.Frequency = 1
	}
	if p.Lacunarity <= 0 {
		p.Lacunarity = 2
	}

	if p.Warp > 0 {
		x, y = s.warp(p, x, y)
	}

	// weight carries the previous octave's height into the next one, and is
	// only read by Ridged.
	sum, norm, amp, freq, weight := 0.0, 0.0, 1.0, p.Frequency, 1.0
	for k := range octaves {
		v := s.Eval(p.Kind, x*freq+s.off[k][0], y*freq+s.off[k][1])
		switch p.Shape {
		case Ridged:
			// Fold, invert, square. The fold is what puts a crease on the
			// zero set; squaring flattens the far end of the range, so the
			// crease stays sharp while the ground between ridges broadens
			// into a basin instead of sloping evenly down to it.
			v = 1 - math.Abs(v)
			v *= v * weight
			// ridgeFeedback of 2 lets an octave at half height still pass a
			// full-strength octave below it, so foothills keep some detail.
			weight = min(v*ridgeFeedback, 1)
			// Ridged octaves are in [0,1]; the sum is remapped below.
		case Billow:
			v = 2*math.Abs(v) - 1
		}
		sum += amp * v
		norm += amp
		amp *= p.Gain
		freq *= p.Lacunarity
	}
	if norm == 0 {
		return 0
	}
	if p.Shape == Ridged {
		return 2*(sum/norm) - 1
	}
	return sum / norm
}

// ridgeFeedback is how strongly one ridged octave gates the next. Higher
// values let detail spread off the ridges and back towards speckle; lower
// values leave everything but the main chains perfectly smooth.
const ridgeFeedback = 2.0

// warp offsets the sample point by two more channels of the same noise, taken
// from octave slots the sum itself does not use so the displacement is not
// correlated with the field it displaces.
func (s *Source) warp(p Fractal, x, y float64) (float64, float64) {
	const a, b = maxOctaves - 1, maxOctaves - 2
	fx, fy := x*p.Frequency, y*p.Frequency
	dx := s.Eval(p.Kind, fx+s.off[a][0], fy+s.off[a][1])
	dy := s.Eval(p.Kind, fx+s.off[b][0], fy+s.off[b][1])
	d := p.Warp / p.Frequency
	return x + d*dx, y + d*dy
}

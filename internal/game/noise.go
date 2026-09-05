// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"math"

	"github.com/mdhender/marajanda/internal/prng"
)

// noiseField is a coherent, deterministic scalar field over the plane.
//
// The field is gradient noise on an integer lattice. Each lattice point owns a
// unit gradient vector drawn from a stream addressed by the field number and
// the lattice coordinate, so a point's gradient never depends on the order
// points are visited: the field is a pure function of (seeds, field, x, y) and
// two runs over the same world agree everywhere. That is the same addressing
// discipline the rest of the engine uses, lifted from hexes to lattice points.
//
// A field memoizes its gradients. Neighbouring samples share lattice points, so
// without the cache a multi-octave sweep rehashes the same handful of points
// thousands of times. The cache is pure memoization and cannot change a result;
// it does mean a field is not safe for concurrent use, which suits generation
// running once, single-threaded, at game creation.
type noiseField struct {
	seeds     prng.Seeds
	field     prng.Key
	gradients map[[2]int][2]float64
}

// newNoiseField returns the field with the given number. Field numbers are
// arbitrary but must be distinct: two fields sharing a number are the same
// field, which would correlate elevation with moisture.
func newNoiseField(seeds prng.Seeds, field prng.Key) *noiseField {
	return &noiseField{seeds: seeds, field: field, gradients: make(map[[2]int][2]float64)}
}

// gradient returns the unit gradient owned by a lattice point.
func (n *noiseField) gradient(x, y int) (float64, float64) {
	if g, ok := n.gradients[[2]int{x, y}]; ok {
		return g[0], g[1]
	}
	// RollRange over a full turn quantizes the gradient direction, which is
	// plenty for terrain and keeps the draw an integer one, so the value is
	// identical on every platform. Floating-point draws would risk drifting
	// between architectures and take the whole map with them.
	const directions = 4096
	angle := 2 * math.Pi * float64(n.seeds.Roller(
		prng.TagWorld, n.field, prng.Key(x), prng.Key(y),
	).RollRange(0, directions-1)) / directions
	gx, gy := math.Cos(angle), math.Sin(angle)
	n.gradients[[2]int{x, y}] = [2]float64{gx, gy}
	return gx, gy
}

// at evaluates the field at a point, returning roughly -1..1.
func (n *noiseField) at(x, y float64) float64 {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)

	dot := func(cx, cy int) float64 {
		gx, gy := n.gradient(cx, cy)
		return gx*(x-float64(cx)) + gy*(y-float64(cy))
	}

	u, v := smoothstep(fx), smoothstep(fy)
	top := lerp(dot(x0, y0), dot(x0+1, y0), u)
	bottom := lerp(dot(x0, y0+1), dot(x0+1, y0+1), u)
	// Gradient noise on a unit lattice spans about ±0.707; scale to ±1 so the
	// octave weights below mean what they look like they mean.
	return lerp(top, bottom, v) * math.Sqrt2
}

// fbm sums octaves of the field, each at twice the frequency and half the
// amplitude of the last. Low octaves make continents, high octaves make the
// ragged detail along their edges.
func (n *noiseField) fbm(x, y float64, octaves int, frequency float64) float64 {
	sum, amplitude, total := 0.0, 1.0, 0.0
	for range octaves {
		sum += n.at(x*frequency, y*frequency) * amplitude
		total += amplitude
		frequency *= 2
		amplitude /= 2
	}
	if total == 0 {
		return 0
	}
	return sum / total
}

// smoothstep is the 6t^5-15t^4+10t^3 interpolant: zero first and second
// derivatives at both ends, so octaves meet without visible lattice creases.
func smoothstep(t float64) float64 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// normalize maps v from [lo, hi] onto [0, 1], clamped.
func normalize(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	return clamp((v-lo)/(hi-lo), 0, 1)
}

func clamp(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}

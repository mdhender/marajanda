// Package noise is lattice gradient noise on the plane: simplex, Perlin and
// value noise from one seeded permutation table, plus the fractal sum that
// turns any of them into terrain.
//
// It knows nothing about hexes. A caller hands it a point in whatever space
// it wants the noise to be isotropic in -- for a hex map that is pixel space,
// not cube coordinates -- and gets a height back.
package noise

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// Kind selects the noise function. All three are continuous, deterministic
// from the seed, and return values in [-1,1].
type Kind int

const (
	// Simplex evaluates on a triangular lattice: three corners per sample
	// instead of four, no axis-aligned features, and a radial kernel rather
	// than a separable one. It is the natural choice here because hex centres
	// already form a triangular lattice, so the noise lattice and the map
	// lattice have the same symmetry group.
	Simplex Kind = iota

	// Perlin is the classic square-lattice gradient noise. It is exactly zero
	// at every lattice point, which is visible as a faint grid at low
	// frequencies and is why octaves are offset from each other.
	Perlin

	// Value interpolates a random height per lattice point rather than a
	// gradient. Blockier and cheaper; kept mostly for comparison.
	Value
)

// Source holds one seeded permutation table. It is immutable after New and so
// safe to share between goroutines.
type Source struct {
	perm [256]uint8
	// off decorrelates the octaves of a fractal sum. Without it every octave
	// shares the origin, and Perlin's lattice zero at the origin stacks into
	// a visible dead spot at the centre of the map.
	off [maxOctaves][2]float64
}

// maxOctaves bounds the offset table, and so the octave count Fractal honours.
const maxOctaves = 16

// New returns a Source seeded from seed.
//
// The source is ChaCha8, matching the rest of the project: seeds here are
// frequently small and sequential, which PCG handles poorly at exactly the
// scale a "seed 1, seed 2, seed 3" sweep uses.
func New(seed uint64) *Source {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	rng := rand.New(rand.NewChaCha8(key))

	s := &Source{}
	for i := range s.perm {
		s.perm[i] = uint8(i)
	}
	rng.Shuffle(len(s.perm), func(i, j int) { s.perm[i], s.perm[j] = s.perm[j], s.perm[i] })
	for k := range s.off {
		s.off[k][0] = rng.Float64() * 256
		s.off[k][1] = rng.Float64() * 256
	}
	return s
}

// Eval evaluates one octave of the named noise function at (x,y), in [-1,1].
// Most callers want At, which sums octaves.
func (s *Source) Eval(k Kind, x, y float64) float64 {
	switch k {
	case Perlin:
		return s.perlin(x, y)
	case Value:
		return s.value(x, y)
	default:
		return s.simplex(x, y)
	}
}

// hash folds a lattice point to one byte.
func (s *Source) hash(i, j int) int {
	return int(s.perm[(int(s.perm[i&255])+j)&255])
}

// Skew factors between the square lattice the integer arithmetic runs on and
// the triangular lattice the noise actually lives on.
var (
	f2 = 0.5 * (math.Sqrt(3) - 1)
	g2 = (3 - math.Sqrt(3)) / 6
)

// grad2 is the twelve-gradient set of the reference implementation. The
// duplicates on the axes are not a mistake: they keep the table length clear
// of the eight-way symmetry of the four diagonals, which would otherwise
// correlate gradients with the lattice.
var grad2 = [12][2]float64{
	{1, 1}, {-1, 1}, {1, -1}, {-1, -1},
	{1, 0}, {-1, 0}, {1, 0}, {-1, 0},
	{0, 1}, {0, -1}, {0, 1}, {0, -1},
}

// simplex is 2D simplex noise.
//
// The point is skewed into a square lattice, the containing unit square is
// split into the two triangles that skew back to the simplex, and the three
// corners of that triangle contribute through a radial falloff. Falling off
// with distance rather than interpolating along the axes is what removes the
// directional bias Perlin has.
func (s *Source) simplex(x, y float64) float64 {
	skew := (x + y) * f2
	i, j := math.Floor(x+skew), math.Floor(y+skew)
	unskew := (i + j) * g2
	x0, y0 := x-(i-unskew), y-(j-unskew)

	// Which half of the square the point landed in decides the second corner.
	di, dj := 1, 0
	if x0 <= y0 {
		di, dj = 0, 1
	}
	x1, y1 := x0-float64(di)+g2, y0-float64(dj)+g2
	x2, y2 := x0-1+2*g2, y0-1+2*g2

	ii, jj := int(i), int(j)
	n := s.corner(ii, jj, x0, y0) +
		s.corner(ii+di, jj+dj, x1, y1) +
		s.corner(ii+1, jj+1, x2, y2)

	// 70 is the constant that brings the summed kernel to about [-1,1].
	return 70 * n
}

// corner is one simplex corner's contribution: a gradient dotted with the
// offset, windowed by (0.5 - r^2)^4 so it vanishes at the kernel radius.
func (s *Source) corner(i, j int, x, y float64) float64 {
	t := 0.5 - x*x - y*y
	if t <= 0 {
		return 0
	}
	g := grad2[s.hash(i, j)%len(grad2)]
	t *= t
	return t * t * (g[0]*x + g[1]*y)
}

// circle8 is eight unit gradients evenly spaced around the circle. Perlin
// dots these with the offset, so unit length keeps the bound tight and the
// even spacing keeps it isotropic.
var circle8 = func() [8][2]float64 {
	var g [8][2]float64
	for i := range g {
		a := float64(i) * math.Pi / 4
		g[i] = [2]float64{math.Cos(a), math.Sin(a)}
	}
	return g
}()

func (s *Source) perlin(x, y float64) float64 {
	fx, fy := math.Floor(x), math.Floor(y)
	i, j := int(fx), int(fy)
	dx, dy := x-fx, y-fy
	u, v := fade(dx), fade(dy)

	dot := func(di, dj int, ox, oy float64) float64 {
		g := circle8[s.hash(i+di, j+dj)&7]
		return g[0]*ox + g[1]*oy
	}
	a := lerp(dot(0, 0, dx, dy), dot(1, 0, dx-1, dy), u)
	b := lerp(dot(0, 1, dx, dy-1), dot(1, 1, dx-1, dy-1), u)

	// With unit gradients 2D Perlin peaks at sqrt(2)/2, so scaling by sqrt(2)
	// makes the range [-1,1].
	return math.Sqrt2 * lerp(a, b, v)
}

func (s *Source) value(x, y float64) float64 {
	fx, fy := math.Floor(x), math.Floor(y)
	i, j := int(fx), int(fy)
	u, v := fade(x-fx), fade(y-fy)

	at := func(di, dj int) float64 { return float64(s.hash(i+di, j+dj))/127.5 - 1 }
	a := lerp(at(0, 0), at(1, 0), u)
	b := lerp(at(0, 1), at(1, 1), u)
	return lerp(a, b, v)
}

// fade is Perlin's improved quintic, 6t^5 - 15t^4 + 10t^3. Its first and
// second derivatives vanish at 0 and 1, which is what keeps lattice lines out
// of the second derivative and so out of any shading built on the field.
func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(a, b, t float64) float64 { return a + t*(b-a) }

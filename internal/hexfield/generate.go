package hexfield

import (
	"encoding/binary"
	"math"
	"math/rand/v2"

	"github.com/mdhender/marjanda/internal/hexgrid"
)

// Stencil selects how a newly inserted point's base height is interpolated
// from the coarse lattice around it.
type Stencil int

const (
	// Loop is the four-point mask 3/8, 3/8, 1/8, 1/8 over the two edge
	// endpoints and the apexes of the two adjacent lattice triangles. It is
	// the edge mask from Loop subdivision, and the hex analogue of
	// diamond-square averaging four corners rather than two. The wider
	// support is Miller's fix for creasing and should be the default.
	Loop Stencil = iota

	// Midpoint averages only the two edge endpoints. It is the naive choice
	// and creases badly; kept so the difference can be seen.
	Midpoint
)

func (s Stencil) String() string {
	if s == Midpoint {
		return "midpoint"
	}
	return "loop"
}

// Params configures Generate.
type Params struct {
	// Levels is the number of subdivision passes, giving a hexagon of radius
	// 2**Levels.
	Levels int

	// Hurst is the Hurst exponent H in (0,1], the roughness knob: 1.0 is
	// smooth rolling hills, ~0.5 is Brownian and looks natural, and values
	// near 0 are violently jagged. The displacement amplitude is multiplied
	// by 2**-H after each level, which is what makes the result fractional
	// Brownian motion rather than white noise or a flat plane.
	Hurst float64

	// Roughness is the displacement amplitude at the coarsest level.
	Roughness float64

	// Seed selects the terrain.
	Seed uint64

	// Stencil selects the interpolation mask.
	Stencil Stencil

	// Relax applies the Loop vertex mask to points that already exist,
	// repositioning them towards the average of their lattice neighbours
	// before the next round of midpoints is inserted.
	//
	// This is the half of Loop subdivision that a naive port leaves out, and
	// it is the effective fix for creasing. Plain subdivision writes a point
	// once and never revisits it, so the handful of values fixed while the
	// amplitude was largest act as frozen scaffolding that shows through the
	// finished field as ridges along the lattice axes. Repositioning
	// dissolves that scaffolding. The cost is that the seven seed values
	// drift, so Relax trades away some of the exact control that made
	// subdivision attractive over summed noise.
	Relax bool

	// SRA enables successive random additions (Voss): every point on the
	// lattice is perturbed at every level, not only the newly inserted ones.
	//
	// On its own this makes creasing worse rather than better, because a
	// point present at coarse levels accumulates independent noise from every
	// level while its later-inserted neighbours only receive the smaller,
	// finer noise -- so old points spike relative to the surface around them.
	// It is useful combined with Relax, which smooths those spikes back down.
	// Relax alone over-corrects, leaving old points flatter than the surface
	// around them; the two together cancel, and that pairing is the default.
	SRA bool

	// Island seeds the centre high and the rim low instead of seeding all
	// seven coarse points randomly.
	Island bool
}

// Generate builds a height field. Heights are not normalized; call
// Field.Normalize if a [0,1] range is wanted.
func Generate(p Params) *Field {
	p.Levels = max(p.Levels, 1)
	if p.Hurst <= 0 {
		p.Hurst = 0.8
	}
	if p.Roughness == 0 {
		p.Roughness = 1
	}

	f := New(p.Levels)
	rng := newRand(p.Seed)

	// Seed the coarsest lattice: the origin and the six points at radius N.
	// These seven values are the whole of the caller's control over the
	// large-scale shape, and pinning them is the reason to prefer recursive
	// subdivision over summed noise octaves in the first place.
	seed := func(c hexgrid.Coord, v float64) { f.Set(c, v) }
	if p.Island {
		seed(hexgrid.Origin, 1)
		for _, d := range hexgrid.Directions {
			seed(d.Scale(f.Radius), -1)
		}
	} else {
		seed(hexgrid.Origin, signed(rng))
		for _, d := range hexgrid.Directions {
			seed(d.Scale(f.Radius), signed(rng))
		}
	}

	amp := p.Roughness
	for k := p.Levels; k >= 1; k-- {
		step := 1 << k
		half := step >> 1

		// Reposition the points that already exist. This must happen before
		// the midpoints are inserted, so that they interpolate from relaxed
		// values, and it must be simultaneous rather than in place, or each
		// point would be smoothed against neighbours already smoothed this
		// pass.
		if p.Relax {
			f.relax(step)
		}

		// The subdivision step: insert the midpoint of every edge of the
		// level-k lattice. There is only one such step per level, because
		// unlike a square lattice the triangular lattice refines uniformly.
		f.eachLattice(step, func(a hexgrid.Coord) {
			for i := range hexgrid.Forward {
				d := hexgrid.Directions[i]

				b := a.Add(d.Scale(step))
				if !f.Contains(b) {
					continue
				}
				m := a.Add(d.Scale(half))

				// The apexes of the two lattice triangles sharing edge (a,b)
				// lie along the directions adjacent to d in the six-cycle.
				c := a.Add(hexgrid.Directions[(i+1)%6].Scale(step))
				e := a.Add(hexgrid.Directions[(i+5)%6].Scale(step))

				// With successive random additions the displacement is
				// applied to the whole lattice afterwards, so this step
				// interpolates cleanly and draws nothing.
				h := f.interpolate(a, b, c, e, p.Stencil)
				if !p.SRA {
					h += amp * signed(rng)
				}
				f.Set(m, h)
			}
		})

		if p.SRA {
			f.eachLattice(half, func(c hexgrid.Coord) {
				f.h[f.index(c)] += amp * signed(rng)
			})
		}

		// Shrink the displacement geometrically. Without this the result is
		// white noise; shrink it too fast and the result is a flat plane.
		amp *= math.Exp2(-p.Hurst)
	}

	return f
}

// relax applies the Loop vertex mask to every point of the level-k lattice,
// where step is 2**k. For a regular vertex with six neighbours the mask is
// 5/8 of the point plus 3/8 of the neighbourhood average; rim points simply
// average over the neighbours they have.
func (f *Field) relax(step int) {
	type update struct {
		c hexgrid.Coord
		v float64
	}
	var updates []update

	f.eachLattice(step, func(c hexgrid.Coord) {
		var sum float64
		var n int
		for _, d := range hexgrid.Directions {
			if o := c.Add(d.Scale(step)); f.Has(o) {
				sum += f.At(o)
				n++
			}
		}
		if n > 0 {
			updates = append(updates, update{c, 0.625*f.At(c) + 0.375*sum/float64(n)})
		}
	})

	for _, u := range updates {
		f.Set(u.c, u.v)
	}
}

// interpolate returns the base height for the midpoint of edge (a,b), where c
// and e are the apexes of the two adjacent lattice triangles.
//
// Edges on the rim of the hexagon have only one adjacent triangle, so one
// apex is missing -- the same situation as the three-neighbour edge case in
// diamond-square. The available weights are renormalized rather than
// substituting a zero, which would drag the rim towards the origin height.
func (f *Field) interpolate(a, b, c, e hexgrid.Coord, s Stencil) float64 {
	ha, hb := f.At(a), f.At(b)
	if s == Midpoint {
		return (ha + hb) / 2
	}

	sum := 0.375*ha + 0.375*hb
	weight := 0.75
	if f.Has(c) {
		sum += 0.125 * f.At(c)
		weight += 0.125
	}
	if f.Has(e) {
		sum += 0.125 * f.At(e)
		weight += 0.125
	}
	return sum / weight
}

// newRand returns the displacement source for a seed.
//
// Displacements are drawn as a stream in the order the lattice is walked.
// That order is fixed, so a seed reproduces a field exactly.
//
// An earlier version hashed (seed, coordinate, level) so any region could be
// regenerated independently of traversal order. That meant hand-rolling a
// mixer; this package uses math/rand/v2 sources only. ChaCha8 rather than PCG
// because seeds here are frequently small and sequential, and ChaCha8 has no
// short-seed correlation to reason about.
func newRand(seed uint64) *rand.Rand {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	return rand.New(rand.NewChaCha8(key))
}

// signed returns a displacement in [-1,1).
func signed(rng *rand.Rand) float64 { return rng.Float64()*2 - 1 }

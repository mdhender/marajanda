package generators

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/mdhender/marajanda/internal/hexgrid"
	"github.com/mdhender/marajanda/internal/mapgen"
)

func init() { mapgen.Register(tectonic{}) }

// tectonic is the generator where regions interact.
//
// Subdivision has a height field and no regions; voronoi has regions that are
// coloured independently, so a border means nothing beyond "a different site
// won here". This one partitions the map into plates the same way voronoi
// does, then gives each plate a drift vector, and the terrain falls out of
// what happens where two of them meet: mountains and trenches where they
// converge, rifts and ridges where they part, fault valleys where they slide.
//
// The height field is therefore seeded almost entirely at the boundaries. Away
// from them a plate is flat at its base height, and everything interesting is
// spread inland by the falloff.
type tectonic struct{}

func (tectonic) Name() string  { return "tectonic" }
func (tectonic) Title() string { return "Plate tectonics" }

func (tectonic) Description() string {
	return "Splits the map into drifting plates and reads the terrain off their " +
		"boundaries: mountains and trenches where plates converge, rifts and ocean " +
		"ridges where they part, fault valleys where they grind past each other."
}

func (tectonic) Params() []mapgen.Param {
	return []mapgen.Param{
		{
			Name: "seed", Label: "Seed", Kind: mapgen.KindSeed,
			Help: "Reloading the page offers a new one.",
		},
		{
			Name: "radius", Label: "Map radius (hexes)", Kind: mapgen.KindInt,
			Default: 48, Min: 2, Max: 200, Step: 1,
		},
		{
			Name: "plates", Label: "Plates", Kind: mapgen.KindInt,
			Default: 12, Min: 2, Max: 80, Step: 1,
			Help: "Upper bound. Plates that relaxation collapses to a speck are folded into a neighbour.",
		},
		{
			Name: "oceanic", Label: "Oceanic fraction", Kind: mapgen.KindFloat,
			Default: 0.55, Min: 0.0, Max: 1.0, Step: 0.05,
			Help: "Chance a plate is oceanic. Oceanic crust sits low and subducts; continental rides over it.",
		},
		{
			Name: "drift", Label: "Drift strength", Kind: mapgen.KindFloat,
			Default: 1.0, Min: 0.1, Max: 2.0, Step: 0.1,
			Help: "How fast plates move, and so how much relief their boundaries raise.",
		},
		{
			Name: "falloff", Label: "Boundary falloff (hexes)", Kind: mapgen.KindFloat,
			Default: 3.0, Min: 0.5, Max: 12.0, Step: 0.5,
			Help: "Distance over which boundary relief decays inland. Small values give one-hex walls.",
		},
		{
			Name: "roughness", Label: "Roughness", Kind: mapgen.KindFloat,
			Default: 0.5, Min: 0.0, Max: 1.0, Step: 0.05,
			Help: "Noise on the uplift, on the falloff distance and on plate interiors. At 0 ranges are smooth even cones.",
		},
		{
			Name: "sea", Label: "Sea level", Kind: mapgen.KindFloat,
			Default: 0.45, Min: 0.0, Max: 0.95, Step: 0.05,
			Help: "Absolute here, not a percentile: below the continental base height, above the oceanic one.",
		},
		{
			Name: "palette", Label: "Palette", Kind: mapgen.KindChoice,
			Default: "terrain", Choices: []string{"terrain", "gray", "plates"},
			Help: "Plates shows the partition and colours each boundary by what it is doing.",
		},
		{
			Name: "size", Label: "Hex size (px)", Kind: mapgen.KindFloat,
			Default: 8.0, Min: 1, Max: 40, Step: 1,
		},
	}
}

// Base heights the two kinds of crust float at, before any boundary relief.
// They straddle the default sea level, so continents are dry and ocean basins
// are wet without the sea level having to be tuned per map.
const (
	oceanBase = 0.26
	contBase  = 0.62
)

// How much relief one unit of relative plate motion raises or drops. These are
// the whole model: everything else is partitioning, falloff and noise.
const (
	upliftCollision = 0.42 // continent meets continent: the big ranges
	upliftMargin    = 0.36 // continental side of a subduction zone: coastal arc
	upliftIsland    = 0.30 // overriding oceanic plate: an island arc
	dropTrench      = 0.32 // subducting oceanic side: a deep trench
	upliftRidge     = 0.10 // ocean floor spreading: a mid-ocean ridge
	dropRift        = 0.26 // continental crust pulling apart: a rift valley
	dropFault       = 0.07 // transform: little elevation change, a visible line
)

// shearDominance is how far shear must outrun convergence before a margin is
// called transform rather than a weak convergent or divergent one. At 1 the
// larger component simply wins, which makes half of all margins transform and
// leaves a map with very little relief; requiring twice the shear brings that
// down to about a third, which is nearer the share Earth has.
const shearDominance = 0.5

// Boundary classes, also used to colour the "plates" palette.
const (
	classNone = iota
	classConvergent
	classDivergent
	classTransform
)

type plate struct {
	vx, vy float64 // drift, in pixel space at unit hex size
	cx, cy float64 // centroid, likewise, which fixes the normal of every margin
	ocean  bool
}

func (tectonic) Generate(v mapgen.Values) (image.Image, error) {
	radius := v.Int("radius")
	size := v.Float("size")
	if w, h := hexgrid.ImageSize(radius, size); w*h > maxPixels {
		return nil, fmt.Errorf("image would be %d×%d pixels, over the %d megapixel cap: "+
			"lower the hex size or the radius", w, h, maxPixels>>20)
	}

	rng := newRand(v.Uint64("seed"))

	// Euclidean, so plate margins run as straight lines rather than following
	// hex steps; three relaxation passes so plates are territory-sized rather
	// than splinters. Neither is worth a knob.
	all, owner, count := partition(radius, v.Int("plates"), 3, true, rng)

	// Everything below is indexed by position in all, which is in hexgrid.Hexes
	// order. Ranging over owner instead would make the map depend on Go's map
	// iteration order and stop the seed reproducing anything.
	idx := make(map[hexgrid.Coord]int, len(all))
	for i, c := range all {
		idx[c] = i
	}

	plates := make([]plate, count)
	oceanic, drift := v.Float("oceanic"), v.Float("drift")
	for i := range plates {
		th := rng.Float64() * 2 * math.Pi
		m := drift * (0.4 + 0.6*rng.Float64())
		plates[i] = plate{
			vx:    m * math.Cos(th),
			vy:    m * math.Sin(th),
			ocean: rng.Float64() < oceanic,
		}
	}
	centroids(plates, all, owner)

	rough := v.Float("roughness")
	bumps := noiseField(all, idx, rng, 16) // coarse: moves coastlines, varies uplift
	jitter := noiseField(all, idx, rng, 4) // fine: ragged falloff distances
	warpQ := noiseField(all, idx, rng, 12)
	warpR := noiseField(all, idx, rng, 12)

	disp, class := boundaryRelief(all, owner, plates)
	for i := range disp {
		disp[i] *= 1 + rough*bumps[i]
	}

	// Crust type steps at the plate boundary, and with sea level between the
	// two base heights that step is the coastline. Read straight off the
	// partition, every continent would be its Voronoi cell, polygon edges and
	// all. So the lookup is domain-warped: each hex takes the crust of a hex a
	// few steps away, in a direction that drifts smoothly across the map. The
	// margins themselves stay where they are -- only the shoreline wanders off
	// them, into bays and peninsulas.
	//
	// The warp is in hexes and scaled to the plates, not the map: an excursion
	// that carves bays out of a large plate would dissolve a small one, and the
	// plate count is what decides which of those a map has.
	warp := min(6.0, float64(radius)/(3*math.Sqrt(float64(count))))
	height := make([]float64, len(all))
	for i, c := range all {
		fq := float64(c.Q) + warp*warpQ[i]
		fr := float64(c.R) + warp*warpR[i]
		o, ok := owner[hexgrid.Round(fq, fr, -fq-fr)]
		if !ok {
			o = owner[c] // warped off the map: keep our own crust
		}
		if plates[o].ocean {
			height[i] = oceanBase
		} else {
			height[i] = contBase
		}
	}
	// A continental shelf, so the step is a slope rather than a cliff.
	relaxField(height, all, idx, 0.5, 4)

	spread(height, disp, class, all, idx, jitter, v.Float("falloff"), rough)

	// Plate interiors would otherwise be perfectly flat, and coastlines would
	// still run straight. Two octaves: the coarse field moves the shoreline,
	// the fine one roughens it.
	for i := range height {
		height[i] += (0.05 + 0.07*rough) * (0.7*bumps[i] + 0.3*jitter[i])
	}

	// Two light passes, enough to turn the one-hex step at a boundary into a
	// slope without flattening the ranges the falloff just built.
	relaxField(height, all, idx, 0.4, 2)

	paint := paletteFor(v, plates, owner, class)
	return hexgrid.Render(radius, size, func(c hexgrid.Coord) (color.RGBA, bool) {
		i, ok := idx[c]
		if !ok {
			return color.RGBA{}, false
		}
		return paint(i, c, hexgrid.Clamp01(height[i])), true
	}), nil
}

// boundaryRelief classifies every hex that touches another plate and returns
// the elevation change it contributes, plus what kind of boundary it is.
//
// Classification is per neighbouring plate, from the relative motion of the two
// resolved along the margin's normal: motion into the neighbour is convergence,
// away is divergence, and motion across the normal is shear. Convergence or
// divergence names the margin unless shear clearly dominates, in which case it
// is transform. A hex on a triple junction borders more than one plate, so its
// contributions are averaged and the class it records is the one that moved the
// ground furthest.
func boundaryRelief(all []hexgrid.Coord, owner map[hexgrid.Coord]int, plates []plate) (disp []float64, class []uint8) {
	disp = make([]float64, len(all))
	class = make([]uint8, len(all))

	for i, c := range all {
		o := owner[c]
		a := plates[o]

		sum, n, strongest := 0.0, 0, 0.0
		for _, d := range hexgrid.Directions {
			nc := c.Add(d)
			other, inside := owner[nc]
			if !inside || other == o {
				continue
			}
			b := plates[other]

			// The margin's normal, not the direction of this particular hex
			// step. The partition is Voronoi in pixel space, so the boundary
			// between two plates is perpendicular to the line joining them and
			// the normal is the same all along it. Using the hex step instead
			// makes the classification flicker between convergent and
			// transform from one hex to the next, which the six directions
			// alone are too coarse to resolve.
			dx, dy := b.cx-a.cx, b.cy-a.cy
			if l := math.Hypot(dx, dy); l > 0 {
				dx, dy = dx/l, dy/l
			} else {
				dx, dy = 0, 0
			}

			// Motion of this plate relative to the neighbour, split into the
			// component along that vector and the component across it.
			rx, ry := a.vx-b.vx, a.vy-b.vy
			conv := rx*dx + ry*dy
			shear := math.Abs(rx*-dy + ry*dx)

			var e float64
			var k uint8
			switch {
			case math.Abs(conv) <= shearDominance*shear:
				// Sliding past: a fault line rather than a mountain chain.
				e, k = -dropFault*shear, classTransform
			case conv > 0:
				k = classConvergent
				switch {
				case !a.ocean && !b.ocean:
					e = upliftCollision * conv
				case !a.ocean:
					e = upliftMargin * conv // the neighbour subducts under us
				case !b.ocean:
					e = -dropTrench * conv // we subduct under the neighbour
				case o < other:
					// Two oceanic plates: the older, denser one goes under.
					// There is no age here, so the lower index overrides --
					// arbitrary, but deterministic, which is what matters.
					e = upliftIsland * conv
				default:
					e = -dropTrench * conv
				}
			default:
				k = classDivergent
				if a.ocean && b.ocean {
					e = upliftRidge * -conv
				} else {
					e = -dropRift * -conv
				}
			}

			sum += e
			n++
			if math.Abs(e) > strongest {
				strongest, class[i] = math.Abs(e), k
			}
		}
		if n > 0 {
			disp[i] = sum / float64(n)
		}
	}
	return disp, class
}

// centroids fills in each plate's centre of mass, which fixes the normal of
// every margin it takes part in. Summing in all order rather than ranging over
// owner keeps the floating-point result reproducible.
func centroids(plates []plate, all []hexgrid.Coord, owner map[hexgrid.Coord]int) {
	n := make([]float64, len(plates))
	for _, c := range all {
		o := owner[c]
		x, y := hexgrid.Center(c, 1)
		plates[o].cx += x
		plates[o].cy += y
		n[o]++
	}
	for i := range plates {
		if n[i] > 0 {
			plates[i].cx /= n[i]
			plates[i].cy /= n[i]
		}
	}
}

// spread carries the boundary relief inland.
//
// A breadth-first sweep outwards from every boundary hex at once gives each
// hex its distance to the nearest boundary and which boundary that is; the
// relief there is then applied with an exponential decay over that distance,
// so a range gets foothills rather than being a wall one hex thick. The
// distance is perturbed per hex, because a clean exponential cone reads as
// obviously generated.
//
// The queue is seeded in all order and neighbours are walked in Directions
// order, so ties resolve the same way on every run.
func spread(height, disp []float64, class []uint8, all []hexgrid.Coord, idx map[hexgrid.Coord]int, jitter []float64, falloff, rough float64) {
	dist := make([]int, len(all))
	src := make([]int, len(all))
	for i := range src {
		dist[i], src[i] = -1, -1
	}

	queue := make([]int, 0, len(all))
	for i := range all {
		if class[i] != classNone {
			dist[i], src[i] = 0, i
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		i := queue[head]
		for _, d := range hexgrid.Directions {
			j, ok := idx[all[i].Add(d)]
			if !ok || dist[j] >= 0 {
				continue
			}
			dist[j], src[j] = dist[i]+1, src[i]
			queue = append(queue, j)
		}
	}

	for i := range height {
		if src[i] < 0 {
			continue // no boundary anywhere: a single plate owns the map
		}
		d := float64(dist[i]) * (1 + 0.6*rough*jitter[i])
		height[i] += disp[src[i]] * math.Exp(-d/falloff)
	}
}

// paletteFor returns the per-hex colouring for the selected palette.
func paletteFor(v mapgen.Values, plates []plate, owner map[hexgrid.Coord]int, class []uint8) func(i int, c hexgrid.Coord, h float64) color.RGBA {
	switch v.String("palette") {
	case "gray":
		return func(_ int, _ hexgrid.Coord, h float64) color.RGBA { return hexgrid.Grayscale(h) }

	case "plates":
		// The diagnostic view: what the model actually decided, before the
		// height field hides it.
		boundary := [...]color.RGBA{
			classConvergent: {R: 208, G: 72, B: 56, A: 255},
			classDivergent:  {R: 72, G: 132, B: 214, A: 255},
			classTransform:  {R: 226, G: 196, B: 84, A: 255},
		}
		fill := make([]color.RGBA, len(plates))
		for i, p := range plates {
			// Golden-ratio hue stepping spreads hues evenly however many
			// plates there are; oceanic ones are darkened so crust type reads
			// at a glance.
			if p.ocean {
				fill[i] = hexgrid.HSV(float64(i)*0.6180339887, 0.45, 0.5)
			} else {
				fill[i] = hexgrid.HSV(float64(i)*0.6180339887, 0.35, 0.88)
			}
		}
		return func(i int, c hexgrid.Coord, _ float64) color.RGBA {
			if class[i] != classNone {
				return boundary[class[i]]
			}
			return fill[owner[c]]
		}

	default:
		pal := hexgrid.Terrain(v.Float("sea"))
		return func(_ int, _ hexgrid.Coord, h float64) color.RGBA { return pal(h) }
	}
}

// noiseField is value noise on the hex grid: one random value per hex, then
// smoothed into blobs a few hexes across. passes sets how coarse those blobs
// are. The result has unit RMS, so a caller multiplies by the amplitude it
// wants a typical hex to move.
func noiseField(all []hexgrid.Coord, idx map[hexgrid.Coord]int, rng *rand.Rand, passes int) []float64 {
	f := make([]float64, len(all))
	for i := range f {
		f[i] = rng.Float64()*2 - 1
	}
	relaxField(f, all, idx, 0.6, passes)

	// Scale to unit RMS rather than to the largest value. Smoothing pulls
	// everything towards the mean, and dividing by a lone outlying peak leaves
	// the typical hex with a value far too small to matter.
	sum := 0.0
	for _, x := range f {
		sum += x * x
	}
	if rms := math.Sqrt(sum / float64(len(f))); rms > 0 {
		for i := range f {
			f[i] /= rms
		}
	}
	return f
}

// relaxField blends every hex towards the mean of its neighbours, w of the way,
// passes times. Hexes on the rim average over the neighbours they have.
func relaxField(vals []float64, all []hexgrid.Coord, idx map[hexgrid.Coord]int, w float64, passes int) {
	next := make([]float64, len(vals))
	for range passes {
		for i, c := range all {
			sum, n := 0.0, 0
			for _, d := range hexgrid.Directions {
				if j, ok := idx[c.Add(d)]; ok {
					sum += vals[j]
					n++
				}
			}
			if n == 0 {
				next[i] = vals[i]
				continue
			}
			next[i] = (1-w)*vals[i] + w*sum/float64(n)
		}
		copy(vals, next)
	}
}

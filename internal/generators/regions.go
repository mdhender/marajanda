package generators

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/mdhender/marajanda/internal/hexgrid"
)

// Partitioning a hexagon into regions is shared between the generators that
// need territory rather than a height field: voronoi colours the regions
// directly, tectonic treats them as plates and looks at what happens where
// two of them meet.

// partition carves a hexagon of the given radius into at most sites regions,
// each hex going to its nearest site, and returns every coordinate in the map
// alongside the region that owns it.
//
// all is in hexgrid.Hexes order, which is the order callers should walk when
// the result has to be reproducible: owner is a map, and Go randomises map
// iteration.
func partition(radius, sites, lloyd int, euclidean bool, rng *rand.Rand) (all []hexgrid.Coord, owner map[hexgrid.Coord]int, regions int) {
	cells := hexgrid.Count(radius)
	sites = min(sites, cells)

	// Scatter sites by reservoir-sampling distinct hexes, so no two regions
	// start on the same cell however small the map is.
	all = make([]hexgrid.Coord, 0, cells)
	hexgrid.Hexes(radius, func(c hexgrid.Coord) bool {
		all = append(all, c)
		return true
	})
	shuffled := slices.Clone(all)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	// Clone: relaxation writes back into seeds, and a subslice of shuffled
	// would share its backing array with all, overwriting the first `sites`
	// hexes of the map itself. Those hexes then never get an owner and render
	// as background.
	seeds := slices.Clone(shuffled[:sites])

	owner = map[hexgrid.Coord]int{}
	assign := func() {
		clear(owner)
		for _, c := range all {
			owner[c] = nearest(c, seeds, euclidean)
		}
	}
	assign()

	// Lloyd relaxation: move each site to the centroid of the region it owns
	// and reassign. A couple of passes turn ragged splinters into something
	// closer to equal-area territory.
	for range lloyd {
		sums := make([]struct{ q, r, n float64 }, len(seeds))
		for c, o := range owner {
			sums[o].q += float64(c.Q)
			sums[o].r += float64(c.R)
			sums[o].n++
		}
		for i, s := range sums {
			if s.n == 0 {
				continue // an empty region keeps its site rather than jumping
			}
			q, r := s.q/s.n, s.r/s.n
			if c := hexgrid.Round(q, r, -q-r); c.Length() <= radius {
				seeds[i] = c
			}
		}
		assign()
	}

	mergeSlivers(owner, len(seeds))
	return all, owner, len(seeds)
}

// minRegion is the smallest area a region may keep.
//
// Lloyd relaxation can push a site inside a neighbour's territory, where it
// ends up owning only the single hex it stands on: every other cell nearby is
// closer to the site that surrounded it. Those specks read as dirt rather than
// territory, especially with borders drawn, so they are folded away.
const minRegion = 3

// mergeSlivers folds regions below minRegion into whichever neighbour borders
// them most, leaving fewer regions than were asked for.
//
// Everything here is indexed by region rather than ranged over a map, because
// Go randomises map iteration order and the result has to be reproducible from
// the seed. Ties go to the lowest region index for the same reason.
func mergeSlivers(owner map[hexgrid.Coord]int, regions int) {
	// A merge can leave its target below the threshold in turn, so repeat;
	// this settles in one or two passes.
	for range 4 {
		cells := make([][]hexgrid.Coord, regions)
		for c, o := range owner {
			cells[o] = append(cells[o], c)
		}

		merged := false
		for o := range regions {
			cs := cells[o]
			if len(cs) == 0 || len(cs) >= minRegion {
				continue
			}

			tally := make([]int, regions)
			for _, c := range cs {
				for _, d := range hexgrid.Directions {
					if n, ok := owner[c.Add(d)]; ok && n != o {
						tally[n]++
					}
				}
			}
			best, bestN := -1, 0
			for cand, n := range tally {
				if n > bestN {
					best, bestN = cand, n
				}
			}
			if best < 0 {
				continue // fully enclosed by nothing: the whole map is one region
			}

			for _, c := range cs {
				owner[c] = best
			}
			merged = true
		}
		if !merged {
			return
		}
	}
}

// nearest returns the index of the site closest to c.
func nearest(c hexgrid.Coord, seeds []hexgrid.Coord, euclidean bool) int {
	best, bestD := 0, math.Inf(1)
	for i, s := range seeds {
		var d float64
		if euclidean {
			// Compare in pixel space at unit size, where the hex lattice is
			// not square, so "straight" borders really are straight.
			x, y := hexgrid.Center(c, 1)
			sx, sy := hexgrid.Center(s, 1)
			d = (x-sx)*(x-sx) + (y-sy)*(y-sy)
		} else {
			d = float64(c.Distance(s))
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// newRand mirrors hexfield's source choice: math/rand/v2 only, and ChaCha8
// rather than PCG because seeds are frequently small and sequential.
func newRand(seed uint64) *rand.Rand {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	return rand.New(rand.NewChaCha8(key))
}

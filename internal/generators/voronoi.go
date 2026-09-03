package generators

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/mdhender/marjanda/internal/hexgrid"
	"github.com/mdhender/marjanda/internal/mapgen"
)

func init() { mapgen.Register(voronoi{}) }

// voronoi carves the map into regions around scattered sites, each hex taking
// the colour of its nearest site. Where subdivision produces a continuous
// height field, this produces discrete areas -- the shape wanted for realms,
// biome patches or faction territory rather than terrain.
type voronoi struct{}

func (voronoi) Name() string  { return "voronoi" }
func (voronoi) Title() string { return "Voronoi regions" }

func (voronoi) Description() string {
	return "Scatters sites across the map and gives every hex to its nearest one, " +
		"carving the field into regions. Lloyd relaxation evens out their size and " +
		"the distance metric decides whether borders run straight or follow hex steps."
}

func (voronoi) Params() []mapgen.Param {
	return []mapgen.Param{
		{
			Name: "seed", Label: "Seed", Kind: mapgen.KindSeed,
			Help: "Reloading the page offers a new one.",
		},
		{
			Name: "radius", Label: "Map radius (hexes)", Kind: mapgen.KindInt,
			Default: 48, Min: 2, Max: 200, Step: 1,
			Help: "Any radius; this generator has no power-of-two constraint.",
		},
		{
			Name: "sites", Label: "Regions", Kind: mapgen.KindInt,
			Default: 32, Min: 2, Max: 400, Step: 1,
			Help: "Upper bound. Regions that relaxation collapses to a speck are folded into a neighbour.",
		},
		{
			Name: "lloyd", Label: "Lloyd relaxation", Kind: mapgen.KindInt,
			Default: 2, Min: 0, Max: 12, Step: 1,
			Help: "Iterations moving each site to its region's centroid. 0 leaves them ragged.",
		},
		{
			Name: "metric", Label: "Distance metric", Kind: mapgen.KindChoice,
			Default: "hex", Choices: []string{"hex", "euclidean"},
			Help: "Hex distance gives borders that follow the grid; euclidean gives straight ones.",
		},
		{
			Name: "fill", Label: "Region colour", Kind: mapgen.KindChoice,
			Default: "biome", Choices: []string{"biome", "distinct", "gray"},
			Help: "Biome picks plausible terrain colours; distinct maximises contrast.",
		},
		{
			Name: "borders", Label: "Draw borders", Kind: mapgen.KindBool,
			Default: true,
		},
		{
			Name: "size", Label: "Hex size (px)", Kind: mapgen.KindFloat,
			Default: 8.0, Min: 1, Max: 40, Step: 1,
		},
	}
}

func (voronoi) Generate(v mapgen.Values) (image.Image, error) {
	radius := v.Int("radius")
	size := v.Float("size")
	if w, h := hexgrid.ImageSize(radius, size); w*h > maxPixels {
		return nil, fmt.Errorf("image would be %d×%d pixels, over the %d megapixel cap: "+
			"lower the hex size or the radius", w, h, maxPixels>>20)
	}

	cells := hexgrid.Count(radius)
	sites := min(v.Int("sites"), cells)
	rng := newRand(v.Uint64("seed"))
	euclidean := v.String("metric") == "euclidean"

	// Scatter sites by reservoir-sampling distinct hexes, so no two regions
	// start on the same cell however small the map is.
	all := make([]hexgrid.Coord, 0, cells)
	hexgrid.Hexes(radius, func(c hexgrid.Coord) bool {
		all = append(all, c)
		return true
	})
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	// Clone: relaxation writes back into seeds, and a subslice of all would
	// share its backing array, overwriting the first `sites` hexes of the map
	// itself. Those hexes then never get an owner and render as background.
	seeds := slices.Clone(all[:sites])

	owner := map[hexgrid.Coord]int{}
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
	for range v.Int("lloyd") {
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

	colors := regionColors(len(seeds), v.String("fill"), rng)
	borders := v.Bool("borders")
	border := color.RGBA{R: 26, G: 24, B: 22, A: 255}

	return hexgrid.Render(radius, size, func(c hexgrid.Coord) (color.RGBA, bool) {
		o, ok := owner[c]
		if !ok {
			return color.RGBA{}, false
		}
		if borders {
			for _, d := range hexgrid.Directions {
				n := c.Add(d)
				// The map edge is not a border; only draw between regions.
				if other, inside := owner[n]; inside && other != o {
					return border, true
				}
			}
		}
		return colors[o], true
	}), nil
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

// biomes are plausible terrain colours for region fills.
var biomes = []color.RGBA{
	{R: 58, G: 124, B: 178, A: 255},  // water
	{R: 122, G: 152, B: 84, A: 255},  // grass
	{R: 66, G: 112, B: 62, A: 255},   // forest
	{R: 44, G: 82, B: 58, A: 255},    // deep forest
	{R: 196, G: 182, B: 128, A: 255}, // steppe
	{R: 214, G: 202, B: 154, A: 255}, // desert
	{R: 126, G: 116, B: 82, A: 255},  // hills
	{R: 138, G: 132, B: 126, A: 255}, // mountain
	{R: 92, G: 116, B: 122, A: 255},  // marsh
	{R: 232, G: 236, B: 240, A: 255}, // tundra
}

func regionColors(n int, fill string, rng *rand.Rand) []color.RGBA {
	out := make([]color.RGBA, n)
	for i := range out {
		switch fill {
		case "distinct":
			// Golden-ratio hue stepping spreads hues evenly however many
			// regions there are, unlike random hues which clump.
			out[i] = hexgrid.HSV(float64(i)*0.6180339887, 0.55, 0.85)
		case "gray":
			out[i] = hexgrid.Grayscale(float64(i) / math.Max(float64(n-1), 1))
		default:
			out[i] = biomes[rng.IntN(len(biomes))]
		}
	}
	return out
}

// newRand mirrors hexfield's source choice: math/rand/v2 only, and ChaCha8
// rather than PCG because seeds are frequently small and sequential.
func newRand(seed uint64) *rand.Rand {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	return rand.New(rand.NewChaCha8(key))
}

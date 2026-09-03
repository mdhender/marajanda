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

	rng := newRand(v.Uint64("seed"))
	_, owner, regions := partition(radius, v.Int("sites"), v.Int("lloyd"), v.String("metric") == "euclidean", rng)

	colors := regionColors(regions, v.String("fill"), rng)
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

package generators

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/mdhender/marajanda/internal/hexgrid"
	"github.com/mdhender/marajanda/internal/mapgen"
	"github.com/mdhender/marajanda/internal/noise"
)

func init() { mapgen.Register(noisemap{}) }

// noisemap samples a fractal noise field at every hex centre.
//
// Where subdivision builds its height field by refining the lattice, this one
// never refines anything: the field is a function of position, defined
// everywhere, and the hexes only choose where to sample it. That is why the
// map radius is free here and a power of two there, and why changing the hex
// size rescales the picture instead of changing the terrain.
type noisemap struct{}

func (noisemap) Name() string  { return "noise" }
func (noisemap) Title() string { return "Fractal noise" }

func (noisemap) Description() string {
	return "Samples a fractal sum of gradient noise at each hex centre. Simplex noise " +
		"lives on a triangular lattice, the same lattice hex centres do, so it has no " +
		"axis to give away. Ridged octaves put mountain chains on the noise's zero set."
}

func (noisemap) Params() []mapgen.Param {
	return []mapgen.Param{
		{
			Name: "seed", Label: "Seed", Kind: mapgen.KindSeed,
			Help: "Reloading the page offers a new one.",
		},
		{
			Name: "radius", Label: "Map radius (hexes)", Kind: mapgen.KindInt,
			Default: 48, Min: 2, Max: 200, Step: 1,
			Help: "Any radius: the field is continuous, so the map is a window onto it.",
		},
		{
			Name: "noise", Label: "Noise function", Kind: mapgen.KindChoice,
			Default: "simplex", Choices: []string{"simplex", "perlin", "value"},
			Help: "Simplex matches the hex lattice; Perlin shows a faint square grid; value is blockier.",
		},
		{
			Name: "shape", Label: "Octave shape", Kind: mapgen.KindChoice,
			Default: "fbm", Choices: []string{"fbm", "ridged", "billow"},
			Help: "fBm rolls, ridged carves chains along the zero set, billow piles up dunes.",
		},
		{
			Name: "frequency", Label: "Base frequency", Kind: mapgen.KindFloat,
			Default: 3.0, Min: 0.25, Max: 16.0, Step: 0.25,
			Help: "Features across the map at the coarsest octave.",
		},
		{
			Name: "octaves", Label: "Octaves", Kind: mapgen.KindInt,
			Default: 5, Min: 1, Max: 12, Step: 1,
			Help: "Octaves finer than a hex only add per-hex speckle; raise the radius with them.",
		},
		{
			Name: "lacunarity", Label: "Lacunarity", Kind: mapgen.KindFloat,
			Default: 2.0, Min: 1.2, Max: 4.0, Step: 0.1,
			Help: "Frequency ratio between octaves. Non-integer values keep octaves from aligning.",
		},
		{
			Name: "gain", Label: "Gain", Kind: mapgen.KindFloat,
			Default: 0.5, Min: 0.1, Max: 0.9, Step: 0.05,
			Help: "Amplitude ratio between octaves. 1/lacunarity is the scale-invariant case.",
		},
		{
			Name: "warp", Label: "Domain warp", Kind: mapgen.KindFloat,
			Default: 0.0, Min: 0.0, Max: 1.5, Step: 0.05,
			Help: "Displace the sample point by the noise itself, in base wavelengths. Bends features into something eroded.",
		},
		{
			Name: "island", Label: "Island falloff", Kind: mapgen.KindFloat,
			Default: 0.0, Min: 0.0, Max: 1.0, Step: 0.05,
			Help: "Pull the rim down towards the bottom of the range, so the map ends in ocean.",
		},
		{
			Name: "sea", Label: "Sea level", Kind: mapgen.KindFloat,
			Default: 0.45, Min: 0.0, Max: 0.95, Step: 0.05,
			Help: "Fraction of the height range below the waterline.",
		},
		{
			Name: "palette", Label: "Palette", Kind: mapgen.KindChoice,
			Default: "terrain", Choices: []string{"terrain", "gray"},
			Help: "Grayscale exposes lattice artefacts that terrain colours hide.",
		},
		{
			Name: "size", Label: "Hex size (px)", Kind: mapgen.KindFloat,
			Default: 6.0, Min: 1, Max: 40, Step: 1,
		},
	}
}

var noiseKinds = map[string]noise.Kind{
	"simplex": noise.Simplex,
	"perlin":  noise.Perlin,
	"value":   noise.Value,
}

var noiseShapes = map[string]noise.Shape{
	"fbm":    noise.FBM,
	"ridged": noise.Ridged,
	"billow": noise.Billow,
}

func (noisemap) Generate(v mapgen.Values) (image.Image, error) {
	radius := v.Int("radius")
	size := v.Float("size")
	if w, h := hexgrid.ImageSize(radius, size); w*h > maxPixels {
		return nil, fmt.Errorf("image would be %d×%d pixels, over the %d megapixel cap: "+
			"lower the hex size or the radius", w, h, maxPixels>>20)
	}

	src := noise.New(v.Uint64("seed"))
	f := noise.Fractal{
		Kind:       noiseKinds[v.String("noise")],
		Shape:      noiseShapes[v.String("shape")],
		Octaves:    v.Int("octaves"),
		Frequency:  v.Float("frequency"),
		Lacunarity: v.Float("lacunarity"),
		Gain:       v.Float("gain"),
		Warp:       v.Float("warp"),
	}

	// Sample in pixel space at unit hex size, scaled so the map spans about
	// two units. Cube coordinates are not a space the noise would look
	// isotropic in -- one step in Q and one in R are the same distance on the
	// map but not in the field -- so sampling them directly shears it. This is
	// the same reason tectonic takes its boundary normals from pixel-space
	// centroids.
	//
	// Dividing by the radius rather than by the pixel width also keeps the
	// terrain fixed as the hex size changes: size zooms the picture, radius
	// widens the window onto the field.
	scale := 1 / float64(radius)
	heights := make(map[hexgrid.Coord]float64, hexgrid.Count(radius))
	lo, hi := math.Inf(1), math.Inf(-1)
	hexgrid.Hexes(radius, func(c hexgrid.Coord) bool {
		x, y := hexgrid.Center(c, 1)
		h := src.At(f, x*scale, y*scale)
		heights[c] = h
		lo, hi = math.Min(lo, h), math.Max(hi, h)
		return true
	})

	// Normalize before the falloff rather than after, so sea level keeps
	// meaning the same thing: the falloff subtracts a known fraction of the
	// range instead of being rescaled away by a normalization that followed
	// it.
	span := hi - lo
	island := v.Float("island")

	pal := hexgrid.Palette(hexgrid.Grayscale)
	if v.String("palette") == "terrain" {
		pal = hexgrid.Terrain(v.Float("sea"))
	}

	return hexgrid.Render(radius, size, func(c hexgrid.Coord) (color.RGBA, bool) {
		h, ok := heights[c]
		if !ok {
			return color.RGBA{}, false
		}
		t := 0.0
		if span > 0 {
			t = (h - lo) / span
		}
		if island > 0 {
			t -= island * smoothstep(0.4, 1.0, float64(c.Length())/float64(radius))
		}
		return pal(hexgrid.Clamp01(t)), true
	}), nil
}

// smoothstep ramps from 0 at a to 1 at b with zero slope at either end, so an
// island's coastline is not marked by a crease where the falloff switches on.
func smoothstep(a, b, x float64) float64 {
	t := hexgrid.Clamp01((x - a) / (b - a))
	return t * t * (3 - 2*t)
}

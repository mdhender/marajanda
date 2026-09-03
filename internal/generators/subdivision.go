// Package generators holds the concrete hex map generators. Each file
// registers itself with mapgen at init time, so adding a generator means
// adding a file here and nothing else.
package generators

import (
	"fmt"
	"image"

	"github.com/mdhender/marjanda/internal/hexfield"
	"github.com/mdhender/marjanda/internal/hexgrid"
	"github.com/mdhender/marjanda/internal/mapgen"
)

func init() { mapgen.Register(subdivision{}) }

// maxPixels caps the rendered image. Levels and hex size multiply, so a
// hand-typed URL can otherwise ask for a buffer of many gigabytes.
const maxPixels = 40 << 20

type subdivision struct{}

func (subdivision) Name() string  { return "subdivision" }
func (subdivision) Title() string { return "Midpoint subdivision" }

func (subdivision) Description() string {
	return "Recursive midpoint displacement on the triangular lattice of hex centres, " +
		"the hex analogue of diamond-square. Seven seed points at the centre and rim " +
		"refine into the full field, four times as many points each pass."
}

func (subdivision) Params() []mapgen.Param {
	return []mapgen.Param{
		{
			Name: "seed", Label: "Seed", Kind: mapgen.KindSeed,
			Help: "Reloading the page offers a new one.",
		},
		{
			Name: "levels", Label: "Levels", Kind: mapgen.KindInt,
			Default: 7, Min: 1, Max: 9, Step: 1,
			Help: "Hexagon radius is 2^levels, giving 3N²+3N+1 hexes.",
		},
		{
			Name: "hurst", Label: "Hurst exponent (H)", Kind: mapgen.KindFloat,
			Default: 0.7, Min: 0.05, Max: 1.0, Step: 0.05,
			Help: "Roughness: 1.0 smooth rolling hills, ~0.5 natural, near 0 jagged.",
		},
		{
			Name: "roughness", Label: "Initial amplitude", Kind: mapgen.KindFloat,
			Default: 1.0, Min: 0.1, Max: 4.0, Step: 0.1,
			Help: "Displacement at the coarsest level.",
		},
		{
			Name: "stencil", Label: "Stencil", Kind: mapgen.KindChoice,
			Default: "loop", Choices: []string{"loop", "midpoint"},
			Help: "Loop is the four-point edge mask; midpoint averages the two endpoints.",
		},
		{
			Name: "relax", Label: "Relax (Loop vertex mask)", Kind: mapgen.KindBool,
			Default: true,
			Help:    "Reposition existing points each level. Alone it over-smooths them.",
		},
		{
			Name: "sra", Label: "Successive random additions", Kind: mapgen.KindBool,
			Default: true,
			Help:    "Perturb every point each level. Alone it makes creasing worse; pair it with relax.",
		},
		{
			Name: "island", Label: "Island bias", Kind: mapgen.KindBool,
			Default: false,
			Help:    "Seed the centre high and the rim low. Relax weakens this.",
		},
		{
			Name: "sea", Label: "Sea level", Kind: mapgen.KindFloat,
			Default: 0.4, Min: 0.0, Max: 0.95, Step: 0.05,
			Help: "Fraction of the height range below the waterline.",
		},
		{
			Name: "palette", Label: "Palette", Kind: mapgen.KindChoice,
			Default: "terrain", Choices: []string{"terrain", "gray"},
			Help: "Grayscale exposes lattice artefacts that terrain colours hide.",
		},
		{
			Name: "size", Label: "Hex size (px)", Kind: mapgen.KindFloat,
			Default: 4.0, Min: 1, Max: 40, Step: 1,
		},
	}
}

func (subdivision) Generate(v mapgen.Values) (image.Image, error) {
	stencil := hexfield.Loop
	if v.String("stencil") == "midpoint" {
		stencil = hexfield.Midpoint
	}

	f := hexfield.Generate(hexfield.Params{
		Levels:    v.Int("levels"),
		Hurst:     v.Float("hurst"),
		Roughness: v.Float("roughness"),
		Seed:      v.Uint64("seed"),
		Stencil:   stencil,
		Relax:     v.Bool("relax"),
		SRA:       v.Bool("sra"),
		Island:    v.Bool("island"),
	})

	size := v.Float("size")
	if w, h := f.ImageSize(size); w*h > maxPixels {
		return nil, fmt.Errorf("image would be %d×%d pixels, over the %d megapixel cap: "+
			"lower the hex size or the levels", w, h, maxPixels>>20)
	}

	pal := hexgrid.Palette(hexgrid.Grayscale)
	if v.String("palette") == "terrain" {
		pal = hexgrid.Terrain(v.Float("sea"))
	}
	return f.Image(size, pal), nil
}

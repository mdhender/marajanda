// Command hexgen generates fractal terrain on a hex grid and renders it as a
// PNG or as ASCII.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/mdhender/marjanda/internal/hexfield"
	"github.com/mdhender/marjanda/internal/hexgrid"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hexgen:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		levels    = flag.Int("levels", 6, "subdivision levels; hexagon radius is 2**levels")
		hurst     = flag.Float64("hurst", 0.8, "Hurst exponent in (0,1]: 1 smooth, 0.5 natural, near 0 jagged")
		roughness = flag.Float64("roughness", 1.0, "displacement amplitude at the coarsest level")
		seed      = flag.Uint64("seed", 1, "terrain seed")
		stencil   = flag.String("stencil", "loop", "interpolation stencil: loop or midpoint")
		relax     = flag.Bool("relax", true, "Loop vertex mask: reposition existing points each level")
		sra       = flag.Bool("sra", true, "successive random additions: perturb every point each level")
		island    = flag.Bool("island", false, "seed the centre high and the rim low")
		sea       = flag.Float64("sea", 0.4, "sea level as a fraction of the height range")
		size      = flag.Float64("size", 6, "hex size in pixels")
		palette   = flag.String("palette", "terrain", "colour palette: terrain or gray")
		asciiOut  = flag.Bool("ascii", false, "write ASCII to stdout instead of a PNG")
		out       = flag.String("out", "terrain.png", "output PNG path")
		compare   = flag.String("compare", "", "write a comparison set into this directory")
	)
	flag.Parse()

	st := hexfield.Loop
	switch *stencil {
	case "loop":
	case "midpoint":
		st = hexfield.Midpoint
	default:
		return fmt.Errorf("unknown stencil %q: want loop or midpoint", *stencil)
	}
	if *levels < 1 || *levels > 10 {
		return fmt.Errorf("levels must be in [1,10], got %d", *levels)
	}

	p := hexfield.Params{
		Levels:    *levels,
		Hurst:     *hurst,
		Roughness: *roughness,
		Seed:      *seed,
		Stencil:   st,
		Relax:     *relax,
		SRA:       *sra,
		Island:    *island,
	}

	var pal hexgrid.Palette
	switch *palette {
	case "terrain":
		pal = hexgrid.Terrain(*sea)
	case "gray":
		// Grayscale shows lattice artefacts that the terrain palette's
		// colour banding hides.
		pal = hexgrid.Grayscale
	default:
		return fmt.Errorf("unknown palette %q: want terrain or gray", *palette)
	}

	if *compare != "" {
		return writeComparison(*compare, p, *size, pal)
	}

	f := hexfield.Generate(p)
	if *asciiOut {
		fmt.Print(f.ASCII(hexfield.Ramp, *sea))
		return nil
	}
	if err := writePNG(*out, f, *size, pal); err != nil {
		return err
	}
	fmt.Printf("%s: radius %d, %d hexes\n", *out, f.Radius, f.Len())
	return nil
}

// writeComparison emits one PNG per variant, named for its settings, so the
// effect of each knob can be seen side by side. Filenames carry the labels;
// nothing is drawn onto the images and no font dependency is needed.
func writeComparison(dir string, base hexfield.Params, size float64, pal hexgrid.Palette) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var made []string
	emit := func(name string, p hexfield.Params) error {
		path := filepath.Join(dir, name+".png")
		if err := writePNG(path, hexfield.Generate(p), size, pal); err != nil {
			return err
		}
		made = append(made, path)
		return nil
	}

	// Roughness sweep, with both creasing fixes on throughout.
	for _, h := range []float64{0.3, 0.5, 0.7, 0.9, 1.0} {
		p := base
		p.Hurst, p.Relax, p.SRA = h, true, true
		if err := emit(fmt.Sprintf("hurst_%.1f", h), p); err != nil {
			return err
		}
	}

	// Creasing: each fix alone and both together, against the naive case.
	// Relax and SRA fail in opposite directions on their own.
	for _, v := range []struct {
		name    string
		stencil hexfield.Stencil
		relax   bool
		sra     bool
	}{
		{"crease_1_midpoint_bare", hexfield.Midpoint, false, false},
		{"crease_2_loop_bare", hexfield.Loop, false, false},
		{"crease_3_loop_sra-only", hexfield.Loop, false, true},
		{"crease_4_loop_relax-only", hexfield.Loop, true, false},
		{"crease_5_loop_relax-sra", hexfield.Loop, true, true},
	} {
		p := base
		p.Stencil, p.Relax, p.SRA = v.stencil, v.relax, v.sra
		if err := emit(v.name, p); err != nil {
			return err
		}
	}

	// Seeding the seven coarse points to force an island.
	p := base
	p.Island, p.Relax, p.SRA = true, true, true
	if err := emit("island", p); err != nil {
		return err
	}

	for _, m := range made {
		fmt.Println(m)
	}
	return nil
}

func writePNG(path string, f *hexfield.Field, size float64, pal hexgrid.Palette) (err error) {
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := fh.Close(); err == nil {
			err = cerr
		}
	}()
	return png.Encode(fh, f.Image(size, pal))
}

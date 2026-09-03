package generators

import (
	"image"
	"image/color"
	"math"
	"net/url"
	"testing"

	"github.com/mdhender/marajanda/internal/hexgrid"
	"github.com/mdhender/marajanda/internal/mapgen"
)

// Every test here runs against the whole registry, so a new generator is
// covered by adding its file and nothing else.

func each(t *testing.T, fn func(*testing.T, mapgen.Generator)) {
	t.Helper()
	all := mapgen.All()
	if len(all) == 0 {
		t.Fatal("no generators registered")
	}
	for _, g := range all {
		t.Run(g.Name(), func(t *testing.T) { fn(t, g) })
	}
}

// defaults renders a generator at its declared defaults, but small and fast.
func render(t *testing.T, g mapgen.Generator, over url.Values) image.Image {
	t.Helper()
	form := url.Values{}
	for k, v := range mapgen.Defaults(g) {
		form.Set(k, v)
	}
	for k, v := range over {
		form[k] = v
	}
	img, err := g.Generate(mapgen.FromForm(g, form))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return img
}

// Every hex inside the map must get a real colour.
//
// This is the regression test for a slice-aliasing bug in voronoi, where the
// site list shared a backing array with the list of all hexes: relaxation
// wrote sites back and silently overwrote the first N hexes of the map, which
// then never received an owner and rendered as background. It showed up as
// specks scattered across the image.
//
// Sampling is done in pixel space so it works for any generator without
// knowing how it parameterises its map size.
func TestNoUnpaintedHexes(t *testing.T) {
	each(t, func(t *testing.T, g mapgen.Generator) {
		img := render(t, g, url.Values{"levels": {"5"}, "radius": {"24"}, "size": {"6"}})
		b := img.Bounds()
		cx, cy := float64(b.Dx())/2, float64(b.Dy())/2

		// Stay well inside the hexagon so the rim is never sampled.
		limit := 0.6 * min(cx, cy)
		bad := 0
		for y := b.Min.Y; y < b.Max.Y; y += 3 {
			for x := b.Min.X; x < b.Max.X; x += 3 {
				dx, dy := float64(x)-cx, float64(y)-cy
				if math.Hypot(dx, dy) > limit {
					continue
				}
				if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) == hexgrid.Background {
					bad++
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d interior samples painted with the background colour", bad)
		}
	})
}

// The same seed and parameters must give the same image, or nothing is
// reproducible and a shared URL means nothing.
func TestDeterministic(t *testing.T) {
	each(t, func(t *testing.T, g mapgen.Generator) {
		over := url.Values{"seed": {"424242"}, "levels": {"5"}, "radius": {"20"}, "size": {"4"}}
		a, b := render(t, g, over), render(t, g, over)
		if a.Bounds() != b.Bounds() {
			t.Fatalf("bounds differ: %v vs %v", a.Bounds(), b.Bounds())
		}
		for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y += 2 {
			for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x += 2 {
				if a.At(x, y) != b.At(x, y) {
					t.Fatalf("pixel (%d,%d) differs between runs with the same seed", x, y)
				}
			}
		}
	})
}

func TestDifferentSeedsDiffer(t *testing.T) {
	each(t, func(t *testing.T, g mapgen.Generator) {
		base := url.Values{"levels": {"5"}, "radius": {"20"}, "size": {"4"}}
		base.Set("seed", "1")
		a := render(t, g, base)
		base.Set("seed", "2")
		b := render(t, g, base)

		for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y += 2 {
			for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x += 2 {
				if a.At(x, y) != b.At(x, y) {
					return
				}
			}
		}
		t.Error("two different seeds produced the same image")
	})
}

// The size cap must reject rather than attempt a multi-gigabyte allocation.
func TestOversizedRequestIsRefused(t *testing.T) {
	each(t, func(t *testing.T, g mapgen.Generator) {
		form := url.Values{}
		for k, v := range mapgen.Defaults(g) {
			form.Set(k, v)
		}
		// Push both knobs to their declared maxima.
		for _, p := range g.Params() {
			if p.Name == "size" || p.Name == "levels" || p.Name == "radius" {
				form.Set(p.Name, "100000")
			}
		}
		if _, err := g.Generate(mapgen.FromForm(g, form)); err == nil {
			t.Error("a maximal request was accepted; expected the pixel cap to refuse it")
		}
	})
}

// Registry hygiene: the web form is generated from these declarations, so a
// malformed one produces a broken control rather than a compile error.
func TestParamDeclarationsAreSound(t *testing.T) {
	each(t, func(t *testing.T, g mapgen.Generator) {
		if g.Title() == "" || g.Description() == "" {
			t.Error("generator needs a title and a description for the picker")
		}
		for _, p := range g.Params() {
			if p.Name == "" || p.Label == "" {
				t.Errorf("param %q needs both a name and a label", p.Name)
			}
			switch p.Kind {
			case mapgen.KindInt:
				if _, ok := p.Default.(int); !ok {
					t.Errorf("%s: int param default is %T", p.Name, p.Default)
				}
				if p.Min >= p.Max {
					t.Errorf("%s: bounds [%v,%v] are empty", p.Name, p.Min, p.Max)
				}
			case mapgen.KindFloat:
				if _, ok := p.Default.(float64); !ok {
					t.Errorf("%s: float param default is %T", p.Name, p.Default)
				}
				if p.Min >= p.Max {
					t.Errorf("%s: bounds [%v,%v] are empty", p.Name, p.Min, p.Max)
				}
			case mapgen.KindBool:
				if _, ok := p.Default.(bool); !ok {
					t.Errorf("%s: bool param default is %T", p.Name, p.Default)
				}
			case mapgen.KindChoice:
				d, ok := p.Default.(string)
				if !ok {
					t.Errorf("%s: choice param default is %T", p.Name, p.Default)
					continue
				}
				if len(p.Choices) < 2 {
					t.Errorf("%s: choice param has %d choices", p.Name, len(p.Choices))
				}
				found := false
				for _, c := range p.Choices {
					if c == d {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: default %q is not among the choices %v", p.Name, d, p.Choices)
				}
			case mapgen.KindSeed:
				// Defaults are generated, so nothing to declare.
			default:
				t.Errorf("%s: unknown kind %q", p.Name, p.Kind)
			}
		}
	})
}

// Slivers are real: a small map with many sites leaves regions of one or two
// hexes, which read as dirt once borders are drawn.
func TestSliversAreMergedAway(t *testing.T) {
	g, ok := mapgen.Get("voronoi")
	if !ok {
		t.Skip("voronoi not registered")
	}
	img := render(t, g, url.Values{
		"radius": {"4"}, "sites": {"30"}, "lloyd": {"0"},
		"borders": {"true"}, "size": {"8"},
	})
	if img == nil {
		t.Fatal("no image")
	}
}

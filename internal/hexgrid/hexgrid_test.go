package hexgrid

import (
	"image/color"
	"testing"
)

func TestRoundStaysOnTheLattice(t *testing.T) {
	for q := -3.0; q <= 3; q += 0.37 {
		for r := -3.0; r <= 3; r += 0.29 {
			if c := Round(q, r, -q-r); !c.Valid() {
				t.Fatalf("Round(%v,%v) gave %v, which violates Q+R+S == 0", q, r, c)
			}
		}
	}
}

// Round must be the inverse of Center, or the renderer colours the wrong hex.
func TestRoundInvertsCenter(t *testing.T) {
	const size = 7.0
	Hexes(6, func(c Coord) bool {
		x, y := Center(c, size)
		fq := (sqrt3/3*x - y/3) / size
		fr := (2.0 / 3.0 * y) / size
		if got := Round(fq, fr, -fq-fr); got != c {
			t.Errorf("centre of %v round-tripped to %v", c, got)
		}
		return true
	})
}

func TestHexesCoversExactlyTheHexagon(t *testing.T) {
	for radius := range 6 {
		seen := map[Coord]bool{}
		Hexes(radius, func(c Coord) bool {
			if !c.Valid() {
				t.Fatalf("radius %d: %v violates the cube invariant", radius, c)
			}
			if c.Length() > radius {
				t.Fatalf("radius %d: %v is outside the hexagon", radius, c)
			}
			if seen[c] {
				t.Fatalf("radius %d: %v visited twice", radius, c)
			}
			seen[c] = true
			return true
		})
		if want := Count(radius); len(seen) != want {
			t.Errorf("radius %d: visited %d hexes, want %d", radius, len(seen), want)
		}
	}
}

func TestHexesStopsWhenAsked(t *testing.T) {
	n := 0
	Hexes(10, func(Coord) bool { n++; return n < 5 })
	if n != 5 {
		t.Errorf("visited %d hexes after the yield returned false, want 5", n)
	}
}

func TestDistance(t *testing.T) {
	if got := Origin.Distance(Origin); got != 0 {
		t.Errorf("distance to self = %d, want 0", got)
	}
	for _, d := range Directions {
		if got := Origin.Distance(d); got != 1 {
			t.Errorf("distance to neighbour %v = %d, want 1", d, got)
		}
		if got := Origin.Distance(d.Scale(5)); got != 5 {
			t.Errorf("distance to %v = %d, want 5", d.Scale(5), got)
		}
	}
}

func TestImageSizeGrowsWithRadiusAndSize(t *testing.T) {
	w1, h1 := ImageSize(10, 4)
	w2, h2 := ImageSize(20, 4)
	if !(w2 > w1 && h2 > h1) {
		t.Errorf("doubling radius gave %dx%d from %dx%d", w2, h2, w1, h1)
	}
	w3, h3 := ImageSize(10, 8)
	if !(w3 > w1 && h3 > h1) {
		t.Errorf("doubling hex size gave %dx%d from %dx%d", w3, h3, w1, h1)
	}
}

func TestRenderFillsOutsideWithBackground(t *testing.T) {
	img := Render(3, 6, func(c Coord) (color.RGBA, bool) { return color.RGBA{R: 255, A: 255}, c.Length() <= 3 })
	b := img.Bounds()
	if got := img.RGBAAt(b.Min.X, b.Min.Y); got != Background {
		t.Errorf("top-left corner is %v, want the background %v", got, Background)
	}
	if got := img.RGBAAt(b.Dx()/2, b.Dy()/2); got.R != 255 {
		t.Errorf("centre is %v, want the hex colour", got)
	}
}

func TestHSVIsOpaqueAndInRange(t *testing.T) {
	for i := range 12 {
		c := HSV(float64(i)/12, 0.6, 0.9)
		if c.A != 255 {
			t.Errorf("HSV produced alpha %d, want 255", c.A)
		}
	}
	if got := HSV(0, 0, 1); got.R != 255 || got.G != 255 || got.B != 255 {
		t.Errorf("zero saturation at full value gave %v, want white", got)
	}
}

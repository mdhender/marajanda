package world

import (
	"bytes"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/maloquacious/hexg"
)

// donjonSample is the geometry the whole package is pinned to, taken from the
// place index of docs/downloads/Eglar.html: a 4000x2000 map, 200 columns of
// flat-top hexes at a 40/3 pixel circumradius, odd columns pushed half a row
// south. The x and y here are donjon's own numbers, so this test fails if the
// layout ever drifts away from output we can compare against.
var donjonSample = []struct {
	col, row int
	x, y     float64
}{
	{32, 22, 646.666666662095, 508.068236877741},
	{159, 24, 3186.66666664414, 565.803263795666}, // odd column: half a row down
	{2, 33, 46.6666666663367, 762.102355316612},   // near the western edge
	{184, 30, 3686.6666666406, 692.820323015102},  // near the eastern edge
	{51, 73, 1026.66666665941, 1697.409791387},    // odd column, far south
}

func donjonGrid() Grid { return Grid{Cols: 200, Rows: 87, WrapEastWest: true} }

// TestLayoutMatchesDonjonSample pins the frame this package hands to hexg.
// The trigonometry is hexg's; what is asserted here is that a map drawn
// through Layout lands on donjon's own pixels.
func TestLayoutMatchesDonjonSample(t *testing.T) {
	g := donjonGrid()
	l := g.Layout(Radius)
	for _, s := range donjonSample {
		p := l.HexToPixel(g.Cube(s.col, s.row))
		if math.Abs(p.X-s.x) > 1e-6 || math.Abs(p.Y-s.y) > 1e-6 {
			t.Errorf("hex (%d,%d) drawn at (%.9f,%.9f), donjon has (%.9f,%.9f)", s.col, s.row, p.X, p.Y, s.x, s.y)
		}
	}
}

func TestSizeMatchesDonjonSample(t *testing.T) {
	// 200 columns at r=40/3 is exactly the 4000px width of Eglar.png. The
	// height follows from the row count, which donjon crops rather than
	// rounds, so only the width is an exact match to the sample.
	w, _ := donjonGrid().span()
	if math.Abs(w*Radius-4000) > 1e-9 {
		t.Errorf("width = %v, want 4000", w*Radius)
	}
}

// TestDirectionsPointWhereTheyClaim checks each direction's name against the
// geometry, using the pixel positions hexg computes rather than a table of
// steps this package would otherwise have to keep in step with hexg's own.
// If hexg ever reorders its direction vectors, this fails rather than
// silently rotating every map.
func TestDirectionsPointWhereTheyClaim(t *testing.T) {
	g := Grid{Cols: 9, Rows: 9}
	l := g.Layout(Radius)
	// want is the sign of the change in x and y for each direction, in screen
	// terms: y grows southward.
	want := map[Direction]struct{ dx, dy int }{
		N:  {0, -1},
		S:  {0, +1},
		NE: {+1, -1},
		SE: {+1, +1},
		NW: {-1, -1},
		SW: {-1, +1},
	}
	for _, col := range []int{4, 5} { // one even column and one odd
		from := l.HexToPixel(g.Cube(col, 4))
		for _, d := range Directions {
			nc, nr, ok := g.Neighbor(col, 4, d)
			if !ok {
				t.Fatalf("Neighbor(%d,4,%d) left a grid it should not have", col, d)
			}
			to := l.HexToPixel(g.Cube(nc, nr))
			if got, w := sign(to.X-from.X), want[d].dx; got != w {
				t.Errorf("from column %d, direction %d moves x %+d, want %+d", col, d, got, w)
			}
			if got, w := sign(to.Y-from.Y), want[d].dy; got != w {
				t.Errorf("from column %d, direction %d moves y %+d, want %+d", col, d, got, w)
			}
		}
	}
}

func sign(f float64) int {
	switch {
	case f < -1e-9:
		return -1
	case f > 1e-9:
		return 1
	}
	return 0
}

func TestWrapClosesTheCylinder(t *testing.T) {
	g := Grid{Cols: 10, Rows: 10, WrapEastWest: true}

	// Stepping west out of column 0 arrives in the last column, and stepping
	// east out of the last column arrives in column 0.
	if c, _, ok := g.Neighbor(0, 5, NW); !ok || c != 9 {
		t.Errorf("NW of (0,5) = (%d, ok=%v), want column 9", c, ok)
	}
	if c, _, ok := g.Neighbor(9, 5, NE); !ok || c != 0 {
		t.Errorf("NE of (9,5) = (%d, ok=%v), want column 0", c, ok)
	}

	// Without the wrap the same steps run off the map.
	flat := Grid{Cols: 10, Rows: 10}
	if _, _, ok := flat.Neighbor(0, 5, NW); ok {
		t.Error("NW of (0,5) stayed on a grid that does not wrap")
	}
	if _, _, ok := flat.Neighbor(9, 5, NE); ok {
		t.Error("NE of (9,5) stayed on a grid that does not wrap")
	}
}

// TestOnlyPolesLoseNeighbors is the property that matters for a generator:
// on a wrapping world every hex has six neighbours except along the top and
// bottom rows, so an algorithm only has to special-case the poles.
func TestOnlyPolesLoseNeighbors(t *testing.T) {
	g := Grid{Cols: 12, Rows: 8, WrapEastWest: true}
	for col := 0; col < g.Cols; col++ {
		for row := 0; row < g.Rows; row++ {
			n := 0
			for _, d := range Directions {
				if _, _, ok := g.Neighbor(col, row, d); ok {
					n++
				}
			}
			pole := row == 0 || row == g.Rows-1
			if !pole && n != 6 {
				t.Errorf("hex (%d,%d) has %d neighbours, want 6", col, row, n)
			}
			if pole && n == 6 {
				t.Errorf("hex (%d,%d) is on a pole row but has all 6 neighbours", col, row)
			}
		}
	}
}

func TestLatLonAndUnit(t *testing.T) {
	g := donjonGrid()

	// The grid reaches close to both poles without standing on either: a hex
	// covers a band of latitude and sits in the middle of it, so the
	// outermost centres are half a band short of the poles themselves.
	north, _ := g.LatLon(0, 0)
	south, _ := g.LatLon(1, g.Rows-1)
	if north <= 80 || north >= 90 {
		t.Errorf("northernmost latitude = %v, want just short of +90", north)
	}
	if south >= -80 || south <= -90 {
		t.Errorf("southernmost latitude = %v, want just short of -90", south)
	}

	// Longitude increases eastward and stays inside the sphere's range.
	_, west := g.LatLon(0, 40)
	_, east := g.LatLon(g.Cols-1, 40)
	if !(west < east) || west < -180 || east > 180 {
		t.Errorf("longitudes %v..%v are not an eastward span of the globe", west, east)
	}

	// Every hex is on the unit sphere.
	for col := 0; col < g.Cols; col += 7 {
		for row := 0; row < g.Rows; row += 5 {
			x, y, z := g.Unit(col, row)
			if d := math.Abs(math.Sqrt(x*x+y*y+z*z) - 1); d > 1e-12 {
				t.Fatalf("Unit(%d,%d) has length 1%+g", col, row, d)
			}
		}
	}

	// The wrap is real geometry, not a bookkeeping trick: one column past the
	// east edge is the same point on the sphere as column 0. This is what a
	// generator working in Unit space gets for free.
	x0, y0, z0 := g.Unit(0, 40)
	x1, y1, z1 := g.Unit(g.Cols, 40)
	if math.Abs(x0-x1) > 1e-12 || math.Abs(y0-y1) > 1e-12 || math.Abs(z0-z1) > 1e-12 {
		t.Errorf("column %d is at (%v,%v,%v), column 0 at (%v,%v,%v)", g.Cols, x1, y1, z1, x0, y0, z0)
	}
}

// TestLatitudeIsSymmetric is the property the half-band offset buys: every
// latitude the grid samples has its mirror image in the other hemisphere. An
// ice cap or a climate band computed from latitude then comes out the same
// size at both ends of the map, which it did not when row 0 sat on the pole
// and the last row stopped a band short of it.
func TestLatitudeIsSymmetric(t *testing.T) {
	for _, g := range []Grid{
		{Cols: 200, Rows: 87, WrapEastWest: true},
		{Cols: 40, Rows: 20, WrapEastWest: true},
		{Cols: 2, Rows: 3, WrapEastWest: true},
	} {
		lats := make([]float64, 0, g.Len())
		for col := range g.Cols {
			for row := range g.Rows {
				lat, _ := g.LatLon(col, row)
				lats = append(lats, lat)
			}
		}
		slices.Sort(lats)
		for i, n := 0, len(lats); i < n/2; i++ {
			if north, south := lats[i], lats[n-1-i]; math.Abs(north+south) > 1e-9 {
				t.Errorf("%dx%d: latitude %v has no mirror; found %v", g.Cols, g.Rows, north, south)
				break
			}
		}
	}
}

// TestWrappingGridsNeedEvenColumns guards a seam that is invisible until it is
// rendered. Odd columns are pushed half a row south, so a cylinder only closes
// when the last column and column 0 have opposite parity -- and with an odd
// column count they do not, leaving two unstaggered columns side by side at
// the join.
func TestWrappingGridsNeedEvenColumns(t *testing.T) {
	w := New(9, 5)
	if err := w.Validate(); err == nil {
		t.Error("a wrapping grid with 9 columns validated")
	}
	w.Grid.WrapEastWest = false
	if err := w.Validate(); err != nil {
		t.Errorf("9 columns without the wrap should be fine: %v", err)
	}
}

func TestIndexRoundTrip(t *testing.T) {
	g := Grid{Cols: 7, Rows: 5}
	seen := make(map[int]bool, g.Len())
	for col := 0; col < g.Cols; col++ {
		for row := 0; row < g.Rows; row++ {
			i := g.Index(col, row)
			if i < 0 || i >= g.Len() {
				t.Fatalf("Index(%d,%d) = %d, outside 0..%d", col, row, i, g.Len())
			}
			if seen[i] {
				t.Fatalf("Index(%d,%d) = %d, already used", col, row, i)
			}
			seen[i] = true
			if c, r := g.ColRow(i); c != col || r != row {
				t.Fatalf("ColRow(Index(%d,%d)) = (%d,%d)", col, row, c, r)
			}
		}
	}
	if len(seen) != g.Len() {
		t.Errorf("covered %d of %d hexes", len(seen), g.Len())
	}
}

// TestCubeIsWhatWorldographerWants pins the conversion at the export
// boundary. hexg's coordinates have no exported fields, so the value can only
// be compared, never read -- which is the whole reason this package stores
// (col,row) and converts here.
func TestCubeIsWhatWorldographerWants(t *testing.T) {
	g := Grid{Cols: 5, Rows: 5}
	for col := 0; col < g.Cols; col++ {
		for row := 0; row < g.Rows; row++ {
			// Straight to the odd-q conversion, bypassing Layout: this pins
			// that the grid is odd-q and not even-q, which is a half-row
			// error that nothing else here would catch.
			if got, want := g.Cube(col, row), hexg.NewOffsetCoord(col, row).QOffsetToCube(false); !got.Equals(want) {
				t.Errorf("Cube(%d,%d) = %v, want %v", col, row, got, want)
			}
		}
	}
}

func filled(t *testing.T) *World {
	t.Helper()
	w := New(6, 4)
	w.Name, w.Seed, w.Generator = "Eglar", 12345, "fault"
	w.Created = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	w.Grid.SeaLevel = 0.42
	w.Terrains = []string{"Water Shallow", "Grassland", "Mountains"}
	w.Layers.Elevation = Alloc(w.Grid, w.Layers.Elevation)
	w.Layers.Temperature = Alloc(w.Grid, w.Layers.Temperature)
	w.Layers.Rainfall = Alloc(w.Grid, w.Layers.Rainfall)
	w.Layers.Terrain = Alloc(w.Grid, w.Layers.Terrain)
	w.Layers.Icy = Alloc(w.Grid, w.Layers.Icy)
	for i := range w.Grid.Len() {
		w.Layers.Elevation[i] = float64(i) / float64(w.Grid.Len())
		w.Layers.Temperature[i] = 1 - float64(i)/float64(w.Grid.Len())
		w.Layers.Rainfall[i] = 0.25
		w.Layers.Terrain[i] = i % len(w.Terrains)
		w.Layers.Icy[i] = i%5 == 0
	}
	return w
}

func TestSaveLoadRoundTrip(t *testing.T) {
	want := filled(t)
	path := filepath.Join(t.TempDir(), "world.json")
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the world:\n got %+v\nwant %+v", got, want)
	}
}

func TestHexGathersTheLayers(t *testing.T) {
	w := filled(t)
	i := w.Grid.Index(3, 2)
	h := w.Hex(3, 2)
	if h.Col != 3 || h.Row != 2 {
		t.Errorf("Hex(3,2) reports (%d,%d)", h.Col, h.Row)
	}
	if h.Elevation != w.Layers.Elevation[i] || h.Terrain != w.Layers.Terrain[i] || h.Icy != w.Layers.Icy[i] {
		t.Errorf("Hex(3,2) = %+v, does not match layer index %d", h, i)
	}

	// A world with no layers reads as zeroes rather than panicking, so a
	// stage that runs before another has something to read.
	if got := New(3, 3).Hex(1, 1); got != (Hex{Col: 1, Row: 1}) {
		t.Errorf("empty world Hex(1,1) = %+v, want zeroes", got)
	}
}

func TestValidateRejects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(*World)
	}{
		{"unknown schema", func(w *World) { w.Schema = "marajanda.world/99" }},
		{"empty grid", func(w *World) { w.Grid.Cols = 0 }},
		{"short layer", func(w *World) { w.Layers.Elevation = w.Layers.Elevation[:2] }},
		{"terrain outside the palette", func(w *World) { w.Layers.Terrain[0] = len(w.Terrains) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := filled(t)
			tc.damage(w)
			if err := w.Validate(); err == nil {
				t.Fatal("Validate accepted it")
			}
			var buf bytes.Buffer
			if err := w.Encode(&buf); err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(&buf); err == nil {
				t.Error("Decode accepted it")
			}
		})
	}
}

func TestAllocReusesWhatFits(t *testing.T) {
	g := Grid{Cols: 4, Rows: 3}
	first := Alloc(g, []float64(nil))
	if len(first) != g.Len() {
		t.Fatalf("Alloc gave %d values, want %d", len(first), g.Len())
	}
	if second := Alloc(g, first); &second[0] != &first[0] {
		t.Error("Alloc reallocated a slice that was already the right size")
	}
	if short := Alloc(g, first[:2]); len(short) != g.Len() {
		t.Errorf("Alloc gave %d values for a short slice, want %d", len(short), g.Len())
	}
}

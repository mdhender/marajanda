package worldgen

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/mdhender/marajanda/internal/world"
)

// antipodal returns, for each hex, the hex diametrically opposite it, and
// insists that such a hex really exists.
//
// It only does for some grids. Latitudes mirror across the equator into the
// other column parity, because odd columns are pushed half a band south, so
// the antipodal column -- col + Cols/2 -- has to have the opposite parity to
// col. That needs Cols/2 to be odd, i.e. Cols = 2 (mod 4). On any other grid
// the nearest hex to a hex's antipode is up to half a band away, and a
// correlation measured against it is blurred by the sampling rather than by
// the field: 48 columns costs about 0.06 of correlation, which is enough to
// hide the very property these tests exist to pin.
func antipodal(t *testing.T, g world.Grid) []int {
	t.Helper()
	if g.Cols%4 != 2 {
		t.Fatalf("%d columns: antipodes are only exact when Cols is 2 mod 4", g.Cols)
	}
	n := g.Len()
	px, py, pz := make([]float64, n), make([]float64, n), make([]float64, n)
	for col := range g.Cols {
		for row := range g.Rows {
			i := g.Index(col, row)
			px[i], py[i], pz[i] = g.Unit(col, row)
		}
	}
	out := make([]int, n)
	for i := range n {
		best, dot := -1, -2.0
		for j := range n {
			if d := -(px[i]*px[j] + py[i]*py[j] + pz[i]*pz[j]); d > dot {
				best, dot = j, d
			}
		}
		if 1-dot > 1e-12 {
			col, row := g.ColRow(i)
			t.Fatalf("hex (%d,%d) has no exact antipode: nearest is off by %.2e", col, row, 1-dot)
		}
		out[i] = best
	}
	return out
}

func correlation(a, b []float64) float64 {
	var ma, mb float64
	for i := range a {
		ma, mb = ma+a[i], mb+b[i]
	}
	ma, mb = ma/float64(len(a)), mb/float64(len(b))
	var sab, saa, sbb float64
	for i := range a {
		da, db := a[i]-ma, b[i]-mb
		sab, saa, sbb = sab+da*db, saa+da*da, sbb+db*db
	}
	if saa == 0 || sbb == 0 {
		return 0
	}
	return sab / math.Sqrt(saa*sbb)
}

// antipodalCorrelation averages over several seeds, since one map is a sample
// and the property under test is a property of the algorithm.
func antipodalCorrelation(t *testing.T, offset float64) float64 {
	t.Helper()
	// 50 columns rather than 48 so that every hex has an exact antipode; see
	// antipodal.
	const cols, rows, seeds = 50, 24, 3
	anti := antipodal(t, world.Grid{Cols: cols, Rows: rows, WrapEastWest: true})
	var sum float64
	for seed := range uint64(seeds) {
		o := Defaults(cols, rows, seed)
		o.Offset = offset
		w, err := Fault(o)
		if err != nil {
			t.Fatal(err)
		}
		h := w.Layers.Elevation
		mirror := make([]float64, len(h))
		for i := range h {
			mirror[i] = h[anti[i]]
		}
		sum += correlation(h, mirror)
	}
	return sum / seeds
}

// TestGreatCirclesAreAntipodallySymmetric pins the pathology this generator
// exists to escape. A cut through the centre of the sphere raises one
// hemisphere and lowers the other, so it is odd under p -> -p and so is any
// sum of cuts. The field is then its own negated antipode, exactly, and every
// continent has a guaranteed ocean on the far side of the world.
//
// This is not a bug being tolerated: it is the reference behaviour, kept
// reachable so the contrast below is measured against something real.
func TestGreatCirclesAreAntipodallySymmetric(t *testing.T) {
	got := antipodalCorrelation(t, 0)
	t.Logf("offset 0.0: corr(h, antipode) = %+.3f", got)
	if got > -0.99 {
		t.Errorf("corr = %+.3f, want -1.0 or very near it: great circles should be exactly antisymmetric", got)
	}
}

// TestSmallCirclesBreakTheSymmetry is the point of the package. Displacing
// the cutting plane makes the two sides of a cut unequal, which destroys the
// oddness above.
func TestSmallCirclesBreakTheSymmetry(t *testing.T) {
	got := antipodalCorrelation(t, DefaultOffset)
	t.Logf("offset %.1f: corr(h, antipode) = %+.3f", DefaultOffset, got)
	// -0.68 is what donjon's own output measures, so clearing it means these
	// maps are less antipodally mirrored than the reference, not merely less
	// than the -1.0 above.
	if got < -0.68 {
		t.Errorf("corr = %+.3f, want better than donjon's own -0.683", got)
	}
	if got > 0 {
		t.Errorf("corr = %+.3f, suspiciously positive: the field should still lean antisymmetric", got)
	}
}

// TestNoSeamAtTheWrap is the other half of the complaint this generator
// answers. Cutting a sphere and sampling it needs no seam hidden or shifted:
// the join is as smooth as anywhere else on the map.
func TestNoSeamAtTheWrap(t *testing.T) {
	// Averaged over several seeds: the seam is 60 differences against an
	// interior of 7,000, so one map's seam is a noisy estimate and a single
	// trial would make this test flaky rather than strict.
	const cols, rows, seeds = 120, 60, 8
	var seamSum, interiorSum float64
	for seed := range uint64(seeds) {
		w, err := Fault(Defaults(cols, rows, seed))
		if err != nil {
			t.Fatal(err)
		}
		g, h := w.Grid, w.Layers.Elevation

		var seam float64
		for row := range g.Rows {
			seam += math.Abs(h[g.Index(0, row)] - h[g.Index(g.Cols-1, row)])
		}
		seamSum += seam / float64(g.Rows)

		var interior float64
		for col := range g.Cols - 1 {
			for row := range g.Rows {
				interior += math.Abs(h[g.Index(col, row)] - h[g.Index(col+1, row)])
			}
		}
		interiorSum += interior / float64((g.Cols-1)*g.Rows)
	}
	seam, interior := seamSum/seeds, interiorSum/seeds

	t.Logf("seam %.4f, interior %.4f (ratio %.2f)", seam, interior, seam/interior)
	if seam > 1.25*interior {
		t.Errorf("seam step %.4f against an interior mean of %.4f: the wrap shows", seam, interior)
	}
}

func TestFaultIsDeterministic(t *testing.T) {
	a, err := Fault(Defaults(60, 30, 42))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Fault(Defaults(60, 30, 42))
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Layers.Elevation {
		if a.Layers.Elevation[i] != b.Layers.Elevation[i] {
			t.Fatalf("same seed differs at %d: %v vs %v", i, a.Layers.Elevation[i], b.Layers.Elevation[i])
		}
	}
	if a.Grid.SeaLevel != b.Grid.SeaLevel {
		t.Errorf("same seed gave sea levels %v and %v", a.Grid.SeaLevel, b.Grid.SeaLevel)
	}

	c, err := Fault(Defaults(60, 30, 43))
	if err != nil {
		t.Fatal(err)
	}
	same := 0
	for i := range a.Layers.Elevation {
		if a.Layers.Elevation[i] == c.Layers.Elevation[i] {
			same++
		}
	}
	if same > len(a.Layers.Elevation)/10 {
		t.Errorf("seeds 42 and 43 agree on %d of %d hexes", same, len(a.Layers.Elevation))
	}
}

func TestElevationIsNormalizedAndRounded(t *testing.T) {
	w, err := Fault(Defaults(80, 40, 11))
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := 1.0, 0.0
	for _, e := range w.Layers.Elevation {
		if e < 0 || e > 1 {
			t.Fatalf("elevation %v is outside 0..1", e)
		}
		if r := math.Round(e*elevationScale) / elevationScale; r != e {
			t.Fatalf("elevation %v is not a multiple of 1/%v", e, elevationScale)
		}
		lo, hi = min(lo, e), max(hi, e)
	}
	if lo != 0 || hi != 1 {
		t.Errorf("elevation spans %v..%v, want the full 0..1", lo, hi)
	}
}

func TestOceanSetsTheSeaLevel(t *testing.T) {
	for _, ocean := range []float64{0, 0.25, 0.65, 0.9, 1} {
		o := Defaults(80, 40, 5)
		o.Ocean = ocean
		w, err := Fault(o)
		if err != nil {
			t.Fatal(err)
		}
		wet := 0
		for col := range w.Grid.Cols {
			for row := range w.Grid.Rows {
				if w.IsWater(col, row) {
					wet++
				}
			}
		}
		got := float64(wet) / float64(w.Grid.Len())
		if math.Abs(got-ocean) > 0.02 {
			t.Errorf("ocean %.2f gave %.3f of the world under water", ocean, got)
		}
	}
}

func TestNoFaultsIsFlat(t *testing.T) {
	o := Defaults(20, 10, 1)
	o.Faults = 0
	w, err := Fault(o)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range w.Layers.Elevation {
		if e != 0.5 {
			t.Fatalf("hex %d of an uncut world is at %v, want 0.5", i, e)
		}
	}
}

func TestOptionsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"no columns", Options{Cols: 0, Rows: 10}},
		{"no rows", Options{Cols: 10, Rows: 0}},
		{"too many hexes", Options{Cols: 1 << 12, Rows: 1 << 12}},
		{"odd columns cannot wrap", Options{Cols: 11, Rows: 10}},
		{"negative faults", Options{Cols: 10, Rows: 10, Faults: -1}},
		{"offset at 1", Options{Cols: 10, Rows: 10, Offset: 1}},
		{"negative offset", Options{Cols: 10, Rows: 10, Offset: -0.1}},
		{"ocean above 1", Options{Cols: 10, Rows: 10, Ocean: 1.5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Fault(tc.opts); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// TestGeneratedWorldIsStorable is the handoff to the datastore: what comes out
// of here has to satisfy the thing that stores it.
func TestGeneratedWorldIsStorable(t *testing.T) {
	w, err := Fault(Defaults(60, 30, 9))
	if err != nil {
		t.Fatal(err)
	}
	w.Name = "Eglar"
	if err := w.Validate(); err != nil {
		t.Fatalf("a generated world does not validate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "world.json")
	if err := w.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := world.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generator != Name || got.Seed != 9 || got.Grid.SeaLevel != w.Grid.SeaLevel {
		t.Errorf("provenance did not survive the round trip: %+v", got)
	}
	for i := range w.Layers.Elevation {
		if got.Layers.Elevation[i] != w.Layers.Elevation[i] {
			t.Fatalf("elevation %d changed: %v -> %v", i, w.Layers.Elevation[i], got.Layers.Elevation[i])
		}
	}
}

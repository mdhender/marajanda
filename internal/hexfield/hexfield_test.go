package hexfield

import (
	"math"
	"testing"

	"github.com/mdhender/marjanda/internal/hexgrid"
)

// The refinement claim: the level-k lattice inside a radius-2**K hexagon
// refines exactly, with no point missed and no point written twice. If this
// holds, the seven seed points expand to fill the field completely.
func TestRefinementIsExact(t *testing.T) {
	for levels := 1; levels <= 6; levels++ {
		f := Generate(Params{Levels: levels, Seed: 7})

		unset := 0
		count := 0
		for c, v := range f.All() {
			count++
			if math.IsNaN(v) {
				unset++
				continue
			}
			if !c.Valid() {
				t.Fatalf("levels=%d: %v violates Q+R+S == 0", levels, c)
			}
		}
		if unset != 0 {
			t.Errorf("levels=%d: %d of %d hexes left unset", levels, unset, count)
		}
		if want := f.Len(); count != want {
			t.Errorf("levels=%d: walked %d hexes, want %d", levels, count, want)
		}
	}
}

// The hexagon counts quoted in the package docs: 7, 19, 61, 217, 817.
func TestHexagonCounts(t *testing.T) {
	want := []int{7, 19, 61, 217, 817}
	for levels, n := range want {
		if got := New(levels).Len(); got != n {
			t.Errorf("levels=%d: got %d hexes, want %d", levels, got, n)
		}
	}
}

// Midpoints of adjacent level-k points must land on the level-(k-1) lattice.
func TestMidpointsLandOnFinerLattice(t *testing.T) {
	const maxK = 5
	for k := maxK; k >= 1; k-- {
		step, half := 1<<k, 1<<(k-1)
		for i := range hexgrid.Forward {
			d := hexgrid.Directions[i]
			a := hexgrid.Coord{Q: 2 * step, R: -2 * step, S: 0}
			b := a.Add(d.Scale(step))
			m := a.Add(d.Scale(half))

			if !m.Valid() {
				t.Fatalf("k=%d dir=%v: midpoint %v violates the cube invariant", k, d, m)
			}
			if got := m.Lattice(maxK); got < k-1 {
				t.Errorf("k=%d dir=%v: midpoint %v is on lattice %d, want >= %d", k, d, m, got, k-1)
			}
			if mid := (hexgrid.Coord{Q: (a.Q + b.Q) / 2, R: (a.R + b.R) / 2, S: (a.S + b.S) / 2}); mid != m {
				t.Errorf("k=%d dir=%v: %v is not the midpoint of %v and %v", k, d, m, a, b)
			}
		}
	}
}

// The three forward directions must cover all six as negations, so every
// lattice edge is visited exactly once.
func TestForwardDirectionsCoverAllEdges(t *testing.T) {
	seen := map[hexgrid.Coord]bool{}
	for i := range hexgrid.Forward {
		d := hexgrid.Directions[i]
		seen[d] = true
		seen[d.Scale(-1)] = true
	}
	if len(seen) != 6 {
		t.Fatalf("hexgrid.Forward directions and their negations cover %d directions, want 6", len(seen))
	}
	for _, d := range hexgrid.Directions {
		if !seen[d] {
			t.Errorf("direction %v is never visited", d)
		}
	}
}

// The apexes found from the six-cycle must be genuine lattice neighbours of
// both edge endpoints, or the four-point stencil is reading the wrong hexes.
func TestApexesAreAdjacentToBothEndpoints(t *testing.T) {
	const step = 4
	a := hexgrid.Origin
	for i := range hexgrid.Forward {
		b := a.Add(hexgrid.Directions[i].Scale(step))
		for _, j := range []int{(i + 1) % 6, (i + 5) % 6} {
			apex := a.Add(hexgrid.Directions[j].Scale(step))
			if d := a.Distance(apex); d != step {
				t.Errorf("dir %d apex %v: distance to %v is %d, want %d", i, apex, a, d, step)
			}
			if d := b.Distance(apex); d != step {
				t.Errorf("dir %d apex %v: distance to %v is %d, want %d", i, apex, b, d, step)
			}
		}
	}
}

// Hashed noise means the field depends only on the seed, not on traversal.
func TestDeterministic(t *testing.T) {
	p := Params{Levels: 5, Hurst: 0.7, Seed: 12345, Relax: true, SRA: true}
	a, b := Generate(p), Generate(p)
	for c, v := range a.All() {
		if w := b.At(c); v != w {
			t.Fatalf("%v: %v != %v across runs with the same seed", c, v, w)
		}
	}

	p.Seed++
	if same, total := 0, 0; true {
		other := Generate(p)
		for c, v := range a.All() {
			total++
			if v == other.At(c) {
				same++
			}
		}
		if same == total {
			t.Error("changing the seed produced an identical field")
		}
	}
}

// Higher H must produce smoother terrain. Measured as mean absolute
// difference between neighbours, which is the thing H actually controls.
func TestHurstControlsRoughness(t *testing.T) {
	rough := neighbourVariation(Generate(Params{Levels: 6, Hurst: 0.3, Seed: 99, Relax: true, SRA: true}))
	smooth := neighbourVariation(Generate(Params{Levels: 6, Hurst: 1.0, Seed: 99, Relax: true, SRA: true}))
	if !(smooth < rough) {
		t.Errorf("H=1.0 variation %.4f is not below H=0.3 variation %.4f", smooth, rough)
	}
}

func neighbourVariation(f *Field) float64 {
	f.Normalize()
	sum, n := 0.0, 0
	for c, v := range f.All() {
		for i := range hexgrid.Forward {
			if o := c.Add(hexgrid.Directions[i]); f.Has(o) {
				sum += math.Abs(v - f.At(o))
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// creaseRatio measures how far points fixed at coarse levels deviate from
// their neighbourhood, relative to points inserted at the finest level. A
// field with no creasing scores near 1: whether a point was written early or
// late should leave no trace in the surface. Above 1 the old points spike;
// below 1 they sit in flat spots.
func creaseRatio(f *Field) float64 {
	f.Normalize()
	var cs, fs float64
	var cn, fn int

	for c, v := range f.All() {
		var sum float64
		var n int
		for _, d := range hexgrid.Directions {
			if o := c.Add(d); f.Has(o) {
				sum += f.At(o)
				n++
			}
		}
		if n < 6 {
			continue // the rim has no neighbourhood to deviate from
		}
		dev := math.Abs(v - sum/float64(n))
		switch {
		case c.Lattice(f.Levels) >= f.Levels-2:
			cs, cn = cs+dev, cn+1
		case c.Lattice(f.Levels) == 0:
			fs, fn = fs+dev, fn+1
		}
	}
	if cn == 0 || fn == 0 || fs == 0 {
		return 1
	}
	return (cs / float64(cn)) / (fs / float64(fn))
}

// Relax and SRA each fail on their own and in opposite directions: relax
// over-smooths the points it repositions, SRA spikes them. Together they
// cancel, which is why both default to on.
func TestRelaxAndSRATogetherReduceCreasing(t *testing.T) {
	mean := func(relax, sra bool) float64 {
		var sum float64
		const trials = 24 // fewer than ~16 and the seeds do not separate the variants
		for seed := range trials {
			sum += creaseRatio(Generate(Params{
				Levels: 7, Hurst: 0.7, Seed: uint64(seed) + 1,
				Stencil: Loop, Relax: relax, SRA: sra,
			}))
		}
		return sum / trials
	}
	off := func(v float64) float64 { return math.Abs(v - 1) }

	bare, sraOnly, relaxOnly, both := mean(false, false), mean(false, true), mean(true, false), mean(true, true)

	if !(off(both) < off(bare)) {
		t.Errorf("relax+SRA ratio %.3f is no closer to 1 than bare %.3f", both, bare)
	}
	if !(off(both) < off(sraOnly)) {
		t.Errorf("relax+SRA ratio %.3f is no closer to 1 than SRA alone %.3f", both, sraOnly)
	}
	if !(off(both) < off(relaxOnly)) {
		t.Errorf("relax+SRA ratio %.3f is no closer to 1 than relax alone %.3f", both, relaxOnly)
	}
	if !(sraOnly > 1) {
		t.Errorf("SRA alone should spike old points, ratio %.3f", sraOnly)
	}
	if !(relaxOnly < 1) {
		t.Errorf("relax alone should over-smooth old points, ratio %.3f", relaxOnly)
	}
	if !(relaxOnly < both && both < sraOnly) {
		t.Errorf("combined ratio %.3f should fall between relax alone %.3f and SRA alone %.3f",
			both, relaxOnly, sraOnly)
	}
}

// The two stencils must actually differ, or the four-point mask is not being
// applied where it is claimed to be.
func TestStencilsDiffer(t *testing.T) {
	p := Params{Levels: 5, Hurst: 0.7, Seed: 4242}
	p.Stencil = Midpoint
	a := Generate(p)
	p.Stencil = Loop
	b := Generate(p)

	for c, v := range a.All() {
		if v != b.At(c) {
			return
		}
	}
	t.Error("loop and midpoint stencils produced identical fields")
}

func TestNormalizeSpansUnitRange(t *testing.T) {
	f := Generate(Params{Levels: 5, Seed: 3})
	f.Normalize()
	lo, hi := f.Range()
	if math.Abs(lo) > 1e-12 || math.Abs(hi-1) > 1e-12 {
		t.Errorf("normalized range is [%v,%v], want [0,1]", lo, hi)
	}
}

func TestIslandSeedPutsHighGroundAtTheCentre(t *testing.T) {
	f := Generate(Params{Levels: 6, Hurst: 0.9, Seed: 8, Relax: true, SRA: true, Island: true})
	f.Normalize()

	var inner, outer, ni, no float64
	for c, v := range f.All() {
		if c.Length() <= f.Radius/4 {
			inner, ni = inner+v, ni+1
		} else if c.Length() > 3*f.Radius/4 {
			outer, no = outer+v, no+1
		}
	}
	if inner/ni <= outer/no {
		t.Errorf("island centre mean %.3f is not above rim mean %.3f", inner/ni, outer/no)
	}
}

// Out-of-bounds reads must not panic or alias a real hex.
func TestOutOfBoundsIsSafe(t *testing.T) {
	f := New(3)
	for _, c := range []hexgrid.Coord{
		{Q: 99, R: -99, S: 0},
		{Q: -50, R: 50, S: 0},
		{Q: 1, R: 1, S: 1}, // violates the cube invariant
	} {
		if f.Contains(c) {
			t.Errorf("%v reported in bounds", c)
		}
		if !math.IsNaN(f.At(c)) {
			t.Errorf("%v returned a height", c)
		}
		f.Set(c, 1) // must be ignored, not panic
		if f.Has(c) {
			t.Errorf("%v accepted a write", c)
		}
	}
}

package noise

import (
	"cmp"
	"math"
	"slices"
	"testing"
)

var kinds = []struct {
	name string
	k    Kind
}{
	{"simplex", Simplex},
	{"perlin", Perlin},
	{"value", Value},
}

// samples walks a patch of the plane on an irrational step, so the samples do
// not land on the lattice and are not periodic with it.
func samples(n int, fn func(x, y float64)) {
	const step = 0.31830988618 // 1/pi
	for i := range n {
		for j := range n {
			fn(float64(i)*step-8, float64(j)*step-8)
		}
	}
}

// Every kind must stay inside [-1,1]: the palettes and the fractal sum both
// take that as given, and a kind whose scale constant is wrong shows up as
// clipping at one end of the ramp rather than as an error.
func TestEvalStaysInRange(t *testing.T) {
	s := New(7)
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			lo, hi := math.Inf(1), math.Inf(-1)
			samples(120, func(x, y float64) {
				v := s.Eval(k.k, x, y)
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			})
			if lo < -1 || hi > 1 {
				t.Errorf("range [%v,%v] leaves [-1,1]", lo, hi)
			}
			// A kind that never gets near the ends is scaled too small, which
			// costs contrast everywhere downstream.
			if hi < 0.5 || lo > -0.5 {
				t.Errorf("range [%v,%v] uses too little of [-1,1]", lo, hi)
			}
		})
	}
}

// Noise with a mean far from zero biases every fractal sum built on it.
func TestEvalIsCentred(t *testing.T) {
	s := New(11)
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			sum, n := 0.0, 0
			samples(120, func(x, y float64) {
				sum += s.Eval(k.k, x, y)
				n++
			})
			if mean := sum / float64(n); math.Abs(mean) > 0.05 {
				t.Errorf("mean %v, want about 0", mean)
			}
		})
	}
}

// The field has to be continuous or the terrain is speckle. The bound is a
// Lipschitz constant, not a tolerance: gradient noise has bounded slope, so a
// step this small cannot produce a jump this large unless the lattice
// arithmetic is wrong somewhere -- which is exactly what a sign error in the
// corner selection or an unmasked negative index looks like.
func TestEvalIsContinuous(t *testing.T) {
	s := New(3)
	const eps = 1e-4
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			worst := 0.0
			samples(90, func(x, y float64) {
				a := s.Eval(k.k, x, y)
				worst = math.Max(worst, math.Abs(s.Eval(k.k, x+eps, y)-a))
				worst = math.Max(worst, math.Abs(s.Eval(k.k, x, y+eps)-a))
			})
			if worst > 20*eps {
				t.Errorf("largest jump over a %v step was %v", eps, worst)
			}
		})
	}
}

// Perlin is identically zero at every lattice point: the offsets all vanish
// there, so every gradient dots with nothing. It is worth pinning because it
// is the reason Fractal offsets its octaves -- without that, the zeros stack
// and the origin becomes a dead spot at every scale at once.
func TestPerlinVanishesOnTheLattice(t *testing.T) {
	s := New(5)
	for i := -4; i <= 4; i++ {
		for j := -4; j <= 4; j++ {
			if v := s.Eval(Perlin, float64(i), float64(j)); v != 0 {
				t.Fatalf("perlin(%d,%d) = %v, want exactly 0", i, j, v)
			}
		}
	}
}

// Simplex has a lattice too -- every gradient noise is zero at its own
// vertices, where there is no offset for the gradient to dot with -- but its
// lattice is triangular rather than the integer grid. The skew vanishes when
// i+j == 0, so exactly one diagonal of integer points lands on a vertex and
// the rest of the grid stays live. That is what it means for simplex to have
// no square grain for the map to inherit.
func TestSimplexLatticeIsTriangularNotSquare(t *testing.T) {
	s := New(5)
	const n = 20
	for i := -n; i <= n; i++ {
		if v := s.Eval(Simplex, float64(i), float64(-i)); v != 0 {
			t.Fatalf("simplex(%d,%d) = %v, want 0: that point is a lattice vertex", i, -i, v)
		}
	}

	// Off that diagonal an occasional exact zero is a gradient landing
	// perpendicular to the offset, which the twelve-gradient table makes
	// likely enough to see. It is scattered, not a lattice.
	zeros, total := 0, 0
	for i := -n; i <= n; i++ {
		for j := -n; j <= n; j++ {
			if i+j == 0 {
				continue
			}
			total++
			if s.Eval(Simplex, float64(i), float64(j)) == 0 {
				zeros++
			}
		}
	}
	if frac := float64(zeros) / float64(total); frac > 0.1 {
		t.Errorf("%.1f%% of off-diagonal integer points are exactly zero; expected a diagonal, not a grid", 100*frac)
	}
}

func TestSameSeedSameField(t *testing.T) {
	a, b := New(9001), New(9001)
	for _, k := range kinds {
		samples(40, func(x, y float64) {
			if va, vb := a.Eval(k.k, x, y), b.Eval(k.k, x, y); va != vb {
				t.Fatalf("%s(%v,%v): %v vs %v from the same seed", k.name, x, y, va, vb)
			}
		})
	}
}

func TestDifferentSeedsDifferentField(t *testing.T) {
	a, b := New(1), New(2)
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			same := true
			samples(40, func(x, y float64) {
				if a.Eval(k.k, x, y) != b.Eval(k.k, x, y) {
					same = false
				}
			})
			if same {
				t.Error("seeds 1 and 2 produced the same field")
			}
		})
	}
}

// Small sequential seeds are the ones a sweep actually uses, and the reason
// the source is ChaCha8 rather than PCG. Neighbouring seeds must be as
// unrelated as distant ones.
func TestSequentialSeedsAreUncorrelated(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		a, b := New(seed), New(seed+1)
		var sa, sb, sab, saa, sbb float64
		n := 0.0
		samples(60, func(x, y float64) {
			va, vb := a.Eval(Simplex, x, y), b.Eval(Simplex, x, y)
			sa, sb = sa+va, sb+vb
			sab, saa, sbb = sab+va*vb, saa+va*va, sbb+vb*vb
			n++
		})
		cov := sab/n - (sa/n)*(sb/n)
		den := math.Sqrt((saa/n - (sa/n)*(sa/n)) * (sbb/n - (sb/n)*(sb/n)))
		if den > 0 && math.Abs(cov/den) > 0.2 {
			t.Errorf("seeds %d and %d correlate at %v", seed, seed+1, cov/den)
		}
	}
}

// The fractal sum normalizes by total amplitude, so the range does not depend
// on the octave count. If it did, changing octaves would move the coastline.
func TestFractalRangeIsIndependentOfOctaves(t *testing.T) {
	s := New(42)
	for _, shape := range []Shape{FBM, Ridged, Billow} {
		for _, oct := range []int{1, 4, 12} {
			p := Fractal{Kind: Simplex, Octaves: oct, Frequency: 2, Lacunarity: 2, Gain: 0.5, Shape: shape}
			lo, hi := math.Inf(1), math.Inf(-1)
			samples(80, func(x, y float64) {
				v := s.At(p, x, y)
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			})
			if lo < -1.0001 || hi > 1.0001 {
				t.Errorf("shape %v, %d octaves: range [%v,%v] leaves [-1,1]", shape, oct, lo, hi)
			}
		}
	}
}

// Octaves are offset from one another so their lattice features do not stack.
// With Perlin, which is zero on its whole lattice, an unoffset sum is zero at
// the origin however many octaves it has.
func TestOctavesAreOffsetFromEachOther(t *testing.T) {
	s := New(17)
	p := Fractal{Kind: Perlin, Octaves: 6, Frequency: 2, Lacunarity: 2, Gain: 0.5}
	if v := s.At(p, 0, 0); v == 0 {
		t.Error("the fractal sum is exactly zero at the origin; octaves share a lattice")
	}
}

// Warping displaces the sample point, so it must change the field -- and it
// must not push it out of range.
func TestWarpChangesTheFieldWithoutLeavingRange(t *testing.T) {
	s := New(23)
	flat := Fractal{Kind: Simplex, Octaves: 4, Frequency: 3, Lacunarity: 2, Gain: 0.5}
	bent := flat
	bent.Warp = 0.6

	moved, lo, hi := 0, math.Inf(1), math.Inf(-1)
	samples(60, func(x, y float64) {
		a, b := s.At(flat, x, y), s.At(bent, x, y)
		if math.Abs(a-b) > 1e-9 {
			moved++
		}
		lo, hi = math.Min(lo, b), math.Max(hi, b)
	})
	if moved == 0 {
		t.Error("warp changed nothing")
	}
	if lo < -1.0001 || hi > 1.0001 {
		t.Errorf("warped range [%v,%v] leaves [-1,1]", lo, hi)
	}
}

// corr is the Pearson correlation of two paired series.
func corr(a, b []float64) float64 {
	n := float64(len(a))
	var sa, sb, sab, saa, sbb float64
	for i := range a {
		sa, sb = sa+a[i], sb+b[i]
		sab, saa, sbb = sab+a[i]*b[i], saa+a[i]*a[i], sbb+b[i]*b[i]
	}
	cov := sab/n - (sa/n)*(sb/n)
	den := math.Sqrt((saa/n - (sa/n)*(sa/n)) * (sbb/n - (sb/n)*(sb/n)))
	if den == 0 {
		return 0
	}
	return cov / den
}

// Ridged and billow both fold the noise at zero; the difference is which way
// up. One octave is enough to pin that, and it is the whole definition of the
// two shapes: ridged is highest where the underlying noise crosses zero --
// a set of curves, hence chains -- and billow is highest at its extremes,
// which are isolated points, hence lumps.
func TestShapesFoldAtZero(t *testing.T) {
	s := New(31)
	const freq = 3
	base := Fractal{Kind: Simplex, Octaves: 1, Frequency: freq, Lacunarity: 2, Gain: 0.5}

	var mag, ridged, billow []float64
	samples(80, func(x, y float64) {
		// The first octave is offset, so sample the underlying noise where
		// the sum actually reads it.
		mag = append(mag, math.Abs(s.Eval(Simplex, x*freq+s.off[0][0], y*freq+s.off[0][1])))

		r, b := base, base
		r.Shape, b.Shape = Ridged, Billow
		ridged = append(ridged, s.At(r, x, y))
		billow = append(billow, s.At(b, x, y))
	})

	if c := corr(mag, ridged); c > -0.9 {
		t.Errorf("ridged against |noise| correlates at %v, want about -1", c)
	}
	if c := corr(mag, billow); c < 0.99 {
		t.Errorf("billow against |noise| correlates at %v, want about +1", c)
	}
}

// The point of the ridged weighting is that fine octaves only reach the parts
// of the map that already have relief, so the valleys stay smooth and the
// detail piles onto the chains. Measured as the roughness at the finest
// octave's scale, high ground must be markedly rougher than low ground --
// and for fBm, which weights nothing, the two must be about equal.
func TestRidgedDetailFollowsRelief(t *testing.T) {
	const freq, lac, oct = 2.0, 2.0, 6
	fine := 0.5 / (freq * math.Pow(lac, oct-1)) // half the finest wavelength

	roughnessRatio := func(shape Shape) float64 {
		s := New(77)
		p := Fractal{Kind: Simplex, Octaves: oct, Frequency: freq, Lacunarity: lac, Gain: 0.5, Shape: shape}

		type sample struct{ v, rough float64 }
		var all []sample
		samples(70, func(x, y float64) {
			v := s.At(p, x, y)
			r := math.Abs(s.At(p, x+fine, y)-v) + math.Abs(s.At(p, x, y+fine)-v)
			all = append(all, sample{v, r})
		})
		slices.SortFunc(all, func(a, b sample) int { return cmp.Compare(a.v, b.v) })

		q := len(all) / 4
		var lo, hi float64
		for i := range q {
			lo += all[i].rough
			hi += all[len(all)-1-i].rough
		}
		return hi / lo
	}

	if got := roughnessRatio(Ridged); got < 1.5 {
		t.Errorf("ridged high ground is only %.2fx as rough as low ground; the octave weighting is not biting", got)
	}
	if got := roughnessRatio(FBM); got < 0.6 || got > 1.6 {
		t.Errorf("fBm roughness ratio %.2f; unweighted octaves should be about even", got)
	}
}

// Zero and negative inputs must not panic on a negative array index, which is
// the classic way a hand-written permutation lookup fails.
func TestNegativeCoordinatesAreFine(t *testing.T) {
	s := New(0)
	for _, k := range kinds {
		for _, p := range [][2]float64{{-1e6, -1e6}, {-0.5, -0.5}, {0, 0}, {-3, 7}} {
			if v := s.Eval(k.k, p[0], p[1]); math.IsNaN(v) {
				t.Errorf("%s(%v) = NaN", k.name, p)
			}
		}
	}
}

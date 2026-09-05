// Copyright (c) 2026 Michael D Henderson.

package cylinder_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/cylinder"
)

// sizes are odd and at least 3, which is every width [cylinder.New] accepts.
var sizes = []int{3, 5, 7, 11, 21}

func mustNew(t *testing.T, columns int) cylinder.Cylinder {
	t.Helper()
	c, err := cylinder.New(columns)
	if err != nil {
		t.Fatalf("New(%d): %v", columns, err)
	}
	return c
}

// referenceDistance is an independent wrapped distance, deliberately not built
// from anything in the package under test. It brute-forces several laps in each
// direction rather than reasoning about how many can matter.
func referenceDistance(columns int, a, b hexg.Hex) int {
	best := -1
	for lap := -3; lap <= 3; lap++ {
		d := a.Distance(hexg.NewHex(b.Q()+lap*columns, b.R()))
		if best < 0 || d < best {
			best = d
		}
	}
	return best
}

// window returns every canonical hex in the given row span.
func window(columns, rows int) []hexg.Hex {
	half := (columns - 1) / 2
	var hexes []hexg.Hex
	for r := -rows; r <= rows; r++ {
		for q := -half; q <= half; q++ {
			hexes = append(hexes, hexg.NewHex(q, r))
		}
	}
	return hexes
}

func TestNewRejectsBadWidths(t *testing.T) {
	for _, tc := range []struct {
		columns int
		want    error
	}{
		{-1, cylinder.ErrTooFewColumns},
		{0, cylinder.ErrTooFewColumns},
		{1, cylinder.ErrTooFewColumns},
		{2, cylinder.ErrTooFewColumns}, // too few is reported before even
		{4, cylinder.ErrEvenColumns},
		{510, cylinder.ErrEvenColumns},
	} {
		if _, err := cylinder.New(tc.columns); !errors.Is(err, tc.want) {
			t.Errorf("New(%d) error = %v, want %v", tc.columns, err, tc.want)
		}
	}
}

func TestNewAcceptsOddWidths(t *testing.T) {
	for _, columns := range []int{3, 5, 511} {
		c, err := cylinder.New(columns)
		if err != nil {
			t.Fatalf("New(%d): %v", columns, err)
		}
		if got := c.Columns(); got != columns {
			t.Errorf("New(%d).Columns() = %d", columns, got)
		}
		if got, want := c.HalfWidth(), (columns-1)/2; got != want {
			t.Errorf("New(%d).HalfWidth() = %d, want %d", columns, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		half := c.HalfWidth()

		distinct := make(map[hexg.Hex]struct{})
		for q := -3 * columns; q <= 3*columns; q++ {
			h := hexg.NewHex(q, 0)
			got := c.Normalize(h)

			if got.Q() < -half || got.Q() > half {
				t.Errorf("columns=%d: Normalize(q=%d) = %d, outside [-%d,%d]",
					columns, q, got.Q(), half, half)
			}
			if got.R() != h.R() {
				t.Errorf("columns=%d: Normalize(q=%d) changed the row", columns, q)
			}
			if again := c.Normalize(got); again != got {
				t.Errorf("columns=%d: Normalize not idempotent at q=%d", columns, q)
			}
			if !c.IsCanonical(got) {
				t.Errorf("columns=%d: Normalize(q=%d) is not canonical", columns, q)
			}
			if lap := c.Normalize(hexg.NewHex(q+columns, 0)); lap != got {
				t.Errorf("columns=%d: a full lap is not the identity at q=%d", columns, q)
			}
			distinct[got] = struct{}{}
		}
		if len(distinct) != columns {
			t.Errorf("columns=%d: got %d distinct canonical hexes, want %d",
				columns, len(distinct), columns)
		}
	}
}

func TestDistance(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		hexes := window(columns, 4)

		for _, a := range hexes {
			for _, b := range hexes {
				got, want := c.Distance(a, b), referenceDistance(columns, a, b)
				if got != want {
					t.Fatalf("columns=%d: Distance(%v,%v) = %d, want %d", columns, a, b, got, want)
				}
				if back := c.Distance(b, a); back != got {
					t.Fatalf("columns=%d: Distance not symmetric for %v,%v", columns, a, b)
				}
				if (got == 0) != (a == b) {
					t.Fatalf("columns=%d: Distance(%v,%v) = 0 but hexes differ", columns, a, b)
				}
			}
		}
	}
}

func TestDistanceTriangleInequality(t *testing.T) {
	c := mustNew(t, 11)
	hexes := window(11, 2)
	for _, a := range hexes {
		for _, b := range hexes {
			for _, m := range hexes {
				if direct, viaM := c.Distance(a, b), c.Distance(a, m)+c.Distance(m, b); direct > viaM {
					t.Fatalf("triangle inequality broken: d(%v,%v)=%d > %d via %v", a, b, direct, viaM, m)
				}
			}
		}
	}
}

func TestNearest(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		hexes := window(columns, 3)
		for _, origin := range hexes {
			for _, h := range hexes {
				near := c.Nearest(origin, h)

				// It must name the same hex...
				if c.Normalize(near) != c.Normalize(h) {
					t.Fatalf("columns=%d: Nearest(%v,%v) = %v names a different hex",
						columns, origin, h, near)
				}
				// ...and be the closest copy, measured on the plane.
				if got, want := origin.Distance(near), referenceDistance(columns, origin, h); got != want {
					t.Fatalf("columns=%d: Nearest(%v,%v) is %d away, want %d",
						columns, origin, h, got, want)
				}
			}
		}
	}
}

func TestNearestIsWhatRenderingNeeds(t *testing.T) {
	// A player standing against the seam. Their eastern neighbour must draw one
	// hex east, not most of a world away.
	c := mustNew(t, 511)
	player := hexg.NewHex(255, 0) // the eastmost canonical column
	east := c.Neighbor(player, 0)

	if got := c.Normalize(east); got.Q() != -255 {
		t.Fatalf("east of the seam normalized to q=%d, want -255", got.Q())
	}
	if got := c.Nearest(player, east); got.Q() != 256 {
		t.Fatalf("Nearest put the eastern neighbour at q=%d, want 256", got.Q())
	}
	if got := c.Distance(player, east); got != 1 {
		t.Fatalf("distance across the seam = %d, want 1", got)
	}
}

func TestNeighbor(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		for _, h := range window(columns, 2) {
			seen := make(map[hexg.Hex]struct{})
			for direction := range 6 {
				n := c.Neighbor(h, direction)
				if !c.IsCanonical(n) {
					t.Fatalf("columns=%d: Neighbor(%v,%d) is not canonical", columns, h, direction)
				}
				if got := c.Distance(h, n); got != 1 {
					t.Fatalf("columns=%d: Neighbor(%v,%d) is %d away", columns, h, direction, got)
				}
				seen[n] = struct{}{}
				// Stepping back the opposite way returns home.
				if back := c.Neighbor(n, direction+3); back != c.Normalize(h) {
					t.Fatalf("columns=%d: %v -> %d -> back gave %v", columns, h, direction, back)
				}
			}
			if len(seen) != 6 {
				t.Fatalf("columns=%d: %v has %d distinct neighbours, want 6", columns, h, len(seen))
			}
		}
	}
}

func TestRingMatchesDistance(t *testing.T) {
	// Radii deliberately sweep past HalfWidth, where the cheap planar path stops
	// being exact and the implementation switches strategies. The switch must be
	// invisible.
	for _, columns := range sizes {
		c := mustNew(t, columns)
		center := hexg.NewHex(0, 0)

		for radius := range 2*columns + 2 {
			got := c.Ring(center, radius)

			var want []hexg.Hex
			for _, h := range window(columns, radius+1) {
				if referenceDistance(columns, center, h) == radius {
					want = append(want, h)
				}
			}
			slices.SortFunc(want, hexg.Hex.Compare)

			if !slices.Equal(got, want) {
				t.Fatalf("columns=%d radius=%d: Ring = %v, want %v", columns, radius, got, want)
			}
			if !slices.IsSortedFunc(got, hexg.Hex.Compare) {
				t.Fatalf("columns=%d radius=%d: Ring is not sorted", columns, radius)
			}
			for i := 1; i < len(got); i++ {
				if got[i] == got[i-1] {
					t.Fatalf("columns=%d radius=%d: Ring has duplicates", columns, radius)
				}
			}
		}
	}
}

func TestSpiral(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		center := hexg.NewHex(1, -1)

		for radius := range columns + 2 {
			got := c.Spiral(center, radius)

			seen := make(map[hexg.Hex]struct{}, len(got))
			for _, h := range got {
				if _, dup := seen[h]; dup {
					t.Fatalf("columns=%d radius=%d: Spiral repeats %v", columns, radius, h)
				}
				seen[h] = struct{}{}
				if d := c.Distance(center, h); d > radius {
					t.Fatalf("columns=%d radius=%d: Spiral holds %v at distance %d", columns, radius, h, d)
				}
			}

			total := 0
			for k := 0; k <= radius; k++ {
				total += len(c.Ring(center, k))
			}
			if len(got) != total {
				t.Fatalf("columns=%d radius=%d: Spiral has %d hexes, rings total %d",
					columns, radius, len(got), total)
			}
		}
	}
}

func TestLineDraw(t *testing.T) {
	for _, columns := range sizes {
		c := mustNew(t, columns)
		hexes := window(columns, 3)
		for _, a := range hexes {
			for _, b := range hexes {
				line := c.LineDraw(a, b)

				if line[0] != c.Normalize(a) {
					t.Fatalf("columns=%d: LineDraw(%v,%v) starts at %v", columns, a, b, line[0])
				}
				if line[len(line)-1] != c.Normalize(b) {
					t.Fatalf("columns=%d: LineDraw(%v,%v) ends at %v", columns, a, b, line[len(line)-1])
				}
				if want := c.Distance(a, b) + 1; len(line) != want {
					t.Fatalf("columns=%d: LineDraw(%v,%v) has %d hexes, want %d",
						columns, a, b, len(line), want)
				}
				for i := 1; i < len(line); i++ {
					if d := c.Distance(line[i-1], line[i]); d != 1 {
						t.Fatalf("columns=%d: LineDraw(%v,%v) jumps %d at step %d",
							columns, a, b, d, i)
					}
				}
			}
		}
	}
}

func TestMarajandaWorld(t *testing.T) {
	// The shape issue #16 settles: --width 255 -> 511 columns.
	const halfWidth = 255
	c := mustNew(t, 2*halfWidth+1)

	if got := c.Columns(); got != 511 {
		t.Fatalf("Columns() = %d, want 511", got)
	}
	if got := c.HalfWidth(); got != halfWidth {
		t.Fatalf("HalfWidth() = %d, want %d", got, halfWidth)
	}

	// Every canonical column, exactly once, symmetric about the origin.
	distinct := make(map[int]struct{})
	for q := -2 * 511; q <= 2*511; q++ {
		n := c.Normalize(hexg.NewHex(q, 0)).Q()
		if n < -halfWidth || n > halfWidth {
			t.Fatalf("Normalize(q=%d) = %d, outside [-255,255]", q, n)
		}
		distinct[n] = struct{}{}
	}
	if len(distinct) != 511 {
		t.Fatalf("got %d distinct columns, want 511", len(distinct))
	}

	// Four hexes west of the origin on a 7-column world is three hexes east:
	// the worked example from the issue thread.
	small := mustNew(t, 7)
	if got := small.Normalize(hexg.NewHex(-4, 0)); got != hexg.NewHex(3, 0) {
		t.Fatalf("on 7 columns, four west of origin = %v, want (3,0,-3)", got)
	}
}

func TestNegativeRadiusPanics(t *testing.T) {
	c := mustNew(t, 11)
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"Ring", func() { c.Ring(hexg.NewHex(0, 0), -1) }},
		{"Spiral", func() { c.Spiral(hexg.NewHex(0, 0), -1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("%s(-1) did not panic", tc.name)
				}
			}()
			tc.call()
		})
	}
}

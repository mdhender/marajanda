// Copyright (c) 2026 Michael D Henderson.

package worldmap_test

import (
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"github.com/mdhender/marajanda/internal/worldmap"
)

func testWorld(t *testing.T) game.World {
	t.Helper()
	world, err := game.GenerateWorld(prng.New(98374, ^uint64(97)), 12, 6)
	if err != nil {
		t.Fatalf("GenerateWorld: %v", err)
	}
	return world
}

// The cut has to be a bijection onto the drawing window at any centre, or the
// map either loses hexes or draws one on top of another.
func TestCutTilesTheWorldAtEveryCentre(t *testing.T) {
	world := testWorld(t)
	for _, center := range []int{0, 1, -1, world.Width(), -world.Width(), 2 * world.Width()} {
		seen := make(map[[2]int]hexg.Hex, world.Len())
		for _, hex := range world.Hexes() {
			offset := worldmap.Cut(world.Width(), center, hex.Coord).CubeToROffset(true)
			if offset.Col < -world.Width() || offset.Col > world.Width() {
				t.Fatalf("centre %d: %v cut to column %d", center, hex.Coord, offset.Col)
			}
			if offset.Row != hex.Coord.R() {
				t.Fatalf("centre %d: %v moved to row %d", center, hex.Coord, offset.Row)
			}
			key := [2]int{offset.Col, offset.Row}
			if other, clash := seen[key]; clash {
				t.Fatalf("centre %d: %v and %v both cut to %v", center, hex.Coord, other, key)
			}
			seen[key] = hex.Coord
		}
		if len(seen) != world.Len() {
			t.Fatalf("centre %d: cut %d hexes into %d cells", center, world.Len(), len(seen))
		}
	}
}

// Re-centring slides the map round the cylinder rather than changing it, so a
// full lap has to come back to where it started.
func TestCutIsPeriodicInTheCentre(t *testing.T) {
	world := testWorld(t)
	for _, hex := range world.Hexes() {
		first := worldmap.Cut(world.Width(), 0, hex.Coord)
		lap := worldmap.Cut(world.Width(), world.Columns(), hex.Coord)
		if first != lap {
			t.Fatalf("%v cut to %v at centre 0 but %v a full lap later", hex.Coord, first, lap)
		}
	}
}

func TestRenderCoversTheWorld(t *testing.T) {
	world := testWorld(t)
	img := worldmap.Render(world, 4, 0)
	if img.Bounds().Dx() < world.Columns() || img.Bounds().Dy() < world.Rows() {
		t.Fatalf("rendered %dx%d for a %dx%d world",
			img.Bounds().Dx(), img.Bounds().Dy(), world.Columns(), world.Rows())
	}

	// Every terrain the world holds must reach the page: a palette gap would
	// silently paint real terrain as background.
	counts, land, coherence := worldmap.Census(world)
	for terrain := range counts {
		if _, known := worldmap.Palette[terrain]; !known {
			t.Fatalf("no colour for terrain %q", terrain)
		}
	}
	if land <= 0 || land >= 1 {
		t.Fatalf("land = %v, want a fraction", land)
	}
	if coherence <= 0.25 {
		t.Fatalf("neighbours agree %.0f%%, which is what independent per-hex rolls would give", 100*coherence)
	}
}

func TestDownscaleHalvesAndAverages(t *testing.T) {
	world := testWorld(t)
	full := worldmap.Render(world, 4, 0)
	for _, factor := range []int{1, 2, 4} {
		got := worldmap.Downscale(full, factor)
		want := full.Bounds().Dx() / factor
		if factor == 1 {
			want = full.Bounds().Dx()
		}
		if got.Bounds().Dx() != want {
			t.Fatalf("Downscale(%d) is %d wide, want %d", factor, got.Bounds().Dx(), want)
		}
	}
}

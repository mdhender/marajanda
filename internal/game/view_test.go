// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
)

func TestDiscCoversRadiusExactly(t *testing.T) {
	for radius := range 5 {
		hexes := disc(radius)
		if want := 3*radius*(radius+1) + 1; len(hexes) != want {
			t.Fatalf("disc(%d) returned %d hexes, want %d", radius, len(hexes), want)
		}
		seen := make(map[hexg.Hex]struct{}, len(hexes))
		for _, hex := range hexes {
			if hex.Length() > radius {
				t.Fatalf("disc(%d) contains %v at distance %d", radius, hex, hex.Length())
			}
			if _, duplicate := seen[hex]; duplicate {
				t.Fatalf("disc(%d) repeated %v", radius, hex)
			}
			seen[hex] = struct{}{}
		}
	}
	if got := disc(-1); got != nil {
		t.Fatalf("disc(-1) = %v, want nil", got)
	}
}

func TestDiscOrderIsStable(t *testing.T) {
	first, second := disc(3), disc(3)
	for index := range first {
		if !first[index].Equals(second[index]) {
			t.Fatalf("disc order differs at %d: %v then %v", index, first[index], second[index])
		}
	}
}

func TestAdminViewShowsEveryHex(t *testing.T) {
	world := mustGenerate(t, testSeeds(), 3, 2)
	tiles := AdminView(world)
	if want := world.Len(); len(tiles) != want {
		t.Fatalf("AdminView returned %d tiles, want %d", len(tiles), want)
	}
	for _, tile := range tiles {
		if !tile.Visible {
			t.Fatalf("AdminView hid %v", tile.Coord)
		}
		if tile.Terrain == "" {
			t.Fatalf("AdminView left %v without terrain", tile.Coord)
		}
	}
	if got := tiles[len(tiles)/2]; !got.Coord.Equals(hexg.NewHex(0, 0)) || !got.Terrain.Valid() {
		t.Fatalf("AdminView center = %v %q, want (0,0) with a valid terrain", got.Coord, got.Terrain)
	}
}

func TestPlayerViewRevealsOnlyVisibleHexes(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(7, -6)
	neighbor := origin.Add(hexg.NewHex(1, 0))

	tiles := PlayerView(world, origin, 2, []hexg.Hex{origin, neighbor})
	if want := len(disc(2)); len(tiles) != want {
		t.Fatalf("PlayerView returned %d tiles, want %d", len(tiles), want)
	}

	visible := make(map[hexg.Hex]Terrain)
	for _, tile := range tiles {
		if tile.Visible {
			visible[tile.Coord] = tile.Terrain
			continue
		}
		if tile.Terrain != "" {
			t.Fatalf("fog tile %v leaked terrain %q", tile.Coord, tile.Terrain)
		}
	}
	if len(visible) != 2 {
		t.Fatalf("PlayerView revealed %d tiles, want 2", len(visible))
	}

	// Coordinates are the world's own now, not a per-account frame.
	originHex, _ := world.At(origin)
	if got, ok := visible[origin]; !ok || got != originHex.Terrain {
		t.Fatalf("PlayerView origin tile = %q ok=%v, want %q", got, ok, originHex.Terrain)
	}
	neighborHex, _ := world.At(neighbor)
	if got, ok := visible[world.Normalize(neighbor)]; !ok || got != neighborHex.Terrain {
		t.Fatalf("PlayerView neighbor tile at %v = %q ok=%v", neighbor, got, ok)
	}
}

// A visible hex beyond a pole has no terrain to show. It must read as fog,
// exactly like ground the player has never seen, rather than drawing a hole in
// the map. Only rows can be outside: there is no eastern or western edge.
func TestPlayerViewFogsHexesBeyondAPole(t *testing.T) {
	world := mustGenerate(t, testSeeds(), 4, 3)
	origin := hexg.NewHex(0, 3)
	outside := hexg.NewHex(0, 5)

	for _, tile := range PlayerView(world, origin, 2, []hexg.Hex{outside}) {
		if tile.Visible {
			t.Fatalf("PlayerView revealed %v from outside the world", tile.Coord)
		}
	}
}

// TestPlayerViewIgnoresVisibleHexesOutsideTheDisc keeps a distant visible hex
// from being drawn at a coordinate it does not occupy.
func TestPlayerViewIgnoresVisibleHexesOutsideTheDisc(t *testing.T) {
	origin := hexg.NewHex(7, -6)
	distant := hexg.NewHex(20, 3)

	for _, tile := range PlayerView(testWorld(t), origin, 2, []hexg.Hex{distant}) {
		if tile.Visible {
			t.Fatalf("PlayerView revealed %v from a hex outside the disc", tile.Coord)
		}
	}
}

// A player standing against the meridian sees across it. Their eastern
// neighbour is canonically the westmost column of the world, and the view must
// carry it as an ordinary neighbouring hex rather than dropping it.
func TestPlayerViewSeesAcrossTheSeam(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(testWorldWidth, 0)
	east := world.Normalize(origin.Neighbor(0))

	if east.Q() != -testWorldWidth {
		t.Fatalf("east of the seam = %v, want q = %d", east, -testWorldWidth)
	}

	found := false
	for _, tile := range PlayerView(world, origin, 2, []hexg.Hex{origin, east}) {
		if tile.Coord.Equals(east) {
			found = true
			if !tile.Visible || tile.Terrain == "" {
				t.Fatalf("tile across the seam at %v is visible=%v terrain=%q", east, tile.Visible, tile.Terrain)
			}
		}
	}
	if !found {
		t.Fatalf("PlayerView from %v never returned %v across the seam", origin, east)
	}
}

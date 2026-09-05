// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
)

func TestToPlayerPlacesOriginAtZero(t *testing.T) {
	origin := hexg.NewHex(7, -16)
	for rotation := range rotations {
		got := ToPlayer(origin, rotation, origin)
		if !got.Equals(hexg.NewHex(0, 0)) {
			t.Fatalf("ToPlayer(origin, %d, origin) = %v, want (0,0)", rotation, got)
		}
	}
}

// TestToPlayerUsesCubeRotation pins the rotation direction to the cube rotation
// on hexg.Hex. hexg.Layout.RotateLeft dispatches to Hex.RotateRight for the
// flat-top layouts this game draws, so swapping one for the other would send
// every rotated player's map the wrong way while still round-tripping cleanly.
func TestToPlayerUsesCubeRotation(t *testing.T) {
	origin := hexg.NewHex(0, 0)
	east := hexg.NewHex(1, 0)

	if got := ToPlayer(origin, 1, east); !got.Equals(hexg.NewHex(1, -1)) {
		t.Fatalf("ToPlayer(rotation 1) = %v, want (1,-1); (0,1) means Layout.RotateLeft crept in", got)
	}
	if got := ToTrue(origin, 1, hexg.NewHex(1, -1)); !got.Equals(east) {
		t.Fatalf("ToTrue(rotation 1) = %v, want (1,0)", got)
	}
}

func TestToPlayerAndToTrueRoundTrip(t *testing.T) {
	origins := []hexg.Hex{
		hexg.NewHex(0, 0),
		hexg.NewHex(7, -16),
		hexg.NewHex(-23, 4),
		hexg.NewHex(18, -7),
	}
	for _, origin := range origins {
		for rotation := range rotations {
			for _, location := range disc(3) {
				player := ToPlayer(origin, rotation, location)
				if got := ToTrue(origin, rotation, player); !got.Equals(location) {
					t.Fatalf("round trip origin %v rotation %d location %v = %v", origin, rotation, location, got)
				}
			}
		}
	}
}

func TestRotationNormalizesOutOfRangeValues(t *testing.T) {
	origin := hexg.NewHex(3, 4)
	location := hexg.NewHex(9, -2)
	for _, test := range []struct{ rotation, equivalent int }{
		{rotation: 6, equivalent: 0},
		{rotation: 7, equivalent: 1},
		{rotation: -1, equivalent: 5},
		{rotation: -6, equivalent: 0},
	} {
		want := ToPlayer(origin, test.equivalent, location)
		if got := ToPlayer(origin, test.rotation, location); !got.Equals(want) {
			t.Fatalf("ToPlayer(rotation %d) = %v, want %v (rotation %d)", test.rotation, got, want, test.equivalent)
		}
	}
}

// TestRotationPreservesDistance guards the transform against any change that
// would move hexes relative to one another rather than rigidly rotating them.
func TestRotationPreservesDistance(t *testing.T) {
	origin := hexg.NewHex(-11, 5)
	for rotation := range rotations {
		for _, location := range disc(4) {
			distance := location.Distance(hexg.NewHex(0, 0))
			player := ToPlayer(origin, rotation, origin.Add(location))
			if got := player.Distance(hexg.NewHex(0, 0)); got != distance {
				t.Fatalf("rotation %d moved %v to distance %d, want %d", rotation, location, got, distance)
			}
		}
	}
}

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
	tiles := AdminView(testSeeds(), 3)
	if want := len(disc(3)); len(tiles) != want {
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
	if got := tiles[len(tiles)/2]; !got.Coord.Equals(hexg.NewHex(0, 0)) || got.Terrain != TerrainMountains {
		t.Fatalf("AdminView center = %v %q, want (0,0) %q", got.Coord, got.Terrain, TerrainMountains)
	}
}

func TestPlayerViewRevealsOnlyVisibleHexes(t *testing.T) {
	seeds := testSeeds()
	origin := hexg.NewHex(7, -16)
	rotation := 2
	neighbor := origin.Add(hexg.NewHex(1, 0))

	tiles := PlayerView(seeds, origin, rotation, 2, []hexg.Hex{origin, neighbor})
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

	center := hexg.NewHex(0, 0)
	if got, ok := visible[center]; !ok || got != TerrainAt(seeds, origin) {
		t.Fatalf("PlayerView origin tile = %q ok=%v, want %q", got, ok, TerrainAt(seeds, origin))
	}
	neighborCoord := ToPlayer(origin, rotation, neighbor)
	if got, ok := visible[neighborCoord]; !ok || got != TerrainAt(seeds, neighbor) {
		t.Fatalf("PlayerView neighbor tile at %v = %q ok=%v", neighborCoord, got, ok)
	}
}

// TestPlayerViewIgnoresVisibleHexesOutsideTheDisc keeps a distant visible hex
// from being drawn at a coordinate it does not occupy.
func TestPlayerViewIgnoresVisibleHexesOutsideTheDisc(t *testing.T) {
	origin := hexg.NewHex(7, -16)
	distant := hexg.NewHex(40, 3)

	for _, tile := range PlayerView(testSeeds(), origin, 0, 2, []hexg.Hex{distant}) {
		if tile.Visible {
			t.Fatalf("PlayerView revealed %v from a hex outside the disc", tile.Coord)
		}
	}
}

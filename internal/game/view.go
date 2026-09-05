// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// rotations is the number of distinct map rotations an account may have.
const rotations = 6

// Tile is one hex of a map view.
//
// Coord is expressed in the view's own frame: true map coordinates for
// [AdminView], account-relative coordinates for [PlayerView]. Terrain is
// meaningful only when Visible is true; a tile that is not visible carries no
// terrain, because the account is not entitled to know it.
type Tile struct {
	Coord   hexg.Hex
	Terrain Terrain
	Visible bool
}

// ToPlayer converts a true map coordinate into the coordinate that an account's
// map displays for it. The account origin always displays as (0, 0).
//
// The rotation is a game rule, not a drawing choice, so this uses the cube
// rotation on [hexg.Hex] rather than the one on hexg.Layout: the Layout methods
// rotate what the viewer sees and swap direction for flat-top layouts, which
// would tie a player's coordinate system to the orientation the map happens to
// be drawn in.
func ToPlayer(origin hexg.Hex, rotation int, location hexg.Hex) hexg.Hex {
	hex := location.Subtract(origin)
	for range normalizeRotation(rotation) {
		hex = hex.RotateLeft()
	}
	return hex
}

// ToTrue converts a coordinate on an account's map back into a true map
// coordinate. It inverts [ToPlayer].
func ToTrue(origin hexg.Hex, rotation int, location hexg.Hex) hexg.Hex {
	hex := location
	for range normalizeRotation(rotation) {
		hex = hex.RotateRight()
	}
	return origin.Add(hex)
}

// AdminView returns every hex within radius of the game origin, in true map
// coordinates. Admins see the whole disc.
func AdminView(seeds prng.Seeds, radius int) []Tile {
	coords := disc(radius)
	tiles := make([]Tile, 0, len(coords))
	for _, coord := range coords {
		tiles = append(tiles, Tile{Coord: coord, Terrain: TerrainAt(seeds, coord), Visible: true})
	}
	return tiles
}

// PlayerView returns every hex within radius of an account's origin, in the
// coordinates that account's map displays.
//
// visible holds the true map coordinates the account can see. Hexes outside it
// are returned as fog: present in the view, carrying no terrain.
func PlayerView(seeds prng.Seeds, origin hexg.Hex, rotation, radius int, visible []hexg.Hex) []Tile {
	seen := make(map[hexg.Hex]struct{}, len(visible))
	for _, hex := range visible {
		seen[hex] = struct{}{}
	}
	coords := disc(radius)
	tiles := make([]Tile, 0, len(coords))
	for _, coord := range coords {
		tile := Tile{Coord: coord}
		if location := ToTrue(origin, rotation, coord); isVisible(seen, location) {
			tile.Terrain, tile.Visible = TerrainAt(seeds, location), true
		}
		tiles = append(tiles, tile)
	}
	return tiles
}

func isVisible(seen map[hexg.Hex]struct{}, location hexg.Hex) bool {
	_, ok := seen[location]
	return ok
}

// disc returns every hex within radius of (0, 0, 0), ordered by q and then r so
// that a rendered map is byte-for-byte stable across runs.
func disc(radius int) []hexg.Hex {
	if radius < 0 {
		return nil
	}
	hexes := make([]hexg.Hex, 0, 3*radius*(radius+1)+1)
	for q := -radius; q <= radius; q++ {
		for r := max(-radius, -q-radius); r <= min(radius, -q+radius); r++ {
			hexes = append(hexes, hexg.NewHex(q, r))
		}
	}
	return hexes
}

func normalizeRotation(rotation int) int {
	return ((rotation % rotations) + rotations) % rotations
}

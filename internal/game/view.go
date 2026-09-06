// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
)

// Tile is one hex of a map view.
//
// Coord is the hex's true, canonical map coordinate in every view. There is no
// longer a per-account frame: on a wrapping rectangle every player agrees which
// way is north-east, so the coordinate a player is told is the coordinate
// everyone else means. Placing a tile on screen is the renderer's problem, and
// near the meridian it needs the representative closest to the view centre
// rather than the canonical one - see cylinder.Cylinder.Nearest.
//
// Terrain and Elevation are meaningful only when Visible is true; a tile that
// is not visible carries neither, because the account is not entitled to know
// them.
type Tile struct {
	Coord     hexg.Hex
	Terrain   Terrain
	Elevation int
	Visible   bool
}

// AdminView returns every hex of the world, in true map coordinates. Admins
// see all of it.
func AdminView(world World) []Tile {
	hexes := world.Hexes()
	tiles := make([]Tile, 0, len(hexes))
	for _, hex := range hexes {
		tiles = append(tiles, Tile{
			Coord: hex.Coord, Terrain: hex.Terrain, Elevation: hex.Elevation, Visible: true,
		})
	}
	return tiles
}

// PlayerView returns every hex within radius of an account's origin.
//
// visible holds the map coordinates the account can see. Hexes outside it are
// returned as fog: present in the view, carrying no terrain. So is anything
// beyond a pole, which a player has no way to tell apart from land they have
// simply never seen. Nothing is ever beyond the eastern or western edge,
// because there is not one.
func PlayerView(world World, origin hexg.Hex, radius int, visible []hexg.Hex) []Tile {
	seen := make(map[hexg.Hex]struct{}, len(visible))
	for _, hex := range visible {
		seen[hex] = struct{}{}
	}
	offsets := disc(radius)
	tiles := make([]Tile, 0, len(offsets))
	for _, offset := range offsets {
		location := world.Normalize(origin.Add(offset))
		tile := Tile{Coord: location}
		if isVisible(seen, location) {
			if hex, ok := world.At(location); ok {
				tile.Terrain, tile.Elevation, tile.Visible = hex.Terrain, hex.Elevation, true
			}
		}
		tiles = append(tiles, tile)
	}
	return tiles
}

func isVisible(seen map[hexg.Hex]struct{}, location hexg.Hex) bool {
	_, ok := seen[location]
	return ok
}

// disc returns every offset within radius of (0, 0, 0), ordered by q and then r
// so that a rendered map is byte-for-byte stable across runs. These are offsets
// from a view centre, not world coordinates: they are added to an origin and
// normalized before anything is looked up.
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

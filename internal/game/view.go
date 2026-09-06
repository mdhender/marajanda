// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
)

// offsetEven selects even-r offset conversion, matching the layout the world is
// generated in. A view is a rectangle of rows and columns, and the offset
// column is what makes a rectangle of a cylinder's axial coordinates.
const offsetEven = true

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

// WindowView returns every hex of a rectangular window of the world, in true
// map coordinates. Admins see all of it.
//
// A window rather than the whole world, because the whole world is 130,305
// hexes and no page draws that: an admin map is a place to stand, and the
// downloadable image is how the world is seen at once.
//
// Columns wrap, so a window may straddle the meridian and still be continuous.
// Rows are clamped to the world instead, because a row beyond a pole is not a
// hex of anything and an admin gains nothing from a margin of nothing.
func WindowView(world World, center hexg.Hex, columns, rows int) []Tile {
	if columns < 1 || rows < 1 {
		return nil
	}
	columns = min(columns, world.Columns())

	offset := center.CubeToROffset(offsetEven)
	minCol := offset.Col - (columns-1)/2
	minRow := max(-world.Height(), offset.Row-(rows-1)/2)
	maxRow := min(world.Height(), minRow+rows-1)

	tiles := make([]Tile, 0, columns*(maxRow-minRow+1))
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col < minCol+columns; col++ {
			coord := world.Normalize(hexg.NewOffsetCoord(col, row).ROffsetToCube(offsetEven))
			hex, ok := world.At(coord)
			if !ok {
				continue
			}
			tiles = append(tiles, Tile{
				Coord: coord, Terrain: hex.Terrain, Elevation: hex.Elevation, Visible: true,
			})
		}
	}
	return tiles
}

// PlayerView returns the hexes a player's map draws: everything the account can
// see, inside a fixed margin of fog.
//
// The window is derived from what the account can see rather than from the
// world, which is the whole point. A player who can see one hex is sent that
// hex and its close neighbours, not a thousand obscured ones - there is nothing
// in an unexplored hex worth the bytes, and a map wide enough to pan is a map
// that answers questions the player has not earned, starting with how far they
// are from the ice.
//
// The margin is the same on every side and does not shrink at a pole. A hex
// beyond a pole is drawn as fog, which a player cannot tell apart from land
// they have simply never seen, so the shape of their map never says where in
// the world they are.
func PlayerView(world World, origin hexg.Hex, margin int, visible []hexg.Hex) []Tile {
	if margin < 0 {
		margin = 0
	}

	// Everything is measured in the copy of the world nearest the origin. A
	// player near the meridian can see hexes whose canonical column is most of
	// a world away, and a bounding box over those would span the world.
	seen := make(map[hexg.Hex]struct{}, len(visible))
	local := make([]hexg.Hex, 0, len(visible))
	for _, hex := range visible {
		seen[world.Normalize(hex)] = struct{}{}
		local = append(local, world.Cylinder().Nearest(origin, hex))
	}
	if len(local) == 0 {
		local = append(local, origin)
	}

	first := local[0].CubeToROffset(offsetEven)
	minCol, maxCol := first.Col, first.Col
	minRow, maxRow := first.Row, first.Row
	for _, hex := range local[1:] {
		offset := hex.CubeToROffset(offsetEven)
		minCol, maxCol = min(minCol, offset.Col), max(maxCol, offset.Col)
		minRow, maxRow = min(minRow, offset.Row), max(maxRow, offset.Row)
	}
	minCol, maxCol = minCol-margin, maxCol+margin
	minRow, maxRow = minRow-margin, maxRow+margin

	tiles := make([]Tile, 0, (maxCol-minCol+1)*(maxRow-minRow+1))
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			coord := world.Normalize(hexg.NewOffsetCoord(col, row).ROffsetToCube(offsetEven))
			tile := Tile{Coord: coord}
			if _, ok := seen[coord]; ok {
				if hex, inWorld := world.At(coord); inWorld {
					tile.Terrain, tile.Elevation, tile.Visible = hex.Terrain, hex.Elevation, true
				}
			}
			tiles = append(tiles, tile)
		}
	}
	return tiles
}

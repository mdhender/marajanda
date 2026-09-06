// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/worldmap"
)

const (
	// playerMapRadius is how far a player map reaches from the account origin.
	// It is smaller than the admin radius because a player can see almost
	// nothing yet, and a wide field of fog reads as a broken page rather than
	// as an unexplored frontier.
	playerMapRadius = 6

	// mapHexSize is the pixel radius of one hexagon. The viewBox scales the
	// finished map to its container, so this only fixes the relative size of a
	// hexagon against its stroke and label.
	mapHexSize = 24.0

	// mapPadding keeps hexagon strokes off the edge of the viewBox.
	mapPadding = 2.0

	// adminMapBudget caps how many hexagons a whole-world map draws.
	//
	// The default world is 130,305 hexes, and one polygon each is a 24 MB page
	// that no browser renders usefully - so drawing every hex is not whole-world
	// inspection, it only looks like it. Above the budget the map becomes a
	// survey: every stride-th hex, replotted so they still tile. Continents,
	// coastlines and climate bands survive that; individual lakes do not.
	adminMapBudget = 12000

	// mapOffsetEven selects even-r offset conversion, matching the layout the
	// world is generated in. It is the conversion Worldographer expects, which
	// is why the world is even-r rather than odd-r.
	mapOffsetEven = true
)

// mapTile is one rendered hexagon.
type mapTile struct {
	Points  string
	Terrain string
	Label   string
}

// mapView is a complete map, ready for the page template to draw as SVG.
type mapView struct {
	ViewBox string
	Tiles   []mapTile
}

func (app *application) adminMap(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requireRole(w, r, "admin")
	if !ok {
		return
	}
	// The admin map draws the whole world rather than a window onto it. The
	// world is bounded, so "all of it" is a finite thing to ask for, and an
	// admin looking at a generated world wants to see what was generated.
	world, ok := app.world(w, r)
	if !ok {
		return
	}
	stride := surveyStride(world)
	app.render(w, http.StatusOK, pageData{
		Title:   "Admin map",
		View:    "admin-map",
		Account: account,
		Map: buildMapView(
			// The admin map is the whole world, so it wants the rectangular cut.
			surveyPlace(world, stride),
			surveyTiles(world, game.AdminView(world), stride),
		),
	})
}

func (app *application) playerMap(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requireRole(w, r, "player")
	if !ok {
		return
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
		return
	}
	if !found || !faction.Configured() {
		http.Redirect(w, r, "/player/faction", http.StatusSeeOther)
		return
	}
	world, ok := app.world(w, r)
	if !ok {
		return
	}
	visible, err := app.store.VisibleHexes(r.Context(), account.Email)
	if err != nil {
		http.Error(w, "Marajanda could not load your map.", http.StatusInternalServerError)
		return
	}
	app.render(w, http.StatusOK, pageData{
		Title:   "Your map",
		View:    "player-map",
		Account: account,
		Faction: faction,
		Map: buildMapView(
			// A player's viewport is placed relative to the player. Near the
			// meridian their eastern neighbour is canonically the westmost
			// column of the world, and drawing it there would fling it most of
			// a world away instead of one hex east.
			func(coord hexg.Hex) hexg.Hex { return world.Cylinder().Nearest(account.Origin, coord) },
			game.PlayerView(world, account.Origin, playerMapRadius, visible),
		),
	})
}

func (app *application) world(w http.ResponseWriter, r *http.Request) (game.World, bool) {
	world, err := app.store.World(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load the world.", http.StatusInternalServerError)
		return game.World{}, false
	}
	return world, true
}

// surveyStride is how many hexes a whole-world map collapses into one, chosen
// so the drawn count stays inside adminMapBudget. It is 1 for any world small
// enough to draw in full.
func surveyStride(world game.World) int {
	stride := 1
	for (world.Columns()/stride)*(world.Rows()/stride) > adminMapBudget {
		stride++
	}
	return stride
}

// surveyTiles collapses each stride-by-stride block of the world into the
// terrain that covers most of it.
//
// Taking every stride-th hex instead would be simpler and wrong: terrain
// features run five to ten hexes across, so point-sampling at a comparable
// stride aliases them into speckle and a coherent world comes out looking like
// per-hex noise. The whole reason to draw this map is to see whether the
// generator produced geography, so the one thing it must not do is destroy the
// evidence.
func surveyTiles(world game.World, tiles []game.Tile, stride int) []game.Tile {
	if stride <= 1 {
		return tiles
	}

	type block struct {
		representative game.Tile
		counts         map[game.Terrain]int
		best           int
	}
	blocks := make(map[[2]int]*block, len(tiles)/(stride*stride)+1)
	order := make([][2]int, 0, len(blocks))

	for _, tile := range tiles {
		offset := worldmap.Cut(world.Width(), 0, tile.Coord).CubeToROffset(mapOffsetEven)
		key := [2]int{(offset.Col + world.Width()) / stride, (offset.Row + world.Height()) / stride}

		current, seen := blocks[key]
		if !seen {
			current = &block{counts: make(map[game.Terrain]int, 4)}
			blocks[key] = current
			order = append(order, key)
		}
		current.counts[tile.Terrain]++
		// Ties break on the terrain already holding the block, and blocks are
		// filled in the world's own order, so the survey is stable.
		if count := current.counts[tile.Terrain]; count > current.best {
			current.best = count
			current.representative = tile
		}
	}

	kept := make([]game.Tile, 0, len(order))
	for _, key := range order {
		kept = append(kept, blocks[key].representative)
	}
	return kept
}

// surveyPlace positions a surveyed tile so the survivors still tile the plane
// rather than scattering across it with gaps where their neighbours were.
//
// The shift by the half-extents makes the division floor rather than truncate
// toward zero, which would otherwise double up the row and column either side
// of the origin.
func surveyPlace(world game.World, stride int) func(hexg.Hex) hexg.Hex {
	return func(coord hexg.Hex) hexg.Hex {
		offset := worldmap.Cut(world.Width(), 0, coord).CubeToROffset(mapOffsetEven)
		if stride <= 1 {
			return hexg.NewOffsetCoord(offset.Col, offset.Row).ROffsetToCube(mapOffsetEven)
		}
		col := (offset.Col + world.Width()) / stride
		row := (offset.Row + world.Height()) / stride
		return hexg.NewOffsetCoord(col, row).ROffsetToCube(mapOffsetEven)
	}
}

// buildMapView turns game tiles into pointy-top SVG geometry.
//
// place decides where a tile is drawn. Tiles carry canonical world
// coordinates, and a cylinder gives every hex infinitely many positions on the
// plane, so the caller has to say which one this map wants: the whole world
// wants a rectangular cut, a player's window wants the copy nearest the player.
func buildMapView(place func(hexg.Hex) hexg.Hex, tiles []game.Tile) mapView {
	layout := hexg.NewLayout(hexg.EvenR, hexg.Point{X: mapHexSize, Y: mapHexSize}, hexg.Point{})

	view := mapView{Tiles: make([]mapTile, 0, len(tiles))}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for _, tile := range tiles {
		var points strings.Builder
		for index, corner := range layout.PolygonCorners(place(tile.Coord)) {
			if index > 0 {
				points.WriteByte(' ')
			}
			fmt.Fprintf(&points, "%.2f,%.2f", corner.X, corner.Y)
			minX, minY = min(minX, corner.X), min(minY, corner.Y)
			maxX, maxY = max(maxX, corner.X), max(maxY, corner.Y)
		}
		view.Tiles = append(view.Tiles, mapTile{
			Points:  points.String(),
			Terrain: terrainClass(tile),
			Label:   tileLabel(tile),
		})
	}

	if len(view.Tiles) == 0 {
		return view
	}
	view.ViewBox = fmt.Sprintf("%.2f %.2f %.2f %.2f",
		minX-mapPadding, minY-mapPadding,
		maxX-minX+2*mapPadding, maxY-minY+2*mapPadding)
	return view
}

// terrainClass names the CSS class that colors a hexagon. A tile that is not
// visible carries no terrain, so it can only ever be fog.
func terrainClass(tile game.Tile) string {
	if !tile.Visible {
		return "fog"
	}
	return string(tile.Terrain)
}

// tileLabel is the hover text for one hexagon. Coordinates are in the view's
// own frame, which for a player is their own rotated map.
func tileLabel(tile game.Tile) string {
	if !tile.Visible {
		return fmt.Sprintf("(%d, %d) unexplored", tile.Coord.Q(), tile.Coord.R())
	}
	return fmt.Sprintf("(%d, %d) %s, %s", tile.Coord.Q(), tile.Coord.R(), tile.Terrain, elevationLabel(tile.Elevation))
}

// elevationLabel reads a hex's elevation the way a map legend would, so that
// water reads as depth rather than as negative height.
func elevationLabel(elevation int) string {
	if elevation < 0 {
		return fmt.Sprintf("%d m deep", -elevation)
	}
	return fmt.Sprintf("%d m", elevation)
}

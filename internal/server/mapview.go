// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
)

const (
	// adminMapRadius is how far the admin map reaches from the game origin.
	// The page has no pan or scroll, so this is the entire viewport.
	adminMapRadius = 20

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
	seeds, ok := app.gameSeeds(w, r)
	if !ok {
		return
	}
	app.render(w, http.StatusOK, pageData{
		Title:   "Admin map",
		View:    "admin-map",
		Account: account,
		Map:     buildMapView(game.AdminView(seeds, adminMapRadius)),
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
	seeds, ok := app.gameSeeds(w, r)
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
		Map:     buildMapView(game.PlayerView(seeds, account.Origin, account.Rotation, playerMapRadius, visible)),
	})
}

func (app *application) gameSeeds(w http.ResponseWriter, r *http.Request) (prng.Seeds, bool) {
	record, err := app.store.Game(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load the game.", http.StatusInternalServerError)
		return prng.Seeds{}, false
	}
	return prng.New(uint64(record.Seed1), uint64(record.Seed2)), true
}

// buildMapView turns game tiles into flat-top SVG geometry.
//
// Tiles arrive in a stable order and every coordinate is already expressed in
// the frame the viewer is entitled to see, so this never consults the account:
// a player view carries player-relative coordinates in and out, and cannot leak
// a true coordinate it was never given.
func buildMapView(tiles []game.Tile) mapView {
	layout := hexg.NewLayout(hexg.EvenQ, hexg.Point{X: mapHexSize, Y: mapHexSize}, hexg.Point{})

	view := mapView{Tiles: make([]mapTile, 0, len(tiles))}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for _, tile := range tiles {
		var points strings.Builder
		for index, corner := range layout.PolygonCorners(tile.Coord) {
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
	return fmt.Sprintf("(%d, %d) %s", tile.Coord.Q(), tile.Coord.R(), tile.Terrain)
}

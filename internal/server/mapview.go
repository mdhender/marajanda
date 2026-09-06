// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/worldmap"
)

const (
	// defaultMapHexSize is the pixel radius of one hexagon on a drawn map.
	//
	// Every function below takes the size as an argument rather than reading a
	// constant, because the right size is a property of a person's screen and
	// eyes rather than of the map: see #23, which lets an account choose it.
	// Until then this is the one value the handlers pass.
	//
	// Twenty-four pixels is a hexagon 42 wide and 48 tall, which is large
	// enough to read its shape and its colour at a glance.
	defaultMapHexSize = 24.0

	// mapPadding keeps hexagon strokes off the edge of the viewBox.
	mapPadding = 2.0

	// The admin window, in hexes.
	//
	// A map is drawn at its natural size and the browser scrolls it, so this is
	// not how much a person sees - it is how much ground they can cover before
	// another request is needed. A thirteen-inch laptop shows about 26 columns
	// and 17 rows of this window at once; a phone shows perhaps 9 by 14.
	//
	// Bigger would scroll further and cost more markup on the device least able
	// to afford it: every hex is a polygon with a title, and these dimensions
	// are already a thousand of them.
	adminWindowColumns = 40
	adminWindowRows    = 26

	// playerFogMargin is how many hexes of fog surround what a player can see.
	// Two puts a five-by-five map around a player who can see one hex: enough
	// to read as a place rather than a sliver, and far too little to navigate
	// by.
	playerFogMargin = 2

	// worldImageHexSize is the pixel radius of one hexagon in the downloadable
	// world image. Four pixels puts the whole default world into a single
	// 3544 x 1532 PNG, which is a picture a browser will show at once and a
	// person can actually take in.
	worldImageHexSize = 4.0

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

// mapPan holds the links that move an admin's window over the world.
//
// Each is an ordinary href and stays one. HTMX asks for the same URL and swaps
// the map region in place, so a browser running it keeps its scroll position
// across a pan and a browser without it follows the link to a new page. The
// server does not need to know which happened until the request arrives with
// HX-Request on it.
type mapPan struct {
	North, South, East, West, Origin string
}

// mapNeighbor is one of the six hexes around the window's centre, as the page
// lists them.
//
// This is where the compass is exercised: the list is built by walking
// [compass.Points] in order, so the page shows the order the movement rules
// will require and the ground it actually reaches. See #27.
type mapNeighbor struct {
	Point   string
	Name    string
	Coord   string
	Terrain string
	// Beyond marks a neighbour that is not a hex of the world. Columns wrap, so
	// this only ever happens off the top or bottom: rows are walls.
	Beyond bool
}

// mapView is a complete map, ready for the page template to draw as SVG.
//
// Width and Height are the drawn size in pixels. The map is rendered at its
// natural size and scrolled rather than scaled to its container: scaling is
// what shrank a 42-pixel hexagon to six and made the grid unreadable.
type mapView struct {
	ViewBox string
	Width   int
	Height  int
	Tiles   []mapTile
	Pan     *mapPan
	Center  string
	Image   string
	// Neighbors is the six hexes around Center, in compass order. Empty on a
	// map that has no centre to be around, which is every player map.
	Neighbors []mapNeighbor
}

func (app *application) adminMap(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requireRole(w, r, "admin")
	if !ok {
		return
	}
	world, ok := app.world(w, r)
	if !ok {
		return
	}

	// The admin map is a window onto the world rather than the whole of it. The
	// whole of it is the PNG, which draws every hex without collapsing any.
	center := mapCenter(r, world)
	view := buildMapView(
		windowPlace(world, center),
		game.WindowView(world, center, adminWindowColumns, adminWindowRows),
		defaultMapHexSize,
	)
	view.Pan = adminPan(world, center)
	view.Center = coordLabel(center)
	view.Image = "/admin/map.png"
	view.Neighbors = mapNeighbors(world, center)

	app.renderMap(w, r, pageData{
		Title:   "Admin map",
		View:    "admin-map",
		Account: account,
		Map:     view,
	})
}

// adminMapImage serves the whole world as one PNG.
//
// This is the answer to "show me all of it", and it is why the page above does
// not have to be. Every hex is drawn at its own colour with no survey and
// nothing collapsed, which an SVG of 130,305 polygons could never be.
func (app *application) adminMapImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.requireRole(w, r, "admin"); !ok {
		return
	}
	world, ok := app.world(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `attachment; filename="marajanda-world.png"`)
	_ = png.Encode(w, worldmap.Render(world, worldImageHexSize, 0))
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
	app.renderMap(w, r, pageData{
		Title:   "Your map",
		View:    "player-map",
		Account: account,
		Faction: faction,
		// No window controls and no window in the query: a player's map is
		// wherever their people are, and there is nothing for them to pan to.
		Map: buildMapView(
			// A player's map is placed relative to the player. Near the
			// meridian their eastern neighbour is canonically the westmost
			// column of the world, and drawing it there would fling it most of
			// a world away instead of one hex east.
			func(coord hexg.Hex) hexg.Hex { return world.Cylinder().Nearest(account.Origin, coord) },
			game.PlayerView(world, account.Origin, playerFogMargin, visible),
			defaultMapHexSize,
		),
	})
}

// mapNeighbors lists the six hexes around a centre, in compass order.
//
// It is the admin map's rehearsal of the movement rules: the order is
// [compass.Points]'s order, and the coordinates come from [compass.Neighbor],
// which normalizes, so a centre on the meridian lists an eastern neighbour one
// hex east rather than most of a world west.
//
// A neighbour off the top or bottom of the world is listed and marked rather
// than dropped. The compass returns six hexes from every hex; whether a hex is
// somewhere a faction may go is a separate question, and collapsing the two
// here would hide which of the six was missing.
func mapNeighbors(world game.World, center hexg.Hex) []mapNeighbor {
	around := make([]mapNeighbor, 0, 6)
	for _, point := range compass.Points() {
		coord := compass.Neighbor(world.Cylinder(), center, point)
		listed := mapNeighbor{
			Point: point.String(),
			Name:  point.Name(),
			Coord: coordLabel(coord),
		}
		if hex, ok := world.At(coord); ok {
			listed.Terrain = string(hex.Terrain)
		} else {
			listed.Beyond = true
		}
		around = append(around, listed)
	}
	return around
}

// renderMap answers a map request with the whole page, or with the map region
// alone when HTMX asked for that.
//
// One URL, two shapes of answer, so the response says what it varied on. A
// cache that did not know would eventually hand a bare region to a browser
// that asked for a page.
func (app *application) renderMap(w http.ResponseWriter, r *http.Request, data pageData) {
	w.Header().Set("Vary", "HX-Request")
	if wantsFragment(r) {
		app.renderFragment(w, http.StatusOK, "map-region", data)
		return
	}
	app.render(w, http.StatusOK, data)
}

func (app *application) world(w http.ResponseWriter, r *http.Request) (game.World, bool) {
	world, err := app.store.World(r.Context())
	if err != nil {
		http.Error(w, "Marajanda could not load the world.", http.StatusInternalServerError)
		return game.World{}, false
	}
	return world, true
}

// mapCenter reads the window centre from the query, defaulting to the game
// origin.
//
// The jump box answers as "at", and the pan links as "q" and "r", so a jump is
// read first and the links are what is left. The two are read differently on
// purpose: see mapJump.
//
// For the links, a column outside the world wraps back into it and a row
// outside it is clamped, because rows are the one thing a cylinder does not
// wrap. Half a window past a pole is still a window a person asked for.
func mapCenter(r *http.Request, world game.World) hexg.Hex {
	if query := r.URL.Query(); query.Has("at") {
		return mapJump(world, query.Get("at"))
	}
	q := queryInt(r, "q")
	row := min(world.Height(), max(-world.Height(), queryInt(r, "r")))
	return world.Normalize(hexg.NewHex(q, row))
}

// mapJump reads the jump box: a coordinate pair as "q,r", with whitespace
// anywhere around either number, naming the hex to centre the window on.
//
// Anything that is not such a hex returns the game origin, which is where the
// page's own "Back to the origin" link goes. Columns wrap, so a column past
// the meridian is a real hex of the cylinder and Normalize names it; rows do
// not, and a row past a pole is a typo rather than a request to look at the
// ice. That is why this does not take mapCenter's lenient path and clamp: a
// person who meant a pole can pan to one, and a person who mistyped a
// coordinate is better told than quietly moved somewhere near it.
func mapJump(world game.World, value string) hexg.Hex {
	origin := hexg.NewHex(0, 0)
	column, row, split := strings.Cut(value, ",")
	if !split {
		return origin
	}
	q, err := strconv.Atoi(strings.TrimSpace(column))
	if err != nil {
		return origin
	}
	// A second comma lands here as part of the row and fails, which is what
	// makes "1,2,3" a typo rather than a hex.
	rr, err := strconv.Atoi(strings.TrimSpace(row))
	if err != nil {
		return origin
	}
	coord := world.Normalize(hexg.NewHex(q, rr))
	if !world.Contains(coord) {
		return origin
	}
	return coord
}

func queryInt(r *http.Request, name string) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(name)))
	if err != nil {
		return 0
	}
	return value
}

// windowPlace draws a window around its own centre.
//
// worldmap.Cut picks the copy of a hex whose offset column is nearest the
// centre column, which is what keeps a window continuous across the meridian:
// the hex one column east of the last column of the world is drawn one column
// east, not five hundred columns west.
func windowPlace(world game.World, center hexg.Hex) func(hexg.Hex) hexg.Hex {
	column := center.CubeToROffset(mapOffsetEven).Col
	return func(coord hexg.Hex) hexg.Hex {
		return worldmap.Cut(world.Width(), column, coord)
	}
}

// adminPan builds the links that move the window.
//
// A step is half a window, so consecutive windows overlap by half and nothing a
// person was looking at leaves the page in one move.
func adminPan(world game.World, center hexg.Hex) *mapPan {
	offset := center.CubeToROffset(mapOffsetEven)
	step := func(columns, rows int) string {
		row := min(world.Height(), max(-world.Height(), offset.Row+rows))
		moved := world.Normalize(hexg.NewOffsetCoord(offset.Col+columns, row).ROffsetToCube(mapOffsetEven))
		return fmt.Sprintf("/admin/map?q=%d&r=%d", moved.Q(), moved.R())
	}
	return &mapPan{
		North:  step(0, -adminWindowRows/2),
		South:  step(0, adminWindowRows/2),
		West:   step(-adminWindowColumns/2, 0),
		East:   step(adminWindowColumns/2, 0),
		Origin: "/admin/map",
	}
}

// buildMapView turns game tiles into pointy-top SVG geometry at hexSize.
//
// place decides where a tile is drawn. Tiles carry canonical world
// coordinates, and a cylinder gives every hex infinitely many positions on the
// plane, so the caller has to say which one this map wants: an admin's window
// wants the copy nearest its centre, a player's map the copy nearest the
// player.
func buildMapView(place func(hexg.Hex) hexg.Hex, tiles []game.Tile, hexSize float64) mapView {
	layout := hexg.NewLayout(hexg.EvenR, hexg.Point{X: hexSize, Y: hexSize}, hexg.Point{})

	view := mapView{Tiles: make([]mapTile, 0, len(tiles))}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for _, tile := range tiles {
		var points strings.Builder
		for index, corner := range layout.PolygonCorners(place(tile.Coord)) {
			if index > 0 {
				points.WriteByte(' ')
			}
			// One decimal place: enough that adjacent hexagons still meet
			// exactly, and a good deal less markup than two.
			fmt.Fprintf(&points, "%.1f,%.1f", corner.X, corner.Y)
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
	view.ViewBox = fmt.Sprintf("%.1f %.1f %.1f %.1f",
		minX-mapPadding, minY-mapPadding,
		maxX-minX+2*mapPadding, maxY-minY+2*mapPadding)
	view.Width = int(math.Ceil(maxX - minX + 2*mapPadding))
	view.Height = int(math.Ceil(maxY - minY + 2*mapPadding))
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

// tileLabel is the hover text for one hexagon.
//
// An unexplored hex is labelled but not located. Its coordinate is not the
// account's to know: a fog margin that printed coordinates would be a ruler
// laid over the world, and reading two of them tells a player exactly where
// they stand.
func tileLabel(tile game.Tile) string {
	if !tile.Visible {
		return "Unexplored"
	}
	return fmt.Sprintf("%s %s, %s", coordLabel(tile.Coord), tile.Terrain, elevationLabel(tile.Elevation))
}

func coordLabel(coord hexg.Hex) string {
	return fmt.Sprintf("(%d, %d)", coord.Q(), coord.R())
}

// elevationLabel reads a hex's elevation the way a map legend would, so that
// water reads as depth rather than as negative height.
func elevationLabel(elevation int) string {
	if elevation < 0 {
		return fmt.Sprintf("%d m deep", -elevation)
	}
	return fmt.Sprintf("%d m", elevation)
}

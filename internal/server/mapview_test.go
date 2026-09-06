// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"github.com/mdhender/marajanda/internal/worldmap"
)

// playerOrigin is far enough from the game origin that its coordinates cannot
// be confused with the player-relative ones the page is allowed to print, and
// it is dry land in the test world.
var playerOrigin = hexg.NewHex(7, -16)

// testMapWorld is the world the map tests draw. It is generated once: the
// generator is deterministic, so a shared world cannot couple the tests, and
// regenerating it per test would dominate their runtime.
var testMapWorld = sync.OnceValue(func() game.World {
	world, err := game.GenerateWorld(testViewSeeds(), testMapWidth, testMapHeight)
	if err != nil {
		panic(err)
	}
	return world
})

// The test world's half-extents, large enough to hold playerOrigin and a player map drawn
// around it.
const (
	testMapWidth  = 20
	testMapHeight = 20
)

func signedInMap(t *testing.T, account datastore.Account, store applicationStore, target string) *httptest.ResponseRecorder {
	t.Helper()
	return signedInMapHeaders(t, account, store, target, nil)
}

// signedInMapHeaders is signedInMap with request headers, which is how a test
// asks the way HTMX asks.
func signedInMapHeaders(t *testing.T, account datastore.Account, store applicationStore, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return account, true, nil
	}, store)
	signIn := submitSignIn(handler, account.Email, "good.luck")
	cookies := signIn.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("sign-in cookies = %d, want 1", len(cookies))
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookies[0])
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAdminMapDrawsAWindowOntoTheWorld(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"},
		&testStore{game: testMapGame(), world: testMapWorld()},
		"/admin/map")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	// A window, not the world. The world is the PNG.
	want := len(game.WindowView(testMapWorld(), hexg.NewHex(0, 0), adminWindowColumns, adminWindowRows))
	if got := strings.Count(body, "<polygon"); got != want {
		t.Fatalf("polygons = %d, want %d", got, want)
	}
	if want >= testMapWorld().Len() {
		t.Fatalf("the test world has %d hexes, which a window of %d does not window", testMapWorld().Len(), want)
	}

	origin, ok := testMapWorld().At(hexg.NewHex(0, 0))
	if !ok {
		t.Fatal("test world is missing the game origin")
	}
	for _, want := range []string{
		// Drawn at its natural size rather than scaled to the container: that
		// scaling is what made a hexagon six pixels wide.
		`<svg width="`, `height="`, `viewBox="`, `role="img"`,
		`<polygon class="mountains"`,
		fmt.Sprintf("(0, 0) %s,", origin.Terrain),
		`href="/admin/dashboard"`, `href="/admin/map.png"`,
		`class="map-pan"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin map missing %q", want)
		}
	}
	if strings.Contains(body, `<polygon class="fog"`) {
		t.Fatal("admin map contains fog; admins see every hex")
	}
}

// Panning is four ordinary links, and each has to land on a window that is
// half a window away from this one.
func TestAdminMapPansByHalfAWindow(t *testing.T) {
	world := testMapWorld()
	store := &testStore{game: testMapGame(), world: world}
	account := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}

	body := signedInMap(t, account, store, "/admin/map").Body.String()
	for _, want := range []string{
		fmt.Sprintf(`href="/admin/map?q=%d&amp;r=0"`, adminWindowColumns/2),
		fmt.Sprintf(`href="/admin/map?q=%d&amp;r=0"`, -adminWindowColumns/2),
		fmt.Sprintf(`r=%d"`, adminWindowRows/2),
		fmt.Sprintf(`r=%d"`, -adminWindowRows/2),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin map missing pan link %q", want)
		}
	}

	// Following one moves the window: its centre, and the hexes it draws.
	moved := signedInMap(t, account, store,
		fmt.Sprintf("/admin/map?q=%d&r=0", adminWindowColumns/2)).Body.String()
	if !strings.Contains(moved, fmt.Sprintf("(%d, 0)", adminWindowColumns/2)) {
		t.Fatalf("panned map is not centred on (%d, 0)", adminWindowColumns/2)
	}
	if moved == body {
		t.Fatal("panning east drew the same window")
	}
}

// Rows do not wrap, so a request for a row beyond a pole is clamped rather than
// answered with an empty map or a page of nothing.
func TestAdminMapClampsAWindowBeyondAPole(t *testing.T) {
	world := testMapWorld()
	response := signedInMap(t,
		datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"},
		&testStore{game: testMapGame(), world: world},
		fmt.Sprintf("/admin/map?q=0&r=%d", 10*world.Height()))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, fmt.Sprintf("(0, %d)", world.Height())) {
		t.Fatalf("a window past the south pole is not clamped to row %d", world.Height())
	}
	if strings.Count(body, "<polygon") == 0 {
		t.Fatal("a window past the south pole drew nothing")
	}
}

// The whole world is a PNG. It is the thing the page deliberately is not.
func TestAdminMapImageServesTheWholeWorld(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"},
		&testStore{game: testMapGame(), world: testMapWorld()},
		"/admin/map.png")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "marajanda-world.png") {
		t.Fatalf("Content-Disposition = %q, want a filename", got)
	}
	image, err := png.Decode(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	// Every hex of the world, at worldImageHexSize, with none collapsed.
	if width := image.Bounds().Dx(); width < testMapWorld().Columns() {
		t.Fatalf("image is %d pixels wide for %d columns", width, testMapWorld().Columns())
	}
}

// A player has no business fetching the world.
func TestAdminMapImageRefusesAPlayer(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin},
		&testStore{game: testMapGame(), world: testMapWorld()},
		"/admin/map.png")

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
}

func TestPlayerMapRevealsOnlyVisibleHexes(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin},
		&testStore{
			game:    testMapGame(),
			world:   testMapWorld(),
			faction: datastore.Faction{Name: "Star Kin", Race: game.RaceHuman, Location: hexg.NewHex(0, 0)},
			found:   true,
			visible: []hexg.Hex{playerOrigin},
		},
		"/player/map")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	polygons := strings.Count(body, "<polygon")
	if want := len(game.PlayerView(testMapWorld(), playerOrigin, playerFogMargin, []hexg.Hex{playerOrigin})); polygons != want {
		t.Fatalf("polygons = %d, want %d", polygons, want)
	}
	if fog := strings.Count(body, `<polygon class="fog"`); fog != polygons-1 {
		t.Fatalf("fog polygons = %d, want %d", fog, polygons-1)
	}

	// The origin hex is the only terrain a player can see today. It renders at
	// its true coordinate now: there is no per-account frame left to hide it in,
	// and on a map everyone shares that is the point rather than a leak.
	origin, ok := testMapWorld().At(playerOrigin)
	if !ok {
		t.Fatalf("test world is missing the player origin %v", playerOrigin)
	}
	if want := formatCoord(playerOrigin) + " " + string(origin.Terrain) + ","; !strings.Contains(body, want) {
		t.Fatalf("player map missing %q", want)
	}
	if !strings.Contains(body, "Star Kin") || !strings.Contains(body, `href="/player/dashboard"`) {
		t.Fatal("player map missing faction name or dashboard link")
	}

	// A player's map is the whole of their map. There is nothing to pan to, no
	// control offering to, and no world image to fetch.
	for _, unwanted := range []string{`class="map-pan"`, `href="/admin/map.png"`, "/admin/map?"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("player map offers %q", unwanted)
		}
	}
}

// Fog is labelled but not located. A margin that printed coordinates would be a
// ruler laid over the world, and two readings of it place the player exactly.
func TestPlayerMapDoesNotLocateFog(t *testing.T) {
	world := testMapWorld()
	// Hard against the north ice, which is the thing a player must not be able
	// to measure their distance from.
	origin := hexg.NewHex(0, -world.Height()+1)
	response := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: origin},
		&testStore{
			game:    testMapGame(),
			world:   world,
			faction: datastore.Faction{Name: "Star Kin", Race: game.RaceHuman, Location: hexg.NewHex(0, 0)},
			found:   true,
			visible: []hexg.Hex{origin},
		},
		"/player/map")

	body := response.Body.String()
	fog := strings.Count(body, `<polygon class="fog"`)
	if fog == 0 {
		t.Fatal("player map drew no fog")
	}
	if got := strings.Count(body, "<title>Unexplored</title>"); got != fog {
		t.Fatalf("%d fog hexes carry %d bare labels, want them all bare", fog, got)
	}
	// The row beyond the pole is drawn, and reads exactly like unexplored land.
	if want := len(game.PlayerView(world, origin, playerFogMargin, []hexg.Hex{origin})); strings.Count(body, "<polygon") != want {
		t.Fatalf("polygons = %d, want %d: the map shrank against the ice", strings.Count(body, "<polygon"), want)
	}
}

func TestMapsRequireSessionAndRole(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
		role   string
		want   string
	}{
		{name: "admin map without session", target: "/admin/map", want: "/sign-in"},
		{name: "player map without session", target: "/player/map", want: "/sign-in"},
		{name: "player reaching the admin map", target: "/admin/map", role: "player", want: "/player/dashboard"},
		{name: "admin reaching the player map", target: "/player/map", role: "admin", want: "/admin/dashboard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response *httptest.ResponseRecorder
			if test.role == "" {
				response = serveRequest(newHandler(nil, &testStore{}), http.MethodGet, test.target)
			} else {
				response = signedInMap(t,
					datastore.Account{Email: test.role + "@example.com", Handle: "someone", Role: test.role},
					&testStore{game: testMapGame(), world: testMapWorld(), found: true, faction: datastore.Faction{Name: "Star Kin", Race: game.RaceHuman}},
					test.target)
			}
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != test.want {
				t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, test.want)
			}
		})
	}
}

func TestPlayerMapRequiresConfiguredFaction(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin},
		&testStore{game: testMapGame(), world: testMapWorld()},
		"/player/map")

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/faction" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/player/faction")
	}
}

func TestBuildMapViewGeometry(t *testing.T) {
	empty := buildMapView(func(coord hexg.Hex) hexg.Hex { return coord }, nil, defaultMapHexSize)
	if empty.ViewBox != "" || len(empty.Tiles) != 0 || empty.Width != 0 || empty.Height != 0 {
		t.Fatalf("buildMapView(nil) = %#v, want an empty view", empty)
	}

	small, err := game.GenerateWorld(testViewSeeds(), 1, 1)
	if err != nil {
		t.Fatalf("GenerateWorld: %v", err)
	}
	place := func(coord hexg.Hex) hexg.Hex { return worldmap.Cut(small.Width(), 0, coord) }
	tiles := game.WindowView(small, hexg.NewHex(0, 0), small.Columns(), small.Rows())
	view := buildMapView(place, tiles, defaultMapHexSize)
	if want := small.Len(); len(view.Tiles) != want {
		t.Fatalf("tiles = %d, want %d", len(view.Tiles), want)
	}
	if view.ViewBox == "" {
		t.Fatal("viewBox is empty")
	}
	for _, tile := range view.Tiles {
		if corners := len(strings.Fields(tile.Points)); corners != 6 {
			t.Fatalf("tile %q has %d corners, want 6", tile.Points, corners)
		}
	}

	// The drawn size is the map's own, in pixels, so the browser scrolls it
	// rather than scaling it. Three columns of pointy-top hexes at radius 24
	// are 3*sqrt(3)*24 wide plus the half-column shove; three rows are
	// 2*24 + 2*(1.5*24) tall.
	wantWidth := int(math.Ceil(3.5*math.Sqrt(3)*defaultMapHexSize + 2*mapPadding))
	wantHeight := int(math.Ceil(2*defaultMapHexSize + 2*1.5*defaultMapHexSize + 2*mapPadding))
	if view.Width != wantWidth || view.Height != wantHeight {
		t.Fatalf("drawn size = %d x %d, want %d x %d", view.Width, view.Height, wantWidth, wantHeight)
	}

	// The size is a parameter, not a constant: halving it halves the map.
	half := buildMapView(place, tiles, defaultMapHexSize/2)
	if half.Width*2 < view.Width-4 || half.Width*2 > view.Width+4 {
		t.Fatalf("halving the hex size gave a map %d wide against %d", half.Width, view.Width)
	}

	// Stable order in means stable markup out, so a rendered map does not churn
	// between identical requests.
	repeat := buildMapView(place, tiles, defaultMapHexSize)
	if repeat.ViewBox != view.ViewBox {
		t.Fatalf("viewBox differs: %q then %q", view.ViewBox, repeat.ViewBox)
	}
	for index := range view.Tiles {
		if view.Tiles[index] != repeat.Tiles[index] {
			t.Fatalf("tile %d differs: %#v then %#v", index, view.Tiles[index], repeat.Tiles[index])
		}
	}
}

func testViewSeeds() prng.Seeds {
	seed2 := int64(-98)
	return prng.New(98374, uint64(seed2))
}

func testMapGame() datastore.Game {
	return datastore.Game{Seed1: 98374, Seed2: -98, Width: testMapWidth, Height: testMapHeight}
}

func formatCoord(hex hexg.Hex) string {
	return fmt.Sprintf("(%d, %d)", hex.Q(), hex.R())
}

// The cut must place the world as an upright rectangle.
//
// Canonical coordinates fix q rather than the offset column, so plotting them
// straight from their axial position leans the map sideways by half a row per
// row. worldmap.Cut is what undoes that, and this is the assertion that it
// actually tiles: every row holds every column exactly once.
func TestRectangularCutTilesTheWorld(t *testing.T) {
	world := testMapWorld()

	seen := make(map[[2]int]hexg.Hex, world.Len())
	for _, hex := range world.Hexes() {
		placed := worldmap.Cut(world.Width(), 0, hex.Coord)
		offset := placed.CubeToROffset(mapOffsetEven)

		if offset.Col < -world.Width() || offset.Col > world.Width() {
			t.Fatalf("%v placed at column %d, outside [-%d,%d]",
				hex.Coord, offset.Col, world.Width(), world.Width())
		}
		if offset.Row != hex.Coord.R() {
			t.Fatalf("%v placed on row %d, want %d", hex.Coord, offset.Row, hex.Coord.R())
		}
		key := [2]int{offset.Col, offset.Row}
		if other, clash := seen[key]; clash {
			t.Fatalf("%v and %v both placed at column %d row %d",
				hex.Coord, other, offset.Col, offset.Row)
		}
		seen[key] = hex.Coord
	}
	if len(seen) != world.Len() {
		t.Fatalf("placed %d hexes into %d cells", world.Len(), len(seen))
	}

	// A parallelogram would be markedly wider than the rectangle it should be:
	// the lean adds half a column per row on top of the world's true width.
	view := buildMapView(func(coord hexg.Hex) hexg.Hex { return worldmap.Cut(world.Width(), 0, coord) },
		game.WindowView(world, hexg.NewHex(0, 0), world.Columns(), world.Rows()), defaultMapHexSize)
	var minX, maxX float64
	if _, err := fmt.Sscanf(view.ViewBox, "%f %f %f %f", &minX, new(float64), &maxX, new(float64)); err != nil {
		t.Fatalf("viewBox %q: %v", view.ViewBox, err)
	}
	// Pointy-top hexes are sqrt(3)*size apart across a row, plus half that for
	// the row shove, plus padding on both sides.
	want := (float64(world.Columns())+0.5)*math.Sqrt(3)*defaultMapHexSize + 2*mapPadding
	if maxX > want*1.02 {
		t.Fatalf("map is %.0f wide, want about %.0f: the cut is leaning", maxX, want)
	}
}

// jumpForm returns the jump box's markup, so a test can assert on the field
// itself rather than on the whole page.
func jumpForm(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<form class="map-jump"`)
	if start < 0 {
		t.Fatal("the page has no jump box")
	}
	end := strings.Index(body[start:], "</form>")
	if end < 0 {
		t.Fatal("the jump box is not closed")
	}
	return body[start : start+end]
}

// The jump box belongs to the admin's window: it moves the window, and a
// player has no window to move.
func TestJumpBoxBelongsToTheAdminMap(t *testing.T) {
	store := &testStore{game: testMapGame(), world: testMapWorld()}
	form := jumpForm(t, signedInMap(t,
		datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"},
		store, "/admin/map").Body.String())

	for _, want := range []string{`action="/admin/map"`, `method="get"`, `name="at"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("jump box missing %q", want)
		}
	}

	player := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin},
		&testStore{
			game:    testMapGame(),
			world:   testMapWorld(),
			faction: datastore.Faction{Name: "Star Kin", Race: game.RaceHuman, Location: hexg.NewHex(0, 0)},
			found:   true,
			visible: []hexg.Hex{playerOrigin},
		},
		"/player/map").Body.String()
	// The stylesheet is the whole site's, so the class name alone is on every
	// page. It is the form that must not be.
	if strings.Contains(player, `<form class="map-jump"`) || strings.Contains(player, `name="at"`) {
		t.Fatal("the player map offers a jump box")
	}
}

// A coordinate a person can name centres the window on it, and anything that is
// not a hex of the world goes back to the origin instead. Either way the box
// comes back empty: the page's centre says where the window is.
func TestAdminMapJumpsToACoordinate(t *testing.T) {
	world := testMapWorld()
	store := &testStore{game: testMapGame(), world: world}
	account := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}

	target := hexg.NewHex(12, -4)
	if !world.Contains(target) {
		t.Fatalf("the test world is missing %v", formatCoord(target))
	}
	// The same hex named from a column past the meridian: columns wrap, so this
	// is a real hex of the cylinder rather than a typo.
	wrapped := hexg.NewHex(target.Q()+world.Columns(), target.R())
	if world.Normalize(wrapped) != target {
		t.Fatalf("%v does not normalize to %v", formatCoord(wrapped), formatCoord(target))
	}

	origin := hexg.NewHex(0, 0)
	for _, test := range []struct {
		name string
		at   string
		want hexg.Hex
	}{
		{name: "a coordinate", at: "12,-4", want: target},
		{name: "surrounding whitespace", at: "  12 , -4  ", want: target},
		{name: "a wrapping column", at: formatPair(wrapped), want: target},
		{name: "not a number", at: "abc", want: origin},
		{name: "one number", at: "12", want: origin},
		{name: "three numbers", at: "1,2,3", want: origin},
		{name: "empty", at: "", want: origin},
		{name: "a row past the south pole", at: fmt.Sprintf("0,%d", world.Height()+1), want: origin},
		{name: "a row past the north pole", at: fmt.Sprintf("0,%d", -world.Height()-1), want: origin},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := signedInMap(t, account, store,
				"/admin/map?"+url.Values{"at": {test.at}}.Encode())
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			body := response.Body.String()
			if want := `<span class="here">` + formatCoord(test.want) + `</span>`; !strings.Contains(body, want) {
				t.Fatalf("the window is not centred on %v", formatCoord(test.want))
			}
			if strings.Count(body, "<polygon") == 0 {
				t.Fatal("the jump drew nothing")
			}
			// Nothing echoes the submitted coordinate back into the box.
			if form := jumpForm(t, body); strings.Contains(form, "value=") {
				t.Fatalf("the jump box came back filled in: %s", form)
			}
		})
	}
}

// The pan links keep their own reading of the query, which clamps a row rather
// than refusing it: half a window past a pole is a window a person asked for.
func TestPanLinksStillClampWhileJumpsDoNot(t *testing.T) {
	world := testMapWorld()
	store := &testStore{game: testMapGame(), world: world}
	account := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	row := world.Height() + 1

	panned := signedInMap(t, account, store, fmt.Sprintf("/admin/map?q=0&r=%d", row)).Body.String()
	if want := formatCoord(hexg.NewHex(0, world.Height())); !strings.Contains(panned, want) {
		t.Fatalf("a pan to row %d is no longer clamped to %s", row, want)
	}
	jumped := signedInMap(t, account, store, fmt.Sprintf("/admin/map?at=0%%2C%d", row)).Body.String()
	if want := `<span class="here">(0, 0)</span>`; !strings.Contains(jumped, want) {
		t.Fatalf("a jump to row %d did not return to the origin", row)
	}
}

// formatPair writes a coordinate the way the jump box takes it.
func formatPair(hex hexg.Hex) string {
	return fmt.Sprintf("%d,%d", hex.Q(), hex.R())
}

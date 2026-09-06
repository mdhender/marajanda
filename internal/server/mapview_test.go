// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
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
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return account, true, nil
	}, store)
	signIn := submitSignIn(handler, account.Email, "good.luck")
	cookies := signIn.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("sign-in cookies = %d, want 1", len(cookies))
	}
	return requestWithCookie(handler, http.MethodGet, target, cookies[0], "")
}

func TestAdminMapDrawsTheWholeDisc(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"},
		&testStore{game: testMapGame(), world: testMapWorld()},
		"/admin/map")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	// The admin map draws the whole world, not a window onto it.
	if want := testMapWorld().Len(); strings.Count(body, "<polygon") != want {
		t.Fatalf("polygons = %d, want %d", strings.Count(body, "<polygon"), want)
	}
	origin, ok := testMapWorld().At(hexg.NewHex(0, 0))
	if !ok {
		t.Fatal("test world is missing the game origin")
	}
	for _, want := range []string{
		`<svg viewBox="`, `role="img"`, `<polygon class="mountains"`,
		fmt.Sprintf("(0, 0) %s,", origin.Terrain), `href="/admin/dashboard"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin map missing %q", want)
		}
	}
	if strings.Contains(body, `<polygon class="fog"`) {
		t.Fatal("admin map contains fog; admins see every hex")
	}
}

func TestPlayerMapRevealsOnlyVisibleHexes(t *testing.T) {
	response := signedInMap(t,
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin},
		&testStore{
			game:    testMapGame(),
			world:   testMapWorld(),
			faction: datastore.Faction{Name: "Star Kin", Location: hexg.NewHex(0, 0)},
			found:   true,
			visible: []hexg.Hex{playerOrigin},
		},
		"/player/map")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	polygons := strings.Count(body, "<polygon")
	if want := len(game.PlayerView(testMapWorld(), playerOrigin, playerMapRadius, []hexg.Hex{playerOrigin})); polygons != want {
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
					&testStore{game: testMapGame(), world: testMapWorld(), found: true, faction: datastore.Faction{Name: "Star Kin"}},
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
	empty := buildMapView(func(coord hexg.Hex) hexg.Hex { return coord }, nil)
	if empty.ViewBox != "" || len(empty.Tiles) != 0 {
		t.Fatalf("buildMapView(nil) = %#v, want an empty view", empty)
	}

	small, err := game.GenerateWorld(testViewSeeds(), 1, 1)
	if err != nil {
		t.Fatalf("GenerateWorld: %v", err)
	}
	place := func(coord hexg.Hex) hexg.Hex { return worldmap.Cut(small.Width(), 0, coord) }
	view := buildMapView(place, game.AdminView(small))
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
	// Stable order in means stable markup out, so a rendered map does not churn
	// between identical requests.
	repeat := buildMapView(place, game.AdminView(small))
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

// The whole-world map must draw as an upright rectangle.
//
// Canonical coordinates fix q rather than the offset column, so plotting them
// straight from their axial position leans the map sideways by half a row per
// row. rectangular is the cut that undoes that, and this is the assertion that
// it actually tiles: every row holds every column exactly once.
// A survey may lose a lake. It may not lose the edge of the world: the polar
// ice is one row deep, so a plain majority vote over a stride-by-stride block
// would drop it and draw a world whose continents run off the top of the map.
func TestSurveyKeepsThePolarIce(t *testing.T) {
	world := testMapWorld()
	const stride = 2

	tiles := game.AdminView(world)
	frozen := make(map[[2]int]bool)
	for _, tile := range tiles {
		if tile.Terrain == game.TerrainIce {
			frozen[surveyBlock(world, tile.Coord, stride)] = true
		}
	}
	if len(frozen) == 0 {
		t.Fatal("test world has no ice")
	}

	kept := surveyTiles(world, tiles, stride)
	drawn := 0
	for _, tile := range kept {
		block := surveyBlock(world, tile.Coord, stride)
		if frozen[block] {
			if tile.Terrain != game.TerrainIce {
				t.Fatalf("block %v holds ice but is drawn as %q", block, tile.Terrain)
			}
			drawn++
			continue
		}
		if tile.Terrain == game.TerrainIce {
			t.Fatalf("block %v is drawn as ice but holds none", block)
		}
	}
	if drawn != len(frozen) {
		t.Fatalf("survey drew %d ice blocks, want %d", drawn, len(frozen))
	}
}

// surveyBlock names the block a coordinate falls in, the way surveyTiles does.
func surveyBlock(world game.World, coord hexg.Hex, stride int) [2]int {
	offset := worldmap.Cut(world.Width(), 0, coord).CubeToROffset(mapOffsetEven)
	return [2]int{(offset.Col + world.Width()) / stride, (offset.Row + world.Height()) / stride}
}

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
		game.AdminView(world))
	var minX, maxX float64
	if _, err := fmt.Sscanf(view.ViewBox, "%f %f %f %f", &minX, new(float64), &maxX, new(float64)); err != nil {
		t.Fatalf("viewBox %q: %v", view.ViewBox, err)
	}
	// Pointy-top hexes are sqrt(3)*size apart across a row, plus half that for
	// the row shove, plus padding on both sides.
	want := (float64(world.Columns())+0.5)*math.Sqrt(3)*mapHexSize + 2*mapPadding
	if maxX > want*1.02 {
		t.Fatalf("map is %.0f wide, want about %.0f: the cut is leaning", maxX, want)
	}
}

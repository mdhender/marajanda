// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
)

// playerOrigin is far enough from the game origin that its coordinates cannot
// be confused with the player-relative ones the page is allowed to print.
var playerOrigin = hexg.NewHex(7, -16)

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
		&testStore{game: datastore.Game{Seed1: 98374, Seed2: -98}},
		"/admin/map")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()

	if want := len(game.AdminView(testViewSeeds(), adminMapRadius)); strings.Count(body, "<polygon") != want {
		t.Fatalf("polygons = %d, want %d", strings.Count(body, "<polygon"), want)
	}
	for _, want := range []string{`<svg viewBox="`, `role="img"`, `<polygon class="mountains"`, "(0, 0) mountains", `href="/admin/dashboard"`} {
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
		datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin, Rotation: 3},
		&testStore{
			game:    datastore.Game{Seed1: 98374, Seed2: -98},
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
	if want := len(game.PlayerView(testViewSeeds(), playerOrigin, 3, playerMapRadius, []hexg.Hex{playerOrigin})); polygons != want {
		t.Fatalf("polygons = %d, want %d", polygons, want)
	}
	if fog := strings.Count(body, `<polygon class="fog"`); fog != polygons-1 {
		t.Fatalf("fog polygons = %d, want %d", fog, polygons-1)
	}

	// The origin hex is the only terrain a player can see today, and it renders
	// at the centre of their own map rather than at its true coordinate.
	terrain := game.TerrainAt(testViewSeeds(), playerOrigin)
	if want := "(0, 0) " + string(terrain); !strings.Contains(body, want) {
		t.Fatalf("player map missing %q", want)
	}
	if !strings.Contains(body, "Star Kin") || !strings.Contains(body, `href="/player/dashboard"`) {
		t.Fatal("player map missing faction name or dashboard link")
	}
}

// TestPlayerMapNeverPrintsTrueCoordinates is the guard that keeps players from
// locating one another: a player who can read true coordinates off their own
// map can triangulate every other player's origin.
func TestPlayerMapNeverPrintsTrueCoordinates(t *testing.T) {
	for _, rotation := range []int{0, 1, 2, 3, 4, 5} {
		response := signedInMap(t,
			datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Origin: playerOrigin, Rotation: rotation},
			&testStore{
				game:    datastore.Game{Seed1: 98374, Seed2: -98},
				faction: datastore.Faction{Name: "Star Kin", Location: hexg.NewHex(0, 0)},
				found:   true,
				visible: []hexg.Hex{playerOrigin},
			},
			"/player/map")
		body := response.Body.String()

		for _, coord := range game.AdminView(testViewSeeds(), playerMapRadius) {
			location := game.ToTrue(playerOrigin, rotation, coord.Coord)
			if location.Equals(hexg.NewHex(0, 0)) {
				continue // the game origin is not this player's secret to keep
			}
			label := formatCoord(location)
			if strings.Contains(body, label) {
				t.Fatalf("rotation %d leaked true coordinate %s", rotation, label)
			}
		}
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
					&testStore{game: datastore.Game{Seed1: 98374, Seed2: -98}, found: true, faction: datastore.Faction{Name: "Star Kin"}},
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
		&testStore{game: datastore.Game{Seed1: 98374, Seed2: -98}},
		"/player/map")

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/faction" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/player/faction")
	}
}

func TestBuildMapViewGeometry(t *testing.T) {
	empty := buildMapView(nil)
	if empty.ViewBox != "" || len(empty.Tiles) != 0 {
		t.Fatalf("buildMapView(nil) = %#v, want an empty view", empty)
	}

	view := buildMapView(game.AdminView(testViewSeeds(), 1))
	if len(view.Tiles) != 7 {
		t.Fatalf("tiles = %d, want 7", len(view.Tiles))
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
	repeat := buildMapView(game.AdminView(testViewSeeds(), 1))
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

func formatCoord(hex hexg.Hex) string {
	return fmt.Sprintf("(%d, %d)", hex.Q(), hex.R())
}

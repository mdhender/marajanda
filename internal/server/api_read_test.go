// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func apiReadStore() *testStore {
	world := testMapWorld()
	entity := datastore.Entity{
		ID: 7, Code: "LEADER-1", Name: "Pathfinder", Kind: game.EntityKindLeader,
		Location: hexg.NewHex(2, -1), Allowance: 6,
	}
	return &testStore{
		faction: datastore.Faction{Name: "The Wayfarers", Race: game.RaceElf, Active: true},
		found:   true,
		game:    datastore.Game{Seed1: 98374, Seed2: -98, Width: world.Width(), Height: world.Height()},
		world:   world,
		turn:    3,
		entities: []datastore.Entity{
			entity,
			{ID: 8, Code: "HAMLET-1", Name: "HAMLET-1", Kind: game.EntityKindHamlet, Location: entity.Location},
		},
		orders: map[int64][]datastore.Order{
			7: {
				{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}},
				{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 2}},
			},
		},
		visible: []hexg.Hex{world.Hexes()[1].Coord, world.Hexes()[world.Len()-2].Coord},
	}
}

func apiRead(t *testing.T, store *testStore, role, target string) *apiResponse {
	t.Helper()
	token := make([]byte, datastore.SessionTokenBytes)
	store.sessionAccount = datastore.Account{
		Email: "player@example.com", Handle: "wanderer", Role: role, Active: true,
		Seated: true, Origin: hexg.NewHex(2, -1),
	}
	store.sessions = map[string]bool{string(token): true}
	response := apiRequest(newHandler(nil, store), http.MethodGet, target, "", map[string]string{
		"Authorization": "Bearer " + base64.RawURLEncoding.EncodeToString(token),
	})
	return &apiResponse{ResponseRecorder: response}
}

// apiResponse gives the read tests a short status assertion without hiding
// the shared JSON decoder or error assertions.
type apiResponse struct {
	*httptest.ResponseRecorder
}

func (response *apiResponse) requireOK(t *testing.T) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestAPIGameSeparatesAdminSecretsFromPlayerState(t *testing.T) {
	store := apiReadStore()
	player := apiRead(t, store, "player", "/api/v1/game")
	player.requireOK(t)
	var playerGame apiGame
	decodeAPIResponse(t, player.ResponseRecorder, &playerGame)
	if playerGame.CurrentTurn != 3 || playerGame.Width != testMapWidth || playerGame.Height != testMapHeight || playerGame.Seeds != nil {
		t.Fatalf("player game = %#v", playerGame)
	}

	admin := apiRead(t, store, "admin", "/api/v1/game")
	admin.requireOK(t)
	var adminGame apiGame
	decodeAPIResponse(t, admin.ResponseRecorder, &adminGame)
	if !reflect.DeepEqual(adminGame.Seeds, []int64{98374, -98}) {
		t.Fatalf("admin seeds = %v", adminGame.Seeds)
	}
}

func TestAPIFactionReturnsConfiguredInactiveFaction(t *testing.T) {
	store := apiReadStore()
	store.faction.Active = false
	response := apiRead(t, store, "player", "/api/v1/faction")
	response.requireOK(t)
	var got apiFaction
	decodeAPIResponse(t, response.ResponseRecorder, &got)
	want := apiFaction{Name: "The Wayfarers", Race: "elf", Configured: true}
	if got != want {
		t.Fatalf("faction = %#v, want %#v", got, want)
	}
}

func TestAPIEntitiesReportFactsAsOfTheirTurn(t *testing.T) {
	store := apiReadStore()
	response := apiRead(t, store, "player", "/api/v1/entities")
	response.requireOK(t)
	var got apiEntities
	decodeAPIResponse(t, response.ResponseRecorder, &got)
	if got.Turn != 3 || store.asOf != got.Turn || len(got.Entities) != 2 {
		t.Fatalf("entities = %#v, store read turn %d", got, store.asOf)
	}
	leader := got.Entities[0]
	if leader.ID != 7 || leader.Code != "LEADER-1" || leader.Name != "Pathfinder" || leader.Kind != "leader" ||
		leader.Location != (apiCoordinate{Q: 2, R: -1}) || leader.Allowance != 6 ||
		!reflect.DeepEqual(leader.OrderKinds, []string{"move", "rest"}) {
		t.Fatalf("leader = %#v", leader)
	}
	if got.Entities[1].Allowance != 0 || len(got.Entities[1].OrderKinds) != 0 {
		t.Fatalf("hamlet = %#v", got.Entities[1])
	}
}

func TestAPIMapReturnsWholeWorldToAdminAndOnlyVisibleHexesToPlayer(t *testing.T) {
	store := apiReadStore()
	admin := apiRead(t, store, "admin", "/api/v1/map")
	admin.requireOK(t)
	var adminMap apiMap
	decodeAPIResponse(t, admin.ResponseRecorder, &adminMap)
	if adminMap.Turn != 3 || adminMap.Width != testMapWidth || adminMap.Height != testMapHeight || len(adminMap.Hexes) != store.world.Len() {
		t.Fatalf("admin map = turn %d dimensions %dx%d hexes %d, want %d", adminMap.Turn, adminMap.Width, adminMap.Height, len(adminMap.Hexes), store.world.Len())
	}

	player := apiRead(t, store, "player", "/api/v1/map")
	player.requireOK(t)
	var playerMap apiMap
	decodeAPIResponse(t, player.ResponseRecorder, &playerMap)
	if len(playerMap.Hexes) != len(store.visible) {
		t.Fatalf("player hexes = %d, want %d", len(playerMap.Hexes), len(store.visible))
	}
	wanted := make(map[apiCoordinate]bool, len(store.visible))
	for _, coord := range store.visible {
		wanted[apiCoordinateFromHex(coord)] = true
	}
	for _, hex := range playerMap.Hexes {
		if !wanted[hex.Coordinate] || hex.Terrain == "" {
			t.Fatalf("player map exposed unexpected hex %#v", hex)
		}
	}
}

func TestAPIOrdersPreserveDetailsSequenceAndEstimate(t *testing.T) {
	store := apiReadStore()
	response := apiRead(t, store, "player", "/api/v1/orders")
	response.requireOK(t)
	var got apiOrders
	decodeAPIResponse(t, response.ResponseRecorder, &got)
	if got.Turn != 3 || store.asOf != got.Turn || len(got.Entities) != 2 {
		t.Fatalf("orders = %#v, store read turn %d", got, store.asOf)
	}
	leader := got.Entities[0]
	if leader.EntityID != 7 || len(leader.Orders) != 2 || leader.Orders[0].Sequence != 1 ||
		leader.Orders[0].Detail.Direction == nil || *leader.Orders[0].Detail.Direction != "ne" ||
		leader.Orders[1].Sequence != 2 || leader.Orders[1].Detail.Count == nil || *leader.Orders[1].Detail.Count != 2 {
		t.Fatalf("leader orders = %#v", leader.Orders)
	}
	estimate := leader.Estimate
	if estimate.Allowance != 6 || estimate.Committed != 5 || estimate.Total != 5 || estimate.Residue != 1 ||
		estimate.Overspend != 0 || estimate.ExhaustsAt != nil || len(estimate.Orders) != 2 ||
		estimate.Orders[0].Cost == nil || *estimate.Orders[0].Cost != 3 || estimate.Orders[1].Cost == nil || *estimate.Orders[1].Cost != 2 {
		t.Fatalf("leader estimate = %#v", estimate)
	}
	if len(got.Entities[1].Orders) != 0 || len(got.Entities[1].Estimate.Orders) != 0 {
		t.Fatalf("hamlet orders = %#v", got.Entities[1])
	}
}

func TestAPIReadResourceAuthorizationAndUnconfiguredState(t *testing.T) {
	for _, target := range []string{"/api/v1/faction", "/api/v1/entities", "/api/v1/orders"} {
		t.Run("admin "+target, func(t *testing.T) {
			assertAPIError(t, apiRead(t, apiReadStore(), "admin", target).ResponseRecorder, http.StatusForbidden, apiCodeForbidden)
		})
	}
	for _, target := range []string{"/api/v1/faction", "/api/v1/entities", "/api/v1/map", "/api/v1/orders"} {
		t.Run("unconfigured "+target, func(t *testing.T) {
			store := apiReadStore()
			store.found = false
			assertAPIError(t, apiRead(t, store, "player", target).ResponseRecorder, http.StatusNotFound, apiCodeFactionNotConfigured)
		})
	}
	for _, target := range []string{"/api/v1/game", "/api/v1/faction", "/api/v1/entities", "/api/v1/map", "/api/v1/orders"} {
		t.Run("unauthenticated "+target, func(t *testing.T) {
			response := apiRequest(newHandler(nil, apiReadStore()), http.MethodGet, target, "", nil)
			assertAPIError(t, response, http.StatusUnauthorized, apiCodeAuthenticationNeeded)
		})
	}
}

func TestAPIReadResourceStoreFailures(t *testing.T) {
	failure := errors.New("store unavailable")
	for _, test := range []struct {
		name   string
		role   string
		target string
		fail   func(*testStore)
	}{
		{name: "game", role: "player", target: "/api/v1/game", fail: func(store *testStore) { store.gameErr = failure }},
		{name: "game turn", role: "player", target: "/api/v1/game", fail: func(store *testStore) { store.turnErr = failure }},
		{name: "faction", role: "player", target: "/api/v1/faction", fail: func(store *testStore) { store.factionErr = failure }},
		{name: "entities", role: "player", target: "/api/v1/entities", fail: func(store *testStore) { store.entitiesErr = failure }},
		{name: "admin map world", role: "admin", target: "/api/v1/map", fail: func(store *testStore) { store.worldErr = failure }},
		{name: "player map visibility", role: "player", target: "/api/v1/map", fail: func(store *testStore) { store.visibleErr = failure }},
		{name: "orders", role: "player", target: "/api/v1/orders", fail: func(store *testStore) { store.ordersErr = failure }},
		{name: "order estimates", role: "player", target: "/api/v1/orders", fail: func(store *testStore) { store.estimateErr = failure }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := apiReadStore()
			test.fail(store)
			assertAPIError(t, apiRead(t, store, test.role, test.target).ResponseRecorder, http.StatusInternalServerError, apiCodeInternal)
		})
	}
}

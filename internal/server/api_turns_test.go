// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// The store reads every one of these as of the turn the path names, and the
// body reports that turn back. apiReadStore sits on turn 3, so turn 2 is a
// closed turn the game has been through.
func TestAPITurnReadsAnswerForTheTurnInThePath(t *testing.T) {
	for _, test := range []struct {
		name, target string
		turn         func(*apiResponse, *testing.T) int
	}{
		{name: "entities", target: "/api/v1/turns/2/entities", turn: func(response *apiResponse, t *testing.T) int {
			var body apiEntities
			decodeAPIResponse(t, response.ResponseRecorder, &body)
			return body.Turn
		}},
		{name: "orders", target: "/api/v1/turns/2/orders", turn: func(response *apiResponse, t *testing.T) int {
			var body apiOrders
			decodeAPIResponse(t, response.ResponseRecorder, &body)
			return body.Turn
		}},
		{name: "map", target: "/api/v1/turns/2/map", turn: func(response *apiResponse, t *testing.T) int {
			var body apiMap
			decodeAPIResponse(t, response.ResponseRecorder, &body)
			return body.Turn
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := apiReadStore()
			response := apiRead(t, store, "player", test.target)
			response.requireOK(t)
			if got := test.turn(response, t); got != 2 {
				t.Errorf("body turn = %d, want 2", got)
			}
			if store.asOf != 2 {
				t.Errorf("store read as of turn %d, want 2", store.asOf)
			}
		})
	}
}

// `current` is a turn a client may name, so reading today never means reading
// the clock first and racing an advance between the two calls.
func TestAPITurnReadsAcceptCurrentByName(t *testing.T) {
	for _, target := range []string{"/api/v1/turns/current/entities", "/api/v1/turns/current/orders", "/api/v1/turns/current/map"} {
		t.Run(target, func(t *testing.T) {
			store := apiReadStore()
			apiRead(t, store, "player", target).requireOK(t)
			if store.asOf != store.turn {
				t.Errorf("store read as of turn %d, want the current %d", store.asOf, store.turn)
			}
		})
	}
}

// A turn the game has not reached is a 404, the way an unknown entity is: the
// address is the right shape for something that does not exist. A segment that
// is not a turn at all is the bad request it looks like.
func TestAPITurnReadsRefuseATurnTheGameHasNoAnswerFor(t *testing.T) {
	for _, resource := range []string{"entities", "orders", "map"} {
		for _, test := range []struct {
			name, segment string
			status        int
			code          string
		}{
			{name: "future", segment: "4", status: http.StatusNotFound, code: apiCodeTurnNotFound},
			{name: "far future", segment: "99999999", status: http.StatusNotFound, code: apiCodeTurnNotFound},
			{name: "zero", segment: "0", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
			{name: "negative", segment: "-1", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
			{name: "words", segment: "yesterday", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
			{name: "empty", segment: "%20", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		} {
			t.Run(resource+" "+test.name, func(t *testing.T) {
				response := apiRead(t, apiReadStore(), "player", "/api/v1/turns/"+test.segment+"/"+resource)
				assertAPIError(t, response.ResponseRecorder, test.status, test.code)
			})
		}
	}
}

// A past map is what the faction knew then, not today's visibility under an old
// date. The fake gives turn 2 one hex and the current turn two, so a read that
// ignored the turn would be caught by the count.
func TestAPITurnMapReportsTheKnowledgeOfThatTurn(t *testing.T) {
	store := apiReadStore()
	store.knownAsOf = map[int][]hexg.Hex{2: {store.visible[0]}}

	var past apiMap
	response := apiRead(t, store, "player", "/api/v1/turns/2/map")
	response.requireOK(t)
	decodeAPIResponse(t, response.ResponseRecorder, &past)

	var now apiMap
	response = apiRead(t, store, "player", "/api/v1/turns/current/map")
	response.requireOK(t)
	decodeAPIResponse(t, response.ResponseRecorder, &now)

	if len(past.Hexes) != 1 || len(now.Hexes) != 2 {
		t.Fatalf("turn 2 saw %d hexes and turn %d saw %d; want 1 and 2", len(past.Hexes), store.turn, len(now.Hexes))
	}
}

// An admin reads the whole world on any turn, because no turn changes it, but
// still cannot read a turn that does not exist.
func TestAPITurnMapServesAdminsAndRefusesPlayerOnlyResources(t *testing.T) {
	store := apiReadStore()
	var body apiMap
	response := apiRead(t, store, "admin", "/api/v1/turns/2/map")
	response.requireOK(t)
	decodeAPIResponse(t, response.ResponseRecorder, &body)
	if len(body.Hexes) != store.world.Len() {
		t.Errorf("admin saw %d hexes, want the whole world's %d", len(body.Hexes), store.world.Len())
	}
	for _, target := range []string{"/api/v1/turns/2/entities", "/api/v1/turns/2/orders"} {
		t.Run(target, func(t *testing.T) {
			assertAPIError(t, apiRead(t, apiReadStore(), "admin", target).ResponseRecorder, http.StatusForbidden, apiCodeForbidden)
		})
	}
}

// An order estimate on a closed turn is the one the player would have been
// shown while the turn was open, so a client checking a bug report gets the
// arithmetic that was actually on screen rather than a refusal or a blank.
func TestAPITurnOrdersCarryTheEstimateOfThatTurn(t *testing.T) {
	response := apiRead(t, apiReadStore(), "player", "/api/v1/turns/2/orders")
	response.requireOK(t)
	var past apiOrders
	decodeAPIResponse(t, response.ResponseRecorder, &past)

	entity := parityEntityOrders(t, past, 7)
	if len(entity.Orders) == 0 {
		t.Fatal("a closed turn reported no orders")
	}
	if entity.Estimate.Allowance == 0 || len(entity.Estimate.Orders) != len(entity.Orders) {
		t.Fatalf("estimate = %#v, want an allowance and one cost per order", entity.Estimate)
	}
}

// An unauthenticated turn read is refused before the turn is looked at, the way
// every other protected route is.
func TestAPITurnReadsRequireAuthentication(t *testing.T) {
	for _, target := range []string{"/api/v1/turns/2/entities", "/api/v1/turns/2/orders", "/api/v1/turns/2/map"} {
		t.Run(target, func(t *testing.T) {
			response := apiRequest(newHandler(nil, apiReadStore()), http.MethodGet, target, "", nil)
			assertAPIError(t, response, http.StatusUnauthorized, apiCodeAuthenticationNeeded)
		})
	}
}

// The scenario issue #61 describes, against the real store: play a turn through
// the API alone, advance, and read the closed turn back. What comes back has to
// be what the player was looking at when they ordered, estimate included, or a
// client that did not cache its own submission still cannot say what it did.
func TestAPITurnReadsRecoverAClosedTurnFromTheRealStore(t *testing.T) {
	store, err := datastore.OpenMemory(t.Context(), datastore.Game{
		Seed1: 98374, Seed2: -98,
		Width: datastore.MinimumWorldWidth, Height: datastore.MinimumWorldHeight,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := newHandler(store.Authenticate, store)

	player := createParitySession(t, handler, "player@marajanda.com", "good.luck")
	playerHeaders := map[string]string{"Authorization": "Bearer " + player.Token}
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPut, "/api/v1/faction",
		`{"name":"The Wayfarers","race":"human"}`, playerHeaders), http.StatusOK)

	var entities apiEntities
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/entities", "", playerHeaders), &entities)
	if entities.Turn != game.FirstTurn || len(entities.Entities) == 0 {
		t.Fatalf("entities on the first turn = %#v", entities)
	}
	var leader apiEntity
	for _, entity := range entities.Entities {
		if entity.Kind == "leader" {
			leader = entity
		}
	}
	if leader.ID == 0 {
		t.Fatalf("no leader among %#v", entities.Entities)
	}

	assertParityMutationStatus(t, apiRequest(handler, http.MethodPost,
		fmt.Sprintf("/api/v1/entities/%d/orders", leader.ID),
		`{"turn":1,"kind":"move","detail":{"direction":"ne"}}`, playerHeaders), http.StatusCreated)

	// What the player saw with the turn still open.
	var open apiOrders
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/orders", "", playerHeaders), &open)
	if open.Turn != game.FirstTurn {
		t.Fatalf("open orders = turn %d, want %d", open.Turn, game.FirstTurn)
	}

	admin := createParitySession(t, handler, "admin@marajanda.com", "good.luck")
	advanced := apiRequest(handler, http.MethodPost, "/api/v1/turns/current/advance", "",
		map[string]string{"Authorization": "Bearer " + admin.Token})
	assertParityMutationStatus(t, advanced, http.StatusOK)

	// The closed turn, read back after the clock moved.
	closedResponse := apiRequest(handler, http.MethodGet, "/api/v1/turns/1/orders", "", playerHeaders)
	if closedResponse.Code != http.StatusOK {
		t.Fatalf("closed turn orders = %d %s", closedResponse.Code, closedResponse.Body.String())
	}
	var closed apiOrders
	decodeAPIResponse(t, closedResponse, &closed)
	if !reflect.DeepEqual(open, closed) {
		t.Fatalf("the closed turn does not read back as it stood:\nopen   = %#v\nclosed = %#v", open, closed)
	}

	// And the current turn is a different answer, so the two are not the same
	// read wearing different numbers.
	var now apiOrders
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/orders", "", playerHeaders), &now)
	if now.Turn != game.FirstTurn+1 {
		t.Fatalf("current orders = turn %d, want %d", now.Turn, game.FirstTurn+1)
	}
	if len(parityEntityOrders(t, now, leader.ID).Orders) != 0 {
		t.Fatal("the new turn opened with the closed turn's orders still in it")
	}

	// The leader moved, so its turn-1 location is not its turn-2 location.
	var past apiEntities
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/turns/1/entities", "", playerHeaders), &past)
	var present apiEntities
	decodeAPIResponse(t, apiRequest(handler, http.MethodGet, "/api/v1/entities", "", playerHeaders), &present)
	if past.Turn != game.FirstTurn || present.Turn != game.FirstTurn+1 {
		t.Fatalf("entity reads = turns %d and %d", past.Turn, present.Turn)
	}
	if parityEntity(t, past, leader.ID).Location == parityEntity(t, present, leader.ID).Location {
		t.Fatal("the leader stands where it did, so this proves nothing about reading the past")
	}
	if parityEntity(t, past, leader.ID).Location != leader.Location {
		t.Fatalf("turn 1 reports the leader at %#v, but turn 1 put it at %#v",
			parityEntity(t, past, leader.ID).Location, leader.Location)
	}
}

func parityEntity(t *testing.T, entities apiEntities, id int64) apiEntity {
	t.Helper()
	for _, entity := range entities.Entities {
		if entity.ID == id {
			return entity
		}
	}
	t.Fatalf("entities contain no entity %d: %#v", id, entities)
	return apiEntity{}
}

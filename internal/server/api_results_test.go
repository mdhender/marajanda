// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// apiResultsStore is apiReadStore with the report of the turn that closed
// before the one it is sitting on.
func apiResultsStore() *testStore {
	store := apiReadStore()
	store.results = map[int][]datastore.TurnResult{
		2: {{
			Turn: 2, EntityID: 7, Allowance: 6, Spent: 1, Lapsed: 5,
			Start: hexg.NewHex(2, -1), End: hexg.NewHex(2, -1),
			Orders: []datastore.OrderResult{
				{Seq: 1, Kind: game.OrderKindMove, Cost: 1, Carried: false, Reason: game.FailureTerrain,
					From: hexg.NewHex(2, -1), Target: hexg.NewHex(2, -2), To: hexg.NewHex(2, -1)},
			},
			Observations: []game.Observation{
				{Seq: 1, Hex: hexg.NewHex(2, -2), State: game.KnowledgeObserved},
			},
		}},
	}
	return store
}

// The unscoped read is the latest processed turn, not the current one. It is
// the one read in this API where unscoped does not mean current, because the
// current turn has no results until it is closed.
func TestAPIResultsReadTheLatestProcessedTurn(t *testing.T) {
	store := apiResultsStore()
	response := apiRead(t, store, "player", "/api/v1/results")
	response.requireOK(t)

	var body apiResults
	decodeAPIResponse(t, response.ResponseRecorder, &body)
	if body.Turn != 2 {
		t.Fatalf("turn = %d, want 2 - the turn before the current %d", body.Turn, store.turn)
	}
	if store.resultsAsOf != 2 {
		t.Fatalf("store read as of turn %d, want 2", store.resultsAsOf)
	}
	if len(body.Entities) != 1 {
		t.Fatalf("entities = %#v, want one", body.Entities)
	}
	entity := body.Entities[0]
	if entity.EntityID != 7 || entity.Ledger.Allowance != 6 || entity.Ledger.Spent != 1 || entity.Ledger.Lapsed != 5 {
		t.Fatalf("ledger = %#v", entity)
	}
	if entity.Ledger.Start != (apiCoordinate{Q: 2, R: -1}) || entity.Ledger.End != (apiCoordinate{Q: 2, R: -1}) {
		t.Fatalf("ledger coordinates = %#v", entity.Ledger)
	}
	if len(entity.Orders) != 1 {
		t.Fatalf("orders = %#v, want one", entity.Orders)
	}
	outcome := entity.Orders[0]
	if outcome.Sequence != 1 || outcome.Kind != string(game.OrderKindMove) || outcome.Cost != 1 || outcome.Carried {
		t.Fatalf("outcome = %#v", outcome)
	}
	// A step that failed on terrain is charged in full and still names where it
	// was aimed, which is the only way a client can say what it walked into.
	if outcome.Reason == nil || *outcome.Reason != string(game.FailureTerrain) {
		t.Fatalf("reason = %#v, want terrain", outcome.Reason)
	}
	if outcome.Target != (apiCoordinate{Q: 2, R: -2}) || outcome.To != (apiCoordinate{Q: 2, R: -1}) {
		t.Fatalf("outcome coordinates = %#v", outcome)
	}
	if len(entity.Observations) != 1 {
		t.Fatalf("observations = %#v, want one", entity.Observations)
	}
	// The grains stay apart: an observation carries the sequence of the order
	// that revealed it rather than being nested inside that order.
	if observation := entity.Observations[0]; observation.Sequence != 1 ||
		observation.Coordinate != (apiCoordinate{Q: 2, R: -2}) ||
		observation.State != string(game.KnowledgeObserved) {
		t.Fatalf("observation = %#v", entity.Observations[0])
	}
}

// A carried order has no reason, and null says that where an empty string would
// read as a reason nobody named.
func TestAPIResultsReportNoReasonForACarriedOrder(t *testing.T) {
	store := apiResultsStore()
	store.results[2][0].Orders[0].Carried = true
	store.results[2][0].Orders[0].Reason = ""
	response := apiRead(t, store, "player", "/api/v1/results")
	response.requireOK(t)

	var body apiResults
	decodeAPIResponse(t, response.ResponseRecorder, &body)
	if reason := body.Entities[0].Orders[0].Reason; reason != nil {
		t.Fatalf("reason = %q, want null", *reason)
	}
	if !strings.Contains(response.Body.String(), `"reason":null`) {
		t.Fatalf("body does not spell the absent reason as null: %s", response.Body.String())
	}
}

// Before the first turn is processed there is nothing to report. The resource
// exists and the faction exists, so the answer is the empty account of no turn
// rather than a refusal.
func TestAPIResultsBeforeAnyTurnIsProcessed(t *testing.T) {
	store := apiResultsStore()
	store.turn = game.FirstTurn
	response := apiRead(t, store, "player", "/api/v1/results")
	response.requireOK(t)

	var body apiResults
	decodeAPIResponse(t, response.ResponseRecorder, &body)
	if body.Turn != game.StartOfTimeTurn {
		t.Fatalf("turn = %d, want %d - before the game's first turn", body.Turn, game.StartOfTimeTurn)
	}
	if len(body.Entities) != 0 {
		t.Fatalf("entities = %#v, want none", body.Entities)
	}
	if store.resultsAsOf != 0 {
		t.Fatalf("store was read as of turn %d with no processed turn", store.resultsAsOf)
	}
}

// The turn-scoped read answers for the turn in the path, `current` included.
// The current turn has no results until it is processed, and an empty
// collection is the true account of it.
func TestAPITurnResultsAnswerForTheTurnInThePath(t *testing.T) {
	for _, test := range []struct {
		name, target string
		want         int
		entities     int
	}{
		{name: "processed", target: "/api/v1/turns/2/results", want: 2, entities: 1},
		{name: "current", target: "/api/v1/turns/current/results", want: 3, entities: 0},
		{name: "first", target: "/api/v1/turns/1/results", want: 1, entities: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := apiResultsStore()
			response := apiRead(t, store, "player", test.target)
			response.requireOK(t)

			var body apiResults
			decodeAPIResponse(t, response.ResponseRecorder, &body)
			if body.Turn != test.want || store.resultsAsOf != test.want {
				t.Fatalf("turn = %d and store read as of %d, want %d", body.Turn, store.resultsAsOf, test.want)
			}
			if len(body.Entities) != test.entities {
				t.Fatalf("entities = %d, want %d", len(body.Entities), test.entities)
			}
		})
	}
}

// The turn-scoped read refuses what every other one does: a turn the game has
// not reached is a 404, and a segment that is not a turn is a bad request.
func TestAPITurnResultsRefuseATurnTheGameHasNoAnswerFor(t *testing.T) {
	for _, test := range []struct {
		name, segment string
		status        int
		code          string
	}{
		{name: "future", segment: "4", status: http.StatusNotFound, code: apiCodeTurnNotFound},
		{name: "zero", segment: "0", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "words", segment: "yesterday", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := apiRead(t, apiResultsStore(), "player", "/api/v1/turns/"+test.segment+"/results")
			assertAPIError(t, response.ResponseRecorder, test.status, test.code)
		})
	}
}

// A turn is selected by path, not by query. The unscoped read refuses a
// selector rather than answering one turn under a name the client meant as
// another.
func TestAPIResultsRefuseATurnSelector(t *testing.T) {
	for _, name := range apiTurnSelectorNames {
		t.Run(name, func(t *testing.T) {
			response := apiRead(t, apiResultsStore(), "player", "/api/v1/results?"+name+"=2")
			assertAPIError(t, response.ResponseRecorder, http.StatusBadRequest, apiCodeInvalidRequest)
		})
	}
}

// Results are a faction's own account of itself, so the resource is a player's.
func TestAPIResultsAreForbiddenToAnAdmin(t *testing.T) {
	for _, target := range []string{"/api/v1/results", "/api/v1/turns/2/results"} {
		t.Run(target, func(t *testing.T) {
			response := apiRead(t, apiResultsStore(), "admin", target)
			assertAPIError(t, response.ResponseRecorder, http.StatusForbidden, apiCodeForbidden)
		})
	}
}

// A player with no faction has nothing to report on, and says so with the same
// refusal every other faction resource makes.
func TestAPIResultsRefuseAPlayerWithNoFaction(t *testing.T) {
	store := apiResultsStore()
	store.found = false
	response := apiRead(t, store, "player", "/api/v1/results")
	assertAPIError(t, response.ResponseRecorder, http.StatusNotFound, apiCodeFactionNotConfigured)
}

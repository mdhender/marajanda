// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"
)

// The turn-scoped reads. A turn is a path segment here rather than a query
// parameter, matching POST /api/v1/turns/current/advance, which already treats
// a turn that way. Each route hands its turn to the same body the current-turn
// route uses, so the past is reported the way the present is.
//
// `current` is spelled out as well as numbered. A client that wants today asks
// for it by name instead of reading the clock first and racing an advance
// between the two calls.

func (app *application) getAPITurnEntities(w http.ResponseWriter, r *http.Request) {
	turn, ok := app.apiTurnFromPath(w, r)
	if !ok {
		return
	}
	app.writeAPIEntitiesAsOf(w, r, turn)
}

func (app *application) getAPITurnOrders(w http.ResponseWriter, r *http.Request) {
	turn, ok := app.apiTurnFromPath(w, r)
	if !ok {
		return
	}
	app.writeAPIOrdersAsOf(w, r, turn)
}

func (app *application) getAPITurnResults(w http.ResponseWriter, r *http.Request) {
	turn, ok := app.apiTurnFromPath(w, r)
	if !ok {
		return
	}
	app.writeAPIResultsAsOf(w, r, turn)
}

func (app *application) getAPITurnMap(w http.ResponseWriter, r *http.Request) {
	turn, ok := app.apiTurnFromPath(w, r)
	if !ok {
		return
	}
	app.writeAPIMapAsOf(w, r, turn)
}

// apiCurrentTurn reads the turn the game is on for a route that has no turn of
// its own, answering the request itself if the clock cannot be read.
func (app *application) apiCurrentTurn(w http.ResponseWriter, r *http.Request) (int, bool) {
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return 0, false
	}
	return turn, true
}

// apiTurnFromPath resolves the {turn} segment, answering the request itself
// when it cannot.
//
// Three refusals, and they are not the same refusal:
//
//   - `invalid_request` for a segment that is not a turn at all. The client
//     sent something no game could answer.
//   - `turn_not_found` for a well-formed turn the game has not reached. The
//     address is the right shape for a thing that does not exist, so it is a
//     404 the way an unknown entity is, and the query spelling would answer the
//     same rather than calling the same miss a bad request.
//
// A turn before the faction was seated is neither. It is a hit: the period
// joins find no fact covering that turn and the read answers with an empty
// collection, which is the true account of a faction that did not exist yet.
func (app *application) apiTurnFromPath(w http.ResponseWriter, r *http.Request) (int, bool) {
	current, ok := app.apiCurrentTurn(w, r)
	if !ok {
		return 0, false
	}
	segment := r.PathValue("turn")
	if segment == apiCurrentTurnSegment {
		return current, true
	}
	turn, ok := positiveAPIInt(w, segment, "turn")
	if !ok {
		return 0, false
	}
	if turn > current {
		writeAPIError(w, http.StatusNotFound, apiCodeTurnNotFound, "The game has not reached that turn.")
		return 0, false
	}
	return turn, true
}

// apiCurrentTurnSegment is the turn a client names when it means "whatever the
// game is on now".
const apiCurrentTurnSegment = "current"

// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// The results reads: what a processed turn did, in JSON.
//
// This is the API side of the turn report, and parity is why it exists at the
// same time as the page rather than after it. The vocabulary is fixed by
// docs/reference/turn-results.md - ledger, outcome, observation - so the JSON
// spells those rather than inventing names for them.

// getAPIResults answers the results read that names no turn.
//
// The unscoped read is the latest processed turn, which is the turn before the
// current one. It is the one exception to "unscoped means current" in this API,
// and it is the resource itself that makes it: results are written by turn
// processing, so the current turn has none until it is closed. A client asking
// for results with no turn in hand wants the turn that just happened, and
// answering with the empty current turn would be answering a question nobody
// asked.
//
// `GET /api/v1/turns/current/results` is still there for a client that means
// today, and it answers with today: an empty collection until the turn is
// processed.
func (app *application) getAPIResults(w http.ResponseWriter, r *http.Request) {
	if rejectAPITurnSelector(w, r) {
		return
	}
	current, ok := app.apiCurrentTurn(w, r)
	if !ok {
		return
	}
	latest := current - 1
	if !game.ValidTurn(latest) {
		// Before the first turn is processed there is no turn to report. The
		// answer is the empty account of no turn rather than a 404: the
		// resource exists, the faction exists, and nothing has happened yet.
		// The turn is game.StartOfTimeTurn, which is the value this project
		// already uses for "before the game's first turn".
		if _, ok := app.apiPlayerFaction(w, r); !ok {
			return
		}
		_ = writeAPIJSON(w, http.StatusOK, apiResults{Turn: game.StartOfTimeTurn, Entities: make([]apiEntityResult, 0)})
		return
	}
	app.writeAPIResultsAsOf(w, r, latest)
}

// writeAPIResultsAsOf answers a results read for one turn.
//
// A turn the game reached but has not processed answers with an empty
// collection, which is what the store answers with and what is true of it.
func (app *application) writeAPIResultsAsOf(w http.ResponseWriter, r *http.Request, turn int) {
	if _, ok := app.apiPlayerFaction(w, r); !ok {
		return
	}
	account := apiAuthenticationFromContext(r.Context()).Account
	results, err := app.store.ResultsAsOf(r.Context(), account.Email, turn)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	response := apiResults{Turn: turn, Entities: make([]apiEntityResult, 0, len(results))}
	for _, result := range results {
		response.Entities = append(response.Entities, apiEntityResultFromStore(result))
	}
	_ = writeAPIJSON(w, http.StatusOK, response)
}

// apiEntityResultFromStore is one entity's recorded turn.
//
// The three grains stay three. An observation carries the sequence of the order
// that revealed it rather than being nested inside that order: the record keeps
// them apart, a client that wants them nested can group by sequence, and
// nesting here would quietly lose a sighting whose order the record does not
// carry.
func apiEntityResultFromStore(result datastore.TurnResult) apiEntityResult {
	response := apiEntityResult{
		EntityID: result.EntityID,
		Ledger: apiResultLedger{
			Allowance: result.Allowance, Spent: result.Spent, Lapsed: result.Lapsed,
			Start: apiCoordinateFromHex(result.Start), End: apiCoordinateFromHex(result.End),
		},
		Orders:       make([]apiOrderOutcome, 0, len(result.Orders)),
		Observations: make([]apiObservation, 0, len(result.Observations)),
	}
	for _, order := range result.Orders {
		outcome := apiOrderOutcome{
			Sequence: order.Seq, Kind: string(order.Kind), Cost: order.Cost, Carried: order.Carried,
			From:   apiCoordinateFromHex(order.From),
			Target: apiCoordinateFromHex(order.Target),
			To:     apiCoordinateFromHex(order.To),
		}
		// A carried order has no reason, and null says that where an empty
		// string would read as a reason nobody named.
		if order.Reason != "" {
			reason := string(order.Reason)
			outcome.Reason = &reason
		}
		response.Orders = append(response.Orders, outcome)
	}
	for _, observation := range result.Observations {
		response.Observations = append(response.Observations, apiObservation{
			Sequence:   observation.Seq,
			Coordinate: apiCoordinateFromHex(observation.Hex),
			State:      string(observation.State),
		})
	}
	return response
}

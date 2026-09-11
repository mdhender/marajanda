// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func (app *application) getAPIGame(w http.ResponseWriter, r *http.Request) {
	stored, err := app.store.Game(r.Context())
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	turn, err := app.store.CurrentTurn(r.Context())
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	response := apiGame{CurrentTurn: turn, Width: stored.Width, Height: stored.Height}
	if apiAuthenticationFromContext(r.Context()).Account.Role == "admin" {
		response.Seeds = []int64{stored.Seed1, stored.Seed2}
	}
	_ = writeAPIJSON(w, http.StatusOK, response)
}

func (app *application) getAPIFaction(w http.ResponseWriter, r *http.Request) {
	faction, ok := app.apiPlayerFaction(w, r)
	if !ok {
		return
	}
	_ = writeAPIJSON(w, http.StatusOK, faction)
}

func (app *application) getAPIEntities(w http.ResponseWriter, r *http.Request) {
	if rejectAPITurnSelector(w, r) {
		return
	}
	turn, ok := app.apiCurrentTurn(w, r)
	if !ok {
		return
	}
	app.writeAPIEntitiesAsOf(w, r, turn)
}

// writeAPIEntitiesAsOf answers an entities read for one turn.
//
// The turn is the caller's. The current-turn route and the turn-scoped route
// differ in how they arrive at a turn and in nothing else, so what a past turn
// reports cannot drift away from what the present one reports.
func (app *application) writeAPIEntitiesAsOf(w http.ResponseWriter, r *http.Request, turn int) {
	if _, ok := app.apiPlayerFaction(w, r); !ok {
		return
	}
	account := apiAuthenticationFromContext(r.Context()).Account
	entities, err := app.store.EntitiesAsOf(r.Context(), account.Email, turn)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	response := apiEntities{Turn: turn, Entities: make([]apiEntity, 0, len(entities))}
	for _, entity := range entities {
		response.Entities = append(response.Entities, apiEntityFromStore(entity))
	}
	_ = writeAPIJSON(w, http.StatusOK, response)
}

func (app *application) getAPIMap(w http.ResponseWriter, r *http.Request) {
	if rejectAPITurnSelector(w, r) {
		return
	}
	turn, ok := app.apiCurrentTurn(w, r)
	if !ok {
		return
	}
	app.writeAPIMapAsOf(w, r, turn)
}

// writeAPIMapAsOf answers a map read for one turn. An admin sees the whole
// world, which no turn changes; a player sees the hexes the faction knew on
// that turn, which is what makes a past map a report rather than today's
// visibility drawn over an old date.
func (app *application) writeAPIMapAsOf(w http.ResponseWriter, r *http.Request, turn int) {
	authentication := apiAuthenticationFromContext(r.Context())
	if authentication.Account.Role != "admin" && authentication.Account.Role != "player" {
		writeAPIError(w, http.StatusForbidden, apiCodeForbidden, "This account cannot use that resource.")
		return
	}
	if authentication.Account.Role == "player" {
		if _, ok := app.apiPlayerFaction(w, r); !ok {
			return
		}
	}
	world, err := app.store.World(r.Context())
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	visible := make(map[hexg.Hex]bool)
	if authentication.Account.Role == "player" {
		known, err := app.store.KnowledgeAsOf(r.Context(), authentication.Account.Email, turn)
		if err != nil {
			app.writeAPIReadFailure(w, r, err)
			return
		}
		hexes := known.Hexes()
		visible = make(map[hexg.Hex]bool, len(hexes))
		for _, coord := range hexes {
			visible[world.Normalize(coord)] = true
		}
	}
	response := apiMap{Turn: turn, Width: world.Width(), Height: world.Height(), Hexes: make([]apiHex, 0)}
	for _, hex := range world.Hexes() {
		if authentication.Account.Role == "player" && !visible[hex.Coord] {
			continue
		}
		response.Hexes = append(response.Hexes, apiHex{
			Coordinate: apiCoordinateFromHex(hex.Coord),
			Terrain:    string(hex.Terrain),
			Elevation:  hex.Elevation,
		})
	}
	_ = writeAPIJSON(w, http.StatusOK, response)
}

func (app *application) getAPIOrders(w http.ResponseWriter, r *http.Request) {
	if rejectAPITurnSelector(w, r) {
		return
	}
	turn, ok := app.apiCurrentTurn(w, r)
	if !ok {
		return
	}
	app.writeAPIOrdersAsOf(w, r, turn)
}

// writeAPIOrdersAsOf answers an orders read for one turn, estimate included.
//
// An estimate on a closed turn is the one the player would have been shown
// while the turn was open: EstimateOrders prices the stored orders against the
// knowledge, locations and allowances of that turn, and terrain and world shape
// do not change. Answering the question a client is really asking - what did
// this look like when I ordered it - is worth more than refusing arithmetic for
// being historical.
func (app *application) writeAPIOrdersAsOf(w http.ResponseWriter, r *http.Request, turn int) {
	if _, ok := app.apiPlayerFaction(w, r); !ok {
		return
	}
	account := apiAuthenticationFromContext(r.Context()).Account
	entities, err := app.store.EntitiesAsOf(r.Context(), account.Email, turn)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	orders, err := app.store.OrdersAsOf(r.Context(), account.Email, turn)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	estimates, err := app.store.EstimateOrders(r.Context(), account.Email, turn)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return
	}
	response := apiOrders{Turn: turn, Entities: make([]apiEntityOrders, 0, len(entities))}
	for _, entity := range entities {
		response.Entities = append(response.Entities, apiEntityOrders{
			EntityID: entity.ID,
			Orders:   apiOrdersFromStore(orders[entity.ID]),
			Estimate: apiEstimateFromGame(estimates[entity.ID]),
		})
	}
	setAPIOrdersETag(w, r, turn, orders)
	_ = writeAPIJSON(w, http.StatusOK, response)
}

func (app *application) apiPlayerFaction(w http.ResponseWriter, r *http.Request) (apiFaction, bool) {
	account := apiAuthenticationFromContext(r.Context()).Account
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		app.writeAPIReadFailure(w, r, err)
		return apiFaction{}, false
	}
	if !found || !faction.Configured() {
		writeAPIError(w, http.StatusNotFound, apiCodeFactionNotConfigured, "Configure a faction before using this resource.")
		return apiFaction{}, false
	}
	return apiFaction{Name: faction.Name, Race: string(faction.Race), Active: faction.Active, Configured: true}, true
}

// writeAPIReadFailure answers a read that failed inside the server. It is the
// read side's name for one internal failure; the error goes to the log, and the
// client is told the same sentence every internal failure produces.
func (app *application) writeAPIReadFailure(w http.ResponseWriter, r *http.Request, err error) {
	app.apiInternalError(w, r, err)
}

func apiCoordinateFromHex(coord hexg.Hex) apiCoordinate {
	return apiCoordinate{Q: coord.Q(), R: coord.R()}
}

func apiEntityFromStore(entity datastore.Entity) apiEntity {
	kinds := entity.Kind.OrderKinds()
	response := apiEntity{
		ID: entity.ID, Code: entity.Code, Name: entity.Name, Kind: string(entity.Kind),
		Location: apiCoordinateFromHex(entity.Location), Allowance: entity.Allowance,
		OrderKinds: make([]string, 0, len(kinds)),
	}
	for _, kind := range kinds {
		response.OrderKinds = append(response.OrderKinds, string(kind))
	}
	return response
}

func apiOrdersFromStore(orders []datastore.Order) []apiOrder {
	response := make([]apiOrder, 0, len(orders))
	for _, order := range orders {
		detail := apiOrderDetail{}
		if order.Kind == game.OrderKindMove && order.Detail.Direction.IsValid() {
			direction := strings.ToLower(order.Detail.Direction.String())
			detail.Direction = &direction
		} else if order.Kind == game.OrderKindRest {
			count := order.Detail.Count
			detail.Count = &count
		}
		response = append(response, apiOrder{Sequence: order.Seq, Kind: string(order.Kind), Detail: detail})
	}
	return response
}

func apiEstimateFromGame(estimate game.Estimate) apiOrderEstimate {
	response := apiOrderEstimate{
		Allowance: estimate.Allowance, Committed: estimate.Committed, Total: estimate.Total,
		Residue: estimate.Residue, Overspend: estimate.Overspend,
		End: apiCoordinateFromHex(estimate.End), Orders: make([]apiOrderCost, 0, len(estimate.Orders)),
	}
	if estimate.ExhaustsAt != 0 {
		exhaustsAt := estimate.ExhaustsAt
		response.ExhaustsAt = &exhaustsAt
	}
	for _, order := range estimate.Orders {
		cost := apiOrderCost{
			Sequence: order.Seq, Kind: string(order.Kind), Running: order.Running, Exhausts: order.Exhausts,
			From: apiCoordinateFromHex(order.From), Target: apiCoordinateFromHex(order.Target), To: apiCoordinateFromHex(order.To),
		}
		if order.Priced {
			value := order.Cost
			cost.Cost = &value
		}
		if order.Warning != "" {
			warning := string(order.Warning)
			cost.Warning = &warning
		}
		response.Orders = append(response.Orders, cost)
	}
	return response
}

// apiTurnSelectorNames are the query parameters a client reaches for when it
// wants a turn other than the current one. None of the read routes can answer
// one yet, so each is rejected by name rather than ignored: a dropped selector
// is answered with the current turn's data under a `turn` the client did not
// ask for, which reads as success and is wrong. See issue #61.
//
// `turn` is listed because it is the obvious guess, not because it is the
// spelling this API will adopt. A read that names a past turn will spell it
// `asOfTurn`, matching the store's EntitiesAsOf and ResultsAsOf, and leaving
// `turn` to mean on writes what it already means there: the turn the client
// read, which makes a stale write a turn_closed conflict.
var apiTurnSelectorNames = []string{"asOfTurn", "asOf", "turn"}

// rejectAPITurnSelector reports whether the request tried to name a turn. It
// answers the request itself when it did.
func rejectAPITurnSelector(w http.ResponseWriter, r *http.Request) bool {
	query := r.URL.Query()
	for _, name := range apiTurnSelectorNames {
		if !query.Has(name) {
			continue
		}
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest,
			"This resource reads the current turn only; remove the "+name+" parameter.")
		return true
	}
	return false
}

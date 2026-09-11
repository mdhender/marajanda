// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func (app *application) advanceAPITurn(w http.ResponseWriter, r *http.Request) {
	turn, err := app.store.AdvanceTurn(r.Context())
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	_ = writeAPIJSON(w, http.StatusOK, apiTurn{Turn: turn})
}

func (app *application) putAPIFaction(w http.ResponseWriter, r *http.Request) {
	if !requireAPIJSONBody(w, r) {
		return
	}
	var request apiPutFactionRequest
	if err := decodeAPIJSON(r.Body, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidJSON, "The request body must contain one valid faction object.")
		return
	}
	name, err := game.NormalizeFactionName(request.Name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest, "The faction name is not valid.")
		return
	}
	race := game.Race(request.Race)
	if !race.Valid() {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest, "Choose a valid faction race.")
		return
	}
	account := apiAuthenticationFromContext(r.Context()).Account
	if _, err := app.store.SaveFaction(r.Context(), account.Email, name, race); err != nil {
		if errors.Is(err, game.ErrNoOrigin) {
			writeAPIError(w, http.StatusConflict, apiCodeNoOrigin, "No origin is available for that faction.")
			return
		}
		app.writeAPIInternalError(w, r, err)
		return
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil || !found {
		app.writeAPIInternalError(w, r, err)
		return
	}
	_ = writeAPIJSON(w, http.StatusOK, apiFaction{
		Name: faction.Name, Race: string(faction.Race), Active: faction.Active, Configured: faction.Configured(),
	})
}

func (app *application) postAPIOrder(w http.ResponseWriter, r *http.Request) {
	entityID, ok := positiveAPIPathInt64(w, r.PathValue("entity"), "entity")
	if !ok || !app.requireAPIOrderFaction(w, r) || !requireAPIJSONBody(w, r) {
		return
	}
	var request apiCreateOrderRequest
	if err := decodeAPIJSON(r.Body, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidJSON, "The request body must contain one valid order object.")
		return
	}
	if request.Turn < 1 || request.Kind == "" || request.Detail == nil || request.Sequence != nil && *request.Sequence < 1 {
		writeAPIInvalidRequest(w)
		return
	}
	detail, ok := apiDetailToGame(w, *request.Detail)
	if !ok {
		return
	}
	expect, ok := apiOrderExpectation(w, r)
	if !ok {
		return
	}
	kind := game.OrderKind(request.Kind)
	sequence := 0
	var err error
	if request.Sequence == nil {
		sequence, err = app.store.AddOrder(r.Context(), apiPlayerEmail(r), request.Turn, entityID, kind, detail, expect...)
	} else {
		sequence = *request.Sequence
		err = app.store.InsertOrder(r.Context(), apiPlayerEmail(r), request.Turn, entityID, sequence, kind, detail, expect...)
	}
	if err != nil {
		app.writeAPIOrderFailure(w, r, err)
		return
	}
	app.writeAPIEntityOrders(w, r, http.StatusCreated, request.Turn, entityID, sequence)
}

func (app *application) patchAPIOrder(w http.ResponseWriter, r *http.Request) {
	entityID, sequence, ok := apiOrderPath(w, r)
	if !ok || !app.requireAPIOrderFaction(w, r) || !requireAPIJSONBody(w, r) {
		return
	}
	var request apiSetOrderDetailRequest
	if err := decodeAPIJSON(r.Body, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidJSON, "The request body must contain one valid order detail object.")
		return
	}
	if request.Turn < 1 || request.Detail == nil {
		writeAPIInvalidRequest(w)
		return
	}
	detail, ok := apiDetailToGame(w, *request.Detail)
	if !ok {
		return
	}
	expect, ok := apiOrderExpectation(w, r)
	if !ok {
		return
	}
	if err := app.store.SetOrderDetail(r.Context(), apiPlayerEmail(r), request.Turn, entityID, sequence, detail, expect...); err != nil {
		app.writeAPIOrderFailure(w, r, err)
		return
	}
	app.writeAPIEntityOrders(w, r, http.StatusOK, request.Turn, entityID, sequence)
}

func (app *application) putAPIOrders(w http.ResponseWriter, r *http.Request) {
	if !app.requireAPIOrderFaction(w, r) || !requireAPIJSONBody(w, r) {
		return
	}
	var request apiSetOrderDetailsRequest
	if err := decodeAPIJSON(r.Body, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidJSON, "The request body must contain one valid order update object.")
		return
	}
	if request.Turn < 1 || request.Updates == nil {
		writeAPIInvalidRequest(w)
		return
	}
	updates := make([]datastore.OrderUpdate, 0, len(request.Updates))
	for _, update := range request.Updates {
		if update.EntityID < 1 || update.Sequence < 1 || update.Detail == nil {
			writeAPIInvalidRequest(w)
			return
		}
		detail, ok := apiDetailToGame(w, *update.Detail)
		if !ok {
			return
		}
		updates = append(updates, datastore.OrderUpdate{EntityID: update.EntityID, Seq: update.Sequence, Detail: detail})
	}
	expect, ok := apiOrderExpectation(w, r)
	if !ok {
		return
	}
	if err := app.store.SetOrderDetails(r.Context(), apiPlayerEmail(r), request.Turn, updates, expect...); err != nil {
		app.writeAPIOrderFailure(w, r, err)
		return
	}
	app.writeAPIOrders(w, r, request.Turn)
}

func (app *application) deleteAPIOrder(w http.ResponseWriter, r *http.Request) {
	entityID, sequence, ok := apiOrderPath(w, r)
	if !ok || !app.requireAPIOrderFaction(w, r) {
		return
	}
	turn, ok := positiveAPIInt(w, r.URL.Query().Get("turn"), "turn")
	if !ok {
		return
	}
	expect, ok := apiOrderExpectation(w, r)
	if !ok {
		return
	}
	if err := app.store.RemoveOrder(r.Context(), apiPlayerEmail(r), turn, entityID, sequence, expect...); err != nil {
		app.writeAPIOrderFailure(w, r, err)
		return
	}
	app.writeAPIEntityOrders(w, r, http.StatusOK, turn, entityID, sequence)
}

func (app *application) requireAPIOrderFaction(w http.ResponseWriter, r *http.Request) bool {
	_, ok := app.apiPlayerFaction(w, r)
	return ok
}

func (app *application) writeAPIEntityOrders(w http.ResponseWriter, r *http.Request, status, turn int, entityID int64, sequence int) {
	orders, err := app.store.OrdersAsOf(r.Context(), apiPlayerEmail(r), turn)
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	estimates, err := app.store.EstimateOrders(r.Context(), apiPlayerEmail(r), turn)
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	app.setAPIOrdersETag(w, r, turn)
	_ = writeAPIJSON(w, status, apiOrderMutation{
		Turn: turn, EntityID: entityID, Sequence: sequence,
		Orders: apiOrdersFromStore(orders[entityID]), Estimate: apiEstimateFromGame(estimates[entityID]),
	})
}

func (app *application) writeAPIOrders(w http.ResponseWriter, r *http.Request, turn int) {
	entities, err := app.store.EntitiesAsOf(r.Context(), apiPlayerEmail(r), turn)
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	orders, err := app.store.OrdersAsOf(r.Context(), apiPlayerEmail(r), turn)
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	estimates, err := app.store.EstimateOrders(r.Context(), apiPlayerEmail(r), turn)
	if err != nil {
		app.writeAPIInternalError(w, r, err)
		return
	}
	response := apiOrders{Turn: turn, Entities: make([]apiEntityOrders, 0, len(entities))}
	for _, entity := range entities {
		response.Entities = append(response.Entities, apiEntityOrders{
			EntityID: entity.ID, Orders: apiOrdersFromStore(orders[entity.ID]), Estimate: apiEstimateFromGame(estimates[entity.ID]),
		})
	}
	app.setAPIOrdersETag(w, r, turn)
	_ = writeAPIJSON(w, http.StatusOK, response)
}

func apiDetailToGame(w http.ResponseWriter, detail apiOrderDetail) (game.OrderDetail, bool) {
	if detail.Direction != nil && detail.Count != nil {
		writeAPIOrderRefused(w)
		return game.OrderDetail{}, false
	}
	if detail.Count != nil {
		if *detail.Count < 1 || *detail.Count > datastore.MaxOrdersPerEntity {
			writeAPIOrderRefused(w)
			return game.OrderDetail{}, false
		}
		return game.OrderDetail{Count: *detail.Count}, true
	}
	if detail.Direction != nil {
		point, err := compass.Parse(*detail.Direction)
		if err != nil {
			writeAPIOrderRefused(w)
			return game.OrderDetail{}, false
		}
		return game.OrderDetail{Direction: point}, true
	}
	return game.OrderDetail{}, true
}

func (app *application) writeAPIOrderFailure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, datastore.ErrFactionInactive):
		writeAPIError(w, http.StatusForbidden, apiCodeFactionInactive, "That faction is not active and cannot issue orders.")
	case errors.Is(err, datastore.ErrUnknownEntity):
		writeAPIError(w, http.StatusNotFound, apiCodeEntityNotFound, "That entity does not belong to this faction.")
	case errors.Is(err, datastore.ErrUnknownOrder):
		writeAPIError(w, http.StatusNotFound, apiCodeOrderNotFound, "That order or insertion position does not exist.")
	case errors.Is(err, datastore.ErrTurnClosed):
		writeAPIError(w, http.StatusConflict, apiCodeTurnClosed, "Only the current turn's orders can be changed.")
	case errors.Is(err, datastore.ErrOrdersChanged):
		writeAPIError(w, http.StatusPreconditionFailed, apiCodePreconditionFailed,
			"The orders changed since they were read; read them again and retry.")
	case errors.Is(err, datastore.ErrTooManyOrders):
		writeAPIError(w, http.StatusUnprocessableEntity, apiCodeOrderLimit, "That entity cannot carry another order this turn.")
	case errors.Is(err, datastore.ErrOrderKindRefused), errors.Is(err, datastore.ErrOrderCountRefused),
		errors.Is(err, datastore.ErrOrderDetailRefused):
		writeAPIOrderRefused(w)
	default:
		app.writeAPIInternalError(w, r, err)
	}
}

func writeAPIOrderRefused(w http.ResponseWriter) {
	writeAPIError(w, http.StatusUnprocessableEntity, apiCodeOrderRefused, "That order kind or detail is not valid for this entity.")
}

func requireAPIJSONBody(w http.ResponseWriter, r *http.Request) bool {
	if !hasAPIJSONContentType(r) {
		writeAPIError(w, http.StatusUnsupportedMediaType, apiCodeUnsupportedMediaType, "Content-Type must be application/json.")
		return false
	}
	return true
}

func writeAPIInvalidRequest(w http.ResponseWriter) {
	writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest, "The request contains a missing or invalid value.")
}

// writeAPIInternalError answers a mutation that failed inside the server. It is
// the write side's name for one internal failure; the error goes to the log,
// and the client is told the same sentence every internal failure produces.
func (app *application) writeAPIInternalError(w http.ResponseWriter, r *http.Request, err error) {
	app.apiInternalError(w, r, err)
}

func apiPlayerEmail(r *http.Request) string {
	return apiAuthenticationFromContext(r.Context()).Account.Email
}

func apiOrderPath(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	entityID, ok := positiveAPIPathInt64(w, r.PathValue("entity"), "entity")
	if !ok {
		return 0, 0, false
	}
	sequence, ok := positiveAPIInt(w, r.PathValue("sequence"), "sequence")
	return entityID, sequence, ok
}

func positiveAPIPathInt64(w http.ResponseWriter, value, _ string) (int64, bool) {
	if !decimalDigits(value) {
		writeAPIInvalidRequest(w)
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 {
		writeAPIInvalidRequest(w)
		return 0, false
	}
	return n, true
}

func positiveAPIInt(w http.ResponseWriter, value, _ string) (int, bool) {
	if !decimalDigits(value) {
		writeAPIInvalidRequest(w)
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		writeAPIInvalidRequest(w)
		return 0, false
	}
	return n, true
}

func decimalDigits(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) == -1
}

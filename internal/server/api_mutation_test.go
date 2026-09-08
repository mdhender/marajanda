// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func apiMutation(t *testing.T, store *testStore, role, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	token := make([]byte, datastore.SessionTokenBytes)
	store.sessionAccount = datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: role, Active: true}
	store.sessions = map[string]bool{string(token): true}
	return apiRequest(newHandler(nil, store), method, target, body, map[string]string{
		"Authorization": "Bearer " + base64.RawURLEncoding.EncodeToString(token),
	})
}

func TestAPIPutFactionConfiguresNormalizesAndReconfigures(t *testing.T) {
	store := apiReadStore()
	store.found = false
	response := apiMutation(t, store, "player", http.MethodPut, "/api/v1/faction", `{"name":"  The   Wayfarers  ","race":"elf"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var faction apiFaction
	decodeAPIResponse(t, response, &faction)
	if faction.Name != "The Wayfarers" || faction.Race != "elf" || !faction.Active || !faction.Configured {
		t.Fatalf("faction = %#v", faction)
	}
	response = apiMutation(t, store, "player", http.MethodPut, "/api/v1/faction", `{"name":"The Wanderers","race":"dwarf"}`)
	decodeAPIResponse(t, response, &faction)
	if response.Code != http.StatusOK || faction.Name != "The Wanderers" || faction.Race != "dwarf" {
		t.Fatalf("reconfigured faction = %#v; status = %d", faction, response.Code)
	}
}

func TestAPIPutFactionRefusals(t *testing.T) {
	for _, test := range []struct {
		name, role, body string
		err              error
		status           int
		code             string
	}{
		{name: "invalid name", role: "player", body: `{"name":"x","race":"human"}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "invalid race", role: "player", body: `{"name":"The Wayfarers","race":"dragon"}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "malformed JSON", role: "player", body: `{"name":`, status: http.StatusBadRequest, code: apiCodeInvalidJSON},
		{name: "no origin", role: "player", body: `{"name":"The Wayfarers","race":"human"}`, err: game.ErrNoOrigin, status: http.StatusConflict, code: apiCodeNoOrigin},
		{name: "store failure", role: "player", body: `{"name":"The Wayfarers","race":"human"}`, err: errors.New("store unavailable"), status: http.StatusInternalServerError, code: apiCodeInternal},
		{name: "wrong role", role: "admin", body: `{"name":"The Wayfarers","race":"human"}`, status: http.StatusForbidden, code: apiCodeForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := apiReadStore()
			store.found = false
			store.saveErr = test.err
			assertAPIError(t, apiMutation(t, store, test.role, http.MethodPut, "/api/v1/faction", test.body), test.status, test.code)
		})
	}
}

func TestAPIOrderMutations(t *testing.T) {
	store := ordersStore()
	appendResponse := apiMutation(t, store, "player", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"kind":"move","detail":{"direction":"ne"}}`)
	if appendResponse.Code != http.StatusCreated {
		t.Fatalf("append status = %d, body = %s", appendResponse.Code, appendResponse.Body.String())
	}
	var mutation apiOrderMutation
	decodeAPIResponse(t, appendResponse, &mutation)
	if mutation.Sequence != 1 || len(mutation.Orders) != 1 || mutation.Orders[0].Detail.Direction == nil || *mutation.Orders[0].Detail.Direction != "ne" {
		t.Fatalf("append = %#v", mutation)
	}

	insertResponse := apiMutation(t, store, "player", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"sequence":1,"kind":"rest","detail":{"count":2}}`)
	decodeAPIResponse(t, insertResponse, &mutation)
	if insertResponse.Code != http.StatusCreated || mutation.Sequence != 1 || len(mutation.Orders) != 2 || mutation.Orders[0].Kind != "rest" || mutation.Orders[1].Sequence != 2 {
		t.Fatalf("insert = %#v; status = %d", mutation, insertResponse.Code)
	}

	patchResponse := apiMutation(t, store, "player", http.MethodPatch, "/api/v1/entities/7/orders/2", `{"turn":3,"detail":{"direction":"w"}}`)
	decodeAPIResponse(t, patchResponse, &mutation)
	if patchResponse.Code != http.StatusOK || mutation.Orders[1].Detail.Direction == nil || *mutation.Orders[1].Detail.Direction != "w" {
		t.Fatalf("patch = %#v; status = %d", mutation, patchResponse.Code)
	}

	batchResponse := apiMutation(t, store, "player", http.MethodPut, "/api/v1/orders", `{"turn":3,"updates":[{"entityId":7,"sequence":1,"detail":{"count":3}},{"entityId":7,"sequence":2,"detail":{"direction":"se"}}]}`)
	if batchResponse.Code != http.StatusOK || len(store.savedUpdates) != 2 || store.savedUpdates[0].Detail.Count != 3 || store.savedUpdates[1].Detail.Direction.String() != "SE" {
		t.Fatalf("batch status = %d, updates = %#v, body = %s", batchResponse.Code, store.savedUpdates, batchResponse.Body.String())
	}

	deleteResponse := apiMutation(t, store, "player", http.MethodDelete, "/api/v1/entities/7/orders/1?turn=3", "")
	decodeAPIResponse(t, deleteResponse, &mutation)
	if deleteResponse.Code != http.StatusOK || mutation.Sequence != 1 || len(mutation.Orders) != 1 || mutation.Orders[0].Sequence != 1 || mutation.Orders[0].Kind != "move" {
		t.Fatalf("delete = %#v; status = %d", mutation, deleteResponse.Code)
	}
}

func TestAPIOrderRequestValidation(t *testing.T) {
	for _, test := range []struct {
		name, method, target, body string
		status                     int
		code                       string
	}{
		{name: "bad entity path", method: http.MethodPost, target: "/api/v1/entities/not-a-number/orders", body: `{}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "bad sequence path", method: http.MethodPatch, target: "/api/v1/entities/7/orders/0", body: `{}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "missing delete turn", method: http.MethodDelete, target: "/api/v1/entities/7/orders/1", status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "malformed JSON", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":`, status: http.StatusBadRequest, code: apiCodeInvalidJSON},
		{name: "missing detail", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":3,"kind":"move"}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
		{name: "two details", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":3,"kind":"move","detail":{"direction":"ne","count":1}}`, status: http.StatusUnprocessableEntity, code: apiCodeOrderRefused},
		{name: "unknown direction", method: http.MethodPatch, target: "/api/v1/entities/7/orders/1", body: `{"turn":3,"detail":{"direction":"north"}}`, status: http.StatusUnprocessableEntity, code: apiCodeOrderRefused},
		{name: "invalid rest count", method: http.MethodPost, target: "/api/v1/entities/7/orders", body: `{"turn":3,"kind":"rest","detail":{"count":0}}`, status: http.StatusUnprocessableEntity, code: apiCodeOrderRefused},
		{name: "missing updates", method: http.MethodPut, target: "/api/v1/orders", body: `{"turn":3}`, status: http.StatusBadRequest, code: apiCodeInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertAPIError(t, apiMutation(t, ordersStore(), "player", test.method, test.target, test.body), test.status, test.code)
		})
	}

	store := ordersStore()
	token := make([]byte, datastore.SessionTokenBytes)
	store.sessionAccount = datastore.Account{Email: "player@example.com", Role: "player"}
	store.sessions = map[string]bool{string(token): true}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/entities/7/orders", strings.NewReader(`{"turn":3,"kind":"move","detail":{}}`))
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token))
	response := httptest.NewRecorder()
	newHandler(nil, store).ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnsupportedMediaType, apiCodeUnsupportedMediaType)
}

func TestAPIOrderStoreFailures(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{datastore.ErrFactionInactive, http.StatusForbidden, apiCodeFactionInactive},
		{datastore.ErrUnknownEntity, http.StatusNotFound, apiCodeEntityNotFound},
		{datastore.ErrUnknownOrder, http.StatusNotFound, apiCodeOrderNotFound},
		{datastore.ErrTurnClosed, http.StatusConflict, apiCodeTurnClosed},
		{datastore.ErrOrderKindRefused, http.StatusUnprocessableEntity, apiCodeOrderRefused},
		{datastore.ErrOrderCountRefused, http.StatusUnprocessableEntity, apiCodeOrderRefused},
		{datastore.ErrOrderDetailRefused, http.StatusUnprocessableEntity, apiCodeOrderRefused},
		{datastore.ErrTooManyOrders, http.StatusUnprocessableEntity, apiCodeOrderLimit},
		{errors.New("store unavailable"), http.StatusInternalServerError, apiCodeInternal},
	} {
		t.Run(test.code, func(t *testing.T) {
			store := ordersStore()
			store.orderErr = test.err
			response := apiMutation(t, store, "player", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"kind":"move","detail":{}}`)
			assertAPIError(t, response, test.status, test.code)
		})
	}

	inactive := ordersStore()
	inactive.faction.Active = false
	inactive.orderErr = datastore.ErrFactionInactive
	assertAPIError(t, apiMutation(t, inactive, "player", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"kind":"move","detail":{}}`), http.StatusForbidden, apiCodeFactionInactive)

	unconfigured := ordersStore()
	unconfigured.found = false
	assertAPIError(t, apiMutation(t, unconfigured, "player", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"kind":"move","detail":{}}`), http.StatusNotFound, apiCodeFactionNotConfigured)

	assertAPIError(t, apiMutation(t, ordersStore(), "admin", http.MethodPost, "/api/v1/entities/7/orders", `{"turn":3,"kind":"move","detail":{}}`), http.StatusForbidden, apiCodeForbidden)
}

func TestAPICookieMutationRetainsCrossOriginProtection(t *testing.T) {
	store := ordersStore()
	handler, cookie := signedInPlayer(t, store)
	request := httptest.NewRequest(http.MethodPost, "https://marajanda.test/api/v1/entities/7/orders", strings.NewReader(`{"turn":3,"kind":"move","detail":{}}`))
	request.Header.Set("Content-Type", apiJSONContentType)
	request.Header.Set("Origin", "https://hostile.example")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusForbidden, apiCodeForbidden)
	if len(store.orders[7]) != 0 {
		t.Fatalf("cross-origin request wrote orders: %#v", store.orders[7])
	}
}

func TestAPIAndHTMXMutationsShareTheRealStore(t *testing.T) {
	store, err := datastore.OpenMemory(t.Context(), datastore.Game{
		Seed1: 98374, Seed2: -98, Width: datastore.MinimumWorldWidth, Height: datastore.MinimumWorldHeight,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, found, err := store.Authenticate(t.Context(), "player@marajanda.com", "good.luck")
	if err != nil || !found {
		t.Fatalf("authenticate = %#v, %t, %v", account, found, err)
	}
	token := make([]byte, datastore.SessionTokenBytes)
	if err := store.CreateSession(t.Context(), token, account); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(store.Authenticate, store)
	headers := map[string]string{"Authorization": "Bearer " + base64.RawURLEncoding.EncodeToString(token)}
	faction := apiRequest(handler, http.MethodPut, "/api/v1/faction", `{"name":"  The   Wayfarers ","race":"human"}`, headers)
	if faction.Code != http.StatusOK {
		t.Fatalf("faction status = %d, body = %s", faction.Code, faction.Body.String())
	}
	entities, err := store.EntitiesAsOf(t.Context(), account.Email, game.FirstTurn)
	if err != nil || len(entities) != 2 {
		t.Fatalf("founded entities = %#v, %v", entities, err)
	}
	var leaderID int64
	for _, entity := range entities {
		if entity.Kind == game.EntityKindLeader {
			leaderID = entity.ID
		}
	}
	created := apiRequest(handler, http.MethodPost, fmt.Sprintf("/api/v1/entities/%d/orders", leaderID), `{"turn":1,"kind":"move","detail":{"direction":"ne"}}`, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(token)}
	page := requestWithCookie(handler, http.MethodGet, "/player/orders", cookie, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `value="ne" selected`) {
		t.Fatalf("HTMX page does not show API order: status %d", page.Code)
	}

	field := fmt.Sprintf("direction.%d.1=w", leaderID)
	changed := requestWithCookie(handler, http.MethodPost, fmt.Sprintf("/player/orders/%d/1", leaderID), cookie, field)
	if changed.Code != http.StatusSeeOther {
		t.Fatalf("HTMX change status = %d, body = %s", changed.Code, changed.Body.String())
	}
	read := apiRequest(handler, http.MethodGet, "/api/v1/orders", "", headers)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"direction":"w"`) {
		t.Fatalf("API does not show HTMX order: status %d, body = %s", read.Code, read.Body.String())
	}
}

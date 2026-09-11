// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// TestAPIAndHTMLCapabilityParity walks the public API as a client would while
// reading the same facts back through the active HTML transport. Both use one
// real store: this is deliberately not another implementation of its behavior.
func TestAPIAndHTMLCapabilityParity(t *testing.T) {
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
	playerCookie := player.Cookie
	if player.Account.Role != "player" || player.Account.FactionConfigured || player.Account.Seated {
		t.Fatalf("new player account = %#v", player.Account)
	}
	assertAPIError(t, apiRequest(handler, http.MethodGet, "/api/v1/faction", "", playerHeaders),
		http.StatusNotFound, apiCodeFactionNotConfigured)
	factionForm := requestWithCookie(handler, http.MethodGet, "/player/faction", playerCookie, "")
	if factionForm.Code != http.StatusOK || !strings.Contains(factionForm.Body.String(), `action="/player/faction"`) {
		t.Fatalf("faction form = %d %q", factionForm.Code, factionForm.Body.String())
	}

	configured := apiRequest(handler, http.MethodPut, "/api/v1/faction",
		`{"name":"  The   Wayfarers  ","race":"human"}`, playerHeaders)
	if configured.Code != http.StatusOK {
		t.Fatalf("configure faction = %d %s", configured.Code, configured.Body.String())
	}
	var faction apiFaction
	decodeAPIResponse(t, configured, &faction)
	if faction.Name != "The Wayfarers" || faction.Race != "human" || !faction.Active || !faction.Configured {
		t.Fatalf("configured faction = %#v", faction)
	}

	accountResponse := apiRequest(handler, http.MethodGet, "/api/v1/account", "", playerHeaders)
	var account apiAccount
	decodeAPIResponse(t, accountResponse, &account)
	if accountResponse.Code != http.StatusOK || !account.Seated || account.Origin == nil || !account.FactionConfigured {
		t.Fatalf("configured account = %#v; status = %d", account, accountResponse.Code)
	}
	gameResponse := apiRequest(handler, http.MethodGet, "/api/v1/game", "", playerHeaders)
	var current apiGame
	decodeAPIResponse(t, gameResponse, &current)
	if gameResponse.Code != http.StatusOK || current.CurrentTurn != game.FirstTurn || current.Seeds != nil {
		t.Fatalf("player game = %#v; status = %d", current, gameResponse.Code)
	}
	entitiesResponse := apiRequest(handler, http.MethodGet, "/api/v1/entities", "", playerHeaders)
	var entities apiEntities
	decodeAPIResponse(t, entitiesResponse, &entities)
	if entitiesResponse.Code != http.StatusOK || entities.Turn != current.CurrentTurn || len(entities.Entities) != 2 {
		t.Fatalf("entities = %#v; status = %d", entities, entitiesResponse.Code)
	}
	var leader apiEntity
	for _, entity := range entities.Entities {
		if entity.Kind == string(game.EntityKindLeader) {
			leader = entity
		}
	}
	if leader.ID == 0 || leader.Code != "LEADER-1" || leader.Location != *account.Origin {
		t.Fatalf("leader = %#v, account origin = %#v", leader, account.Origin)
	}
	playerDashboard := requestWithCookie(handler, http.MethodGet, "/player/dashboard", playerCookie, "")
	for _, fact := range []string{"The Wayfarers", `<p class="label">Turn</p>`, "<strong>1</strong>", "LEADER-1", fmt.Sprintf("(%d, %d)", leader.Location.Q, leader.Location.R)} {
		if playerDashboard.Code != http.StatusOK || !strings.Contains(playerDashboard.Body.String(), fact) {
			t.Fatalf("player dashboard does not expose %q: status %d", fact, playerDashboard.Code)
		}
	}

	mapResponse := apiRequest(handler, http.MethodGet, "/api/v1/map", "", playerHeaders)
	var playerMap apiMap
	decodeAPIResponse(t, mapResponse, &playerMap)
	if mapResponse.Code != http.StatusOK || playerMap.Turn != current.CurrentTurn || len(playerMap.Hexes) != 7 {
		t.Fatalf("player map has %d hexes at turn %d; status = %d", len(playerMap.Hexes), playerMap.Turn, mapResponse.Code)
	}
	mapPage := requestWithCookie(handler, http.MethodGet, "/player/map", playerCookie, "")
	if mapPage.Code != http.StatusOK || !strings.Contains(mapPage.Body.String(), "The Wayfarers") || !strings.Contains(mapPage.Body.String(), "Unexplored") {
		t.Fatalf("player map page = %d", mapPage.Code)
	}

	ordersResponse := apiRequest(handler, http.MethodGet, "/api/v1/orders", "", playerHeaders)
	var orders apiOrders
	decodeAPIResponse(t, ordersResponse, &orders)
	if ordersResponse.Code != http.StatusOK || orders.Turn != current.CurrentTurn || len(orders.Entities) != 2 {
		t.Fatalf("initial orders = %#v; status = %d", orders, ordersResponse.Code)
	}
	entityOrders := fmt.Sprintf("/api/v1/entities/%d/orders", leader.ID)
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPost, entityOrders,
		`{"turn":1,"kind":"move","detail":{"direction":"ne"}}`, playerHeaders), http.StatusCreated)
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPost, entityOrders,
		`{"turn":1,"sequence":1,"kind":"move","detail":{"direction":"e"}}`, playerHeaders), http.StatusCreated)
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPatch, entityOrders+"/2",
		`{"turn":1,"detail":{"direction":"w"}}`, playerHeaders), http.StatusOK)
	batch := fmt.Sprintf(`{"turn":1,"updates":[{"entityId":%d,"sequence":1,"detail":{"direction":"se"}},{"entityId":%d,"sequence":2,"detail":{"direction":"nw"}}]}`, leader.ID, leader.ID)
	assertParityMutationStatus(t, apiRequest(handler, http.MethodPut, "/api/v1/orders", batch, playerHeaders), http.StatusOK)
	assertParityMutationStatus(t, apiRequest(handler, http.MethodDelete, entityOrders+"/1?turn=1", "", playerHeaders), http.StatusOK)
	ordersPage := requestWithCookie(handler, http.MethodGet, "/player/orders", playerCookie, "")
	if ordersPage.Code != http.StatusOK || !strings.Contains(ordersPage.Body.String(), `value="nw" selected`) || !strings.Contains(ordersPage.Body.String(), "Orders for turn 1") {
		t.Fatalf("orders page does not expose API mutations: status %d", ordersPage.Code)
	}

	// Write once through HTMX and read the same stored list through JSON.
	form := url.Values{"kind." + fmt.Sprint(leader.ID): {"move"}, "add": {fmt.Sprint(leader.ID)}}
	htmx := parityRequestWithCookie(handler, http.MethodPost, "/player/orders", playerCookie, form.Encode(), true)
	if htmx.Code != http.StatusOK || !strings.Contains(htmx.Body.String(), `id="orders"`) {
		t.Fatalf("HTMX add = %d %s", htmx.Code, htmx.Body.String())
	}
	ordersResponse = apiRequest(handler, http.MethodGet, "/api/v1/orders", "", playerHeaders)
	decodeAPIResponse(t, ordersResponse, &orders)
	leadersOrders := parityEntityOrders(t, orders, leader.ID)
	if len(leadersOrders.Orders) != 2 || leadersOrders.Orders[0].Detail.Direction == nil ||
		*leadersOrders.Orders[0].Detail.Direction != "nw" || leadersOrders.Orders[1].Kind != "move" {
		t.Fatalf("API orders after HTMX add = %#v", leadersOrders.Orders)
	}

	admin := createParitySession(t, handler, "admin@marajanda.com", "good.luck")
	adminHeaders := map[string]string{"Authorization": "Bearer " + admin.Token}
	adminGameResponse := apiRequest(handler, http.MethodGet, "/api/v1/game", "", adminHeaders)
	var adminGame apiGame
	decodeAPIResponse(t, adminGameResponse, &adminGame)
	if adminGameResponse.Code != http.StatusOK || len(adminGame.Seeds) != 2 || adminGame.Seeds[0] != 98374 || adminGame.Seeds[1] != -98 {
		t.Fatalf("admin game = %#v; status = %d", adminGame, adminGameResponse.Code)
	}
	adminMapResponse := apiRequest(handler, http.MethodGet, "/api/v1/map", "", adminHeaders)
	var adminMap apiMap
	decodeAPIResponse(t, adminMapResponse, &adminMap)
	wantHexes := (2*adminMap.Width + 1) * (2*adminMap.Height + 1)
	if adminMapResponse.Code != http.StatusOK || len(adminMap.Hexes) != wantHexes || len(adminMap.Hexes) <= len(playerMap.Hexes) {
		t.Fatalf("admin map has %d hexes, want %d; status = %d", len(adminMap.Hexes), wantHexes, adminMapResponse.Code)
	}
	adminMapPage := requestWithCookie(handler, http.MethodGet, "/admin/map", admin.Cookie, "")
	if adminMapPage.Code != http.StatusOK || !strings.Contains(adminMapPage.Body.String(), "Download the whole world") {
		t.Fatalf("admin map page = %d", adminMapPage.Code)
	}

	advanced := apiRequest(handler, http.MethodPost, "/api/v1/turns/current/advance", "", adminHeaders)
	var turn apiTurn
	decodeAPIResponse(t, advanced, &turn)
	if advanced.Code != http.StatusOK || turn.Turn != game.FirstTurn+1 {
		t.Fatalf("advanced turn = %#v; status = %d", turn, advanced.Code)
	}
	adminDashboard := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", admin.Cookie, "")
	playerDashboard = requestWithCookie(handler, http.MethodGet, "/player/dashboard", playerCookie, "")
	if !strings.Contains(adminDashboard.Body.String(), "<strong>Turn 2</strong>") || !strings.Contains(playerDashboard.Body.String(), "<strong>2</strong>") {
		t.Fatalf("HTML dashboards do not expose advanced turn: admin %d, player %d", adminDashboard.Code, playerDashboard.Code)
	}

	// The turn that just closed is the one both transports report without being
	// told which. The page and the API read the same record: the ledger the JSON
	// carries is the ledger the page prints.
	resultsResponse := apiRequest(handler, http.MethodGet, "/api/v1/results", "", playerHeaders)
	var results apiResults
	decodeAPIResponse(t, resultsResponse, &results)
	if resultsResponse.Code != http.StatusOK || results.Turn != game.FirstTurn || len(results.Entities) == 0 {
		t.Fatalf("results = %#v; status = %d", results, resultsResponse.Code)
	}
	var leaderResult apiEntityResult
	for _, entity := range results.Entities {
		if entity.EntityID == leader.ID {
			leaderResult = entity
		}
	}
	if leaderResult.EntityID != leader.ID || leaderResult.Ledger.Allowance != leader.Allowance {
		t.Fatalf("leader result = %#v, want the allowance %d it was given", leaderResult, leader.Allowance)
	}
	resultsPage := requestWithCookie(handler, http.MethodGet, "/player/results", playerCookie, "")
	resultsBody := resultsPage.Body.String()
	if resultsPage.Code != http.StatusOK || !strings.Contains(resultsBody, "<strong>Turn 1</strong>") {
		t.Fatalf("results page = %d %s", resultsPage.Code, resultsBody)
	}
	if !strings.Contains(resultsBody, fmt.Sprintf("<dt>Spent</dt><dd>%d AP</dd>", leaderResult.Ledger.Spent)) {
		t.Fatalf("results page does not print the ledger the API reports (%#v): %s", leaderResult.Ledger, resultsBody)
	}

	revoked := apiRequest(handler, http.MethodDelete, "/api/v1/session", "", playerHeaders)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", revoked.Code, revoked.Body.String())
	}
	assertAPIError(t, apiRequest(handler, http.MethodGet, "/api/v1/account", "", playerHeaders),
		http.StatusUnauthorized, apiCodeAuthenticationNeeded)
}

func TestCookieAndBearerSessionsSurvivePersistentServerRestart(t *testing.T) {
	root := t.TempDir()
	storedGame := datastore.Game{
		Seed1: 98374, Seed2: -98,
		Width: datastore.MinimumWorldWidth, Height: datastore.MinimumWorldHeight,
	}
	store, err := datastore.Open(t.Context(), root, datastore.SeedAccount{
		Email: "admin@example.com", Secret: "temporary", Handle: "keeper",
	}, &storedGame)
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(store.Authenticate, store)
	bearer := createParitySession(t, handler, "admin@example.com", "temporary")
	browserSignIn := submitSignIn(handler, "admin@example.com", "temporary")
	if browserSignIn.Code != http.StatusSeeOther || len(browserSignIn.Result().Cookies()) != 1 {
		store.Close()
		t.Fatalf("browser sign-in = %d with %d cookies", browserSignIn.Code, len(browserSignIn.Result().Cookies()))
	}
	browserCookie := browserSignIn.Result().Cookies()[0]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = datastore.Open(t.Context(), root, datastore.SeedAccount{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler = newHandler(store.Authenticate, store)
	account := apiRequest(handler, http.MethodGet, "/api/v1/account", "", map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if account.Code != http.StatusOK || !strings.Contains(account.Body.String(), `"email":"admin@example.com"`) {
		t.Fatalf("bearer after restart = %d %s", account.Code, account.Body.String())
	}
	dashboard := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", browserCookie, "")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "Welcome, keeper.") {
		t.Fatalf("cookie after restart = %d %s", dashboard.Code, dashboard.Body.String())
	}
}

// This executable matrix makes the parity promise a change boundary. Adding an
// authenticated route to handler.go fails this test until its capability and
// API counterpart (or explicit public exclusion) are recorded here and in the
// consumer reference.
func TestAuthenticatedRouteCapabilityMatrix(t *testing.T) {
	type capability struct {
		name, documentation string
		ui, api             []string
	}
	// The turn-scoped reads sit in the capability they read rather than in one
	// of their own. They are the same capability addressed by turn, and the UI
	// has no past-turn view to pair them with; the matrix asks every capability
	// to have both sides, and a surplus API route is not a parity gap.
	matrix := []capability{
		{name: "sessions", documentation: "| Sign in and out |", ui: []string{"POST /sign-out"}, api: []string{"POST /api/v1/sessions", "DELETE /api/v1/session"}},
		{name: "identity", documentation: "| Current identity and role |", ui: []string{"GET /admin/dashboard", "GET /player/dashboard"}, api: []string{"GET /api/v1/account"}},
		{name: "game", documentation: "| Current game and turn |", ui: []string{"GET /admin/dashboard", "GET /player/dashboard", "GET /player/orders"}, api: []string{"GET /api/v1/game"}},
		{name: "faction", documentation: "| Configure a faction |", ui: []string{"GET /player/faction", "POST /player/faction"}, api: []string{"GET /api/v1/faction", "PUT /api/v1/faction"}},
		{name: "entities", documentation: "| Read player entities |", ui: []string{"GET /player/dashboard", "GET /player/orders"}, api: []string{"GET /api/v1/entities", "GET /api/v1/turns/{turn}/entities"}},
		{name: "admin map", documentation: "| Read the whole world |", ui: []string{"GET /admin/map", "GET /admin/map.png"}, api: []string{"GET /api/v1/map", "GET /api/v1/turns/{turn}/map"}},
		{name: "player map", documentation: "| Read visible terrain |", ui: []string{"GET /player/map"}, api: []string{"GET /api/v1/map", "GET /api/v1/turns/{turn}/map"}},
		{name: "order reads", documentation: "| Read and estimate orders |", ui: []string{"GET /player/orders"}, api: []string{"GET /api/v1/orders", "GET /api/v1/turns/{turn}/orders"}},
		{name: "turn results", documentation: "| Read what a processed turn did |", ui: []string{"GET /player/results"}, api: []string{"GET /api/v1/results", "GET /api/v1/turns/{turn}/results"}},
		{name: "order writes", documentation: "| Add, insert, edit, batch-save, and remove orders |", ui: []string{"POST /player/orders", "POST /player/orders/{entity}/{seq}", "POST /player/orders/{entity}/{seq}/insert", "DELETE /player/orders/{entity}/{seq}"}, api: []string{"POST /api/v1/entities/{entity}/orders", "PUT /api/v1/entities/{entity}/orders", "PATCH /api/v1/entities/{entity}/orders/{sequence}", "PUT /api/v1/orders", "DELETE /api/v1/entities/{entity}/orders/{sequence}"}},
		{name: "turn advance", documentation: "| Advance the turn |", ui: []string{"POST /admin/turn"}, api: []string{"POST /api/v1/turns/current/advance"}},
	}

	source, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool)
	for _, match := range regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+ /[^\"]*)"`).FindAllStringSubmatch(string(source), -1) {
		registered[match[1]] = true
	}
	for _, public := range []string{"GET /api/healthz", "GET /assets/{name}", "GET /", "GET /sign-in", "POST /sign-in"} {
		delete(registered, public)
	}
	documented, err := os.ReadFile("../../docs/reference/api-v1.md")
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for _, capability := range matrix {
		if len(capability.ui) == 0 || len(capability.api) == 0 {
			t.Errorf("%s has no UI or API route", capability.name)
		}
		if !strings.Contains(string(documented), capability.documentation) {
			t.Errorf("API reference has no capability row for %s", capability.name)
		}
		for _, route := range append(append([]string{}, capability.ui...), capability.api...) {
			want[route] = true
			if !registered[route] {
				t.Errorf("%s matrix route %q is not registered", capability.name, route)
			}
		}
	}
	if got, expected := sortedParityRoutes(registered), sortedParityRoutes(want); strings.Join(got, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("authenticated route matrix drifted\nregistered:\n%s\nmatrix:\n%s", strings.Join(got, "\n"), strings.Join(expected, "\n"))
	}
}

type paritySession struct {
	Token   string
	Cookie  *http.Cookie
	Account apiAccount
}

func createParitySession(t *testing.T, handler http.Handler, email, passphrase string) paritySession {
	t.Helper()
	body, err := json.Marshal(apiCreateSessionRequest{Email: email, Passphrase: passphrase})
	if err != nil {
		t.Fatal(err)
	}
	response := apiRequest(handler, http.MethodPost, "/api/v1/sessions", string(body), nil)
	if response.Code != http.StatusCreated || len(response.Result().Cookies()) != 1 {
		t.Fatalf("create %s session = %d with %d cookies: %s", email, response.Code, len(response.Result().Cookies()), response.Body.String())
	}
	var session apiSession
	decodeAPIResponse(t, response, &session)
	return paritySession{Token: session.Token, Cookie: response.Result().Cookies()[0], Account: session.Account}
}

func assertParityMutationStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("mutation status = %d, want %d; body = %s", response.Code, want, response.Body.String())
	}
}

func parityEntityOrders(t *testing.T, orders apiOrders, entityID int64) apiEntityOrders {
	t.Helper()
	for _, entity := range orders.Entities {
		if entity.EntityID == entityID {
			return entity
		}
	}
	t.Fatalf("orders contain no entity %d: %#v", entityID, orders)
	return apiEntityOrders{}
}

func parityRequestWithCookie(handler http.Handler, method, target string, cookie *http.Cookie, body string, htmx bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func sortedParityRoutes(routes map[string]bool) []string {
	values := make([]string, 0, len(routes))
	for route := range routes {
		values = append(values, route)
	}
	sort.Strings(values)
	return values
}

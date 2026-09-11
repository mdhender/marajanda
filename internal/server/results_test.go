// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// resultsStore is a faction two turns into a game, with the report of the turn
// that just closed: a leader that walked into a lake and then ran out of
// points, and a hamlet that did nothing because a hamlet cannot.
func resultsStore() *testStore {
	store := &testStore{
		faction: datastore.Faction{Name: "The Wayfarers", Race: game.RaceHuman, Active: true},
		found:   true,
		turn:    3,
		entities: []datastore.Entity{
			{ID: 7, Code: "LEADER-1", Name: "Wayfinder", Kind: game.EntityKindLeader, Location: hexg.NewHex(4, -1), Allowance: game.LeaderAllowance},
			{ID: 9, Code: "HAMLET-1", Name: "Mudville", Kind: game.EntityKindHamlet, Location: hexg.NewHex(2, -1)},
		},
	}
	store.results = map[int][]datastore.TurnResult{
		2: {
			{
				Turn: 2, EntityID: 7, Allowance: 6, Spent: 4, Lapsed: 2,
				Start: hexg.NewHex(2, -1), End: hexg.NewHex(4, -1),
				Orders: []datastore.OrderResult{
					{Seq: 1, Kind: game.OrderKindMove, Cost: 3, Carried: true,
						From: hexg.NewHex(2, -1), Target: hexg.NewHex(3, -1), To: hexg.NewHex(3, -1)},
					{Seq: 2, Kind: game.OrderKindMove, Cost: 1, Carried: true,
						From: hexg.NewHex(3, -1), Target: hexg.NewHex(4, -1), To: hexg.NewHex(4, -1)},
					{Seq: 3, Kind: game.OrderKindMove, Cost: 0, Carried: false, Reason: game.FailureExhaust,
						From: hexg.NewHex(4, -1), Target: hexg.NewHex(5, -1), To: hexg.NewHex(4, -1)},
				},
				Observations: []game.Observation{
					{Seq: 1, Hex: hexg.NewHex(3, -1), State: game.KnowledgeExplored},
					{Seq: 1, Hex: hexg.NewHex(3, -2), State: game.KnowledgeObserved},
					{Seq: 2, Hex: hexg.NewHex(4, -1), State: game.KnowledgeExplored},
				},
			},
			{Turn: 2, EntityID: 9, Start: hexg.NewHex(2, -1), End: hexg.NewHex(2, -1)},
		},
		1: {
			{
				Turn: 1, EntityID: 7, Allowance: 6, Spent: 1, Lapsed: 5,
				Start: hexg.NewHex(2, -1), End: hexg.NewHex(2, -1),
				Orders: []datastore.OrderResult{
					{Seq: 1, Kind: game.OrderKindMove, Cost: 1, Carried: false, Reason: game.FailureTerrain,
						From: hexg.NewHex(2, -1), Target: hexg.NewHex(2, -2), To: hexg.NewHex(2, -1)},
				},
				Observations: []game.Observation{
					{Seq: 1, Hex: hexg.NewHex(2, -2), State: game.KnowledgeObserved},
				},
			},
			{Turn: 1, EntityID: 9, Start: hexg.NewHex(2, -1), End: hexg.NewHex(2, -1)},
		},
	}
	return store
}

func resultsRequest(t *testing.T, store *testStore, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler, cookie := signedInPlayer(t, store)
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookie)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// The page opens on the turn that was just processed, which is the turn before
// the one being ordered. Asking for the report of the turn a player is still
// writing orders for would be asking for a turn nothing has happened in yet.
func TestResultsPageOpensOnTheLatestProcessedTurn(t *testing.T) {
	store := resultsStore()
	response := resultsRequest(t, store, resultsPath, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if store.resultsAsOf != 2 {
		t.Fatalf("read results as of turn %d, want 2 - the turn before the current one", store.resultsAsOf)
	}
	body := response.Body.String()
	for _, want := range []string{
		"<strong>Turn 2</strong>",
		"LEADER-1",
		"Wayfinder",
		"Spent 4 of its 6 points; 2 points lapsed.",
		"Started</dt><dd>(2, -1)",
		"Ended</dt><dd>(4, -1)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page does not contain %q: %s", want, body)
		}
	}
}

// The failures are the lines a player came to read, and the page owes each of
// them a sentence rather than the word the record keeps.
func TestResultsPageExplainsWhyAnOrderDidNotHappen(t *testing.T) {
	store := resultsStore()
	body := resultsRequest(t, store, resultsPath, nil).Body.String()
	if !strings.Contains(body, "It had run out of action points.") {
		t.Fatalf("page does not explain an exhausted order: %s", body)
	}
	if strings.Contains(body, ">exhaust<") {
		t.Fatalf("page shows the recorded vocabulary instead of a sentence: %s", body)
	}
	if !strings.Contains(body, "Did not happen") {
		t.Fatalf("page does not mark the order as failed: %s", body)
	}

	older := resultsRequest(t, store, resultsPath+"?asOfTurn=1", nil).Body.String()
	if !strings.Contains(older, "It could not enter that hex.") {
		t.Fatalf("page does not explain a step into impassable ground: %s", older)
	}
}

// What an order revealed is counted on the line and listed behind it. The two
// states are a documented game concept, so the count names both rather than
// totalling them away.
func TestResultsPageCountsWhatEachOrderRevealed(t *testing.T) {
	store := resultsStore()
	body := resultsRequest(t, store, resultsPath, nil).Body.String()
	for _, want := range []string{
		"Revealed 2 hexes: 1 explored, 1 observed",
		"Revealed 1 hex: 1 explored",
		"<details class=\"revealed\">",
		"(3, -2)",
		"observed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page does not contain %q: %s", want, body)
		}
	}
}

// An entity kind that takes no orders has a ledger of zeroes, which says
// nothing. It still appears - a player reads their whole force in one place -
// and it says what it is instead of showing the zeroes.
func TestResultsPageSaysAHamletTakesNoOrders(t *testing.T) {
	store := resultsStore()
	body := resultsRequest(t, store, resultsPath, nil).Body.String()
	if !strings.Contains(body, "HAMLET-1") || !strings.Contains(body, "Mudville") {
		t.Fatalf("page leaves the hamlet out: %s", body)
	}
	if !strings.Contains(body, "A hamlet takes no orders, so it has no account to give.") {
		t.Fatalf("page does not say why the hamlet has nothing to report: %s", body)
	}
	if strings.Count(body, "<dl class=\"ledger\">") != 1 {
		t.Fatalf("page draws a ledger for an entity that accounts for nothing: %s", body)
	}
}

// An entity that could have been given orders and was not is the line a player
// asks about, so its whole allowance is reported as lapsed rather than left to
// be inferred from an absence.
func TestResultsPageReportsAnEntityThatWasGivenNothingToDo(t *testing.T) {
	store := resultsStore()
	store.results[2] = []datastore.TurnResult{{
		Turn: 2, EntityID: 7, Allowance: 6, Spent: 0, Lapsed: 6,
		Start: hexg.NewHex(2, -1), End: hexg.NewHex(2, -1),
	}}
	body := resultsRequest(t, store, resultsPath, nil).Body.String()
	if !strings.Contains(body, "No orders were given, so all 6 points lapsed.") {
		t.Fatalf("page does not report an idle entity: %s", body)
	}
	if !strings.Contains(body, "where it started") {
		t.Fatalf("page does not say the entity stayed put: %s", body)
	}
}

// The turns are walked with links that keep an href, so the report works with
// the script blocked exactly as it does with it loaded.
func TestResultsPageLinksTheTurnsEitherSide(t *testing.T) {
	store := resultsStore()
	latest := resultsRequest(t, store, resultsPath, nil).Body.String()
	if !strings.Contains(latest, `href="/player/results?asOfTurn=1"`) {
		t.Fatalf("latest report does not link the turn before it: %s", latest)
	}
	if !strings.Contains(latest, "The latest turn") {
		t.Fatalf("latest report does not say it is the latest: %s", latest)
	}

	first := resultsRequest(t, store, resultsPath+"?asOfTurn=1", nil)
	body := first.Body.String()
	if first.Code != http.StatusOK || store.resultsAsOf != 1 {
		t.Fatalf("turn 1 report = %d, read as of %d", first.Code, store.resultsAsOf)
	}
	if !strings.Contains(body, `href="/player/results?asOfTurn=2"`) {
		t.Fatalf("first report does not link the turn after it: %s", body)
	}
	if !strings.Contains(body, "The first turn") {
		t.Fatalf("first report does not say it is the first: %s", body)
	}
}

// A turn the game has no report for is the API's two refusals in a page: a
// well-formed turn that does not exist, and something that is not a turn. Both
// still draw the latest report, because a mistyped link is not a reason to show
// a player nothing.
func TestResultsPageRefusesATurnItHasNoReportFor(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
		status int
		want   string
	}{
		{name: "unreached", target: resultsPath + "?asOfTurn=9", status: http.StatusNotFound, want: "There is no report for turn 9."},
		{name: "current", target: resultsPath + "?asOfTurn=3", status: http.StatusNotFound, want: "There is no report for turn 3."},
		{name: "not a turn", target: resultsPath + "?asOfTurn=soon", status: http.StatusBadRequest, want: "That is not a turn."},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := resultsStore()
			response := resultsRequest(t, store, test.target, nil)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			body := response.Body.String()
			if !strings.Contains(body, test.want) {
				t.Fatalf("page does not say %q: %s", test.want, body)
			}
			if !strings.Contains(body, "<strong>Turn 2</strong>") {
				t.Fatalf("refusal does not fall back to the latest report: %s", body)
			}
		})
	}
}

// A game on its first turn has processed nothing. The page says so rather than
// drawing an empty report that looks broken, and the dashboard does not offer a
// link to it until there is something to read.
func TestResultsPageBeforeTheFirstTurnIsProcessed(t *testing.T) {
	store := resultsStore()
	store.turn = game.FirstTurn
	response := resultsRequest(t, store, resultsPath, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "No turn has been processed yet.") {
		t.Fatalf("page does not say there is nothing to report: %s", body)
	}
	if strings.Contains(body, `aria-label="Turn reports"`) {
		t.Fatalf("page offers turn links with no turns to walk: %s", body)
	}
	if store.resultsAsOf != 0 {
		t.Fatalf("page read results as of turn %d with no processed turn", store.resultsAsOf)
	}

	dashboard := resultsRequest(t, store, "/player/dashboard", nil).Body.String()
	if strings.Contains(dashboard, resultsPath) {
		t.Fatalf("dashboard links a report that does not exist yet: %s", dashboard)
	}
}

// The dashboard is how a player finds the report at all. A page nobody is told
// about is not delivered.
func TestPlayerDashboardLinksTheTurnReport(t *testing.T) {
	store := resultsStore()
	body := resultsRequest(t, store, "/player/dashboard", nil).Body.String()
	if !strings.Contains(body, `href="/player/results"`) {
		t.Fatalf("dashboard does not link the turn report: %s", body)
	}
}

// One URL, two shapes of answer. A turn link asks for the region alone, and the
// response says what it varied on so a cache cannot hand a bare region to a
// browser that asked for a page.
func TestResultsPageAnswersHTMXWithTheRegionAlone(t *testing.T) {
	store := resultsStore()
	response := resultsRequest(t, store, resultsPath+"?asOfTurn=1", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Vary"); got != "HX-Request" {
		t.Fatalf("Vary = %q, want HX-Request", got)
	}
	body := response.Body.String()
	if strings.Contains(body, "<!doctype html>") {
		t.Fatalf("fragment request answered with a page: %s", body)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), `<div id="results-region"`) {
		t.Fatalf("fragment is not the results region: %s", body)
	}
}

// A deactivated faction may read what its people already did. The flag stops a
// faction acting; it does not lock a person out of their own game, which is the
// rule the player map already follows.
func TestResultsPageIsReadableByADeactivatedFaction(t *testing.T) {
	store := resultsStore()
	store.faction.Active = false
	response := resultsRequest(t, store, resultsPath, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "<strong>Turn 2</strong>") {
		t.Fatalf("deactivated faction cannot read its report: %s", response.Body.String())
	}
}

// A read that fails inside the server is one sentence to the client and the
// error to the log, the way every other page read is.
func TestResultsPageReportsAStoreFailure(t *testing.T) {
	store := resultsStore()
	store.resultsErr = errors.New("boom")
	response := resultsRequest(t, store, resultsPath, nil)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "boom") {
		t.Fatalf("page leaks the failure: %s", response.Body.String())
	}
}

// A reason nobody has written a sentence for yet is still a failure the player
// is entitled to see, so the word the turn recorded is reported rather than
// swallowed.
func TestResultReasonLabelReportsAnUnwrittenReason(t *testing.T) {
	if got := resultReasonLabel(""); got != "" {
		t.Fatalf("carried order reason = %q, want empty", got)
	}
	got := resultReasonLabel(game.FailureReason("blocked"))
	if !strings.Contains(got, "blocked") {
		t.Fatalf("unwritten reason = %q, want it to name the recorded word", got)
	}
}

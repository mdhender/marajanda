// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

// orderSeat is where the founding entities of the test faction stand.
var orderSeat = hexg.NewHex(2, -1)

// ordersStore is a configured faction with the two entities it is founded
// with, on the turn it is playing.
func ordersStore() *testStore {
	return &testStore{
		faction: datastore.Faction{Name: "The Wayfarers", Race: game.RaceHuman, Active: true},
		found:   true,
		turn:    3,
		entities: []datastore.Entity{
			{ID: 7, Code: "LEADER-1", Name: "LEADER-1", Kind: game.EntityKindLeader, Location: orderSeat, Allowance: game.LeaderAllowance},
			{ID: 9, Code: "HAMLET-1", Name: "Mudville", Kind: game.EntityKindHamlet, Location: orderSeat},
		},
		orders: map[int64][]datastore.Order{},
	}
}

// ordersRequest signs a player in and makes one request to the orders page.
func ordersRequest(t *testing.T, store *testStore, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler, cookie := signedInPlayer(t, store)
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(cookie)
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// htmxHeader is how HTMX asks for a fragment.
var htmxHeader = map[string]string{"HX-Request": "true"}

// The page lists the whole force, whether or not a piece of it can be given an
// order, and offers each entity only the kinds its own kind accepts.
func TestOrdersPageListsTheWholeForce(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}}}
	response := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, want := range []string{
		"Orders for turn 3",
		// The picker holds the one faction a player commands, and it is chosen.
		`<option value="player@example.com" selected>The Wayfarers</option>`,
		"LEADER-1",
		"HAMLET-1",
		"Mudville",
		// A hamlet takes no orders today and is still on the page.
		"No orders available yet.",
		`<option value="move">Move</option>`,
		// A leader accepts a rest as well as a move.
		`<option value="rest">Rest</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orders page missing %q", want)
		}
	}
	// The kinds offered are the entity's own. The hamlet accepts none, so
	// there is one add control on the page and it belongs to the leader.
	if got := strings.Count(body, `name="add"`); got != 1 {
		t.Fatalf("add controls = %d, want the leader's alone", got)
	}
	if got := strings.Count(body, `name="kind.9"`); got != 0 {
		t.Fatalf("the hamlet is offered %d order kinds, want none", got)
	}
	// The orders read are the orders of the turn the page names.
	if store.asOf != 3 {
		t.Fatalf("orders read as of turn %d, want 3", store.asOf)
	}
}

// An order is one action, so it is one select. Each carries its whole address:
// in its name, for a save that submits the page, and in the URL it posts to,
// for a save that does not.
func TestEachOrderIsOneDirectionSelect(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
		{Seq: 3, Kind: game.OrderKindMove},
	}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	for seq, selected := range []string{"nw", "e", ""} {
		name := fmt.Sprintf(`name="direction.7.%d"`, seq+1)
		post := fmt.Sprintf(`hx-post="/player/orders/7/%d"`, seq+1)
		if !strings.Contains(body, name) || !strings.Contains(body, post) {
			t.Fatalf("order %d is missing %s or %s", seq+1, name, post)
		}
		if selected == "" {
			continue
		}
		if want := fmt.Sprintf(`<option value="%s" selected>`, selected); !strings.Contains(body, want) {
			t.Fatalf("order %d does not show %q", seq+1, selected)
		}
	}
	// Three orders, three selects. There is no blank box on the end of a row
	// and no fourth row: the add control is what lengthens the list.
	if got, want := strings.Count(body, `name="direction.7.`), 3; got != want {
		t.Fatalf("selects = %d, want %d - one per order", got, want)
	}
	// Every select offers the six points in compass order, and the blank that
	// says nothing has been chosen.
	for _, want := range []string{`value=""`, `value="ne">NE north-east`, `value="nw">NW north-west`} {
		if !strings.Contains(body, want) {
			t.Fatalf("a select is missing the option %q", want)
		}
	}
}

// Every control on the page is one a browser can work without HTMX, and the
// script-free page carries the one Save button that submits them all.
func TestOrdersPageWorksWithoutScript(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove}}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	for _, want := range []string{
		`<form class="orders-form" action="/player/orders" method="post">`,
		`name="remove" value="7.1"`,
		`name="insert" value="7.1"`,
		`name="add" value="7"`,
		"<noscript>",
		`type="submit">Save orders</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orders page missing %q", want)
		}
	}
	// Enter in a field submits a form through its first submit button. That
	// button saves; without it, Enter in a direction select would press the
	// first Remove on the page.
	if index := strings.Index(body, `<button class="visually-hidden" type="submit" tabindex="-1">Save orders</button>`); index < 0 {
		t.Fatal("the form has no default submit button, so Enter would remove an order")
	} else if remove := strings.Index(body, `name="remove"`); remove < index {
		t.Fatal("a remove button comes before the form's default submit button")
	}
	// The same controls are wired to HTMX, so a browser running it never
	// submits the form.
	for _, want := range []string{
		`<div id="orders" hx-target="#orders" hx-swap="outerHTML" hx-indicator="#orders">`,
		`hx-post="/player/orders/7/1" hx-trigger="change"`,
		`hx-post="/player/orders/7/1/insert"`,
		`hx-delete="/player/orders/7/1"`,
		`hx-post="/player/orders"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orders page missing %q", want)
		}
	}
}

// A write answers HTMX with the whole orders region and nothing around it.
func TestSettingADirectionAnswersWithTheRegionAlone(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove}}
	response := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1",
		"direction.7.1=ne", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, unwanted := range []string{"<!doctype html>", "· Marajanda</title>", "/sign-out"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("fragment contains %q, want the orders region alone", unwanted)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(body), `<div id="orders"`) {
		t.Fatalf("fragment starts %.60q, want the orders region", strings.TrimSpace(body))
	}
	if !strings.Contains(body, "Saved at ") {
		t.Fatalf("fragment says nothing about the save")
	}
	// The direction landed, and it was written for the turn the page is on.
	if got := store.orders[7][0].Detail.Direction; got != compass.NE {
		t.Fatalf("direction = %v, want NE", got)
	}
	if store.wroteTurn != 3 {
		t.Fatalf("wrote turn %d, want 3", store.wroteTurn)
	}
	// The answer is the whole region, and the order is still one row.
	if got, want := strings.Count(body, `name="direction.7.`), 1; got != want {
		t.Fatalf("selects = %d, want %d", got, want)
	}
}

// HTMX sends the whole enclosing form with every request, so the other selects
// arrive too. The route changes the one order its URL names.
func TestSettingADirectionIgnoresTheOtherSelectsInTheForm(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
	}
	ordersRequest(t, store, http.MethodPost, "/player/orders/7/2",
		"direction.7.1=sw&direction.7.2=se", htmxHeader)

	orders := store.orders[7]
	if orders[0].Detail.Direction != compass.NW || orders[1].Detail.Direction != compass.SE {
		t.Fatalf("orders = %#v, want NW SE - the first left alone", orders)
	}
}

// The blank option empties a row without removing it. Removing is what the
// remove control does.
func TestTheBlankOptionEmptiesARowWithoutRemovingIt(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
	}
	body := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "direction.7.1=", htmxHeader).Body.String()

	orders := store.orders[7]
	if len(orders) != 2 || orders[0].Detail.Direction.IsValid() || orders[1].Detail.Direction != compass.E {
		t.Fatalf("orders = %#v, want the first emptied and both still there", orders)
	}
	if got, want := strings.Count(body, `name="direction.7.`), 2; got != want {
		t.Fatalf("selects = %d, want %d - the row is still on the page", got, want)
	}
}

// The insert control puts a new order after the one it names, so a list can be
// corrected in the middle.
func TestInsertingAnOrderAfterAnother(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
	}
	response := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1/insert", "kind.7=move", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	// The button names the order the new one goes after; the position asked
	// for is the next one.
	if store.insertedAt != 2 {
		t.Fatalf("inserted at %d, want 2", store.insertedAt)
	}
	orders := store.orders[7]
	if len(orders) != 3 || orders[1].Detail.Direction.IsValid() || orders[2].Detail.Direction != compass.E {
		t.Fatalf("orders = %#v, want a blank move between NW and E", orders)
	}
	// The same button works without script, through the form.
	unscripted := ordersStore()
	unscripted.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}}}
	ordersRequest(t, unscripted, http.MethodPost, "/player/orders", "insert=7.1&kind.7=move", nil)
	if unscripted.insertedAt != 2 || len(unscripted.orders[7]) != 2 {
		t.Fatalf("unscripted insert put %d orders in at %d, want 2 at 2",
			len(unscripted.orders[7]), unscripted.insertedAt)
	}
}

// The add control appends an order of the kind it names, and the remove
// control takes one away. Both work from the same form.
func TestAddingAndRemovingAnOrder(t *testing.T) {
	store := ordersStore()
	response := ordersRequest(t, store, http.MethodPost, "/player/orders", "add=7&kind.7=move", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("add status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := store.orders[7]; len(got) != 1 || got[0].Kind != game.OrderKindMove || got[0].Seq != 1 {
		t.Fatalf("orders = %#v, want one move", got)
	}
	// A new order has no direction, so its select shows the blank option.
	if got, want := strings.Count(response.Body.String(), `name="direction.7.1"`), 1; got != want {
		t.Fatalf("selects = %d, want %d", got, want)
	}

	removed := ordersRequest(t, store, http.MethodDelete, "/player/orders/7/1", "", htmxHeader)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want %d", removed.Code, http.StatusOK)
	}
	if got := store.orders[7]; len(got) != 0 {
		t.Fatalf("orders = %#v, want none", got)
	}
}

// A browser without script submits every select at once, and the answer is a
// redirect rather than a page a refresh would post again.
func TestSavingTheWholePageRedirects(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}},
	}
	form := url.Values{
		"direction.7.1": {"e"},
		"direction.7.2": {""},
	}
	response := ordersRequest(t, store, http.MethodPost, "/player/orders", form.Encode(), nil)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/orders" {
		t.Fatalf("save response = %d %q, want %d /player/orders",
			response.Code, response.Header().Get("Location"), http.StatusSeeOther)
	}
	if len(store.savedUpdates) != 2 {
		t.Fatalf("saved %#v, want both orders", store.savedUpdates)
	}
	// The rows arrive in a fixed order, by entity then by sequence, however a
	// browser laid the form out.
	first, second := store.savedUpdates[0], store.savedUpdates[1]
	if first.EntityID != 7 || first.Seq != 1 || first.Detail.Direction != compass.E {
		t.Fatalf("first order saved as %#v, want E", first)
	}
	// A blank select is a direction cleared, not a row dropped. A save that
	// dropped it would leave the order pointing where it used to.
	if second.Seq != 2 || second.Detail.Direction.IsValid() {
		t.Fatalf("second order saved as %#v, want no direction", second)
	}
	if got := store.orders[7]; len(got) != 2 || got[1].Detail.Direction.IsValid() {
		t.Fatalf("orders = %#v, want the second still there and empty", got)
	}
}

// A refusal HTMX can read is one it swaps in. A failed request is not swapped,
// so a scripted write answers 200 and carries the message in the fragment; the
// script-free page gets the status the refusal deserves.
func TestARefusedWriteIsReadableBothWays(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{"a closed turn", datastore.ErrTurnClosed, http.StatusConflict, "The turn advanced while this page was open."},
		{"an order the kind refuses", datastore.ErrOrderKindRefused, http.StatusUnprocessableEntity, "That order is not one this can be given."},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := ordersStore()
			store.orderErr = test.err
			fragment := ordersRequest(t, store, http.MethodPost, "/player/orders", "add=7&kind.7=move", htmxHeader)
			if fragment.Code != http.StatusOK {
				t.Fatalf("fragment status = %d, want %d so HTMX swaps it in", fragment.Code, http.StatusOK)
			}
			if !strings.Contains(fragment.Body.String(), test.wantText) {
				t.Fatalf("fragment does not say %q", test.wantText)
			}

			page := ordersRequest(t, store, http.MethodPost, "/player/orders", "add=7&kind.7=move", nil)
			if page.Code != test.wantStatus {
				t.Fatalf("page status = %d, want %d", page.Code, test.wantStatus)
			}
			if !strings.HasPrefix(page.Body.String(), "<!doctype html>") {
				t.Fatalf("page starts %.40q, want a whole page", page.Body.String())
			}
			if !strings.Contains(page.Body.String(), test.wantText) {
				t.Fatalf("page does not say %q", test.wantText)
			}
		})
	}
}

// A refusal that belongs to one order is shown beside that order rather than
// at the top of the page.
func TestAnOrderRefusalIsShownBesideTheOrder(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove}}
	store.orderErr = datastore.ErrUnknownOrder
	body := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "direction.7.1=ne", htmxHeader).Body.String()

	if !strings.Contains(body, `<p class="message stanza-error" role="alert">That order is no longer there.</p>`) {
		t.Fatalf("the refusal is not beside its order: %s", body)
	}
	if strings.Contains(body, "Saved at ") {
		t.Fatal("a refused write reports a save")
	}
}

// A direction the compass does not know is refused rather than stored.
func TestAnUnknownDirectionIsRefused(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove}}
	response := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "direction.7.1=north", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "no hex lies in that direction") {
		t.Fatalf("the page does not say why north was refused")
	}
	if got := store.orders[7][0].Detail.Direction; got.IsValid() {
		t.Fatalf("direction = %v, want none", got)
	}
}

// The page is a player's, and it is a configured faction's.
func TestOrdersPageIsGated(t *testing.T) {
	unconfigured := &testStore{turn: 1}
	response := ordersRequest(t, unconfigured, http.MethodGet, "/player/orders", "", nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/faction" {
		t.Fatalf("unconfigured player = %d %q, want %d /player/faction",
			response.Code, response.Header().Get("Location"), http.StatusSeeOther)
	}

	// An admin has no orders page in this issue.
	admin := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return admin, true, nil
	}, ordersStore())
	signIn := submitSignIn(handler, admin.Email, "good.luck")
	sent := requestWithCookie(handler, http.MethodGet, "/player/orders", signIn.Result().Cookies()[0], "")
	if sent.Code != http.StatusSeeOther || sent.Header().Get("Location") != "/admin/dashboard" {
		t.Fatalf("admin = %d %q, want %d /admin/dashboard",
			sent.Code, sent.Header().Get("Location"), http.StatusSeeOther)
	}

	// And without a session there is nothing to give orders for.
	signedOut := serveRequest(newHandler(nil, ordersStore()), http.MethodGet, "/player/orders")
	if signedOut.Code != http.StatusSeeOther || signedOut.Header().Get("Location") != "/sign-in" {
		t.Fatalf("signed out = %d %q, want %d /sign-in",
			signedOut.Code, signedOut.Header().Get("Location"), http.StatusSeeOther)
	}
}

// The admin dashboard names the turn and carries the one control that moves it.
func TestAdminAdvancesTheTurn(t *testing.T) {
	store := &testStore{game: datastore.Game{Seed1: 98374, Seed2: -98}, turn: 4}
	admin := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return admin, true, nil
	}, store)
	cookie := submitSignIn(handler, admin.Email, "good.luck").Result().Cookies()[0]

	dashboard := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", cookie, "")
	for _, want := range []string{
		`<form class="turn-control" action="/admin/turn" method="post">`,
		"<strong>Turn 4</strong>",
		"Advance the turn",
	} {
		if !strings.Contains(dashboard.Body.String(), want) {
			t.Fatalf("admin dashboard missing %q", want)
		}
	}

	advanced := requestWithCookie(handler, http.MethodPost, "/admin/turn", cookie, "")
	if advanced.Code != http.StatusSeeOther || advanced.Header().Get("Location") != "/admin/dashboard" {
		t.Fatalf("advance response = %d %q, want %d /admin/dashboard",
			advanced.Code, advanced.Header().Get("Location"), http.StatusSeeOther)
	}
	if store.advanced != 1 || store.turn != 5 {
		t.Fatalf("advanced %d times to turn %d, want once to turn 5", store.advanced, store.turn)
	}
	if got := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", cookie, ""); !strings.Contains(got.Body.String(), "<strong>Turn 5</strong>") {
		t.Fatal("the dashboard still names the old turn")
	}
}

// The clock is the admin's. A player cannot advance it.
func TestAPlayerCannotAdvanceTheTurn(t *testing.T) {
	store := ordersStore()
	response := ordersRequest(t, store, http.MethodPost, "/admin/turn", "", nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("player advance = %d %q, want %d /player/dashboard",
			response.Code, response.Header().Get("Location"), http.StatusSeeOther)
	}
	if store.advanced != 0 {
		t.Fatalf("the clock moved %d times", store.advanced)
	}
}

// The player dashboard is where a player finds the page.
func TestPlayerDashboardLinksToTheOrdersPage(t *testing.T) {
	response := ordersRequest(t, ordersStore(), http.MethodGet, "/player/dashboard", "", nil)
	if !strings.Contains(response.Body.String(), `href="/player/orders"`) {
		t.Fatal("the player dashboard does not link to the orders page")
	}
}

// An order is one action, so a row is one price. The page draws the estimate
// against every row and the budget below them, and says that it is an estimate.
func TestEveryOrderRowCarriesItsEstimatedCost(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove},
	}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	// The fake keeps no knowledge, so every step is onto ground the faction
	// does not know and costs the exploration price.
	if want := fmt.Sprintf(`<span class="stanza-cost">%d AP</span>`, game.UnknownStepCost); !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}
	// A move with no direction yet has nowhere to go, so it has no price
	// rather than a price of nothing.
	if want := "<span class=\"stanza-cost\">\u2014</span>"; !strings.Contains(body, want) {
		t.Fatalf("orders page missing the unpriced row %q", want)
	}
	// The budget line is what the orders cost and what they leave idle.
	for _, want := range []string{
		fmt.Sprintf(`<span class="budget-idle">%d idle</span>`, game.LeaderAllowance-game.UnknownStepCost),
		fmt.Sprintf("%d of %d action points, estimated.", game.UnknownStepCost, game.LeaderAllowance),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orders page missing %q", want)
		}
	}
	// The hamlet takes no orders, so it is owed no budget: one line on the
	// page, and it is the leader's.
	if got := strings.Count(body, `class="order-budget"`); got != 1 {
		t.Fatalf("budget lines = %d, want the leader's alone", got)
	}
}

// Overspending is allowed during entry. The page owes the player the running
// total, the row where the cost crosses the allowance, and a mark on every row
// from there on.
func TestAnOverspendingListSaysWhatWillExhaust(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
		{Seq: 3, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.SE}},
	}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	// Three explorations at 3 AP against an allowance of 6: the third order is
	// where the total crosses, and the residue is gone rather than negative.
	if want := "Order 3 and everything after it will exhaust."; !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}
	if want := fmt.Sprintf("Over by %d.", 3*game.UnknownStepCost-game.LeaderAllowance); !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}
	if want := `<span class="budget-idle">0 idle</span>`; !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q: the line is drawn whatever the count is", want)
	}
	if got := strings.Count(body, `class="stanza-exhausts"`); got != 1 {
		t.Fatalf("exhaust marks = %d, want the third row's alone", got)
	}
}

// A rest says how long it lasts rather than which way it goes, so its row
// carries a count and not a direction select. A count has no blank: a rest
// lasts at least one point.
func TestARestRowCarriesACount(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 2}},
		{Seq: 2, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
	}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	want := fmt.Sprintf(`<input type="number" name="count.7.1" value="2" min="1" max="%d" step="1" hx-post="/player/orders/7/1" hx-trigger="change">`,
		datastore.MaxOrdersPerEntity)
	if !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}
	// A rest costs its count, and the move after it still costs a step.
	if want := `<span class="stanza-cost">2 AP</span>`; !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}
	// The rest is not last, so it is the player's and is drawn as a row. The
	// budget line below is still the residue.
	if got := strings.Count(body, `name="count.7.`); got != 1 {
		t.Fatalf("count controls = %d, want the rest's alone", got)
	}
}

// The trailing Rest is the residue, not a row a player wrote, so it is drawn on
// A rest the player put last is an order like any other: it is drawn with its
// own controls, and it is not the idle points. The idle line reports what the
// whole list leaves over, which is now the allowance less the move and the
// rest together.
func TestARestOnTheEndIsAStanzaLikeAnyOther(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
		{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}},
	}
	body := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()

	if !strings.Contains(body, `name="count.7.2"`) {
		t.Fatal("the rest was not drawn as an editable row")
	}
	if !strings.Contains(body, `value="7.2"`) {
		t.Fatal("the rest carries no insert and remove controls")
	}
	idle := game.LeaderAllowance - game.UnknownStepCost - 3
	if want := fmt.Sprintf(`<span class="budget-idle">%d idle</span>`, idle); !strings.Contains(body, want) {
		t.Fatalf("orders page missing the budget line %q", want)
	}
}

// A rest has no state a player fills in afterwards, so an added one starts at
// one point rather than at nothing.
func TestAddingARestStartsItAtOnePoint(t *testing.T) {
	store := ordersStore()
	response := ordersRequest(t, store, http.MethodPost, "/player/orders", "kind.7=rest&add=7", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	orders := store.orders[7]
	if len(orders) != 1 || orders[0].Kind != game.OrderKindRest || orders[0].Detail.Count != 1 {
		t.Fatalf("orders = %#v, want one rest of one point", orders)
	}
}

// A count is set the way a direction is: the URL names the order and the form
// carries the value under the name that addresses it.
func TestSettingARestCount(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 1}}}
	response := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "count.7.1=4", htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := store.orders[7][0].Detail.Count; got != 4 {
		t.Fatalf("rest count = %d, want 4", got)
	}

	// A count outside what storage admits is refused, and the row says so
	// rather than the page silently keeping the old value.
	refused := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "count.7.1=0", htmxHeader)
	if refused.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: a scripted write is always swapped", refused.Code, http.StatusOK)
	}
	if !strings.Contains(refused.Body.String(), datastore.ErrOrderCountRefused.Error()) {
		t.Fatalf("the refusal is not on the page: %s", refused.Body.String())
	}
	if got := store.orders[7][0].Detail.Count; got != 4 {
		t.Fatalf("rest count = %d after a refused write, want 4", got)
	}
}

// restIdleButton is the markup the rest-the-idle-points control renders as,
// up to the attribute that says it is off.
const restIdleButton = `name="restIdle" value="7" hx-post="/player/orders"`

// The idle points can be spent in one press, and what lands is an ordinary
// Rest: a stanza with its own controls, sized to what the list left over.
func TestTheIdlePointsCanBeRestedInOnePress(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}}}
	idle := game.LeaderAllowance - game.UnknownStepCost

	body := ordersRequest(t, store, http.MethodGet, ordersPath, "", nil).Body.String()
	if want := fmt.Sprintf(`%s>Rest the remaining %d points`, restIdleButton, idle); !strings.Contains(body, want) {
		t.Fatalf("orders page missing the enabled control %q", want)
	}

	response := ordersRequest(t, store, http.MethodPost, ordersPath, "restIdle=7", htmxHeader)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	orders := store.orders[7]
	if len(orders) != 2 {
		t.Fatalf("orders = %#v, want the move and one rest", orders)
	}
	rest := orders[1]
	if rest.Kind != game.OrderKindRest || rest.Detail.Count != idle || rest.Seq != 2 {
		t.Fatalf("appended order = %#v, want a rest of %d points at the end", rest, idle)
	}
	// It is an order like any other from here on: drawn with its own count box
	// and its own remove control, and nothing will resize it.
	drawn := response.Body.String()
	if !strings.Contains(drawn, `name="count.7.2"`) || !strings.Contains(drawn, `value="7.2"`) {
		t.Fatalf("the appended rest is not an ordinary stanza: %s", drawn)
	}
}

// The button names a number and posts an entity. What is written is the residue
// the list has when the write lands, not the one the page was drawn with, so a
// page held open while the orders moved cannot write a stale count.
func TestTheRestedPointsAreCountedWhenTheButtonIsPressed(t *testing.T) {
	store := ordersStore()
	// Drawn with nothing ordered, so the button says the whole allowance.
	body := ordersRequest(t, store, http.MethodGet, ordersPath, "", nil).Body.String()
	if want := fmt.Sprintf("Rest the remaining %d points", game.LeaderAllowance); !strings.Contains(body, want) {
		t.Fatalf("orders page missing %q", want)
	}

	// Then the list moves on underneath it, and the button is pressed anyway.
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}}}
	ordersRequest(t, store, http.MethodPost, ordersPath, "restIdle=7", htmxHeader)

	want := game.LeaderAllowance - game.UnknownStepCost
	if got := store.orders[7][1].Detail.Count; got != want {
		t.Fatalf("rested %d points, want %d: the count came from the page and not from the list", got, want)
	}
}

// A list that already ends in a rest gets a second one. The button never edits
// an order the player wrote - that ambiguity is what #56 was about - so the two
// rests stand side by side and either can be removed on its own.
func TestRestingIdlePointsAppendsASecondRest(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 2}}}
	ordersRequest(t, store, http.MethodPost, ordersPath, "restIdle=7", htmxHeader)

	orders := store.orders[7]
	if len(orders) != 2 {
		t.Fatalf("orders = %#v, want two rests", orders)
	}
	if orders[0].Detail.Count != 2 {
		t.Fatalf("the player's rest was rewritten: %#v", orders[0])
	}
	if want := game.LeaderAllowance - 2; orders[1].Kind != game.OrderKindRest || orders[1].Detail.Count != want {
		t.Fatalf("appended order = %#v, want a rest of %d points", orders[1], want)
	}
}

// Nothing to rest is a disabled control and a reason, not a button that would
// write Rest x0 and not a control that disappears. The attribute is the real
// one, so the button is out of the tab order and refuses a press on its own.
func TestTheRestIdleButtonIsDisabledWhenThereIsNothingToRest(t *testing.T) {
	move := datastore.Order{Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}}
	for name, test := range map[string]struct {
		orders []datastore.Order
		reason string
	}{
		"every point spent": {
			orders: []datastore.Order{
				{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
				{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}},
			},
			reason: "These orders spend every point.",
		},
		"already overspent": {
			// Three steps onto ground the faction does not know is nine points
			// of a six point allowance.
			orders: []datastore.Order{
				{Seq: 1, Kind: move.Kind, Detail: move.Detail},
				{Seq: 2, Kind: move.Kind, Detail: move.Detail},
				{Seq: 3, Kind: move.Kind, Detail: move.Detail},
			},
			reason: "These orders already cost 3 points more than the allowance.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := ordersStore()
			store.orders[7] = test.orders
			body := ordersRequest(t, store, http.MethodGet, ordersPath, "", nil).Body.String()

			if want := restIdleButton + " disabled>"; !strings.Contains(body, want) {
				t.Fatalf("the control is not really disabled, only styled: %s", body)
			}
			if !strings.Contains(body, test.reason) {
				t.Fatalf("the control gives no reason, want %q", test.reason)
			}
		})
	}
}

// The disabled attribute is what the page says, and the server is what decides.
// A request built by hand, or made by a page drawn before the last point was
// spent, is refused rather than writing a rest that lasts no time.
func TestRestingWhenThereIsNothingIdleIsRefused(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.E}},
		{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}},
	}
	response := ordersRequest(t, store, http.MethodPost, ordersPath, "restIdle=7", nil)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(response.Body.String(), "no idle action points to rest") {
		t.Fatalf("the refusal says nothing a player can act on: %s", response.Body.String())
	}
	if len(store.orders[7]) != 2 {
		t.Fatalf("a refused press still wrote: %#v", store.orders[7])
	}
}

// An entity that cannot be told to rest is offered no control to rest with. The
// hamlet takes no orders at all, so its section carries neither.
func TestAnEntityThatTakesNoOrdersIsOfferedNoRestControl(t *testing.T) {
	body := ordersRequest(t, ordersStore(), http.MethodGet, ordersPath, "", nil).Body.String()
	if strings.Contains(body, `value="9" hx-post="/player/orders"`) {
		t.Fatalf("the hamlet was offered a control: %s", body)
	}
	if strings.Count(body, `name="restIdle"`) != 1 {
		t.Fatalf("want one rest control, for the leader alone: %s", body)
	}
}

// The estimate is never asked for from the request. A player who hand-builds
// one cannot ask for the accurate answer, because there is nothing in the
// request that says which answer to give.
func TestTheAccuracyLevelIsNotAFormField(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NW}}}
	fogged := ordersRequest(t, store, http.MethodGet, "/player/orders", "", nil).Body.String()
	asked := ordersRequest(t, store, http.MethodGet, "/player/orders?sight=truth&accuracy=exact", "", nil).Body.String()

	if want := fmt.Sprintf(`<span class="stanza-cost">%d AP</span>`, game.UnknownStepCost); !strings.Contains(asked, want) {
		t.Fatalf("orders page missing %q", want)
	}
	if fogged != asked {
		t.Fatal("asking for a different accuracy in the query string changed the page")
	}
}

// pageOrdersTag is the tag the page renders into its hidden field, which is
// what every one of its writes sends back.
func pageOrdersTag(t *testing.T, store *testStore) string {
	t.Helper()
	body := ordersRequest(t, store, http.MethodGet, ordersPath, "", nil).Body.String()
	const marker = `name="ordersTag" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("the orders page carries no tag")
	}
	rest := body[start+len(marker):]
	end := strings.Index(rest, `"`)
	if end <= 0 {
		t.Fatal("the orders page's tag is empty")
	}
	return rest[:end]
}

// The page carries the tag of the list it was drawn from, and it moves when the
// orders do.
func TestOrdersPageCarriesTheTagOfTheListItDrew(t *testing.T) {
	store := ordersStore()
	empty := pageOrdersTag(t, store)
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	if withOrder := pageOrdersTag(t, store); withOrder == empty {
		t.Fatal("the page's tag did not move when the orders did")
	}
}

// A scripted write that lost a race swaps the notice alone. The list the player
// knows is left where it is, because the orders the other client wrote are
// orders this player has never seen.
func TestOrdersPageConflictSwapsTheNoticeAndNotTheList(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	stale := pageOrdersTag(t, store)

	// Somebody else writes, so the tag the page holds is no longer the list.
	store.orders[7] = append(store.orders[7], datastore.Order{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}})

	form := url.Values{"ordersTag": {stale}, "direction.7.1": {"e"}}
	response := ordersRequest(t, store, http.MethodPost, ordersPath+"/7/1", form.Encode(), htmxHeader)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d so HTMX swaps the notice", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("HX-Retarget"); got != "#orders-notice" {
		t.Fatalf("HX-Retarget = %q, want #orders-notice", got)
	}
	if got := response.Header().Get("HX-Reswap"); got != "innerHTML" {
		t.Fatalf("HX-Reswap = %q, want innerHTML", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Conflicting update") || !strings.Contains(body, "Refresh") {
		t.Fatalf("conflict notice = %q", body)
	}
	// The response is the notice and nothing else: no stanza, no other
	// client's order, no hidden tag that would re-arm the stale page.
	for _, unwanted := range []string{`id="orders"`, "LEADER-1", "ordersTag", "<select"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the conflict response carries %q, so it is replacing the list: %q", unwanted, body)
		}
	}
	// And nothing was written.
	if len(store.orders[7]) != 2 || store.orders[7][0].Detail.Direction != compass.NE {
		t.Fatalf("the refused write changed the orders: %#v", store.orders[7])
	}
}

// Every control the form holds is inside one fieldset, so the conflict notice
// has one attribute to set rather than a walk over every control - and the
// attribute is a real one. See #66.
func TestTheOrdersFormHoldsItsControlsInOneFieldset(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	body := ordersRequest(t, store, http.MethodGet, ordersPath, "", nil).Body.String()

	start := strings.Index(body, `<form class="orders-form"`)
	if start < 0 {
		t.Fatalf("the page carries no orders form: %s", body)
	}
	form := body[start:]
	form = form[:strings.Index(form, "</form>")+len("</form>")]

	fieldset := strings.Index(form, `<fieldset id="orders-controls">`)
	closing := strings.Index(form, "</fieldset>")
	if fieldset < 0 || closing < fieldset {
		t.Fatalf("the form does not hold one fieldset: %s", form)
	}
	// Every control is inside it, the hidden tag included: one that sat outside
	// would be the one a conflict left live.
	controls := form[fieldset:closing]
	for _, want := range []string{`name="ordersTag"`, `name="direction.7.1"`, `name="remove"`, `name="add"`, `name="restIdle"`} {
		if !strings.Contains(controls, want) {
			t.Fatalf("%s is outside the fieldset, so a conflict would leave it live", want)
		}
	}
	// The Refresh link the notice carries is the one thing that must stay live,
	// and it is outside the form entirely.
	if strings.Contains(controls, `hx-get="/player/orders"`) {
		t.Fatal("the refresh link is inside the fieldset, so a conflict would disable the way out of it")
	}
}

// The page no longer claims to disable anything with pointer-events. That rule
// stopped a mouse and nothing else: a keyboard still reached every control and
// an accessibility tree with no disabled state in it reported a live form. What
// turns the controls off now is the attribute, and the styling only describes
// it. See #66.
func TestTheConflictStylingDoesNotStandInForDisabling(t *testing.T) {
	body := ordersRequest(t, ordersStore(), http.MethodGet, ordersPath, "", nil).Body.String()

	for _, rule := range strings.Split(body, "\n") {
		if !strings.Contains(rule, "pointer-events") {
			continue
		}
		// The page's own backdrop uses it to stay out of the way of a cursor,
		// which is what the property is for. A control is what it must not
		// reach.
		if strings.Contains(rule, "orders") || strings.Contains(rule, "button") {
			t.Fatalf("a pointer-events rule is back on a control: %s", strings.TrimSpace(rule))
		}
	}
	if !strings.Contains(body, ".orders-form fieldset[disabled]") {
		t.Fatal("nothing styles the real disabled state")
	}
}

// Every one of the page's write controls carries the tag and answers a conflict
// the same way. A control that dropped it would be the one that always wins.
func TestOrdersPageWriteControlsAllCarryTheTag(t *testing.T) {
	for name, test := range map[string]struct {
		method, target string
		form           url.Values
	}{
		"one detail": {http.MethodPost, ordersPath + "/7/1", url.Values{"direction.7.1": {"e"}}},
		"insert":     {http.MethodPost, ordersPath + "/7/1/insert", url.Values{"kind.7": {"move"}}},
		"remove":     {http.MethodDelete, ordersPath + "/7/1", url.Values{}},
		"save":       {http.MethodPost, ordersPath, url.Values{"direction.7.1": {"e"}}},
		"add":        {http.MethodPost, ordersPath, url.Values{"add": {"7"}, "kind.7": {"move"}}},
		"rest idle":  {http.MethodPost, ordersPath, url.Values{"restIdle": {"7"}}},
	} {
		t.Run(name, func(t *testing.T) {
			store := ordersStore()
			store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
			stale := pageOrdersTag(t, store)
			store.orders[7] = append(store.orders[7], datastore.Order{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}})
			before := len(store.orders[7])

			form := url.Values{"ordersTag": {stale}}
			for key, values := range test.form {
				form[key] = values
			}
			target, body := test.target, form.Encode()
			if test.method == http.MethodDelete {
				// A DELETE's values ride in the query, which is where HTMX
				// puts them and where r.Form finds them.
				target, body = target+"?"+body, ""
			}
			response := ordersRequest(t, store, test.method, target, body, htmxHeader)
			if got := response.Header().Get("HX-Retarget"); got != "#orders-notice" {
				t.Fatalf("%s answered %d with HX-Retarget %q, want the conflict notice; body = %s",
					name, response.Code, got, response.Body.String())
			}
			if len(store.orders[7]) != before {
				t.Fatalf("%s wrote despite the conflict: %#v", name, store.orders[7])
			}
		})
	}
}

// A write with the tag the page is holding goes through, so the guard does not
// stand in the way of ordinary play.
func TestOrdersPageWritesWithACurrentTagSucceed(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	form := url.Values{"ordersTag": {pageOrdersTag(t, store)}, "direction.7.1": {"e"}}
	response := ordersRequest(t, store, http.MethodPost, ordersPath+"/7/1", form.Encode(), htmxHeader)
	if response.Code != http.StatusOK || response.Header().Get("HX-Retarget") != "" {
		t.Fatalf("a current tag was refused: %d %s", response.Code, response.Body.String())
	}
	if store.orders[7][0].Detail.Direction != compass.E {
		t.Fatalf("the write did not land: %#v", store.orders[7])
	}
}

// Without script there is no pending state to protect: the submission is a page
// load, so the conflict is reported on the page the load draws.
func TestOrdersPageConflictWithoutScriptRedrawsWithTheNotice(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	stale := pageOrdersTag(t, store)
	store.orders[7] = append(store.orders[7], datastore.Order{Seq: 2, Kind: game.OrderKindRest, Detail: game.OrderDetail{Count: 3}})

	form := url.Values{"ordersTag": {stale}, "direction.7.1": {"e"}}
	response := ordersRequest(t, store, http.MethodPost, ordersPath, form.Encode(), nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Conflicting update") || !strings.Contains(body, "redrawn") {
		t.Fatalf("unscripted conflict page = %q", body)
	}
	if !strings.Contains(body, "LEADER-1") {
		t.Fatal("the unscripted conflict page did not redraw the list")
	}
}

// A write that sends no tag is unconditional, which is what a hand-built
// request does and what the page did before it carried one.
func TestOrdersPageWritesWithoutATagAreUnconditional(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Detail: game.OrderDetail{Direction: compass.NE}}}
	form := url.Values{"direction.7.1": {"e"}}
	response := ordersRequest(t, store, http.MethodPost, ordersPath+"/7/1", form.Encode(), htmxHeader)
	if response.Code != http.StatusOK || response.Header().Get("HX-Retarget") != "" {
		t.Fatalf("an untagged write was refused: %d %s", response.Code, response.Body.String())
	}
	if store.orders[7][0].Detail.Direction != compass.E {
		t.Fatalf("the write did not land: %#v", store.orders[7])
	}
}

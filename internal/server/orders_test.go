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
			{ID: 7, Code: "LEADER-1", Name: "LEADER-1", Kind: game.EntityKindLeader, Location: orderSeat},
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
	store.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW}}
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
		{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW},
		{Seq: 2, Kind: game.OrderKindMove, Direction: compass.E},
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
	if got := store.orders[7][0].Direction; got != compass.NE {
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
		{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW},
		{Seq: 2, Kind: game.OrderKindMove, Direction: compass.E},
	}
	ordersRequest(t, store, http.MethodPost, "/player/orders/7/2",
		"direction.7.1=sw&direction.7.2=se", htmxHeader)

	orders := store.orders[7]
	if orders[0].Direction != compass.NW || orders[1].Direction != compass.SE {
		t.Fatalf("orders = %#v, want NW SE - the first left alone", orders)
	}
}

// The blank option empties a row without removing it. Removing is what the
// remove control does.
func TestTheBlankOptionEmptiesARowWithoutRemovingIt(t *testing.T) {
	store := ordersStore()
	store.orders[7] = []datastore.Order{
		{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW},
		{Seq: 2, Kind: game.OrderKindMove, Direction: compass.E},
	}
	body := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1", "direction.7.1=", htmxHeader).Body.String()

	orders := store.orders[7]
	if len(orders) != 2 || orders[0].Direction.IsValid() || orders[1].Direction != compass.E {
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
		{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW},
		{Seq: 2, Kind: game.OrderKindMove, Direction: compass.E},
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
	if len(orders) != 3 || orders[1].Direction.IsValid() || orders[2].Direction != compass.E {
		t.Fatalf("orders = %#v, want a blank move between NW and E", orders)
	}
	// The same button works without script, through the form.
	unscripted := ordersStore()
	unscripted.orders[7] = []datastore.Order{{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW}}
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
		{Seq: 1, Kind: game.OrderKindMove, Direction: compass.NW},
		{Seq: 2, Kind: game.OrderKindMove, Direction: compass.NE},
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
	if len(store.savedDirections) != 2 {
		t.Fatalf("saved %#v, want both orders", store.savedDirections)
	}
	// The rows arrive in a fixed order, by entity then by sequence, however a
	// browser laid the form out.
	first, second := store.savedDirections[0], store.savedDirections[1]
	if first.EntityID != 7 || first.Seq != 1 || first.Direction != compass.E {
		t.Fatalf("first order saved as %#v, want E", first)
	}
	// A blank select is a direction cleared, not a row dropped. A save that
	// dropped it would leave the order pointing where it used to.
	if second.Seq != 2 || second.Direction.IsValid() {
		t.Fatalf("second order saved as %#v, want no direction", second)
	}
	if got := store.orders[7]; len(got) != 2 || got[1].Direction.IsValid() {
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
	if got := store.orders[7][0].Direction; got.IsValid() {
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

// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
)

// A deactivated account that presented the right passphrase is told so. The
// store is what decides that, and it says so with a sentinel the handler tells
// apart from a failure it could do nothing about.
func TestSignInRefusesADeactivatedAccount(t *testing.T) {
	handler := newHandler(func(_ context.Context, _, secret string) (datastore.Account, bool, error) {
		if secret != "good.luck" {
			return datastore.Account{}, false, nil
		}
		return datastore.Account{}, false, fmt.Errorf("%w: %s", datastore.ErrAccountInactive, "player@example.com")
	}, nil)

	response := submitSignIn(handler, "player@example.com", "good.luck")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), "That account is not active") {
		t.Fatalf("body = %q, want the deactivation message", response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("a refused sign-in started a session")
	}

	// A wrong passphrase on the same account is the refusal every wrong
	// passphrase gets, so an email address alone still says nothing.
	wrong := submitSignIn(handler, "player@example.com", "not.right")
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-passphrase status = %d, want %d", wrong.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(wrong.Body.String(), "credentials were not accepted") {
		t.Fatalf("wrong-passphrase body = %q, want the rejected-credentials message", wrong.Body.String())
	}
	if strings.Contains(wrong.Body.String(), "not active") {
		t.Fatalf("wrong-passphrase body = %q, want no mention of the flag", wrong.Body.String())
	}
}

// The player dashboard is where a deactivated faction is told why, so it is the
// one player page that says it and the one that drops the link to orders.
func TestPlayerDashboardSaysAFactionIsNotActive(t *testing.T) {
	store := ordersStore()
	store.faction.Active = false
	handler, cookie := signedInPlayer(t, store)

	request := httptest.NewRequest(http.MethodGet, "/player/dashboard", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "This faction is not active") {
		t.Fatalf("dashboard body = %q, want it to say the faction is not active", body)
	}
	if strings.Contains(body, `href="/player/orders"`) {
		t.Fatal("the dashboard offers orders to a faction that cannot give them")
	}
	// The rest of the player's game is untouched: the flag stops a faction
	// acting, it does not lock a person out of looking at their own game.
	for _, want := range []string{"The Wayfarers", "LEADER-1", `href="/player/map"`, `action="/sign-out"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard body missing %q", want)
		}
	}
}

// Every orders route is a page a deactivated faction cannot reach. It is sent
// to its dashboard rather than to the faction form: it is configured, and the
// form would ask it to build a faction it already has.
func TestOrdersRoutesRefuseADeactivatedFaction(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "the page", method: http.MethodGet, target: "/player/orders"},
		{name: "the whole form", method: http.MethodPost, target: "/player/orders", body: "add=7&kind.7=move"},
		{name: "one step box", method: http.MethodPost, target: "/player/orders/7/1/1", body: "step.7.1.1=ne"},
		{name: "one stanza", method: http.MethodDelete, target: "/player/orders/7/1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := ordersStore()
			store.faction.Active = false
			response := ordersRequest(t, store, test.method, test.target, test.body, nil)

			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/dashboard" {
				t.Fatalf("response = %d %q, want %d /player/dashboard",
					response.Code, response.Header().Get("Location"), http.StatusSeeOther)
			}
			if len(store.orders[7]) != 0 || store.savedSteps != nil {
				t.Fatalf("a refused request wrote %#v and %#v", store.orders, store.savedSteps)
			}

			// HTMX follows a 303 itself and swaps what comes back into the
			// region it asked for, which would land a whole dashboard page
			// inside the orders form. A gate has to take the browser off the
			// page instead.
			scripted := ordersRequest(t, store, test.method, test.target, test.body, htmxHeader)
			if scripted.Header().Get("HX-Redirect") != "/player/dashboard" {
				t.Fatalf("scripted response = %d, HX-Redirect %q; want /player/dashboard",
					scripted.Code, scripted.Header().Get("HX-Redirect"))
			}
			if scripted.Code != http.StatusOK {
				t.Fatalf("scripted status = %d, want %d so HTMX reads the header", scripted.Code, http.StatusOK)
			}
			if strings.Contains(scripted.Body.String(), "Your force") {
				t.Fatalf("scripted body = %q, want no page swapped into the region", scripted.Body.String())
			}
		})
	}
}

// The store refuses the writes as well as the page. What reaches this is the
// faction being deactivated between the page's read and its write.
func TestADeactivatedFactionsWriteIsAnsweredForbidden(t *testing.T) {
	store := ordersStore()
	store.orderErr = fmt.Errorf("%w: %s", datastore.ErrFactionInactive, "player@example.com")

	response := ordersRequest(t, store, http.MethodPost, "/player/orders/7/1/1", "step.7.1.1=ne", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), "This faction is not active") {
		t.Fatalf("body = %q, want the deactivation message", response.Body.String())
	}
}

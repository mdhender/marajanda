// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
)

func TestLandingAndSignInForm(t *testing.T) {
	handler := newHandler(nil, nil)

	landing := serveRequest(handler, http.MethodGet, "/")
	if landing.Code != http.StatusOK {
		t.Fatalf("landing status = %d, want %d", landing.Code, http.StatusOK)
	}
	for _, want := range []string{"The realm remembers", `href="/sign-in"`, "Unwritten frontiers"} {
		if !strings.Contains(landing.Body.String(), want) {
			t.Fatalf("landing body missing %q", want)
		}
	}
	for _, want := range []string{
		"v0.2.1-beta",
		`href="https://github.com/mdhender/marajanda/issues"`,
		`aria-label="Marajanda issues on GitHub"`,
	} {
		if !strings.Contains(landing.Body.String(), want) {
			t.Fatalf("landing footer missing %q", want)
		}
	}

	form := serveRequest(handler, http.MethodGet, "/sign-in")
	if form.Code != http.StatusOK {
		t.Fatalf("sign-in status = %d, want %d", form.Code, http.StatusOK)
	}
	for _, want := range []string{
		`name="account" type="text"`,
		`name="passphrase" type="password"`,
		`action="/sign-in" method="post"`,
	} {
		if !strings.Contains(form.Body.String(), want) {
			t.Fatalf("sign-in body missing %q", want)
		}
	}
}

func TestSignInRejectsMalformedAccountBeforeAuthentication(t *testing.T) {
	authenticationCalls := 0
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		authenticationCalls++
		return datastore.Account{}, false, nil
	}, nil)

	response := submitSignIn(handler, "not-an-email", "anything")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if authenticationCalls != 0 {
		t.Fatalf("authentication calls = %d, want 0", authenticationCalls)
	}
	if !strings.Contains(response.Body.String(), "credentials were not accepted") {
		t.Fatalf("body = %q, want rejected-credentials message", response.Body.String())
	}
}

func TestSignInRejectsInvalidCredentials(t *testing.T) {
	handler := newHandler(func(_ context.Context, email, secret string) (datastore.Account, bool, error) {
		if email != "admin@example.com" || secret != "wrong" {
			t.Fatalf("credentials = %q, %q", email, secret)
		}
		return datastore.Account{}, false, nil
	}, nil)

	response := submitSignIn(handler, " ADMIN@EXAMPLE.COM ", "wrong")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "credentials were not accepted") {
		t.Fatalf("body = %q, want rejected-credentials message", response.Body.String())
	}
}

func TestSignInCreatesSessionAndRoutesByRole(t *testing.T) {
	for _, test := range []struct {
		role          string
		handle        string
		wantPath      string
		otherPath     string
		wantDashboard []string
	}{
		{role: "admin", handle: "keeper", wantPath: "/admin/dashboard", otherPath: "/player/dashboard", wantDashboard: []string{"Game seeds", "Seed 1", "98374", "Seed 2", "-98", "cannot be changed"}},
		{role: "player", handle: "wanderer", wantPath: "/player/dashboard", otherPath: "/admin/dashboard", wantDashboard: []string{"The Wayfarers", "(2, -1)"}},
	} {
		t.Run(test.role, func(t *testing.T) {
			var store applicationStore
			if test.role == "player" {
				store = &testStore{faction: datastore.Faction{Name: "The Wayfarers", Location: hexg.NewHex(2, -1)}, found: true}
			} else {
				store = &testStore{game: datastore.Game{Seed1: 98374, Seed2: -98}}
			}
			handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
				return datastore.Account{Email: test.role + "@example.com", Handle: test.handle, Role: test.role}, true, nil
			}, store)
			response := submitSignIn(handler, test.role+"@example.com", "good.luck")
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != test.wantPath {
				t.Fatalf("sign-in response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, test.wantPath)
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != sessionCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 0 {
				t.Fatalf("session cookie = %#v", cookie)
			}

			dashboardRequest := httptest.NewRequest(http.MethodGet, test.wantPath, nil)
			dashboardRequest.AddCookie(cookie)
			dashboard := httptest.NewRecorder()
			handler.ServeHTTP(dashboard, dashboardRequest)
			if dashboard.Code != http.StatusOK {
				t.Fatalf("dashboard status = %d, want %d", dashboard.Code, http.StatusOK)
			}
			wants := []string{
				"Welcome, " + test.handle + ".",
				`action="/sign-out" method="post"`,
				`type="submit">Sign out</button>`,
			}
			wants = append(wants, test.wantDashboard...)
			for _, want := range wants {
				if !strings.Contains(dashboard.Body.String(), want) {
					t.Fatalf("dashboard body missing %q", want)
				}
			}
			if test.role == "admin" && strings.Contains(dashboard.Body.String(), "<input") {
				t.Fatal("admin dashboard contains an editable input")
			}

			otherRequest := httptest.NewRequest(http.MethodGet, test.otherPath, nil)
			otherRequest.AddCookie(cookie)
			other := httptest.NewRecorder()
			handler.ServeHTTP(other, otherRequest)
			if other.Code != http.StatusSeeOther || other.Header().Get("Location") != test.wantPath {
				t.Fatalf("other dashboard response = %d %q, want %d %q", other.Code, other.Header().Get("Location"), http.StatusSeeOther, test.wantPath)
			}
		})
	}
}

func TestSignOutEndsSession(t *testing.T) {
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Handle: "wanderer", Role: "player"}, true, nil
	}, nil)
	signIn := submitSignIn(handler, "player@example.com", "good.luck")
	sessionCookie := signIn.Result().Cookies()[0]

	request := httptest.NewRequest(http.MethodPost, "/sign-out", nil)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sign-in" {
		t.Fatalf("sign-out response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/sign-in")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	expiredCookie := cookies[0]
	if expiredCookie.Name != sessionCookieName || expiredCookie.MaxAge != -1 || !expiredCookie.HttpOnly || !expiredCookie.Secure || expiredCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expired session cookie = %#v", expiredCookie)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/player/dashboard", nil)
	dashboardRequest.AddCookie(sessionCookie)
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	if dashboard.Code != http.StatusSeeOther || dashboard.Header().Get("Location") != "/sign-in" {
		t.Fatalf("dashboard response = %d %q, want %d %q", dashboard.Code, dashboard.Header().Get("Location"), http.StatusSeeOther, "/sign-in")
	}
}

func TestDashboardRequiresSession(t *testing.T) {
	response := serveRequest(newHandler(nil, nil), http.MethodGet, "/admin/dashboard")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sign-in" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/sign-in")
	}
}

func TestPlayerConfiguresFactionBeforeDashboard(t *testing.T) {
	factions := &testStore{}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player"}, true, nil
	}, factions)
	signIn := submitSignIn(handler, "player@example.com", "good.luck")
	cookie := signIn.Result().Cookies()[0]

	dashboard := requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	if dashboard.Code != http.StatusSeeOther || dashboard.Header().Get("Location") != "/player/faction" {
		t.Fatalf("dashboard response = %d %q, want %d %q", dashboard.Code, dashboard.Header().Get("Location"), http.StatusSeeOther, "/player/faction")
	}

	form := requestWithCookie(handler, http.MethodGet, "/player/faction", cookie, "")
	if form.Code != http.StatusOK || !strings.Contains(form.Body.String(), `action="/player/faction" method="post"`) {
		t.Fatalf("faction form = %d %q, want configuration form", form.Code, form.Body.String())
	}

	invalid := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie, url.Values{"name": {"ab"}}.Encode())
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "3 to 32 printable characters") {
		t.Fatalf("invalid response = %d %q, want validation error", invalid.Code, invalid.Body.String())
	}

	configured := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie, url.Values{"name": {"  Star   Kin  "}}.Encode())
	if configured.Code != http.StatusSeeOther || configured.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("configuration response = %d %q, want %d %q", configured.Code, configured.Header().Get("Location"), http.StatusSeeOther, "/player/dashboard")
	}
	if factions.email != "player@example.com" || factions.faction.Name != "Star Kin" {
		t.Fatalf("saved faction = %q %#v, want normalized Star Kin faction", factions.email, factions.faction)
	}

	dashboard = requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	for _, want := range []string{"Star Kin", "Current location", "(0, 0)"} {
		if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), want) {
			t.Fatalf("dashboard = %d %q, want %q", dashboard.Code, dashboard.Body.String(), want)
		}
	}
	form = requestWithCookie(handler, http.MethodGet, "/player/faction", cookie, "")
	if form.Code != http.StatusSeeOther || form.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("completed configuration response = %d %q, want dashboard redirect", form.Code, form.Header().Get("Location"))
	}
}

func TestCrossOriginSignInIsRejected(t *testing.T) {
	values := url.Values{"account": {"admin@example.com"}, "passphrase": {"good.luck"}}
	request := httptest.NewRequest(http.MethodPost, "https://marajanda.test/sign-in", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://hostile.example")
	response := httptest.NewRecorder()

	newHandler(nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func serveRequest(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	handler.ServeHTTP(response, request)
	return response
}

func submitSignIn(handler http.Handler, account, passphrase string) *httptest.ResponseRecorder {
	values := url.Values{"account": {account}, "passphrase": {passphrase}}
	request := httptest.NewRequest(http.MethodPost, "/sign-in", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requestWithCookie(handler http.Handler, method, target string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(cookie)
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type testStore struct {
	email   string
	faction datastore.Faction
	found   bool
	game    datastore.Game
	visible []hexg.Hex
}

func (s *testStore) Game(context.Context) (datastore.Game, error) {
	return s.game, nil
}

func (s *testStore) Faction(context.Context, string) (datastore.Faction, bool, error) {
	return s.faction, s.found, nil
}

func (s *testStore) VisibleHexes(context.Context, string) ([]hexg.Hex, error) {
	return s.visible, nil
}

func (s *testStore) SaveFaction(_ context.Context, email, name string) error {
	s.email = email
	s.faction = datastore.Faction{Name: name, Location: hexg.NewHex(0, 0)}
	s.found = true
	return nil
}

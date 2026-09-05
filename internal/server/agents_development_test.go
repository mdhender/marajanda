// Copyright (c) 2026 Michael D Henderson.

//go:build !production

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func TestAgentSignInCreatesNormalSession(t *testing.T) {
	var gotEmail string
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		gotEmail = email
		return datastore.Account{Email: email, Handle: "reviewer", Role: "player"}, nil
	}, &testStore{faction: datastore.Faction{Name: "Reviewers"}, found: true}, "development")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/Reviewer@Example.Test?returnTo=%2Fplayer%2Fdashboard")

	if gotEmail != "reviewer@example.test" {
		t.Fatalf("account email = %q, want normalized email", gotEmail)
	}
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/player/dashboard")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 0 {
		t.Fatalf("session cookie = %#v", cookie)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/player/dashboard", nil)
	dashboardRequest.AddCookie(cookie)
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "Welcome, reviewer.") {
		t.Fatalf("dashboard = %d %q, want authenticated reviewer", dashboard.Code, dashboard.Body.String())
	}
}

func TestAgentSignInRejectsUnsafeReturnPaths(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		return datastore.Account{Handle: "reviewer", Role: "player"}, nil
	}, nil, "development")
	for _, value := range []string{
		"",
		"dashboard",
		"https://hostile.example/steal",
		"//hostile.example/steal",
		`/\hostile.example/steal`,
		"/%2f%2fhostile.example/steal",
	} {
		t.Run(value, func(t *testing.T) {
			target := "/__agents/log-me-in/agent@example.test"
			if value != "" {
				target += "?returnTo=" + url.QueryEscape(value)
			}
			response := serveRequest(handler, http.MethodGet, target)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
				t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/")
			}
		})
	}
}

func TestAgentSignInRequiresEmail(t *testing.T) {
	calls := 0
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		calls++
		return datastore.Account{}, nil
	}, nil, "development")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/not-an-email")
	if response.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("response status = %d, account calls = %d; want %d, 0", response.Code, calls, http.StatusBadRequest)
	}
}

func TestAgentSignInNotRegisteredInProductionEnvironment(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		t.Fatal("production route called account lookup")
		return datastore.Account{}, nil
	}, nil, "production")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestAgentSignInGeneratesFactionWhenMissing(t *testing.T) {
	store := &testStore{}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "agent", Role: "player"}, nil
	}, store, "development")

	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test?returnTo=%2Fplayer%2Fdashboard")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/player/dashboard")
	}
	if store.email != "agent@example.test" {
		t.Fatalf("saved faction for %q, want agent@example.test", store.email)
	}
	if !store.faction.Configured() {
		t.Fatal("development account left without a faction")
	}
	if _, err := game.NormalizeFactionName(store.faction.Name); err != nil {
		t.Fatalf("generated faction name %q is invalid: %v", store.faction.Name, err)
	}

	// The whole point is that the guard has nothing to block, so the dashboard
	// must now answer rather than divert to the configuration form.
	cookie := response.Result().Cookies()[0]
	dashboard := requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), store.faction.Name) {
		t.Fatalf("dashboard = %d %q, want the generated faction", dashboard.Code, dashboard.Body.String())
	}
}

func TestAgentSignInKeepsAnExistingFaction(t *testing.T) {
	store := &testStore{faction: datastore.Faction{Name: "Star Kin"}, found: true}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "agent", Role: "player"}, nil
	}, store, "development")

	serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test?returnTo=%2Fplayer%2Fdashboard")

	if store.email != "" || store.faction.Name != "Star Kin" {
		t.Fatalf("faction = %q saved for %q, want Star Kin untouched", store.faction.Name, store.email)
	}
}

func TestAgentSignInLeavesAdminsWithoutAFaction(t *testing.T) {
	store := &testStore{}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "keeper", Role: "admin"}, nil
	}, store, "development")

	serveRequest(handler, http.MethodGet, "/__agents/log-me-in/keeper@example.test?returnTo=%2Fadmin%2Fdashboard")

	if store.email != "" || store.faction.Configured() {
		t.Fatalf("admin received faction %q, want none", store.faction.Name)
	}
}

// TestAgentFactionNameAlwaysValid pins the property the route depends on: the
// generator can never produce a name the faction rules reject, so a development
// sign-in cannot fail on a random draw.
func TestAgentFactionNameAlwaysValid(t *testing.T) {
	for range 500 {
		name := agentFactionName()
		normalized, err := game.NormalizeFactionName(name)
		if err != nil {
			t.Fatalf("generated %q, which the faction rules reject: %v", name, err)
		}
		if normalized != name {
			t.Fatalf("generated %q, which normalizes to %q", name, normalized)
		}
	}
}

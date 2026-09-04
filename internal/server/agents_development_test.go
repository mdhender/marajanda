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
)

func TestAgentSignInCreatesNormalSession(t *testing.T) {
	var gotEmail string
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		gotEmail = email
		return datastore.Account{Handle: "reviewer", Role: "player"}, nil
	}, "development")
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
	}, "development")
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
	}, "development")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/not-an-email")
	if response.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("response status = %d, account calls = %d; want %d, 0", response.Code, calls, http.StatusBadRequest)
	}
}

func TestAgentSignInNotRegisteredInProductionEnvironment(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		t.Fatal("production route called account lookup")
		return datastore.Account{}, nil
	}, "production")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

// Copyright (c) 2026 Michael D Henderson.

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

func TestLandingAndSignInForm(t *testing.T) {
	handler := newHandler(nil)

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
		"v0.1.7-beta",
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
	})

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
	})

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
		wantDashboard string
	}{
		{role: "admin", handle: "keeper", wantPath: "/admin/dashboard", otherPath: "/player/dashboard", wantDashboard: "The realm awaits its keeper."},
		{role: "player", handle: "wanderer", wantPath: "/player/dashboard", otherPath: "/admin/dashboard", wantDashboard: "Your faction awaits its first command."},
	} {
		t.Run(test.role, func(t *testing.T) {
			handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
				return datastore.Account{Handle: test.handle, Role: test.role}, true, nil
			})
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
			for _, want := range []string{
				"Welcome, " + test.handle + ".",
				test.wantDashboard,
				`action="/sign-out" method="post"`,
				`type="submit">Sign out</button>`,
			} {
				if !strings.Contains(dashboard.Body.String(), want) {
					t.Fatalf("dashboard body missing %q", want)
				}
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
	})
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
	response := serveRequest(newHandler(nil), http.MethodGet, "/admin/dashboard")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sign-in" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/sign-in")
	}
}

func TestCrossOriginSignInIsRejected(t *testing.T) {
	values := url.Values{"account": {"admin@example.com"}, "passphrase": {"good.luck"}}
	request := httptest.NewRequest(http.MethodPost, "https://marajanda.test/sign-in", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://hostile.example")
	response := httptest.NewRecorder()

	newHandler(nil).ServeHTTP(response, request)

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

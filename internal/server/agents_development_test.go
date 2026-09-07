// Copyright (c) 2026 Michael D Henderson.

//go:build !production

package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func TestAgentSignInCreatesNormalSession(t *testing.T) {
	var gotEmail string
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		gotEmail = email
		return datastore.Account{Email: email, Handle: "reviewer", Role: "player"}, nil
	}, &testStore{faction: datastore.Faction{Name: "Reviewers", Race: game.RaceHuman, Active: true}, found: true}, "development", nil)
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
	}, nil, "development", nil)
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
	}, nil, "development", nil)
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/not-an-email")
	if response.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("response status = %d, account calls = %d; want %d, 0", response.Code, calls, http.StatusBadRequest)
	}
}

func TestAgentSignInNotRegisteredInProductionEnvironment(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		t.Fatal("production route called account lookup")
		return datastore.Account{}, nil
	}, nil, "production", nil)
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestAgentSignInGeneratesFactionWhenMissing(t *testing.T) {
	store := &testStore{}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "agent", Role: "player"}, nil
	}, store, "development", nil)

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
	store := &testStore{faction: datastore.Faction{Name: "Star Kin", Race: game.RaceHuman, Active: true}, found: true}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "agent", Role: "player"}, nil
	}, store, "development", nil)

	serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test?returnTo=%2Fplayer%2Fdashboard")

	if store.email != "" || store.faction.Name != "Star Kin" {
		t.Fatalf("faction = %q saved for %q, want Star Kin untouched", store.faction.Name, store.email)
	}
}

func TestAgentSignInLeavesAdminsWithoutAFaction(t *testing.T) {
	store := &testStore{}
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "keeper", Role: "admin"}, nil
	}, store, "development", nil)

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

// The development route honours the account flag, or it is a way around it.
func TestAgentSignInRefusesADeactivatedAccount(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		return datastore.Account{}, fmt.Errorf("%w: %s", datastore.ErrAccountInactive, "agent@example.test")
	}, &testStore{}, "development", nil)

	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("a refused development sign-in started a session")
	}
}

// The shutdown route ends the server it is served by, and it is the admin
// session that opens it. Nothing supervises the development daemon, so this is
// how an agent that started one stops it.
func TestAgentShutDownStopsTheServer(t *testing.T) {
	stopped := 0
	handler, cookie := agentSession(t, "keeper@example.test", "admin", func() { stopped++ })

	response := requestWithCookie(handler, http.MethodPost, "/__agents/shut-it-down", cookie, "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if stopped != 1 {
		t.Fatalf("shutdown called %d times, want 1", stopped)
	}
	if body := response.Body.String(); !strings.Contains(body, "shutting down") {
		t.Fatalf("body = %q, want it to say the server is shutting down", body)
	}
}

// The response is written before the shutdown is asked for, so a client reads
// an answer rather than a reset connection. Shutdown waits for an active
// request to return, and this is the ordering that relies on it.
func TestAgentShutDownAnswersBeforeItStops(t *testing.T) {
	var bodyWhenStopped string
	var response *httptest.ResponseRecorder
	handler, cookie := agentSession(t, "keeper@example.test", "admin", func() {
		bodyWhenStopped = response.Body.String()
	})

	request := httptest.NewRequest(http.MethodPost, "/__agents/shut-it-down", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !strings.Contains(bodyWhenStopped, "shutting down") {
		t.Fatalf("body at shutdown = %q, want the whole response already written", bodyWhenStopped)
	}
}

// A player may not stop the server. The route is role-gated like every other
// admin route, so a player is sent to their own dashboard.
func TestAgentShutDownRefusesAPlayer(t *testing.T) {
	stopped := 0
	handler, cookie := agentSession(t, "agent@example.test", "player", func() { stopped++ })

	response := requestWithCookie(handler, http.MethodPost, "/__agents/shut-it-down", cookie, "")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("response = %d %q, want a redirect to the player dashboard", response.Code, response.Header().Get("Location"))
	}
	if stopped != 0 {
		t.Fatal("a player stopped the server")
	}
}

// An anonymous request is sent to sign in, the same as every other role-gated
// route. A route that stops the server is not the place to make an exception.
func TestAgentShutDownRefusesAnonymousRequests(t *testing.T) {
	stopped := 0
	handler := newConfiguredHandler(nil, nil, &testStore{}, "development", func() { stopped++ })

	response := serveRequest(handler, http.MethodPost, "/__agents/shut-it-down")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sign-in" {
		t.Fatalf("response = %d %q, want a redirect to sign-in", response.Code, response.Header().Get("Location"))
	}
	if stopped != 0 {
		t.Fatal("an anonymous request stopped the server")
	}
}

// The route is a development one, so ENV=production omits it in a
// non-production build exactly as it omits the sign-in route.
//
// An absent POST route answers 405 rather than 404. "GET /" is a catch-all
// that matches every path, so the mux finds the path and refuses the method.
// What matters is that it is not 202: nothing was accepted and nothing stopped.
func TestProductionEnvironmentOmitsAgentShutDown(t *testing.T) {
	handler := newConfiguredHandler(nil, nil, &testStore{}, "production", func() {
		t.Fatal("a production environment stopped the server")
	})
	response := serveRequest(handler, http.MethodPost, "/__agents/shut-it-down")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

// A handler built without a way to stop a server does not carry a route that
// would have to reach for one, so nothing can reach a nil shutdown.
func TestAgentShutDownIsAbsentWithoutAServerToStop(t *testing.T) {
	handler := newConfiguredHandler(nil, nil, &testStore{}, "development", nil)
	response := serveRequest(handler, http.MethodPost, "/__agents/shut-it-down")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

// agentSession signs an agent in through the development route and returns the
// handler and the session cookie it produced.
func agentSession(t *testing.T, email, role string, shutdown func()) (http.Handler, *http.Cookie) {
	t.Helper()
	handler := newConfiguredHandler(nil, func(_ context.Context, email string) (datastore.Account, error) {
		return datastore.Account{Email: email, Handle: "keeper", Role: role}, nil
	}, &testStore{faction: datastore.Faction{Name: "Keepers", Race: game.RaceHuman, Active: true}, found: true}, "development", shutdown)

	signIn := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/"+email)
	cookies := signIn.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("the development sign-in set no session cookie")
	}
	return handler, cookies[0]
}

// The whole thing, over a real connection: a running server, an admin session
// from the development sign-in, and a POST that stops it.
//
// This is what the unit tests cannot show. It proves the client reads a whole
// response rather than a reset connection, that Run returns nil so the process
// exits 0, and that http.CrossOriginProtection lets the POST through - a
// request from a client that sends no Sec-Fetch-Site is not cross-origin.
func TestAgentShutDownStopsARunningServer(t *testing.T) {
	port := freePort(t)
	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), Config{
			Root:    ":memory:",
			Game:    new(datastore.Game{Seed1: 98374, Seed2: -98}),
			Address: DefaultAddress,
			Port:    port,
			// A backstop, not the thing under test. Without it a route that
			// failed to stop the server would hang the suite instead of
			// failing it.
			Timeout: 30 * time.Second,
		})
	}()

	base := fmt.Sprintf("http://%s:%d", DefaultAddress, port)
	client := &http.Client{Timeout: 5 * time.Second}
	waitForServer(t, client, base)

	// The seeded admin, because the development route creates players and this
	// route wants an admin. Signing in as one that already exists is the same
	// route doing the same thing.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	signIn, err := client.Get(base + "/__agents/log-me-in/admin@marajanda.com?returnTo=%2Fadmin%2Fdashboard")
	if err != nil {
		t.Fatal(err)
	}
	signIn.Body.Close()

	response, err := client.Post(base+"/__agents/shut-it-down", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d %q, want %d", response.StatusCode, body, http.StatusAccepted)
	}
	if !strings.Contains(string(body), "shutting down") {
		t.Fatalf("body = %q, want it to say the server is shutting down", body)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop")
	}
}

// freePort asks the kernel for a port and gives it straight back. There is a
// window between the two in which something else could take it, which is why
// nothing else in the suite binds a fixed port.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(DefaultAddress, "0"))
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForServer(t *testing.T, client *http.Client, base string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(base + "/api/healthz")
		if err == nil {
			response.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the server never answered")
}

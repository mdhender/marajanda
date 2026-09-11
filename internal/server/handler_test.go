// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda"
	"github.com/mdhender/marajanda/internal/cylinder"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
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
		"v" + marajanda.Version().Short(),
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
		{role: "player", handle: "wanderer", wantPath: "/player/dashboard", otherPath: "/admin/dashboard", wantDashboard: []string{"The Wayfarers", "LEADER-1", "Mudville", "(2, -1)"}},
	} {
		t.Run(test.role, func(t *testing.T) {
			var store applicationStore
			if test.role == "player" {
				store = &testStore{
					faction: datastore.Faction{Name: "The Wayfarers", Race: game.RaceHuman, Active: true},
					found:   true,
					turn:    3,
					entities: []datastore.Entity{
						{ID: 1, Code: "LEADER-1", Name: "LEADER-1", Kind: game.EntityKindLeader, Location: hexg.NewHex(2, -1)},
						{ID: 2, Code: "HAMLET-1", Name: "Mudville", Kind: game.EntityKindHamlet, Location: hexg.NewHex(2, -1)},
					},
				}
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
	store := &testStore{}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player"}, true, nil
	}, store)
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

func TestSessionStorageFailureDoesNotAuthenticate(t *testing.T) {
	store := &testStore{}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player"}, true, nil
	}, store)
	signIn := submitSignIn(handler, "player@example.com", "good.luck")
	cookie := signIn.Result().Cookies()[0]

	store.sessionErr = errors.New("session storage unavailable")
	response := requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("dashboard status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Header().Get("Location") != "" {
		t.Fatalf("storage failure redirected to %q instead of refusing the request", response.Header().Get("Location"))
	}
}

func TestBrowserSessionSurvivesPersistentStoreReopen(t *testing.T) {
	root := t.TempDir()
	gameRecord := datastore.Game{
		Seed1: 98374, Seed2: -98,
		Width: datastore.MinimumWorldWidth, Height: datastore.MinimumWorldHeight,
	}
	store, err := datastore.Open(t.Context(), root, datastore.SeedAccount{
		Email: "admin@example.com", Secret: "temporary", Handle: "keeper",
	}, &gameRecord)
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(store.Authenticate, store)
	signIn := submitSignIn(handler, "admin@example.com", "temporary")
	result := signIn.Result()
	if signIn.Code != http.StatusSeeOther || len(result.Cookies()) != 1 {
		store.Close()
		t.Fatalf("sign-in = %d with %d cookies", signIn.Code, len(result.Cookies()))
	}
	cookie := result.Cookies()[0]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = datastore.Open(t.Context(), root, datastore.SeedAccount{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler = newHandler(store.Authenticate, store)
	dashboard := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", cookie, "")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "Welcome, keeper.") {
		t.Fatalf("dashboard after reopen = %d %q, want authenticated keeper", dashboard.Code, dashboard.Body.String())
	}
}

func TestDashboardRequiresSession(t *testing.T) {
	response := serveRequest(newHandler(nil, nil), http.MethodGet, "/admin/dashboard")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sign-in" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Header().Get("Location"), http.StatusSeeOther, "/sign-in")
	}
}

func TestPlayerConfiguresFactionBeforeDashboard(t *testing.T) {
	factions := &testStore{seat: hexg.NewHex(2, -1)}
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

	configured := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie,
		url.Values{"name": {"  Star   Kin  "}, "race": {"elf"}}.Encode())
	if configured.Code != http.StatusSeeOther || configured.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("configuration response = %d %q, want %d %q", configured.Code, configured.Header().Get("Location"), http.StatusSeeOther, "/player/dashboard")
	}
	if factions.email != "player@example.com" || factions.faction.Name != "Star Kin" || factions.faction.Race != game.RaceElf {
		t.Fatalf("saved faction = %q %#v, want normalized Star Kin elves", factions.email, factions.faction)
	}

	dashboard = requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	// The dashboard reports where the faction's entities are. The faction has
	// no location of its own to report.
	for _, want := range []string{"Star Kin", "elf", "Your force", "LEADER-1", "HAMLET-1", "(2, -1)"} {
		if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), want) {
			t.Fatalf("dashboard = %d %q, want %q", dashboard.Code, dashboard.Body.String(), want)
		}
	}
	form = requestWithCookie(handler, http.MethodGet, "/player/faction", cookie, "")
	if form.Code != http.StatusSeeOther || form.Header().Get("Location") != "/player/dashboard" {
		t.Fatalf("completed configuration response = %d %q, want dashboard redirect", form.Code, form.Header().Get("Location"))
	}
}

// A list of entities is only true of one turn, so the dashboard names the turn
// it read and reads the entities as of that same turn rather than asking each
// question for the latest answer.
func TestPlayerDashboardReadsEntitiesAsOfTheTurnItShows(t *testing.T) {
	store := &testStore{
		faction: datastore.Faction{Name: "Star Kin", Race: game.RaceElf, Active: true},
		found:   true,
		turn:    7,
		entities: []datastore.Entity{
			{ID: 1, Code: "HAMLET-1", Name: "Smirnopolis", Kind: game.EntityKindHamlet, Location: hexg.NewHex(-3, 4)},
		},
	}
	handler, cookie := signedInPlayer(t, store)

	dashboard := requestWithCookie(handler, http.MethodGet, "/player/dashboard", cookie, "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard = %d", dashboard.Code)
	}
	if store.asOf != 7 {
		t.Fatalf("entities read as of turn %d, want the displayed turn 7", store.asOf)
	}
	for _, want := range []string{"HAMLET-1", "Smirnopolis", "hamlet", "(-3, 4)", "<strong>7</strong>"} {
		if !strings.Contains(dashboard.Body.String(), want) {
			t.Fatalf("dashboard body missing %q", want)
		}
	}
}

// The form offers exactly the six peoples, with human selected until the player
// chooses otherwise.
func TestFactionFormOffersEveryRace(t *testing.T) {
	handler, cookie := signedInPlayer(t, &testStore{})

	form := requestWithCookie(handler, http.MethodGet, "/player/faction", cookie, "")
	if form.Code != http.StatusOK {
		t.Fatalf("faction form = %d", form.Code)
	}
	body := form.Body.String()
	for _, race := range game.Races() {
		if !strings.Contains(body, `<option value="`+string(race)+`"`) {
			t.Errorf("faction form does not offer %q", race)
		}
	}
	if !strings.Contains(body, `<option value="human" selected>`) {
		t.Errorf("faction form does not default to human: %q", body)
	}
}

// An omitted race is the default. A race the game does not know is a rejection:
// accepting it would seat the player as something they never chose.
func TestConfigureFactionValidatesRace(t *testing.T) {
	for _, test := range []struct {
		name   string
		race   string
		status int
		want   game.Race
	}{
		{name: "omitted", race: "", status: http.StatusSeeOther, want: game.RaceHuman},
		{name: "chosen", race: "kobold", status: http.StatusSeeOther, want: game.RaceKobold},
		{name: "normalized", race: "  DWARF ", status: http.StatusSeeOther, want: game.RaceDwarf},
		{name: "unregistered", race: "wyrm", status: http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testStore{}
			handler, cookie := signedInPlayer(t, store)

			values := url.Values{"name": {"Star Kin"}}
			if test.race != "" {
				values.Set("race", test.race)
			}
			response := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie, values.Encode())
			if response.Code != test.status {
				t.Fatalf("response = %d, want %d", response.Code, test.status)
			}
			if test.want == "" {
				if store.found {
					t.Fatalf("an unregistered race saved %#v", store.faction)
				}
				return
			}
			if store.faction.Race != test.want {
				t.Fatalf("saved race = %q, want %q", store.faction.Race, test.want)
			}
		})
	}
}

// A world with nowhere left to settle refuses the faction rather than reporting
// an internal failure, and says so on the form the player is still looking at.
func TestConfigureFactionReportsAFullWorld(t *testing.T) {
	store := &testStore{saveErr: fmt.Errorf("place account: %w", game.ErrNoOrigin)}
	handler, cookie := signedInPlayer(t, store)

	response := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie,
		url.Values{"name": {"Star Kin"}, "race": {"orc"}}.Encode())
	if response.Code != http.StatusConflict {
		t.Fatalf("response = %d, want %d", response.Code, http.StatusConflict)
	}
	if !strings.Contains(response.Body.String(), "nowhere left to settle") {
		t.Fatalf("body = %q, want the placement failure explained", response.Body.String())
	}
	if store.found {
		t.Fatalf("a failed placement saved %#v", store.faction)
	}
}

// Configuring a faction gives a player an origin. Session resolution reads the
// current account row, so the next request observes that seat without an
// in-process snapshot rewrite.
func TestConfigureFactionSeatsTheSession(t *testing.T) {
	store := &testStore{seat: hexg.NewHex(7, -16), world: testMapWorld(), game: testMapGame()}
	handler, cookie := signedInPlayer(t, store)

	response := requestWithCookie(handler, http.MethodPost, "/player/faction", cookie,
		url.Values{"name": {"Star Kin"}, "race": {"human"}}.Encode())
	if response.Code != http.StatusSeeOther {
		t.Fatalf("response = %d, want %d", response.Code, http.StatusSeeOther)
	}

	store.visible = []hexg.Hex{store.seat}
	mapPage := requestWithCookie(handler, http.MethodGet, "/player/map", cookie, "")
	if mapPage.Code != http.StatusOK {
		t.Fatalf("map = %d, want %d", mapPage.Code, http.StatusOK)
	}
	if !strings.Contains(mapPage.Body.String(), formatCoord(store.seat)) {
		t.Fatalf("map does not draw the seat %v the faction form assigned", store.seat)
	}
}

// signedInPlayer returns a handler and the cookie of a player with no faction,
// which is where every faction-configuration path starts.
func signedInPlayer(t *testing.T, store *testStore) (http.Handler, *http.Cookie) {
	t.Helper()
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player"}, true, nil
	}, store)
	signIn := submitSignIn(handler, "player@example.com", "good.luck")
	cookies := signIn.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("sign-in set no session cookie")
	}
	return handler, cookies[0]
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
	world   game.World
	visible []hexg.Hex
	// knownAsOf overrides visible for one turn, so a test can give the past a
	// different shape from the present and see which one a read returns.
	knownAsOf map[int][]hexg.Hex
	turn      int
	entities  []datastore.Entity
	// asOf is the turn EntitiesAsOf was last asked for, so a test can check
	// that the dashboard reads the entities as of the turn it displays.
	asOf int
	// seat is the origin SaveFaction hands back, and saveErr is what it fails
	// with instead of seating anybody.
	seat    hexg.Hex
	saveErr error
	// orders are the orders the faction's entities hold on turn, keyed by
	// entity. The fake keeps them so a handler can be watched writing and then
	// rendering what it wrote; the rules that decide what is stored are tested
	// against the real store.
	orders map[int64][]datastore.Order
	// orderErr is what the next order write fails with instead of doing
	// anything, and wroteTurn is the turn the last one carried.
	orderErr  error
	wroteTurn int
	advanced  int
	// savedUpdates is what the last whole-page save asked for, which is where
	// a test reads what the handler made of the form on the way in.
	savedUpdates []datastore.OrderUpdate
	// insertedAt is the sequence the last insert asked for, so a test can see
	// that "insert after order 1" asked for position 2.
	insertedAt int
	// sessionAccount is the account row ResolveSession reads. Tests set it when
	// their authentication fake returns an account, and SaveFaction updates it
	// as the real datastore updates the row.
	sessionAccount datastore.Account
	sessions       map[string]bool
	sessionErr     error
	gameErr        error
	factionErr     error
	turnErr        error
	entitiesErr    error
	ordersErr      error
	estimateErr    error
	worldErr       error
	visibleErr     error
}

func (s *testStore) CreateSession(_ context.Context, token []byte, account datastore.Account) error {
	if s.sessionErr != nil {
		return s.sessionErr
	}
	s.sessionAccount = account
	if s.sessions == nil {
		s.sessions = make(map[string]bool)
	}
	s.sessions[string(token)] = true
	return nil
}

func (s *testStore) ResolveSession(_ context.Context, token []byte) (datastore.Account, bool, error) {
	if s.sessionErr != nil {
		return datastore.Account{}, false, s.sessionErr
	}
	return s.sessionAccount, s.sessions[string(token)], nil
}

func (s *testStore) DeleteSession(_ context.Context, token []byte) error {
	if s.sessionErr != nil {
		return s.sessionErr
	}
	delete(s.sessions, string(token))
	return nil
}

func (s *testStore) OrdersAsOf(_ context.Context, _ string, turn int) (map[int64][]datastore.Order, error) {
	s.asOf = turn
	if s.ordersErr != nil {
		return nil, s.ordersErr
	}
	return s.orders, nil
}

func (s *testStore) AddOrder(_ context.Context, _ string, turn int, entityID int64, kind game.OrderKind, detail game.OrderDetail, opts ...datastore.OrderWriteOption) (int, error) {
	s.wroteTurn = turn
	if err := s.expectation(turn, opts); err != nil {
		return 0, err
	}
	if s.orderErr != nil {
		return 0, s.orderErr
	}
	if s.orders == nil {
		s.orders = make(map[int64][]datastore.Order)
	}
	seq := len(s.orders[entityID]) + 1
	s.orders[entityID] = append(s.orders[entityID], datastore.Order{Seq: seq, Kind: kind, Detail: detail})
	return seq, nil
}

func (s *testStore) InsertOrder(_ context.Context, _ string, turn int, entityID int64, seq int, kind game.OrderKind, detail game.OrderDetail, opts ...datastore.OrderWriteOption) error {
	s.wroteTurn = turn
	s.insertedAt = seq
	if err := s.expectation(turn, opts); err != nil {
		return err
	}
	if s.orderErr != nil {
		return s.orderErr
	}
	if s.orders == nil {
		s.orders = make(map[int64][]datastore.Order)
	}
	orders := s.orders[entityID]
	if seq < 1 || seq > len(orders)+1 {
		return datastore.ErrUnknownOrder
	}
	inserted := make([]datastore.Order, 0, len(orders)+1)
	inserted = append(inserted, orders[:seq-1]...)
	inserted = append(inserted, datastore.Order{Kind: kind, Detail: detail})
	inserted = append(inserted, orders[seq-1:]...)
	for index := range inserted {
		inserted[index].Seq = index + 1
	}
	s.orders[entityID] = inserted
	return nil
}

func (s *testStore) SetOrderDetail(ctx context.Context, email string, turn int, entityID int64, seq int, detail game.OrderDetail, opts ...datastore.OrderWriteOption) error {
	return s.SetOrderDetails(ctx, email, turn, []datastore.OrderUpdate{
		{EntityID: entityID, Seq: seq, Detail: detail},
	}, opts...)
}

func (s *testStore) SetOrderDetails(_ context.Context, _ string, turn int, updates []datastore.OrderUpdate, opts ...datastore.OrderWriteOption) error {
	s.wroteTurn = turn
	if err := s.expectation(turn, opts); err != nil {
		return err
	}
	if s.orderErr != nil {
		return s.orderErr
	}
	s.savedUpdates = updates
	for _, wanted := range updates {
		found := false
		for index, order := range s.orders[wanted.EntityID] {
			if order.Seq != wanted.Seq {
				continue
			}
			// The order's kind decides which half of the detail is used, the
			// way the real store does it.
			if order.Kind == game.OrderKindRest {
				s.orders[wanted.EntityID][index].Detail.Count = wanted.Detail.Count
			} else {
				s.orders[wanted.EntityID][index].Detail.Direction = wanted.Detail.Direction
			}
			found = true
		}
		if !found {
			return datastore.ErrUnknownOrder
		}
	}
	return nil
}

// EstimateOrders prices the fake's orders the way the real store does, through
// the one cost function in internal/game. The fake keeps no knowledge and no
// world, so every step is onto ground the faction does not know.
func (s *testStore) EstimateOrders(_ context.Context, _ string, _ int) (map[int64]game.Estimate, error) {
	if s.estimateErr != nil {
		return nil, s.estimateErr
	}
	world, err := cylinder.New(2*testMapWidth + 1)
	if err != nil {
		return nil, err
	}
	estimates := make(map[int64]game.Estimate, len(s.entities))
	for _, entity := range s.entities {
		estimates[entity.ID] = game.Price(game.Plan{
			Kind:      entity.Kind,
			World:     world,
			Start:     entity.Location,
			Allowance: entity.Allowance,
			Orders:    s.orders[entity.ID],
		})
	}
	return estimates, nil
}

func (s *testStore) RemoveOrder(_ context.Context, _ string, turn int, entityID int64, seq int, opts ...datastore.OrderWriteOption) error {
	s.wroteTurn = turn
	if err := s.expectation(turn, opts); err != nil {
		return err
	}
	if s.orderErr != nil {
		return s.orderErr
	}
	kept := make([]datastore.Order, 0, len(s.orders[entityID]))
	for _, order := range s.orders[entityID] {
		if order.Seq == seq {
			continue
		}
		order.Seq = len(kept) + 1
		kept = append(kept, order)
	}
	s.orders[entityID] = kept
	return nil
}

// expectation enforces what ExpectOrders asked for, so a handler that forgot to
// pass the header through is caught by a test rather than by a player. It tags
// the fake's own order map with the real function, which is what makes the tag
// a handler sends back comparable to the one it was given.
func (s *testStore) expectation(turn int, opts []datastore.OrderWriteOption) error {
	expect, ok := datastore.OrderExpectation(opts)
	if !ok {
		return nil
	}
	if expect != datastore.OrdersTag(s.sessionAccount.Email, turn, s.orders) {
		return datastore.ErrOrdersChanged
	}
	return nil
}

func (s *testStore) ReplaceOrders(_ context.Context, _ string, turn int, entityID int64, orders []datastore.Order, opts ...datastore.OrderWriteOption) error {
	s.wroteTurn = turn
	if err := s.expectation(turn, opts); err != nil {
		return err
	}
	if s.orderErr != nil {
		return s.orderErr
	}
	if s.orders == nil {
		s.orders = make(map[int64][]datastore.Order)
	}
	replacement := make([]datastore.Order, 0, len(orders))
	for index, order := range orders {
		order.Seq = index + 1
		replacement = append(replacement, order)
	}
	s.orders[entityID] = replacement
	return nil
}

func (s *testStore) AdvanceTurn(context.Context) (int, error) {
	if s.turnErr != nil {
		return 0, s.turnErr
	}
	s.turn++
	s.advanced++
	return s.turn, nil
}

func (s *testStore) Game(context.Context) (datastore.Game, error) {
	if s.gameErr != nil {
		return datastore.Game{}, s.gameErr
	}
	return s.game, nil
}

func (s *testStore) World(context.Context) (game.World, error) {
	if s.worldErr != nil {
		return game.World{}, s.worldErr
	}
	return s.world, nil
}

func (s *testStore) Faction(context.Context, string) (datastore.Faction, bool, error) {
	if s.factionErr != nil {
		return datastore.Faction{}, false, s.factionErr
	}
	return s.faction, s.found, nil
}

func (s *testStore) CurrentTurn(context.Context) (int, error) {
	if s.turnErr != nil {
		return 0, s.turnErr
	}
	return s.turn, nil
}

func (s *testStore) EntitiesAsOf(_ context.Context, _ string, turn int) ([]datastore.Entity, error) {
	s.asOf = turn
	if s.entitiesErr != nil {
		return nil, s.entitiesErr
	}
	return s.entities, nil
}

func (s *testStore) VisibleHexes(context.Context, string) ([]hexg.Hex, error) {
	if s.visibleErr != nil {
		return nil, s.visibleErr
	}
	return s.visible, nil
}

// KnowledgeAsOf answers from the same visible hexes VisibleHexes does, and
// fails with the same error, because the real store's VisibleHexes is this read
// on the current turn. knownAsOf, when a test sets it, answers a particular turn
// instead, which is how a map read is watched asking for the past.
func (s *testStore) KnowledgeAsOf(_ context.Context, _ string, turn int) (game.KnowledgeSet, error) {
	s.asOf = turn
	if s.visibleErr != nil {
		return nil, s.visibleErr
	}
	hexes, ok := s.knownAsOf[turn]
	if !ok {
		hexes = s.visible
	}
	known := make(game.KnowledgeSet, len(hexes))
	for _, hex := range hexes {
		known[hex] = game.KnowledgeObserved
	}
	return known, nil
}

func (s *testStore) SaveFaction(_ context.Context, email, name string, race game.Race) (datastore.Account, error) {
	if s.saveErr != nil {
		return datastore.Account{}, s.saveErr
	}
	s.email = email
	s.faction = datastore.Faction{Name: name, Race: race, Active: true}
	s.found = true
	// Saving a faction founds it, the way the store does: a leader and a hamlet
	// standing on the seat the save handed back.
	s.turn = game.FirstTurn
	s.entities = []datastore.Entity{
		{ID: 1, Code: "LEADER-1", Name: "LEADER-1", Kind: game.EntityKindLeader, Location: s.seat},
		{ID: 2, Code: "HAMLET-1", Name: "HAMLET-1", Kind: game.EntityKindHamlet, Location: s.seat},
	}
	seated := s.sessionAccount
	seated.Email = email
	seated.Role = "player"
	seated.Origin = s.seat
	seated.Seated = true
	s.sessionAccount = seated
	return seated, nil
}

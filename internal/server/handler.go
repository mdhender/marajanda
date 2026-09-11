// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

const sessionCookieName = "marajanda_session"

type authenticateFunc func(context.Context, string, string) (datastore.Account, bool, error)
type findOrCreateFunc func(context.Context, string) (datastore.Account, error)

type applicationStore interface {
	CreateSession(context.Context, []byte, datastore.Account) error
	ResolveSession(context.Context, []byte) (datastore.Account, bool, error)
	DeleteSession(context.Context, []byte) error
	Game(context.Context) (datastore.Game, error)
	World(context.Context) (game.World, error)
	CurrentTurn(context.Context) (int, error)
	Faction(context.Context, string) (datastore.Faction, bool, error)
	EntitiesAsOf(context.Context, string, int) ([]datastore.Entity, error)
	OrdersAsOf(context.Context, string, int) (map[int64][]datastore.Order, error)
	ResultsAsOf(context.Context, string, int) ([]datastore.TurnResult, error)
	EstimateOrders(context.Context, string, int) (map[int64]game.Estimate, error)
	AddOrder(context.Context, string, int, int64, game.OrderKind, game.OrderDetail, ...datastore.OrderWriteOption) (int, error)
	InsertOrder(context.Context, string, int, int64, int, game.OrderKind, game.OrderDetail, ...datastore.OrderWriteOption) error
	SetOrderDetail(context.Context, string, int, int64, int, game.OrderDetail, ...datastore.OrderWriteOption) error
	SetOrderDetails(context.Context, string, int, []datastore.OrderUpdate, ...datastore.OrderWriteOption) error
	RemoveOrder(context.Context, string, int, int64, int, ...datastore.OrderWriteOption) error
	ReplaceOrders(context.Context, string, int, int64, []datastore.Order, ...datastore.OrderWriteOption) error
	AdvanceTurn(context.Context) (int, error)
	SaveFaction(context.Context, string, string, game.Race) (datastore.Account, error)
	VisibleHexes(context.Context, string) ([]hexg.Hex, error)
	KnowledgeAsOf(context.Context, string, int) (game.KnowledgeSet, error)
}

type application struct {
	authenticate        authenticateFunc
	findOrCreateAccount findOrCreateFunc
	store               applicationStore
	// shutdown ends the server the handler is serving. It is nil unless the
	// caller supplied one, which is what keeps the development route that
	// calls it out of a handler that has no server to stop.
	shutdown func()
	// logger is where a failure the client is not told about goes. It is
	// never nil: serverLogger substitutes a discarding one, so a handler
	// built without a logger is silent rather than writing to the global
	// default. See logging.go.
	logger *slog.Logger
}

type pageData struct {
	Title   string
	View    string
	Message string
	Version string
	Account datastore.Account
	Faction datastore.Faction
	Game    datastore.Game
	// Turn is the turn Entities was read as of. The two travel together: a
	// list of entities is only true of the turn it was read on.
	Turn     int
	Entities []datastore.Entity
	// Orders is the orders page. It is read as of Turn as well, and only the
	// current turn's is writable.
	Orders ordersView
	// Results is the turn report. It carries its own turn, which is a
	// processed one and so is never the turn above it.
	Results resultsView
	Name    string
	Race    game.Race
	Races   []game.Race
	Map     mapView
	// Scripts are what the page loads, in the order they load: HTMX, then the
	// project's own. They are filled in by render rather than by every handler,
	// the way Version is.
	Scripts []string
}

func newHandler(authenticate authenticateFunc, store applicationStore) http.Handler {
	return newConfiguredHandler(authenticate, nil, store, "production", nil, nil)
}

func newConfiguredHandler(authenticate authenticateFunc, findOrCreate findOrCreateFunc, store applicationStore, environment string, shutdown func(), logger *slog.Logger) http.Handler {
	app := &application{
		authenticate:        authenticate,
		findOrCreateAccount: findOrCreate,
		store:               store,
		shutdown:            shutdown,
		logger:              serverLogger(logger),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/sessions", app.createAPISession)
	mux.HandleFunc("DELETE /api/v1/session", app.requireAPIAuthentication(app.deleteAPISession))
	mux.HandleFunc("GET /api/v1/account", app.requireAPIAuthentication(app.getAPIAccount))
	mux.HandleFunc("GET /api/v1/game", app.requireAPIAuthentication(app.getAPIGame))
	mux.HandleFunc("GET /api/v1/faction", app.requireAPIRole("player", app.getAPIFaction))
	mux.HandleFunc("PUT /api/v1/faction", app.requireAPIRole("player", app.putAPIFaction))
	mux.HandleFunc("GET /api/v1/entities", app.requireAPIRole("player", app.getAPIEntities))
	mux.HandleFunc("GET /api/v1/map", app.requireAPIAuthentication(app.getAPIMap))
	mux.HandleFunc("GET /api/v1/orders", app.requireAPIRole("player", app.getAPIOrders))
	mux.HandleFunc("GET /api/v1/results", app.requireAPIRole("player", app.getAPIResults))
	mux.HandleFunc("PUT /api/v1/orders", app.requireAPIRole("player", app.putAPIOrders))
	mux.HandleFunc("POST /api/v1/entities/{entity}/orders", app.requireAPIRole("player", app.postAPIOrder))
	mux.HandleFunc("PUT /api/v1/entities/{entity}/orders", app.requireAPIRole("player", app.putAPIEntityOrders))
	mux.HandleFunc("PATCH /api/v1/entities/{entity}/orders/{sequence}", app.requireAPIRole("player", app.patchAPIOrder))
	mux.HandleFunc("DELETE /api/v1/entities/{entity}/orders/{sequence}", app.requireAPIRole("player", app.deleteAPIOrder))
	mux.HandleFunc("POST /api/v1/turns/current/advance", app.requireAPIRole("admin", app.advanceAPITurn))
	mux.HandleFunc("GET /api/v1/turns/{turn}/entities", app.requireAPIRole("player", app.getAPITurnEntities))
	mux.HandleFunc("GET /api/v1/turns/{turn}/orders", app.requireAPIRole("player", app.getAPITurnOrders))
	mux.HandleFunc("GET /api/v1/turns/{turn}/map", app.requireAPIAuthentication(app.getAPITurnMap))
	mux.HandleFunc("GET /api/v1/turns/{turn}/results", app.requireAPIRole("player", app.getAPITurnResults))
	mux.HandleFunc("GET /assets/{name}", app.asset)
	mux.HandleFunc("GET /", app.landing)
	mux.HandleFunc("GET /sign-in", app.signInForm)
	mux.HandleFunc("POST /sign-in", app.signIn)
	mux.HandleFunc("POST /sign-out", app.signOut)
	mux.HandleFunc("GET /admin/dashboard", app.dashboard("admin"))
	mux.HandleFunc("POST /admin/turn", app.advanceTurn)
	mux.HandleFunc("GET /admin/map", app.adminMap)
	mux.HandleFunc("GET /admin/map.png", app.adminMapImage)
	mux.HandleFunc("GET /player/dashboard", app.dashboard("player"))
	mux.HandleFunc("GET /player/map", app.playerMap)
	mux.HandleFunc("GET /player/faction", app.factionForm)
	mux.HandleFunc("POST /player/faction", app.configureFaction)
	mux.HandleFunc("GET /player/results", app.results)
	mux.HandleFunc("GET /player/orders", app.orders)
	mux.HandleFunc("POST /player/orders", app.saveOrders)
	mux.HandleFunc("POST /player/orders/{entity}/{seq}", app.setOrderDetail)
	mux.HandleFunc("POST /player/orders/{entity}/{seq}/insert", app.insertOrder)
	mux.HandleFunc("DELETE /player/orders/{entity}/{seq}", app.removeOrder)
	registerAgentRoutes(mux, app, environment)

	protection := new(http.CrossOriginProtection)
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			writeAPIError(w, http.StatusForbidden, apiCodeForbidden, "Cross-origin requests are not allowed.")
			return
		}
		http.Error(w, "cross-origin request detected", http.StatusForbidden)
	}))
	return protection.Handler(mux)
}

func (app *application) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	account, ok, err := app.currentAccount(r)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load the session.")
		return
	}
	if ok {
		http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
		return
	}
	app.render(w, http.StatusOK, pageData{Title: "A world waiting to awaken", View: "landing"})
}

func (app *application) signInForm(w http.ResponseWriter, r *http.Request) {
	account, ok, err := app.currentAccount(r)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load the session.")
		return
	}
	if ok {
		http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
		return
	}
	app.render(w, http.StatusOK, pageData{Title: "Sign in", View: "sign-in"})
}

func (app *application) signIn(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.renderSignInFailure(w, http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("account")))
	if !isEmail(email) {
		app.renderSignInFailure(w, http.StatusUnauthorized)
		return
	}
	account, ok, err := app.authenticate(r.Context(), email, r.FormValue("passphrase"))
	if err != nil {
		// A deactivated account that presented the right passphrase is told so
		// plainly. It costs the enumeration guard nothing an attacker does not
		// already have: they had to know the passphrase to be told anything but
		// the one refusal below.
		if errors.Is(err, datastore.ErrAccountInactive) {
			app.render(w, http.StatusForbidden, pageData{
				Title:   "Sign in",
				View:    "sign-in",
				Message: "That account is not active. Ask the game's administrator to restore it.",
			})
			return
		}
		app.serverError(w, r, err, "Marajanda could not complete the sign-in request.")
		return
	}
	if !ok {
		app.renderSignInFailure(w, http.StatusUnauthorized)
		return
	}
	if err := app.startSession(r.Context(), w, account); err != nil {
		app.serverError(w, r, err, "Marajanda could not create the session.")
		return
	}
	http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
}

func (app *application) signOut(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if token, err := decodeSessionToken(cookie.Value); err == nil && app.store != nil {
			if err := app.store.DeleteSession(r.Context(), token); err != nil {
				app.serverError(w, r, err, "Marajanda could not end the session.")
				return
			}
		}
	}
	expireSessionCookie(w)
	http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
}

func (app *application) startSession(ctx context.Context, w http.ResponseWriter, account datastore.Account) error {
	token, err := app.createSession(ctx, account)
	if err != nil {
		return err
	}
	setSessionCookie(w, token)
	return nil
}

func (app *application) createSession(ctx context.Context, account datastore.Account) ([]byte, error) {
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	if app.store == nil {
		return nil, errStoreNotConfigured
	}
	if err := app.store.CreateSession(ctx, token, account); err != nil {
		return nil, err
	}
	return token, nil
}

func setSessionCookie(w http.ResponseWriter, token []byte) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(token),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func expireSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (app *application) dashboard(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok, err := app.currentAccount(r)
		if err != nil {
			app.serverError(w, r, err, "Marajanda could not load the session.")
			return
		}
		if !ok {
			http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
			return
		}
		if account.Role != role {
			http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
			return
		}
		var data pageData
		if role == "admin" {
			if app.store == nil {
				app.serverError(w, r, errStoreNotConfigured, "Marajanda could not load the game.")
				return
			}
			game, err := app.store.Game(r.Context())
			if err != nil {
				app.serverError(w, r, err, "Marajanda could not load the game.")
				return
			}
			// The admin's control moves the clock, so the dashboard has to
			// name the turn it is moving.
			turn, err := app.store.CurrentTurn(r.Context())
			if err != nil {
				app.serverError(w, r, err, "Marajanda could not load the game.")
				return
			}
			data.Game = game
			data.Turn = turn
		} else {
			if app.store == nil {
				app.serverError(w, r, errStoreNotConfigured, "Marajanda could not load your faction.")
				return
			}
			faction, found, err := app.store.Faction(r.Context(), account.Email)
			if err != nil {
				app.serverError(w, r, err, "Marajanda could not load your faction.")
				return
			}
			if !found || !faction.Configured() {
				http.Redirect(w, r, "/player/faction", http.StatusSeeOther)
				return
			}
			// The turn is read first and the entities as of it, rather than
			// each being asked for the latest. A dashboard that named one turn
			// and listed another turn's entities would be wrong in the way
			// these tables exist to prevent.
			turn, err := app.store.CurrentTurn(r.Context())
			if err != nil {
				app.serverError(w, r, err, "Marajanda could not load your faction.")
				return
			}
			entities, err := app.store.EntitiesAsOf(r.Context(), account.Email, turn)
			if err != nil {
				app.serverError(w, r, err, "Marajanda could not load your faction.")
				return
			}
			data.Faction = faction
			data.Turn = turn
			data.Entities = entities
		}
		title := "Player dashboard"
		if role == "admin" {
			title = "Admin dashboard"
		}
		data.Title = title
		data.View = role
		data.Account = account
		app.render(w, http.StatusOK, data)
	}
}

func (app *application) factionForm(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requirePlayer(w, r)
	if !ok {
		return
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load your faction.")
		return
	}
	if found && faction.Configured() {
		http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
		return
	}
	app.render(w, http.StatusOK, factionPage(account, "", game.DefaultRace, ""))
}

// factionPage builds the faction form, echoing back whatever the player last
// submitted so a rejected entry is not retyped from scratch.
func factionPage(account datastore.Account, name string, race game.Race, message string) pageData {
	if !race.Valid() {
		race = game.DefaultRace
	}
	return pageData{
		Title:   "Configure faction",
		View:    "faction",
		Account: account,
		Name:    name,
		Race:    race,
		Races:   game.Races(),
		Message: message,
	}
}

func (app *application) configureFaction(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requirePlayer(w, r)
	if !ok {
		return
	}
	faction, found, err := app.store.Faction(r.Context(), account.Email)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load your faction.")
		return
	}
	if found && faction.Configured() {
		http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.render(w, http.StatusBadRequest, factionPage(account, "", game.DefaultRace, "Marajanda could not read that faction."))
		return
	}
	// An omitted race is the default rather than an error; a race the game does
	// not know is a rejection, because accepting it would silently seat the
	// player as something they did not choose.
	race := game.Race(strings.ToLower(strings.TrimSpace(r.FormValue("race"))))
	if race == "" {
		race = game.DefaultRace
	}
	name, err := game.NormalizeFactionName(r.FormValue("name"))
	if err != nil {
		app.render(w, http.StatusUnprocessableEntity, factionPage(account, r.FormValue("name"), race, err.Error()))
		return
	}
	if !race.Valid() {
		app.render(w, http.StatusUnprocessableEntity, factionPage(account, name, game.DefaultRace, "Choose one of the peoples of Marajanda."))
		return
	}
	// Saving the faction seats the account. The next session resolution reads
	// that updated account row, so there is no in-process snapshot to rewrite.
	_, err = app.store.SaveFaction(r.Context(), account.Email, name, race)
	if err != nil {
		if errors.Is(err, game.ErrNoOrigin) {
			app.render(w, http.StatusConflict, factionPage(account, name, race,
				"Marajanda has nowhere left to settle a faction of that people. Try another."))
			return
		}
		app.serverError(w, r, err, "Marajanda could not save your faction.")
		return
	}
	http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
}

func (app *application) requirePlayer(w http.ResponseWriter, r *http.Request) (datastore.Account, bool) {
	return app.requireRole(w, r, "player")
}

// requireRole resolves the session account and confirms it holds role. It
// redirects to sign-in without a session and to the account's own dashboard
// when the role does not match, so no page answers for a role it does not
// belong to.
func (app *application) requireRole(w http.ResponseWriter, r *http.Request, role string) (datastore.Account, bool) {
	account, ok, err := app.currentAccount(r)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not load the session.")
		return datastore.Account{}, false
	}
	if !ok {
		http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
		return datastore.Account{}, false
	}
	if account.Role != role {
		http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
		return datastore.Account{}, false
	}
	if app.store == nil {
		app.serverError(w, r, errStoreNotConfigured, "Marajanda could not load the game.")
		return datastore.Account{}, false
	}
	return account, true
}

func (app *application) currentAccount(r *http.Request) (datastore.Account, bool, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || app.store == nil {
		return datastore.Account{}, false, nil
	}
	token, err := decodeSessionToken(cookie.Value)
	if err != nil {
		return datastore.Account{}, false, nil
	}
	return app.store.ResolveSession(r.Context(), token)
}

func (app *application) renderSignInFailure(w http.ResponseWriter, status int) {
	app.render(w, status, pageData{
		Title:   "Sign in",
		View:    "sign-in",
		Message: "Those credentials were not accepted. Check your account and passphrase, then try again.",
	})
}

func (app *application) render(w http.ResponseWriter, status int, data pageData) {
	app.prepare(&data, w)
	w.WriteHeader(status)
	_ = pageTemplate.Execute(w, data)
}

// renderFragment writes one named block of the page template instead of the
// page, for a request HTMX made.
//
// It sets the same headers as a page, because a fragment is HTML served from
// the same origin and nothing about it wants a weaker policy.
func (app *application) renderFragment(w http.ResponseWriter, status int, name string, data pageData) {
	app.prepare(&data, w)
	w.WriteHeader(status)
	_ = pageTemplate.ExecuteTemplate(w, name, data)
}

// prepare fills in the values every rendered page shares and sets the response
// headers.
//
// script-src is named even though "default-src 'self'" already covers it: the
// page loads scripts, and the policy should say so where a reader looks for it
// rather than leave it to be inferred. Both of them need nothing beyond 'self'.
// HTMX fetches over XHR to this origin, and the project uses none of the
// attributes (hx-on, js: expressions, event filters) that would ask for
// 'unsafe-eval' - which is also why the project's own script is a served file
// and not an inline handler.
func (app *application) prepare(data *pageData, w http.ResponseWriter) {
	data.Version = marajanda.Version().Short()
	data.Scripts = pageScripts()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// wantsFragment reports whether HTMX asked for part of a page rather than a
// page.
//
// A history restore is the exception. HTMX sends it with HX-Request set, but
// what it does with the answer is replace the whole body from it, so it has to
// be given the whole page.
func wantsFragment(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-History-Restore-Request") != "true"
}

func isEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func newSessionToken() ([]byte, error) {
	bytes := make([]byte, datastore.SessionTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

func decodeSessionToken(value string) ([]byte, error) {
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) != datastore.SessionTokenBytes {
		return nil, errors.New("invalid session token")
	}
	return token, nil
}

func dashboardPath(account datastore.Account) string {
	if account.Role == "admin" {
		return "/admin/dashboard"
	}
	return "/player/dashboard"
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · Marajanda</title>
  {{range .Scripts}}<script src="{{.}}" defer></script>
  {{end}}
  <style>
    :root { color-scheme: dark; --ink: #f7f1dc; --muted: #bfb89f; --gold: #e5bd68; --ember: #c66a43; --night: #0d171c; --panel: #14252a; --line: rgba(229,189,104,.24); --grassland: #7f9c5a; --forest: #3f6b46; --hills: #a98a4e; --marsh: #5b7d78; --mountains: #8a8378; --ocean: #1d4a63; --lake: #2f7d95; --ice: #dce6eb; --fog: #1b2e35; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; color: var(--ink); background: radial-gradient(circle at 78% 12%, rgba(67,117,106,.28), transparent 31rem), radial-gradient(circle at 15% 85%, rgba(198,106,67,.16), transparent 28rem), var(--night); font: 1rem/1.6 Georgia, 'Times New Roman', serif; }
    body::before { content: ''; position: fixed; inset: 0; pointer-events: none; opacity: .18; background-image: linear-gradient(rgba(255,255,255,.025) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.025) 1px, transparent 1px); background-size: 36px 36px; mask-image: linear-gradient(to bottom, black, transparent 80%); }
    a { color: inherit; }
    .shell { display: flex; flex-direction: column; width: min(1120px, calc(100% - 2rem)); min-height: 100vh; margin: 0 auto; }
    header { display: flex; align-items: center; justify-content: space-between; min-height: 5.5rem; border-bottom: 1px solid var(--line); }
    .brand { text-decoration: none; color: var(--gold); font-size: 1.25rem; letter-spacing: .18em; text-transform: uppercase; }
    .sign-out-form { display: block; margin: 0; }
    .sign-link { padding: .55rem 1rem; color: inherit; background: transparent; border: 1px solid var(--line); border-radius: 999px; font: inherit; text-decoration: none; cursor: pointer; }
    .sign-link:hover { border-color: var(--gold); }
    main { position: relative; flex: 1; padding: clamp(3rem, 9vw, 7rem) 0 5rem; }
    .eyebrow { margin: 0 0 1rem; color: var(--gold); font: .75rem/1.2 system-ui, sans-serif; font-weight: 700; letter-spacing: .22em; text-transform: uppercase; }
    h1 { max-width: 14ch; margin: 0; font-size: clamp(3rem, 8vw, 6.75rem); font-weight: 400; line-height: .95; letter-spacing: -.045em; }
    .lede { max-width: 42rem; margin: 2rem 0; color: var(--muted); font-size: clamp(1.1rem, 2vw, 1.35rem); }
    .primary { display: inline-block; padding: .85rem 1.25rem; color: #17150f; background: var(--gold); border: 0; border-radius: 2px; font: 700 .8rem/1 system-ui, sans-serif; letter-spacing: .12em; text-decoration: none; text-transform: uppercase; cursor: pointer; }
    .primary:hover { background: #f1d28f; }
    .wonders { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1px; margin-top: clamp(4rem, 9vw, 8rem); background: var(--line); border: 1px solid var(--line); }
    .wonder { min-height: 14rem; padding: 1.75rem; background: rgba(13,23,28,.88); }
    .wonder span { color: var(--ember); font-size: .8rem; letter-spacing: .16em; }
    .wonder h2 { margin: 2.5rem 0 .5rem; font-size: 1.45rem; font-weight: 400; }
    .wonder p { margin: 0; color: var(--muted); }
    .card { width: min(31rem, 100%); margin: 0 auto; padding: clamp(1.5rem, 5vw, 3rem); background: rgba(20,37,42,.82); border: 1px solid var(--line); box-shadow: 0 2rem 6rem rgba(0,0,0,.22); }
    .card h1 { max-width: none; font-size: clamp(2.5rem, 7vw, 4rem); }
    form { display: grid; gap: 1.25rem; margin-top: 2.25rem; }
    label { display: grid; gap: .45rem; color: var(--muted); font: .78rem/1.2 system-ui, sans-serif; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
    input, select { width: 100%; padding: .85rem 1rem; color: var(--ink); background: #0c191d; border: 1px solid #385057; border-radius: 2px; font: 1rem/1.4 system-ui, sans-serif; }
    input:focus, select:focus { outline: 2px solid var(--gold); outline-offset: 2px; }
    select { text-transform: capitalize; }
    .message { margin: 1.25rem 0 0; padding: .8rem 1rem; color: #ffe7d8; background: rgba(198,106,67,.18); border-left: 3px solid var(--ember); }
    .orders-conflict { margin: 1.25rem 0 0; padding: .9rem 1.1rem; color: #ffe7d8; background: rgba(198,106,67,.22); border-left: 3px solid var(--ember); }
    .orders-conflict p { margin: .35rem 0 0; }
    .orders-conflict-title { margin-top: 0; font-weight: 600; letter-spacing: .02em; }
    /* The fieldset is a wrapper and not a box to be drawn. */
    .orders-form fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
    /* A conflict leaves the list a player knew on screen and takes the controls
       away from them, because a second edit against that list would be refused
       too. What takes them away is a real disabled attribute on the fieldset,
       set by assets/marajanda.js, so this only has to look like what the
       controls already are. Refresh is outside the form and stays live. */
    .orders-form fieldset[disabled] { opacity: .5; filter: saturate(.4); }
    /* And if that script never arrived, the page still says so. Dimming is a
       hint rather than a claim, so this one is safe to state twice; the
       pointer-events rule that used to sit here was not, because it told a
       mouse the form was off and left a keyboard driving it. See #66. */
    #orders:has(.orders-conflict) .orders-form { opacity: .5; filter: saturate(.4); }
    .dashboard { max-width: 52rem; }
    .dashboard h1 { max-width: 12ch; overflow-wrap: anywhere; }
    .dashboard-panel { margin-top: 3rem; padding: 2rem; background: rgba(20,37,42,.7); border: 1px solid var(--line); }
    .dashboard-panel h2 { margin-top: 0; font-weight: 400; }
    .dashboard-panel p { margin-bottom: 0; color: var(--muted); }
	.game-seeds { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; margin: 1.5rem 0 0; background: var(--line); border: 1px solid var(--line); }
	.game-seeds div { padding: 1.25rem; background: rgba(13,23,28,.88); }
	.game-seeds dt { color: var(--gold); font: 700 .72rem/1.2 system-ui, sans-serif; letter-spacing: .16em; text-transform: uppercase; }
	.game-seeds dd { margin: .45rem 0 0; overflow-wrap: anywhere; font: 400 1.65rem/1.2 Georgia, 'Times New Roman', serif; }
	.faction-summary { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 2rem; }
	.faction-summary h2 { margin-bottom: .35rem; font-size: clamp(1.75rem, 4vw, 2.5rem); }
	.faction-summary .label { margin: 0; color: var(--gold); font: 700 .72rem/1.2 system-ui, sans-serif; letter-spacing: .16em; text-transform: uppercase; }
	.faction-summary .people { margin: 0 0 .75rem; color: var(--muted); text-transform: capitalize; }
	.location { min-width: 9rem; padding-left: 2rem; border-left: 1px solid var(--line); }
	.location strong { display: block; margin-top: .35rem; color: var(--ink); font: 400 1.65rem/1.2 Georgia, 'Times New Roman', serif; }
	.force { margin-top: 2rem; }
	.force h2 { margin: 0; font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; letter-spacing: .02em; }
	.force > p { max-width: 44rem; margin: .35rem 0 1rem; color: var(--muted); font: .82rem/1.5 system-ui, sans-serif; }
	.force ol { display: grid; max-width: 34rem; gap: 1px; margin: 0; padding: 0; list-style: none; background: var(--line); border: 1px solid var(--line); }
	.force li { display: flex; align-items: baseline; gap: .7rem; padding: .6rem .9rem; background: rgba(13,23,28,.88); font: .82rem/1.4 system-ui, sans-serif; }
	.force b { min-width: 8ch; color: var(--gold); font-weight: 700; letter-spacing: .08em; }
	.force .entity-name { min-width: 8rem; color: var(--ink); }
	.force .entity-kind { color: var(--muted); text-transform: capitalize; }
	.force .entity-coord { margin-left: auto; color: var(--muted); font-variant-numeric: tabular-nums; white-space: nowrap; }
	.turn { margin-top: 2.5rem; }
	.turn h2 { font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; }
	.turn p { margin: .35rem 0 1rem; font: .82rem/1.5 system-ui, sans-serif; }
	.turn-control { display: flex; align-items: baseline; gap: 1rem; margin: 0; }
	.turn-control .primary { padding: .6rem 1.1rem; }
	.orders-page { max-width: 60rem; }
	.faction-picker { max-width: 22rem; margin-top: 2.5rem; }
	.orders-form { display: block; margin-top: 1.5rem; }
	.entity-orders { margin-top: 2rem; padding: 1.25rem 1.5rem; background: rgba(13,23,28,.88); border: 1px solid var(--line); }
	.entity-orders h2 { margin: 0; font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; }
	.entity-orders h2 b { color: var(--gold); font-weight: 700; letter-spacing: .08em; }
	.entity-orders .entity-where { margin: .2rem 0 0; color: var(--muted); font: .82rem/1.4 system-ui, sans-serif; text-transform: capitalize; }
	.entity-orders .no-orders { margin: 1rem 0 0; color: var(--muted); font: .82rem/1.4 system-ui, sans-serif; }
	.stanzas { display: grid; gap: .75rem; margin: 1.25rem 0 0; padding: 0; list-style: none; }
	.stanza { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; }
	.stanza .stanza-kind { min-width: 5rem; color: var(--gold); font: 700 .78rem/1.2 system-ui, sans-serif; letter-spacing: .1em; text-transform: uppercase; }
	.stanza select { width: auto; min-width: 8.5rem; padding: .45rem .6rem; font-size: .85rem; }
	.stanza input[type="number"] { width: 5rem; padding: .45rem .6rem; font-size: .85rem; }
	/* A move's select and a rest's count sit in a slot of one width, so the
	   price column below reads as a column. */
	.stanza .direction, .stanza .count { min-width: 8.5rem; }
	.stanza .stanza-cost { min-width: 3.5rem; color: var(--muted); font: .82rem/1.2 ui-monospace, SFMono-Regular, Menlo, monospace; text-align: right; }
	.stanza .stanza-exhausts { color: var(--gold); font: .78rem/1.2 system-ui, sans-serif; text-transform: uppercase; letter-spacing: .06em; }
	/* A warning is a sentence rather than a stamp, so it is not uppercased and
	   it is allowed to wrap onto its own line on a narrow screen. */
	.stanza .stanza-warning { flex-basis: 100%; color: var(--gold); font: .82rem/1.4 system-ui, sans-serif; }
	.stanza .stanza-error { flex-basis: 100%; margin: 0; }
	.order-budget { display: flex; flex-wrap: wrap; align-items: baseline; gap: .5rem .75rem; margin: 1rem 0 0; padding-top: .75rem; border-top: 1px solid var(--rule, rgba(255,255,255,.12)); }
	.order-budget .budget-idle { min-width: 5rem; color: var(--gold); font: 700 .78rem/1.2 system-ui, sans-serif; letter-spacing: .1em; text-transform: uppercase; }
	.order-budget .budget-spent { color: var(--muted); font: .82rem/1.4 system-ui, sans-serif; }
	.order-budget .budget-overspend { flex-basis: 100%; margin: 0; }
	.rest-idle { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem .75rem; margin: .75rem 0 0; }
	/* The button carries a real disabled attribute when there is nothing to
	   rest, so this only has to look like what it already is. A disabled
	   control is out of the tab order and refuses a click on its own. */
	.rest-idle button[disabled] { opacity: .55; cursor: default; }
	.rest-idle button[disabled]:hover { border-color: var(--line); }
	.rest-idle .rest-idle-reason { color: var(--muted); font: .82rem/1.4 system-ui, sans-serif; }
	.add-order { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem .75rem; margin: 1.25rem 0 0; }
	.add-order label { display: flex; align-items: center; gap: .5rem; }
	.add-order select { width: auto; min-width: 8rem; padding: .45rem .6rem; font-size: .85rem; }
	.saved { margin: 1.25rem 0 0; color: var(--muted); font: .78rem/1.2 system-ui, sans-serif; letter-spacing: .1em; text-transform: uppercase; }
	.save-orders { margin: 2rem 0 0; }
	/* The whole region dims while a change is in flight, the way the map does.
	   It is the only indicator the page has, because it is the only thing a
	   save replaces. */
	#orders { transition: opacity .12s ease-in; }
	#orders.htmx-request { opacity: .45; }
	.results-page { max-width: 60rem; }
	.results-turns { display: flex; flex-wrap: wrap; align-items: center; gap: .75rem; margin-top: 2.5rem; }
	.results-turns strong { font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; letter-spacing: .02em; }
	/* The end of the range keeps a slot rather than losing one, so the current
	   turn does not slide sideways as a player walks back through the reports. */
	.results-turns .results-turn-end { color: var(--muted); border-style: dashed; cursor: default; }
	.results-turns .results-turn-end:hover { border-color: var(--line); }
	.entity-results { margin-top: 2rem; padding: 1.25rem 1.5rem; background: rgba(13,23,28,.88); border: 1px solid var(--line); }
	.entity-results h2 { margin: 0; font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; }
	.entity-results h2 b { color: var(--gold); font-weight: 700; letter-spacing: .08em; }
	.entity-results .entity-summary { margin: .35rem 0 0; color: var(--muted); font: .82rem/1.5 system-ui, sans-serif; }
	.ledger { display: grid; grid-template-columns: repeat(auto-fit, minmax(7rem, 1fr)); gap: .75rem 1.25rem; margin: 1.25rem 0 0; padding-top: .9rem; border-top: 1px solid var(--line); }
	.ledger dt { color: var(--muted); font: .72rem/1.2 system-ui, sans-serif; letter-spacing: .12em; text-transform: uppercase; }
	.ledger dd { margin: .25rem 0 0; font-variant-numeric: tabular-nums; }
	.ledger .ledger-aside { color: var(--muted); }
	.outcomes { display: grid; gap: .75rem; margin: 1.25rem 0 0; padding: 0; list-style: none; }
	.outcome { display: flex; flex-wrap: wrap; align-items: baseline; gap: .5rem .75rem; }
	.outcome .outcome-kind { min-width: 5rem; color: var(--gold); font: 700 .78rem/1.2 system-ui, sans-serif; letter-spacing: .1em; text-transform: uppercase; }
	.outcome .outcome-where { color: var(--muted); font: .82rem/1.4 ui-monospace, SFMono-Regular, Menlo, monospace; }
	.outcome .outcome-cost { min-width: 3.5rem; color: var(--muted); font: .82rem/1.2 ui-monospace, SFMono-Regular, Menlo, monospace; text-align: right; }
	.outcome .outcome-verdict { font: .82rem/1.4 system-ui, sans-serif; }
	/* A failure is the line a player came to read, so it is marked and its
	   sentence is given the whole width rather than squeezed in beside a price. */
	.outcome-failed .outcome-verdict { color: var(--ember); }
	.outcome .outcome-reason { flex-basis: 100%; color: var(--gold); font: .82rem/1.4 system-ui, sans-serif; }
	.outcome .revealed { flex-basis: 100%; color: var(--muted); font: .82rem/1.5 system-ui, sans-serif; }
	.outcome .revealed summary { cursor: pointer; }
	.outcome .revealed ul { display: grid; grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr)); gap: .2rem .75rem; margin: .5rem 0 0; padding: 0; list-style: none; }
	.outcome .revealed li { display: flex; gap: .5rem; }
	.outcome .revealed .observed-coord { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-variant-numeric: tabular-nums; }
	.outcome .revealed .observed-state { text-transform: capitalize; }
	#results-region { transition: opacity .12s ease-in; }
	#results-region.htmx-request { opacity: .45; }
	.visually-hidden { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
    .map-page { max-width: none; }
    /* The map scrolls inside its frame rather than being scaled down to fit it.
       Scrolling is the browser's own gesture, so a phone pans it with one
       finger, with momentum, and nothing here has to implement any of that.
       overscroll-behavior keeps a pan that reaches the edge of the world from
       carrying on into scrolling the page underneath. */
    .map { display: grid; justify-content: safe center; max-height: min(70vh, 40rem); margin-top: 2.5rem; padding: 1rem; overflow: auto; overscroll-behavior: contain; background: rgba(13,23,28,.6); border: 1px solid var(--line); }
    .map svg { display: block; }
    #map-region.htmx-request .map { opacity: .45; }
    .map { transition: opacity .12s ease-in; }
    .map-pan { display: grid; grid-template-areas: ". north ." "west here east" ". south ."; justify-content: center; gap: .5rem; margin-top: 1.25rem; }
    .map-pan .north { grid-area: north; }
    .map-pan .south { grid-area: south; }
    .map-pan .west { grid-area: west; }
    .map-pan .east { grid-area: east; }
    .map-pan .here { grid-area: here; align-self: center; color: var(--muted); font: .78rem/1.2 system-ui, sans-serif; letter-spacing: .1em; text-align: center; }
    .map-jump { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: .5rem .75rem; margin-top: 1.25rem; }
    .map-jump label { flex: none; }
    .map-jump input { width: 9rem; padding: .5rem .75rem; }
    .map-jump .primary { padding: .6rem 1rem; }
    .map-jump small { flex-basis: 100%; color: var(--muted); font: .78rem/1.4 system-ui, sans-serif; text-align: center; }
    .map polygon { stroke: rgba(13,23,28,.55); stroke-width: 1; }
    .map .grassland, .legend .grassland, .neighbours .grassland { fill: var(--grassland); background: var(--grassland); }
    .map .forest, .legend .forest, .neighbours .forest { fill: var(--forest); background: var(--forest); }
    .map .hills, .legend .hills, .neighbours .hills { fill: var(--hills); background: var(--hills); }
    .map .marsh, .legend .marsh, .neighbours .marsh { fill: var(--marsh); background: var(--marsh); }
    .map .mountains, .legend .mountains, .neighbours .mountains { fill: var(--mountains); background: var(--mountains); }
    .map .ocean, .legend .ocean, .neighbours .ocean { fill: var(--ocean); background: var(--ocean); }
    .map .lake, .legend .lake, .neighbours .lake { fill: var(--lake); background: var(--lake); }
    .map .ice, .legend .ice, .neighbours .ice { fill: var(--ice); background: var(--ice); }
    .map .fog, .legend .fog, .neighbours .fog { fill: var(--fog); background: var(--fog); }
    .legend { display: flex; flex-wrap: wrap; gap: 1.1rem; margin: 1.25rem 0 0; padding: 0; color: var(--muted); font: .72rem/1.2 system-ui, sans-serif; letter-spacing: .14em; list-style: none; text-transform: uppercase; }
    .legend li { display: flex; align-items: center; gap: .5rem; }
    .legend i { width: .95rem; height: .95rem; border: 1px solid var(--line); }
    .neighbours { margin-top: 2.5rem; }
    .neighbours h2 { margin: 0; font: 400 1.15rem/1.3 Georgia, 'Times New Roman', serif; letter-spacing: .02em; }
    .neighbours > p { max-width: 44rem; margin: .35rem 0 1rem; color: var(--muted); font: .82rem/1.5 system-ui, sans-serif; }
    /* One column, numbered. The order is the contract these six are here to
       show, so they are read down the page rather than found in a grid. */
    .neighbours ol { display: grid; max-width: 34rem; gap: 1px; margin: 0; padding: 0; counter-reset: point; list-style: none; background: var(--line); border: 1px solid var(--line); }
    .neighbours li { display: flex; align-items: baseline; gap: .7rem; padding: .6rem .9rem; background: rgba(13,23,28,.88); font: .82rem/1.4 system-ui, sans-serif; }
    .neighbours li::before { counter-increment: point; content: counter(point); min-width: 1ch; color: var(--muted); font-variant-numeric: tabular-nums; }
    .neighbours b { min-width: 2.5ch; color: var(--gold); font-weight: 700; letter-spacing: .08em; }
    .neighbours .point-name { min-width: 7rem; color: var(--ink); white-space: nowrap; }
    .neighbours .point-coord { color: var(--muted); font-variant-numeric: tabular-nums; white-space: nowrap; }
    .neighbours .point-terrain { display: flex; align-items: center; gap: .4rem; margin-left: auto; color: var(--muted); }
    .neighbours .point-terrain i { width: .8rem; height: .8rem; border: 1px solid var(--line); border-radius: 2px; }
    .neighbours .beyond { margin-left: auto; color: var(--ember); white-space: nowrap; }
    .map-actions { display: flex; flex-wrap: wrap; gap: .75rem; margin-top: 2rem; }
    footer { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 2rem 0 3rem; color: #898674; border-top: 1px solid var(--line); font-size: .85rem; }
    .project-meta { display: flex; align-items: center; gap: .65rem; }
    .github-link { display: flex; color: var(--muted); }
    .github-link:hover { color: var(--gold); }
    .github-link svg { width: 1.15rem; height: 1.15rem; fill: currentColor; }
    @media (max-width: 720px) { .wonders, .game-seeds { grid-template-columns: 1fr; } .wonder { min-height: auto; } .wonder h2 { margin-top: 1.5rem; } .faction-summary { grid-template-columns: 1fr; } .location { padding: 1.5rem 0 0; border-top: 1px solid var(--line); border-left: 0; } footer { align-items: flex-start; flex-direction: column; } }
  </style>
</head>
<body>
  <div class="shell">
    <header>
      <a class="brand" href="/">Marajanda</a>
      {{if eq .View "landing"}}<a class="sign-link" href="/sign-in">Sign in</a>{{end}}
      {{if or (eq .View "admin") (eq .View "player") (eq .View "faction") (eq .View "admin-map") (eq .View "player-map") (eq .View "orders") (eq .View "results")}}<form class="sign-out-form" action="/sign-out" method="post"><button class="sign-link" type="submit">Sign out</button></form>{{end}}
    </header>
    <main>
      {{if eq .View "landing"}}
      <section>
        <p class="eyebrow">An open-ended fantasy world</p>
        <h1>The realm remembers what is yet to come.</h1>
        <p class="lede">Marajanda will be a living world of ambitious factions, uncertain alliances, and powers discovered one hard-won secret at a time. For now, the old maps are still being drawn.</p>
        <a class="primary" href="/sign-in">Enter Marajanda</a>
      </section>
      <section class="wonders" aria-label="The wonders ahead">
        <article class="wonder"><span>01 · EXPLORE</span><h2>Unwritten frontiers</h2><p>Cross strange country, uncover forgotten roads, and give names to places no chronicle has yet recorded.</p></article>
        <article class="wonder"><span>02 · COMMAND</span><h2>A faction of your own</h2><p>Guide a people from fragile beginnings toward influence, mastery, and a legacy that reshapes the realm.</p></article>
        <article class="wonder"><span>03 · BECOME</span><h2>Stories without rails</h2><p>Choose your ambitions. Every pact, rivalry, discovery, and defeat will become part of Marajanda's history.</p></article>
      </section>
      {{else if eq .View "sign-in"}}
      <section class="card">
        <p class="eyebrow">Return to the realm</p>
        <h1>Sign in</h1>
        <p class="lede">Present the credentials entrusted to your account.</p>
        {{if .Message}}<p class="message" role="alert">{{.Message}}</p>{{end}}
        <form action="/sign-in" method="post">
          <label>Account<input name="account" type="text" autocomplete="username" required autofocus></label>
          <label>Passphrase<input name="passphrase" type="password" autocomplete="current-password" required></label>
          <button class="primary" type="submit">Sign in</button>
        </form>
      </section>
      {{else if eq .View "faction"}}
	  <section class="card">
		<p class="eyebrow">Begin your legacy</p>
		<h1>Name your faction</h1>
		<p class="lede">Choose the name by which your people will be known throughout Marajanda.</p>
		{{if .Message}}<p class="message" role="alert">{{.Message}}</p>{{end}}
		<form action="/player/faction" method="post">
		  <label>Faction name<input name="name" type="text" value="{{.Name}}" aria-describedby="faction-name-help" required autofocus></label>
		  <small id="faction-name-help">Use 3 to 32 printable characters. Spaces between words will be normalized.</small>
		  <label>People<select name="race" aria-describedby="faction-race-help">{{$chosen := .Race}}{{range .Races}}<option value="{{.}}"{{if eq . $chosen}} selected{{end}}>{{.}}</option>{{end}}</select></label>
		  <small id="faction-race-help">Your people decide the country your faction is settled in.</small>
		  <button class="primary" type="submit">Establish faction</button>
		</form>
	  </section>
	  {{else if or (eq .View "admin-map") (eq .View "player-map")}}
	  <section class="dashboard map-page">
		{{if eq .View "admin-map"}}
		<p class="eyebrow">The true map</p>
		<h1>Marajanda</h1>
		<p class="lede">A window onto the world in true coordinates, drawn hex by hex. Scroll inside the frame to look around it, pan to move it, and download the whole world as an image. Hover a hex for its terrain and elevation.</p>
		{{else}}
		<p class="eyebrow">Your map</p>
		<h1>{{.Faction.Name}}</h1>
		<p class="lede">The land your people have seen, and the little of it they can make out beyond. Everything past that is still rumour.</p>
		{{end}}
		{{template "map-region" .}}
		<ul class="legend">
		  <li><i class="grassland"></i>Grassland</li>
		  <li><i class="forest"></i>Forest</li>
		  <li><i class="hills"></i>Hills</li>
		  <li><i class="marsh"></i>Marsh</li>
		  <li><i class="mountains"></i>Mountains</li>
		  <li><i class="ocean"></i>Ocean</li>
		  <li><i class="lake"></i>Lake</li>
		  <li><i class="ice"></i>Ice</li>
		  {{if eq .View "player-map"}}<li><i class="fog"></i>Unexplored</li>{{end}}
		</ul>
		<p class="map-actions"><a class="sign-link" href="{{if eq .View "admin-map"}}/admin/dashboard{{else}}/player/dashboard{{end}}">Back to dashboard</a>{{if .Map.Pan}}<a class="sign-link" href="{{.Map.Pan.Origin}}" hx-get="{{.Map.Pan.Origin}}" hx-target="#map-region" hx-swap="outerHTML" hx-push-url="true" hx-indicator="#map-region">Back to the origin</a>{{end}}{{if .Map.Image}}<a class="sign-link" href="{{.Map.Image}}">Download the whole world</a>{{end}}</p>
	  </section>
	  {{else if eq .View "orders"}}
	  <section class="dashboard orders-page">
		<p class="eyebrow">Faction command</p>
		<h1>Orders for turn {{.Turn}}</h1>
		<p class="lede">Build this turn's orders one row at a time, and they are saved as you make them.</p>
		{{/* A player commands one faction, so the picker holds one entry and it
		     is selected. It is here so the page has a stable shape for the day
		     something commands more than one. */}}
		<label class="faction-picker">Faction<select name="faction" aria-describedby="orders-faction-help"><option value="{{.Account.Email}}" selected>{{.Faction.Name}}</option></select></label>
		<small id="orders-faction-help">You command one faction, so there is one to choose.</small>
		{{template "orders-list" .}}
		<p class="map-actions"><a class="sign-link" href="/player/dashboard">Back to dashboard</a></p>
	  </section>
	  {{else if eq .View "results"}}
	  <section class="dashboard results-page">
		<p class="eyebrow">Faction command</p>
		<h1>{{.Faction.Name}}</h1>
		<p class="lede">The account of a turn already processed: what each order cost, what it did, and what it revealed.</p>
		{{template "results-region" .}}
		<p class="map-actions"><a class="sign-link" href="/player/dashboard">Back to dashboard</a><a class="sign-link" href="/player/map">View your map</a>{{if .Faction.Active}}<a class="sign-link" href="/player/orders">Give orders</a>{{end}}</p>
	  </section>
	  {{else}}
      <section class="dashboard">
        <p class="eyebrow">{{if eq .View "admin"}}Steward of Marajanda{{else}}Faction command{{end}}</p>
        <h1>Welcome, {{.Account.Handle}}.</h1>
        <p class="lede">Your place in Marajanda is ready, even while the world beyond it is still taking shape.</p>
        <div class="dashboard-panel">
		  {{if eq .View "admin"}}
		  <h2>Game seeds</h2>
		  <p>These values were set when the realm was created and cannot be changed.</p>
		  <dl class="game-seeds">
			<div><dt>Seed 1</dt><dd>{{.Game.Seed1}}</dd></div>
			<div><dt>Seed 2</dt><dd>{{.Game.Seed2}}</dd></div>
		  </dl>
		  <p class="map-actions"><a class="sign-link" href="/admin/map">View the map</a></p>
		  {{/* Advancing carries out the orders built for the turn and then moves
		       the clock, both in one transaction. What each faction is told
		       about what happened is not built yet; see
		       docs/reference/turn-processing.md. */}}
		  <section class="turn" aria-label="The turn">
			<h2>The turn</h2>
			<p>Advancing the turn carries out the orders factions have built for it, then closes it.</p>
			<form class="turn-control" action="/admin/turn" method="post">
			  <strong>Turn {{.Turn}}</strong>
			  <button class="primary" type="submit">Advance the turn</button>
			</form>
		  </section>
		  {{else}}
		  <div class="faction-summary">
			<div>
			  <p class="label">Your faction</p>
			  <h2>{{.Faction.Name}}</h2>
			  <p class="people">{{.Faction.Race}}</p>
			  {{if .Faction.Active}}<p>Your people await your command.</p>{{else}}<p>Your people are still here. They are taking no commands.</p>{{end}}
			</div>
			<div class="location">
			  <p class="label">Turn</p>
			  <strong>{{.Turn}}</strong>
			</div>
		  </div>
		  {{/* A faction has no location of its own. Its entities do, so this is
		       where the dashboard answers "where am I": every entity the
		       faction controls, as it stands on the turn named above. */}}
		  <section class="force" aria-label="What your faction controls">
			<h2>Your force</h2>
			{{if .Entities}}
			<p>Everything your faction controls, and where it stands this turn.</p>
			<ol>
			  {{/* A name defaults to its entity's code, so it is printed only
			       once it says something the code does not. The span stays
			       either way to keep the columns lined up. */}}
			  {{range .Entities}}<li><b>{{.Code}}</b><span class="entity-name">{{if ne .Name .Code}}{{.Name}}{{end}}</span><span class="entity-kind">{{.Kind}}</span><span class="entity-coord">({{.Location.Q}}, {{.Location.R}})</span></li>
			  {{end}}
			</ol>
			{{else}}
			<p>Your faction controls nothing yet.</p>
			{{end}}
		  </section>
		  {{/* A deactivated faction gives no orders, so the page does not
		       offer the link. Everything else it has stays reachable: the
		       flag stops a faction acting, it does not lock a person out of
		       looking at their own game. */}}
		  {{if not .Faction.Active}}<p class="message" role="status">This faction is not active. It cannot be given orders until an administrator restores it.</p>{{end}}
		  {{/* A game on its first turn has processed nothing, so there is no
		       report to send a player to yet. The link arrives with the first
		       thing it has to say. */}}
		  <p class="map-actions"><a class="sign-link" href="/player/map">View your map</a>{{if gt .Turn 1}}<a class="sign-link" href="/player/results">Read the last turn</a>{{end}}{{if .Faction.Active}}<a class="sign-link" href="/player/orders">Give orders</a>{{end}}</p>
		  {{end}}
        </div>
      </section>
      {{end}}
    </main>
    <footer>
      <span>Marajanda · The world is under construction.</span>
      <span class="project-meta">
        <span>v{{.Version}}</span>
        <a class="github-link" href="https://github.com/mdhender/marajanda/issues" aria-label="Marajanda issues on GitHub">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.11.79-.25.79-.56v-2.24c-3.22.7-3.9-1.37-3.9-1.37-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.71.08-.71 1.16.08 1.78 1.2 1.78 1.2 1.04 1.77 2.72 1.26 3.38.96.1-.75.4-1.26.74-1.55-2.57-.29-5.27-1.29-5.27-5.68 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.47.11-3.05 0 0 .97-.31 3.16 1.18A10.98 10.98 0 0 1 12 6.11c.98 0 1.94.13 2.85.38 2.2-1.49 3.16-1.18 3.16-1.18.63 1.58.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.4-2.71 5.38-5.29 5.67.42.36.79 1.07.79 2.16v3.27c0 .31.21.68.8.56A11.5 11.5 0 0 0 12 .7Z"/></svg>
        </a>
      </span>
    </footer>
  </div>
</body>
</html>

{{/* The map region is the part of a map page a pan or a jump changes: the
     frame, the pan links and the jump box. Everything around it - the heading,
     the legend, the sign-out form - is the same window to window, so HTMX asks
     for this block alone and swaps it in place. The page keeps its scroll
     position, which a page load does not, and the browser is not handed a
     thousand polygons of chrome it already has.

     The links and the form keep their href and action. Without JavaScript they
     are the ordinary navigation #22 shipped and the map works exactly as it
     did; HTMX only intercepts them when it has loaded. */}}
{{define "map-region"}}		<div id="map-region" hx-target="#map-region" hx-swap="outerHTML" hx-push-url="true" hx-indicator="#map-region">
		<div class="map">
		  {{if .Map.Tiles}}
		  <svg width="{{.Map.Width}}" height="{{.Map.Height}}" viewBox="{{.Map.ViewBox}}" role="img" aria-label="{{if eq .View "admin-map"}}Hex map of the world around {{.Map.Center}}{{else}}Hex map of the land your people have seen{{end}}">
			{{range .Map.Tiles}}<polygon class="{{.Terrain}}" points="{{.Points}}"><title>{{.Label}}</title></polygon>
			{{end}}
		  </svg>
		  {{else}}
		  <p class="lede">There is nothing to draw yet.</p>
		  {{end}}
		</div>
		{{if .Map.Pan}}
		<nav class="map-pan" aria-label="Move the map">
		  <a class="sign-link north" href="{{.Map.Pan.North}}" hx-get="{{.Map.Pan.North}}">North</a>
		  <a class="sign-link west" href="{{.Map.Pan.West}}" hx-get="{{.Map.Pan.West}}">West</a>
		  <span class="here">{{.Map.Center}}</span>
		  <a class="sign-link east" href="{{.Map.Pan.East}}" hx-get="{{.Map.Pan.East}}">East</a>
		  <a class="sign-link south" href="{{.Map.Pan.South}}" hx-get="{{.Map.Pan.South}}">South</a>
		</nav>
		{{/* The box is deliberately empty on the way back. A jump is a new
		     window, which is a new page, so nothing echoes the submitted
		     coordinate into value= and the field arrives blank whether the
		     jump landed or was a typo sent back to the origin. The page's
		     own centre says where the window is; a value= would only argue
		     with it. */}}
		<form class="map-jump" action="/admin/map" method="get" hx-get="/admin/map">
		  <label for="map-jump-at">Jump to</label>
		  <input id="map-jump-at" name="at" type="text" placeholder="q,r" autocomplete="off" spellcheck="false" aria-describedby="map-jump-help">
		  <button class="primary" type="submit">Go</button>
		  <small id="map-jump-help">A coordinate pair, such as 12,-4. Anything that is not a hex of the world returns to the origin.</small>
		</form>
		{{end}}
		{{if .Map.Neighbors}}
		<section class="neighbours" aria-label="The hexes around {{.Map.Center}}">
		  <h2>Around {{.Map.Center}}</h2>
		  <p>The six hexes a faction could step to, in the order the movement rules will visit them.</p>
		  <ol>
			{{range .Map.Neighbors}}<li><b>{{.Point}}</b><span class="point-name">{{.Name}}</span><span class="point-coord">{{.Coord}}</span>{{if .Beyond}}<span class="beyond">beyond the world</span>{{else}}<span class="point-terrain"><i class="{{.Terrain}}"></i>{{.Terrain}}</span>{{end}}</li>
			{{end}}
		  </ol>
		</section>
		{{end}}
		</div>{{end}}

{{/* The orders region is everything a write changes: every entity, its
     orders, their directions, and whatever the last write had to say. A write
     answers with the whole of it rather than with the control that was
     touched, so numbering and validation are decided by the server and there
     is no client-side state to drift.

     One form wraps the lot, and every control inside it is one a browser can
     work without HTMX: the selects and the buttons submit to POST
     /player/orders with the script-free Save button, which is why each select
     carries its whole address - entity and sequence - in its name. With HTMX
     loaded, a select posts itself to the URL that names the same order and the
     page never submits at all. */}}
{{/* orders-conflict is swapped into #orders-notice, alone, when a write is
     refused because somebody else changed the list. The list keeps what the
     player knew; Refresh is what asks for the new draw, so the other client's
     orders arrive because they were asked for and not underneath a cursor. */}}
{{define "orders-conflict"}}<div class="orders-conflict" role="alert">
		  <p class="orders-conflict-title">Conflicting update</p>
		  <p>These orders were changed somewhere else, so your change was not applied. Refresh to carry on.</p>
		  {{/* The link keeps its href, so it is a page load when the script
		       is blocked and a new draw of the list when it is not - the same
		       bargain the map's pan links make. */}}
		  <p><a class="sign-link" href="/player/orders" hx-get="/player/orders" hx-target="#orders" hx-swap="outerHTML">Refresh</a></p>
		</div>{{end}}

{{define "orders-list"}}		<div id="orders" hx-target="#orders" hx-swap="outerHTML" hx-indicator="#orders">
		{{/* The notice region is swapped on its own when a write loses a race,
		     which is the one case where the list below must not be replaced:
		     the orders the other client wrote are not orders this player has
		     ever seen. Empty the rest of the time. */}}
		<div id="orders-notice"></div>
		{{if .Orders.Message}}<p class="message" role="alert">{{.Orders.Message}}</p>{{end}}
		{{if .Orders.Saved}}<p class="saved" role="status">Saved at {{.Orders.Saved}}</p>{{end}}
		<form class="orders-form" action="/player/orders" method="post">
		{{/* Every control the form holds, inside one fieldset, so that one
		     attribute turns the lot off. A write that loses a race is answered
		     with the notice alone and cannot redraw these controls, so the
		     attribute is set on the page by assets/marajanda.js - and the
		     fieldset is what gives it one attribute to set instead of a walk
		     over every control. Disabled and not inert: the orders stay
		     readable, which is the point of leaving them on screen. See #66. */}}
		<fieldset id="orders-controls">
		{{/* Which list this page was drawn from. Every control posts the form,
		     so every write says what it believed it was writing to. */}}
		<input type="hidden" name="ordersTag" value="{{.Orders.Tag}}">
		{{/* Enter in any field submits a form through its first submit button,
		     and every other button here removes, inserts or adds an order.
		     Without this one, Enter in a direction select would delete the
		     first order on the page.
		     It saves, which is what Enter means everywhere else on this form,
		     and it is out of the tab order because the visible controls are
		     what a keyboard should reach. */}}
		<button class="visually-hidden" type="submit" tabindex="-1">Save orders</button>
		{{$directions := .Orders.Directions}}
		{{range .Orders.Entities}}
		  <section class="entity-orders" aria-label="Orders for {{.Entity.Code}}">
			<h2><b>{{.Entity.Code}}</b>{{if ne .Entity.Name .Entity.Code}} {{.Entity.Name}}{{end}}</h2>
			<p class="entity-where">{{.Entity.Kind}} · ({{.Entity.Location.Q}}, {{.Entity.Location.R}})</p>
			{{if .Stanzas}}
			<ol class="stanzas">
			  {{/* An order is one action, so a row is one select: the way
			       this order goes. The blank option is an order whose
			       direction has not been chosen, not an order to delete -
			       Remove does that, and Insert puts a fresh row after this
			       one so a list can be corrected in the middle. */}}
			  {{range .Stanzas}}<li class="stanza">
				<span class="stanza-kind">{{.Label}}</span>
				{{/* A move says which way it goes and a rest says how long it
				     lasts, so a row carries one control and which one is its
				     kind. A count has no blank option: a rest lasts at least
				     one point, so there is no "not chosen yet" to draw. */}}
				{{if .IsRest}}<label class="count"><span class="visually-hidden">{{.SelectLabel}}</span>
				<input type="number" name="{{.Name}}" value="{{.Current}}" min="1" max="{{.CountMax}}" step="1" hx-post="{{.Post}}" hx-trigger="change"></label>
				{{else}}<label class="direction"><span class="visually-hidden">{{.SelectLabel}}</span>
				<select name="{{.Name}}" hx-post="{{.Post}}" hx-trigger="change">
				  <option value=""{{if not .Current}} selected{{end}}>—</option>
				  {{$chosen := .Current}}{{range $directions}}<option value="{{.Value}}"{{if eq .Value $chosen}} selected{{end}}>{{.Label}}</option>{{end}}
				</select></label>{{end}}
				<span class="stanza-cost">{{.Cost}}</span>
				{{if .Exhausts}}<span class="stanza-exhausts">Will exhaust</span>{{end}}
				{{if .Warning}}<span class="stanza-warning">{{.Warning}}</span>{{end}}
				<button class="sign-link" type="submit" name="insert" value="{{.InsertValue}}" hx-post="{{.InsertURL}}">Insert after</button>
				<button class="sign-link" type="submit" name="remove" value="{{.RemoveValue}}" hx-delete="{{.RemoveURL}}">Remove</button>
				{{if .Error}}<p class="message stanza-error" role="alert">{{.Error}}</p>{{end}}
			  </li>
			  {{end}}
			</ol>
			{{end}}
			{{/* Idle points are what the orders above leave unspent. They are
			     a number, not an order: nothing rests an entity that was not
			     ordered to, because a player may be spending one to
			     exhaustion on purpose. The line is drawn whatever the count
			     is, so the page does not change shape as a player edits. The
			     numbers are estimates: every order is priced as though it
			     lands, and unknown ground is priced at a flat exploration
			     cost whatever is actually there. */}}
			{{with .Budget}}
			<p class="order-budget">
			  <span class="budget-idle">{{.Idle}} idle</span>
			  <span class="budget-spent">{{.Spent}} of {{.Allowance}} action points, estimated.</span>
			  {{if .Overspend}}<span class="message budget-overspend" role="status">Over by {{.Overspend}}. Order {{.ExhaustsAt}} and everything after it will exhaust.</span>{{end}}
			</p>
			{{/* Spending the idle points is an action, not a mode: it appends
			     one ordinary Rest and is finished, so there is nothing to keep
			     in step afterwards and nothing to tell apart from an order the
			     player typed. The button posts the entity and never the count -
			     the number here is what this draw of the page is looking at,
			     and the server reads the residue again when the write lands.
			     It is a submit button in the form, so a browser with the script
			     blocked presses it the same way it presses Add. */}}
			{{with .RestIdle}}
			<p class="rest-idle">
			  <button class="sign-link" type="submit" name="restIdle" value="{{.Value}}" hx-post="/player/orders"{{if .Disabled}} disabled{{end}}>{{.Label}}</button>
			  {{/* Nothing to rest is a disabled button and a reason beside it
			       rather than a button that would write Rest x0, and rather
			       than a control that disappears and moves the line under the
			       cursor. See issue #58. */}}
			  {{if .Reason}}<span class="rest-idle-reason">{{.Reason}}</span>{{end}}
			</p>
			{{end}}
			{{end}}
			{{if .Kinds}}
			<p class="add-order">
			  <label>Add order<select name="{{.KindField}}">{{range .Kinds}}<option value="{{.Value}}">{{.Label}}</option>{{end}}</select></label>
			  <button class="sign-link" type="submit" name="add" value="{{.AddValue}}" hx-post="/player/orders">Add</button>
			</p>
			{{else}}
			<p class="no-orders">No orders available yet.</p>
			{{end}}
		  </section>
		{{end}}
		{{/* Without script nothing saves itself, so the page carries one Save
		     button that submits every box at once. With script there is no
		     button and no pending state to have one for. CSP forbids inline
		     script, so noscript is the only script-free detector available. */}}
		<noscript><p class="save-orders"><button class="primary" type="submit">Save orders</button></p></noscript>
		</fieldset>
		</form>
		</div>{{end}}

{{/* The results region is the part of the report a turn link changes: the turn
     it names, the links either side of it, and every section below them.
     Everything around it - the faction heading, the page links, the sign-out
     form - is the same turn to turn, so HTMX asks for this block alone.

     The turn links keep their href, so walking back through the reports is
     ordinary navigation with the script blocked. The hexes an order revealed
     are in a details element for the same reason: a disclosure that needs no
     script is a disclosure that works without one. */}}
{{define "results-region"}}		<div id="results-region" hx-target="#results-region" hx-swap="outerHTML" hx-push-url="true" hx-indicator="#results-region">
		{{if .Results.Message}}<p class="message" role="status">{{.Results.Message}}</p>{{end}}
		{{if .Results.Turn}}
		<nav class="results-turns" aria-label="Turn reports">
		  {{if .Results.Older}}<a class="sign-link" href="{{.Results.Older.URL}}">&#8592; {{.Results.Older.Label}}</a>{{else}}<span class="sign-link results-turn-end">The first turn</span>{{end}}
		  <strong>Turn {{.Results.Turn}}</strong>
		  {{if .Results.Newer}}<a class="sign-link" href="{{.Results.Newer.URL}}">{{.Results.Newer.Label}} &#8594;</a>{{else}}<span class="sign-link results-turn-end">The latest turn</span>{{end}}
		</nav>
		{{range .Results.Entities}}
		<article class="entity-results">
		  <h2><b>{{.Entity.Code}}</b>{{if ne .Entity.Name .Entity.Code}} {{.Entity.Name}}{{end}}</h2>
		  <p class="entity-summary">{{.Summary}}</p>
		  {{/* An entity that takes no orders has a ledger of zeroes, and a row
		       of zeroes says nothing its one sentence has not said already. */}}
		  {{if .TakesOrders}}
		  <dl class="ledger">
			<div><dt>Allowance</dt><dd>{{.Ledger.Allowance}} AP</dd></div>
			<div><dt>Spent</dt><dd>{{.Ledger.Spent}} AP</dd></div>
			<div><dt>Lapsed</dt><dd>{{.Ledger.Lapsed}} AP</dd></div>
			<div><dt>Started</dt><dd>{{.Ledger.Start}}</dd></div>
			<div><dt>Ended</dt><dd>{{.Ledger.End}}{{if not .Ledger.Moved}} <span class="ledger-aside">where it started</span>{{end}}</dd></div>
		  </dl>
		  {{if .Orders}}
		  <ol class="outcomes">
			{{range .Orders}}<li class="outcome{{if not .Carried}} outcome-failed{{end}}">
			  <span class="outcome-kind">{{.Label}}</span>
			  <span class="outcome-where">{{.From}}{{if .Aimed}} &#8594; {{.Target}}{{end}}</span>
			  <span class="outcome-cost">{{.Cost}}</span>
			  <span class="outcome-verdict">{{if .Carried}}Carried out{{else}}Did not happen{{end}}</span>
			  {{if .Reason}}<span class="outcome-reason">{{.Reason}}</span>{{end}}
			  {{if .Revealed}}<details class="revealed"><summary>{{.Revealed}}</summary>
				<ul>{{range .Observations}}<li><span class="observed-coord">{{.Coord}}</span><span class="observed-state">{{.State}}</span></li>
				{{end}}</ul>
			  </details>{{end}}
			</li>
			{{end}}
		  </ol>
		  {{end}}
		  {{end}}
		</article>
		{{end}}
		{{if not .Results.Entities}}<p class="no-orders">Nothing of your faction has a report for this turn.</p>{{end}}
		{{end}}
		</div>{{end}}
`))

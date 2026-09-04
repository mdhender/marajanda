// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/mail"
	"strings"
	"sync"

	"github.com/mdhender/marajanda"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

const sessionCookieName = "marajanda_session"

type authenticateFunc func(context.Context, string, string) (datastore.Account, bool, error)
type findOrCreateFunc func(context.Context, string) (datastore.Account, error)

type factionStore interface {
	Faction(context.Context, string) (datastore.Faction, bool, error)
	SaveFaction(context.Context, string, string) error
}

type application struct {
	authenticate        authenticateFunc
	findOrCreateAccount findOrCreateFunc
	factions            factionStore
	sessionsMu          sync.RWMutex
	sessions            map[string]datastore.Account
}

type pageData struct {
	Title   string
	View    string
	Message string
	Version string
	Account datastore.Account
	Faction datastore.Faction
	Name    string
}

func newHandler(authenticate authenticateFunc, factions factionStore) http.Handler {
	return newConfiguredHandler(authenticate, nil, factions, "production")
}

func newConfiguredHandler(authenticate authenticateFunc, findOrCreate findOrCreateFunc, factions factionStore, environment string) http.Handler {
	app := &application{
		authenticate:        authenticate,
		findOrCreateAccount: findOrCreate,
		factions:            factions,
		sessions:            make(map[string]datastore.Account),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /", app.landing)
	mux.HandleFunc("GET /sign-in", app.signInForm)
	mux.HandleFunc("POST /sign-in", app.signIn)
	mux.HandleFunc("POST /sign-out", app.signOut)
	mux.HandleFunc("GET /admin/dashboard", app.dashboard("admin"))
	mux.HandleFunc("GET /player/dashboard", app.dashboard("player"))
	mux.HandleFunc("GET /player/faction", app.factionForm)
	mux.HandleFunc("POST /player/faction", app.configureFaction)
	registerAgentRoutes(mux, app, environment)

	return new(http.CrossOriginProtection).Handler(mux)
}

func (app *application) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if account, ok := app.currentAccount(r); ok {
		http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
		return
	}
	app.render(w, http.StatusOK, pageData{Title: "A world waiting to awaken", View: "landing"})
}

func (app *application) signInForm(w http.ResponseWriter, r *http.Request) {
	if account, ok := app.currentAccount(r); ok {
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
		http.Error(w, "Marajanda could not complete the sign-in request.", http.StatusInternalServerError)
		return
	}
	if !ok {
		app.renderSignInFailure(w, http.StatusUnauthorized)
		return
	}
	if err := app.startSession(w, account); err != nil {
		http.Error(w, "Marajanda could not create the session.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
}

func (app *application) signOut(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		app.sessionsMu.Lock()
		delete(app.sessions, cookie.Value)
		app.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
}

func (app *application) startSession(w http.ResponseWriter, account datastore.Account) error {
	token, err := newSessionToken()
	if err != nil {
		return err
	}
	app.sessionsMu.Lock()
	app.sessions[token] = account
	app.sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (app *application) dashboard(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := app.currentAccount(r)
		if !ok {
			http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
			return
		}
		if account.Role != role {
			http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
			return
		}
		var faction datastore.Faction
		if role == "player" {
			if app.factions == nil {
				http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
				return
			}
			var found bool
			var err error
			faction, found, err = app.factions.Faction(r.Context(), account.Email)
			if err != nil {
				http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
				return
			}
			if !found || !faction.Configured() {
				http.Redirect(w, r, "/player/faction", http.StatusSeeOther)
				return
			}
		}
		title := "Player dashboard"
		if role == "admin" {
			title = "Admin dashboard"
		}
		app.render(w, http.StatusOK, pageData{Title: title, View: role, Account: account, Faction: faction})
	}
}

func (app *application) factionForm(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requirePlayer(w, r)
	if !ok {
		return
	}
	faction, found, err := app.factions.Faction(r.Context(), account.Email)
	if err != nil {
		http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
		return
	}
	if found && faction.Configured() {
		http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
		return
	}
	app.render(w, http.StatusOK, pageData{Title: "Configure faction", View: "faction", Account: account})
}

func (app *application) configureFaction(w http.ResponseWriter, r *http.Request) {
	account, ok := app.requirePlayer(w, r)
	if !ok {
		return
	}
	faction, found, err := app.factions.Faction(r.Context(), account.Email)
	if err != nil {
		http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
		return
	}
	if found && faction.Configured() {
		http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.render(w, http.StatusBadRequest, pageData{Title: "Configure faction", View: "faction", Account: account, Message: "Marajanda could not read that faction name."})
		return
	}
	name, err := game.NormalizeFactionName(r.FormValue("name"))
	if err != nil {
		app.render(w, http.StatusUnprocessableEntity, pageData{Title: "Configure faction", View: "faction", Account: account, Name: r.FormValue("name"), Message: err.Error()})
		return
	}
	if err := app.factions.SaveFaction(r.Context(), account.Email, name); err != nil {
		http.Error(w, "Marajanda could not save your faction.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/player/dashboard", http.StatusSeeOther)
}

func (app *application) requirePlayer(w http.ResponseWriter, r *http.Request) (datastore.Account, bool) {
	account, ok := app.currentAccount(r)
	if !ok {
		http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
		return datastore.Account{}, false
	}
	if account.Role != "player" {
		http.Redirect(w, r, dashboardPath(account), http.StatusSeeOther)
		return datastore.Account{}, false
	}
	if app.factions == nil {
		http.Error(w, "Marajanda could not load your faction.", http.StatusInternalServerError)
		return datastore.Account{}, false
	}
	return account, true
}

func (app *application) currentAccount(r *http.Request) (datastore.Account, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return datastore.Account{}, false
	}
	app.sessionsMu.RLock()
	account, ok := app.sessions[cookie.Value]
	app.sessionsMu.RUnlock()
	return account, ok
}

func (app *application) renderSignInFailure(w http.ResponseWriter, status int) {
	app.render(w, status, pageData{
		Title:   "Sign in",
		View:    "sign-in",
		Message: "Those credentials were not accepted. Check your account and passphrase, then try again.",
	})
}

func (app *application) render(w http.ResponseWriter, status int, data pageData) {
	data.Version = marajanda.Version().Short()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = pageTemplate.Execute(w, data)
}

func isEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func newSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
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
  <style>
    :root { color-scheme: dark; --ink: #f7f1dc; --muted: #bfb89f; --gold: #e5bd68; --ember: #c66a43; --night: #0d171c; --panel: #14252a; --line: rgba(229,189,104,.24); }
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
    input { width: 100%; padding: .85rem 1rem; color: var(--ink); background: #0c191d; border: 1px solid #385057; border-radius: 2px; font: 1rem/1.4 system-ui, sans-serif; }
    input:focus { outline: 2px solid var(--gold); outline-offset: 2px; }
    .message { margin: 1.25rem 0 0; padding: .8rem 1rem; color: #ffe7d8; background: rgba(198,106,67,.18); border-left: 3px solid var(--ember); }
    .dashboard { max-width: 52rem; }
    .dashboard h1 { max-width: 12ch; overflow-wrap: anywhere; }
    .dashboard-panel { margin-top: 3rem; padding: 2rem; background: rgba(20,37,42,.7); border: 1px solid var(--line); }
    .dashboard-panel h2 { margin-top: 0; font-weight: 400; }
    .dashboard-panel p { margin-bottom: 0; color: var(--muted); }
	.faction-summary { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 2rem; }
	.faction-summary h2 { margin-bottom: .35rem; font-size: clamp(1.75rem, 4vw, 2.5rem); }
	.faction-summary .label { margin: 0; color: var(--gold); font: 700 .72rem/1.2 system-ui, sans-serif; letter-spacing: .16em; text-transform: uppercase; }
	.location { min-width: 9rem; padding-left: 2rem; border-left: 1px solid var(--line); }
	.location strong { display: block; margin-top: .35rem; color: var(--ink); font: 400 1.65rem/1.2 Georgia, 'Times New Roman', serif; }
    footer { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 2rem 0 3rem; color: #898674; border-top: 1px solid var(--line); font-size: .85rem; }
    .project-meta { display: flex; align-items: center; gap: .65rem; }
    .github-link { display: flex; color: var(--muted); }
    .github-link:hover { color: var(--gold); }
    .github-link svg { width: 1.15rem; height: 1.15rem; fill: currentColor; }
    @media (max-width: 720px) { .wonders { grid-template-columns: 1fr; } .wonder { min-height: auto; } .wonder h2 { margin-top: 1.5rem; } .faction-summary { grid-template-columns: 1fr; } .location { padding: 1.5rem 0 0; border-top: 1px solid var(--line); border-left: 0; } footer { align-items: flex-start; flex-direction: column; } }
  </style>
</head>
<body>
  <div class="shell">
    <header>
      <a class="brand" href="/">Marajanda</a>
      {{if eq .View "landing"}}<a class="sign-link" href="/sign-in">Sign in</a>{{end}}
      {{if or (eq .View "admin") (eq .View "player") (eq .View "faction")}}<form class="sign-out-form" action="/sign-out" method="post"><button class="sign-link" type="submit">Sign out</button></form>{{end}}
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
		  <button class="primary" type="submit">Establish faction</button>
		</form>
	  </section>
	  {{else}}
      <section class="dashboard">
        <p class="eyebrow">{{if eq .View "admin"}}Steward of Marajanda{{else}}Faction command{{end}}</p>
        <h1>Welcome, {{.Account.Handle}}.</h1>
        <p class="lede">Your place in Marajanda is ready, even while the world beyond it is still taking shape.</p>
        <div class="dashboard-panel">
		  {{if eq .View "admin"}}
		  <h2>The realm awaits its keeper.</h2>
		  <p>Soon this dashboard will hold the instruments for guiding the server, its people, and the singular power of Marajanda.</p>
		  {{else}}
		  <div class="faction-summary">
			<div>
			  <p class="label">Your faction</p>
			  <h2>{{.Faction.Name}}</h2>
			  <p>Your people await their first command.</p>
			</div>
			<div class="location">
			  <p class="label">Current location</p>
			  <strong>({{.Faction.Location.Q}}, {{.Faction.Location.R}})</strong>
			</div>
		  </div>
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
</html>`))

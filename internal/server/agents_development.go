// Copyright (c) 2026 Michael D Henderson.

//go:build !production

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/maloquacious/phrases"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func registerAgentRoutes(mux *http.ServeMux, app *application, environment string) {
	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		return
	}
	// Each route is registered only when the handler was given what it needs.
	// A handler built without them is not a development server, and a route
	// that would panic on a nil field is worse than one that answers 404.
	if app.findOrCreateAccount != nil {
		mux.HandleFunc("GET /__agents/log-me-in/{email}", app.agentSignIn)
	}
	if app.shutdown != nil {
		mux.HandleFunc("POST /__agents/shut-it-down", app.agentShutDown)
	}
}

// agentShutDown stops the server it is served by.
//
// Nothing supervises the development daemon, so an agent that starts one has
// to stop it, and stopping it by pid means finding the pid and trusting a
// pattern match not to take more than it meant to. This is the same graceful
// shutdown --timeout and a cancelled context already produce: in-flight
// requests drain, the store closes, and Run returns nil.
//
// It is POST because it changes the world, and it takes an admin session
// rather than a secret on a command line. An agent reaches one the way it
// reaches any other: log-me-in signs in as an admin as readily as a player.
//
// The response is written and flushed before the shutdown is asked for.
// http.Server.Shutdown waits for an active request to return, so the body is
// on the wire before the listener closes and the client reads a whole answer
// rather than a reset connection.
func (app *application) agentShutDown(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.requireRole(w, r, "admin"); !ok {
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, "Marajanda is shutting down.\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	app.shutdown()
}

func (app *application) agentSignIn(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
	if !isEmail(email) {
		http.Error(w, "invalid account", http.StatusBadRequest)
		return
	}
	account, err := app.findOrCreateAccount(r.Context(), email)
	if err != nil {
		// The flag holds here too, or this route is a way around it.
		if errors.Is(err, datastore.ErrAccountInactive) {
			http.Error(w, "that account is not active", http.StatusForbidden)
			return
		}
		app.serverError(w, r, err, "Marajanda could not create the development session.")
		return
	}
	account, err = app.ensureAgentFaction(r.Context(), account)
	if err != nil {
		app.serverError(w, r, err, "Marajanda could not create the development session.")
		return
	}
	if err := app.startSession(r.Context(), w, account); err != nil {
		app.serverError(w, r, err, "Marajanda could not create the development session.")
		return
	}
	http.Redirect(w, r, safeReturnPath(r.URL.Query().Get("returnTo")), http.StatusSeeOther)
}

// ensureAgentFaction gives a development account a faction so that the player
// dashboard has nothing to divert the agent away from. Without it the route
// lands on the configuration form no matter what returnTo asked for, because a
// player with no faction cannot reach any player page.
//
// It fills the gap for an account it just created and for one created before
// this route did so. Only players control factions, so admins are left alone,
// and a faction that is already configured is never overwritten.
//
// Configuring the faction is also what seats the account, so the account it
// returns - not the unseated one it was handed - is the one the session gets.
// The race is always the default: a development sign-in picks nothing, and
// drawing one would make the same agent land somewhere new each time.
func (app *application) ensureAgentFaction(ctx context.Context, account datastore.Account) (datastore.Account, error) {
	if app.store == nil || account.Role != "player" {
		return account, nil
	}
	faction, found, err := app.store.Faction(ctx, account.Email)
	if err != nil {
		return account, err
	}
	if found && faction.Configured() {
		return account, nil
	}
	generated, err := agentFactionName()
	if err != nil {
		return account, err
	}
	name, err := game.NormalizeFactionName(generated)
	if err != nil {
		return account, err
	}
	return app.store.SaveFaction(ctx, account.Email, name, game.DefaultRace)
}

// agentFactionName builds a faction name from the passphrase generator.
//
// The generator draws on math/rand/v2, never the game PRNG: a convenience name
// is not game state, and addressing it to a deterministic stream would make a
// development sign-in change what that stream produces.
//
// Two words of the wordlist run from seven to thirteen characters together, so
// the result always satisfies the three-to-thirty-two faction rule. Every word
// is lowercase ASCII, so capitalizing the leading byte is safe.
func agentFactionName() (string, error) {
	phrase, err := phrases.Generate(2, " ")
	if err != nil {
		return "", err
	}
	words := strings.Fields(phrase)
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " "), nil
}

func safeReturnPath(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || value == "" || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" {
		return "/"
	}
	if !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, "\\") {
		return "/"
	}
	return value
}

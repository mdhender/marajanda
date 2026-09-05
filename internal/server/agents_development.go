// Copyright (c) 2026 Michael D Henderson.

//go:build !production

package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
	phrases "github.com/mdhender/phrases/v2"
)

func registerAgentRoutes(mux *http.ServeMux, app *application, environment string) {
	if strings.EqualFold(strings.TrimSpace(environment), "production") || app.findOrCreateAccount == nil {
		return
	}
	mux.HandleFunc("GET /__agents/log-me-in/{email}", app.agentSignIn)
}

func (app *application) agentSignIn(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
	if !isEmail(email) {
		http.Error(w, "invalid account", http.StatusBadRequest)
		return
	}
	account, err := app.findOrCreateAccount(r.Context(), email)
	if err != nil {
		http.Error(w, "Marajanda could not create the development session.", http.StatusInternalServerError)
		return
	}
	if err := app.ensureAgentFaction(r.Context(), account); err != nil {
		http.Error(w, "Marajanda could not create the development session.", http.StatusInternalServerError)
		return
	}
	if err := app.startSession(w, account); err != nil {
		http.Error(w, "Marajanda could not create the development session.", http.StatusInternalServerError)
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
func (app *application) ensureAgentFaction(ctx context.Context, account datastore.Account) error {
	if app.store == nil || account.Role != "player" {
		return nil
	}
	faction, found, err := app.store.Faction(ctx, account.Email)
	if err != nil {
		return err
	}
	if found && faction.Configured() {
		return nil
	}
	name, err := game.NormalizeFactionName(agentFactionName())
	if err != nil {
		return err
	}
	return app.store.SaveFaction(ctx, account.Email, name)
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
func agentFactionName() string {
	words := strings.Fields(phrases.Generate(2, " "))
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
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

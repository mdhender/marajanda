// Copyright (c) 2026 Michael D Henderson.

//go:build !production

package server

import (
	"net/http"
	"net/url"
	"strings"
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
	if err := app.startSession(w, account); err != nil {
		http.Error(w, "Marajanda could not create the development session.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, safeReturnPath(r.URL.Query().Get("returnTo")), http.StatusSeeOther)
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

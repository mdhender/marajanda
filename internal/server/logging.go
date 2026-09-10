// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"errors"
	"log/slog"
	"net/http"
)

// errStoreNotConfigured is the failure behind a handler that was built without
// a store and then asked to serve a page that needs one. It is a programming
// error rather than a runtime one, which is exactly why it is worth logging:
// the client is told the same sentence a database failure produces, so the log
// is the only thing that tells the two apart.
var errStoreNotConfigured = errors.New("datastore is not configured")

// errAuthenticateNotConfigured is the same kind of failure as
// errStoreNotConfigured, for a handler asked to authenticate without anything
// to authenticate against.
var errAuthenticateNotConfigured = errors.New("authenticator is not configured")

// serverLogger returns the logger a handler should hold.
//
// A nil logger is a handler that was built without one, which every test does
// and which a caller may do on purpose. It discards rather than falling back to
// the package-level default: nothing here writes to the global logger, so a
// handler nobody gave a logger to is silent instead of writing wherever the
// process happens to point.
func serverLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.DiscardHandler)
}

// logError records a failure a handler is about to answer for.
//
// This is the only place the package logs. The message a client is given is
// deliberately vague, so the error itself has to land somewhere a person can
// read it, and every 500 in this package goes through here to get there.
//
// The method and route are the two things that make a line searchable. The
// route is r.Pattern - the pattern the mux matched, not the URL - so a request
// for an entity nobody else has logs the same line as everyone else's rather
// than one carrying an identifier. Nothing else from the request is recorded:
// a session token, a path value or a form field would be a credential or a
// player's own business, and neither belongs in a log.
func (app *application) logError(r *http.Request, err error, message string) {
	app.logger.Error(message,
		slog.String("method", r.Method),
		slog.String("route", r.Pattern),
		slog.Any("error", err),
	)
}

// serverError answers an HTML request that failed inside the server.
//
// message is what the client is told and what the log line is titled, so the
// two read as the same event seen from either side. err is what actually
// happened and never reaches the client.
func (app *application) serverError(w http.ResponseWriter, r *http.Request, err error, message string) {
	app.logError(r, err, message)
	http.Error(w, message, http.StatusInternalServerError)
}

// apiInternalError answers an API request that failed inside the server.
//
// The envelope is fixed - one code and one sentence for every internal failure,
// because an API client is told no more than a browser is. What the failure was
// goes to the log.
func (app *application) apiInternalError(w http.ResponseWriter, r *http.Request, err error) {
	app.logError(r, err, "the server could not complete the request")
	writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
}

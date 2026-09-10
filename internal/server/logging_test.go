// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
)

// loggingHandler builds a handler that logs into a buffer the caller can read.
// The buffer is returned rather than the logger, because what a test wants to
// know is what came out.
func loggingHandler(t *testing.T, store applicationStore, account datastore.Account) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := newConfiguredHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return account, true, nil
	}, nil, store, "production", nil, logger)
	return handler, &logged
}

// A 500 tells the client nothing on purpose. The error it is hiding has to
// reach the log, or nobody can find out what happened.
func TestAFailedPageLogsTheErrorItDoesNotDisclose(t *testing.T) {
	failure := errors.New("the database went away")
	admin := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	store := &testStore{sessionAccount: admin, gameErr: failure}
	handler, logged := loggingHandler(t, store, admin)

	cookie := submitSignIn(handler, admin.Email, "good.luck").Result().Cookies()[0]
	response := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", cookie, "")

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if body := response.Body.String(); strings.Contains(body, failure.Error()) {
		t.Fatalf("the client was told what went wrong: %s", body)
	}

	line := logged.String()
	for _, want := range []string{
		failure.Error(),
		"Marajanda could not load the game.",
		"method=GET",
		"/admin/dashboard",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("the log does not carry %q: %s", want, line)
		}
	}
}

// The route logged is the pattern the mux matched, never the URL. A path value
// is a player's own business, and an identifier in a log line is one more place
// it has to be looked after.
func TestTheLogNamesTheRouteRatherThanTheURL(t *testing.T) {
	player := datastore.Account{Email: "player@example.com", Handle: "chip", Role: "player"}
	store := ordersStore()
	store.sessionAccount = player
	store.orderErr = errors.New("write failed")
	handler, logged := loggingHandler(t, store, player)

	cookie := submitSignIn(handler, player.Email, "good.luck").Result().Cookies()[0]
	requestWithCookie(handler, http.MethodDelete, "/player/orders/7/2", cookie, "")

	line := logged.String()
	if strings.Contains(line, "/player/orders/7/2") {
		t.Fatalf("the log carries the URL and its path values: %s", line)
	}
	if !strings.Contains(line, "/player/orders/{entity}/{seq}") {
		t.Fatalf("the log does not name the matched route: %s", line)
	}
}

// An API failure answers in the envelope and logs the same way a page does.
func TestAFailedAPIRequestLogsTheErrorItDoesNotDisclose(t *testing.T) {
	failure := errors.New("no such table: sessions")
	admin := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	store := &testStore{sessionAccount: admin, sessionErr: failure}
	handler, logged := loggingHandler(t, store, admin)

	response := apiRequest(handler, http.MethodPost, "/api/v1/sessions",
		`{"email":"admin@example.com","passphrase":"good.luck"}`, nil)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if body := response.Body.String(); strings.Contains(body, failure.Error()) {
		t.Fatalf("the envelope discloses the failure: %s", body)
	}
	if line := logged.String(); !strings.Contains(line, failure.Error()) {
		t.Fatalf("the log does not carry the failure: %s", line)
	}
}

// A handler built without a logger is silent. It does not fall back to the
// package-level default, because a library that writes to the global logger
// writes wherever its embedder was not expecting.
func TestAHandlerWithoutALoggerIsSilentRatherThanGlobal(t *testing.T) {
	var global bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&global, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	admin := datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin"}
	store := &testStore{sessionAccount: admin, gameErr: errors.New("the database went away")}
	handler := newConfiguredHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return admin, true, nil
	}, nil, store, "production", nil, nil)

	cookie := submitSignIn(handler, admin.Email, "good.luck").Result().Cookies()[0]
	response := requestWithCookie(handler, http.MethodGet, "/admin/dashboard", cookie, "")

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if global.Len() != 0 {
		t.Fatalf("a handler with no logger wrote to the default one: %s", global.String())
	}
}

// globalLoggerCalls are the slog functions that write to, or replace, the
// package-level logger. Constructors and attribute helpers are not here:
// slog.New, slog.String and their kind are how a logger is built and how a line
// is furnished, and neither reaches the global one.
var globalLoggerCalls = map[string]bool{
	"Debug": true, "DebugContext": true,
	"Info": true, "InfoContext": true,
	"Warn": true, "WarnContext": true,
	"Error": true, "ErrorContext": true,
	"Log": true, "LogAttrs": true,
	"Default": true, "SetDefault": true, "SetLogLoggerLevel": true,
}

// The server logs through the logger it was given and no other. This is a rule
// about the package rather than about one call, so it is checked over the
// package's own source: a handler that reaches for slog.Error writes to
// wherever the process points, which is exactly what passing a logger in is
// meant to prevent.
func TestNothingInThePackageUsesTheGlobalLogger(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "slog" || !globalLoggerCalls[selector.Sel.Name] {
				return true
			}
			t.Errorf("%s: slog.%s writes to the package-level logger; use the logger the handler holds",
				fileSet.Position(call.Pos()), selector.Sel.Name)
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no source files were checked")
	}
}

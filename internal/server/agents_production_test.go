// Copyright (c) 2026 Michael D Henderson.

//go:build production

package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
)

func TestProductionBuildOmitsAgentSignIn(t *testing.T) {
	handler := newConfiguredHandler(nil, func(context.Context, string) (datastore.Account, error) {
		t.Fatal("production build called account lookup")
		return datastore.Account{}, nil
	}, nil, "development", nil, nil)
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

// The production build omits the shutdown route with the rest of them. A
// production server is stopped by whatever supervises it, never by a request.
func TestProductionBuildOmitsAgentShutDown(t *testing.T) {
	handler := newConfiguredHandler(nil, nil, nil, "development", func() {
		t.Fatal("production build stopped the server")
	}, nil)
	response := serveRequest(handler, http.MethodPost, "/__agents/shut-it-down")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

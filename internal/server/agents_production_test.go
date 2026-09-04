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
	}, nil, "development")
	response := serveRequest(handler, http.MethodGet, "/__agents/log-me-in/agent@example.test")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

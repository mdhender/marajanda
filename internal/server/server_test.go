// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdhender/marajanda/internal/datastore"
)

func TestHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)

	newHandler(nil, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/healthz", nil)

	newHandler(nil, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestRunStopsAfterTimeout(t *testing.T) {
	start := time.Now()
	err := Run(t.Context(), Config{
		Root:    ":memory:",
		Game:    new(datastore.Game{Seed1: 98374, Seed2: -98}),
		Address: DefaultAddress,
		Port:    0,
		Timeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond || elapsed > time.Second {
		t.Fatalf("elapsed = %s, want between 10ms and 1s", elapsed)
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "port", cfg: Config{Port: -1}, want: "invalid server port"},
		{name: "timeout", cfg: Config{Timeout: -time.Second}, want: "invalid server timeout"},
		{name: "game seed", cfg: Config{Root: ":memory:"}, want: "game seed is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Run(t.Context(), test.cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Run error = %v, want %q", err, test.want)
			}
		})
	}
}

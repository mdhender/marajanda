// Copyright (c) 2026 Michael D Henderson.

// Package server owns Marajanda server startup and lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mdhender/marajanda/internal/datastore"
)

const (
	// DefaultAddress is the network address used when none is configured.
	DefaultAddress = "127.0.0.1"
	// DefaultPort is the network port used when none is configured.
	DefaultPort = 8443
)

// Config contains server startup configuration.
type Config struct {
	Root        string
	AdminEmail  string
	AdminSecret string
	AdminHandle string
	Game        *datastore.Game
	Environment string
	Address     string
	Port        int
	Timeout     time.Duration
	// Logger receives failures the server answers for but does not disclose.
	// A nil logger discards: the server never writes to the package-level
	// default, so a caller that wants output has to supply one.
	Logger *slog.Logger
}

// Run initializes the datastore and serves HTTP until the context is canceled
// or the configured timeout expires.
func Run(ctx context.Context, cfg Config) (err error) {
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid server port %d", cfg.Port)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("invalid server timeout %s", cfg.Timeout)
	}

	var store *datastore.Store
	if cfg.Root == ":memory:" {
		if cfg.Game == nil {
			return errors.New("game seed is required when seeding a new database")
		}
		store, err = datastore.OpenMemory(ctx, *cfg.Game)
		if err != nil {
			return err
		}
	} else {
		if err := os.Chdir(cfg.Root); err != nil {
			return fmt.Errorf("change to server root %q: %w", cfg.Root, err)
		}
		store, err = datastore.Open(ctx, ".", datastore.SeedAccount{
			Email:  cfg.AdminEmail,
			Secret: cfg.AdminSecret,
			Handle: cfg.AdminHandle,
		}, cfg.Game)
		if err != nil {
			return err
		}
	}
	defer func() {
		err = errors.Join(err, store.Close())
	}()

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}

	// The context that decides how long the server serves, built before the
	// handler because the handler is given a way to end it. Three things can:
	// the caller's context, --timeout, and the development shutdown route.
	// They all arrive here, so there is one way out of Run rather than three.
	serveCtx := ctx
	if cfg.Timeout > 0 {
		var timedOut context.CancelFunc
		serveCtx, timedOut = context.WithTimeout(serveCtx, cfg.Timeout)
		defer timedOut()
	}
	serveCtx, shutdown := context.WithCancel(serveCtx)
	defer shutdown()

	httpServer := &http.Server{Handler: newConfiguredHandler(store.Authenticate, store.FindOrCreateDevelopmentAccount, store, cfg.Environment, shutdown, cfg.Logger)}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-serveCtx.Done():
		if err := httpServer.Shutdown(context.WithoutCancel(ctx)); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}
}

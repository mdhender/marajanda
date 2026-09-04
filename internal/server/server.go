// Copyright (c) 2026 Michael D Henderson.

// Package server owns Marajanda server startup and lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
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
	Environment string
	Address     string
	Port        int
	Timeout     time.Duration
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
		store, err = datastore.OpenMemory(ctx)
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
		})
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

	httpServer := &http.Server{Handler: newConfiguredHandler(store.Authenticate, store.FindOrCreateDevelopmentAccount, store, cfg.Environment)}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	serveCtx := ctx
	cancel := func() {}
	if cfg.Timeout > 0 {
		serveCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
	}
	defer cancel()

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

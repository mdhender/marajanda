// Copyright (c) 2026 Michael D Henderson.

// Package server owns Marajanda server startup and lifecycle.
package server

import (
	"context"
	"fmt"
	"os"

	"github.com/mdhender/marajanda/internal/datastore"
)

// Config contains server startup configuration.
type Config struct {
	Root        string
	AdminEmail  string
	AdminSecret string
	AdminHandle string
}

// Run initializes the server datastore. HTTP serving is intentionally not yet implemented.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Root == ":memory:" {
		store, err := datastore.OpenMemory(ctx)
		if err != nil {
			return err
		}
		return store.Close()
	}

	if err := os.Chdir(cfg.Root); err != nil {
		return fmt.Errorf("change to server root %q: %w", cfg.Root, err)
	}
	store, err := datastore.Open(ctx, ".", datastore.SeedAccount{
		Email:  cfg.AdminEmail,
		Secret: cfg.AdminSecret,
		Handle: cfg.AdminHandle,
	})
	if err != nil {
		return err
	}
	return store.Close()
}

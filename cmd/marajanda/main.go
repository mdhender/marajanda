// Copyright (c) 2026 Michael D Henderson.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/dotenv"
	"github.com/mdhender/marajanda/internal/server"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

const envPrefix = "MARAJANDA"

func main() {
	environment := cmp.Or(os.Getenv("ENV"), "development")
	if err := dotenv.Load(environment); err != nil {
		fmt.Fprintf(os.Stderr, "marajanda: load %s environment: %v\n", environment, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "marajanda: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := ff.NewFlagSet("marajanda")
	root := flags.StringLong("root", "", "path containing all server files, or :memory:")
	adminEmail := flags.StringLong("admin-email", "", "initial admin email for a new persistent database")
	adminSecret := flags.StringLong("admin-secret", "", "initial admin secret for a new persistent database")
	adminHandle := flags.StringLong("admin-handle", "", "initial admin handle for a new persistent database")
	var game *datastore.Game
	flags.FuncConfigVar(ff.FlagConfig{
		LongName:    "game-seed",
		Placeholder: "SEED1,SEED2",
		Usage:       "two comma-separated int64 PRNG seeds for a new database",
	}, func(value string) error {
		parsed, err := parseGameSeed(value)
		if err != nil {
			return err
		}
		game = parsed
		return nil
	})
	address := flags.StringLong("address", server.DefaultAddress, "network address to listen on")
	port := flags.IntLong("port", server.DefaultPort, "network port to listen on")
	timeout := flags.DurationLong("timeout", 0, "stop the server after this duration; zero disables the timeout")

	command := &ff.Command{
		Name:      "marajanda",
		Usage:     "marajanda --root PATH [FLAGS]",
		ShortHelp: "run the Marajanda server",
		Flags:     flags,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unexpected arguments: %v", args)
			}
			if *root == "" {
				return errors.New("--root is required")
			}
			return server.Run(ctx, server.Config{
				Root:        *root,
				AdminEmail:  *adminEmail,
				AdminSecret: *adminSecret,
				AdminHandle: *adminHandle,
				Game:        game,
				Environment: cmp.Or(os.Getenv("ENV"), "development"),
				Address:     *address,
				Port:        *port,
				Timeout:     *timeout,
			})
		},
	}

	err := command.ParseAndRun(ctx, args,
		ff.WithEnvVarPrefix(envPrefix),
		ff.WithConfigIgnoreUndefinedFlags(),
	)
	if errors.Is(err, ff.ErrHelp) {
		fmt.Fprint(stdout, ffhelp.Command(command))
		return nil
	}
	return err
}

func parseGameSeed(value string) (*datastore.Game, error) {
	first, second, ok := strings.Cut(value, ",")
	if !ok || strings.Contains(second, ",") {
		return nil, errors.New("--game-seed must contain exactly two comma-separated int64 values")
	}
	seed1, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse first --game-seed value: %w", err)
	}
	seed2, err := strconv.ParseInt(strings.TrimSpace(second), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse second --game-seed value: %w", err)
	}
	return new(datastore.Game{Seed1: seed1, Seed2: seed2}), nil
}

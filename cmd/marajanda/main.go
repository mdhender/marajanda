// Copyright (c) 2026 Michael D Henderson.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mdhender/marajanda/internal/dotenv"
	"github.com/mdhender/marajanda/internal/server"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

const envPrefix = "MARAJANDA"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "marajanda: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	environment := cmp.Or(os.Getenv("MARAJANDA_ENV"), "development")
	if err := dotenv.Load(environment); err != nil {
		return fmt.Errorf("load %s environment: %w", environment, err)
	}

	flags := ff.NewFlagSet("marajanda")
	root := flags.StringLong("root", "", "path containing all server files, or :memory:")
	adminEmail := flags.StringLong("admin-email", "", "initial admin email for a new persistent database")
	adminSecret := flags.StringLong("admin-secret", "", "initial admin secret for a new persistent database")
	adminHandle := flags.StringLong("admin-handle", "", "initial admin handle for a new persistent database")

	command := &ff.Command{
		Name:      "marajanda",
		Usage:     "marajanda --root PATH [FLAGS]",
		ShortHelp: "initialize the Marajanda server",
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

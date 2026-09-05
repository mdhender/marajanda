// Copyright (c) 2026 Michael D Henderson.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdhender/marajanda/internal/datastore"
)

func TestRunRequiresRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetenv(t, "MARAJANDA_ROOT")
	if err := run(t.Context(), nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--root is required") {
		t.Fatalf("run error = %v", err)
	}
}

func TestRunUsesConfiguredEnvironment(t *testing.T) {
	workingDirectory := t.TempDir()
	root := filepath.Join(workingDirectory, "server")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Chdir(workingDirectory)
	t.Setenv("MARAJANDA_ROOT", root)
	t.Setenv("MARAJANDA_ADMIN_EMAIL", "ADMIN@EXAMPLE.COM")
	t.Setenv("MARAJANDA_ADMIN_SECRET", "test-only-value")
	t.Setenv("MARAJANDA_ADMIN_HANDLE", "keeper")
	t.Setenv("MARAJANDA_GAME_SEED", "98374,-98")
	t.Setenv("MARAJANDA_ADDRESS", "127.0.0.1")
	t.Setenv("MARAJANDA_PORT", "0")
	t.Setenv("MARAJANDA_TIMEOUT", "1ms")
	if err := run(t.Context(), nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, datastore.Filename)); err != nil {
		t.Fatalf("stat database: %v", err)
	}
	store, err := datastore.Open(t.Context(), root, datastore.SeedAccount{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.Game(t.Context())
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if game != (datastore.Game{Seed1: 98374, Seed2: -98}) {
		t.Fatalf("Game = %#v, want configured seeds", game)
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, rootInfo) {
		t.Fatalf("working directory = %q, want %q", got, root)
	}
}

func TestRunMemoryUsesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{
		"MARAJANDA_ADMIN_EMAIL",
		"MARAJANDA_ADMIN_SECRET",
		"MARAJANDA_ADMIN_HANDLE",
		"MARAJANDA_GAME_SEED",
	} {
		unsetenv(t, name)
	}
	if err := run(t.Context(), []string{"--root", ":memory:", "--game-seed", "98374,-98", "--port", "0", "--timeout", "1ms"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunMemoryRequiresGameSeed(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetenv(t, "MARAJANDA_GAME_SEED")
	err := run(t.Context(), []string{"--root", ":memory:", "--port", "0", "--timeout", "1ms"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "game seed is required") {
		t.Fatalf("run error = %v, want required game seed", err)
	}
}

func TestRunExistingPersistentDatabaseDoesNotRequireGameSeed(t *testing.T) {
	workingDirectory := t.TempDir()
	root := filepath.Join(workingDirectory, "server")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := datastore.Open(t.Context(), root, datastore.SeedAccount{
		Email: "admin@example.com", Secret: "temporary", Handle: "admin",
	}, new(datastore.Game{Seed1: 1, Seed2: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	t.Chdir(workingDirectory)
	for _, name := range []string{"MARAJANDA_GAME_SEED", "MARAJANDA_ADMIN_EMAIL", "MARAJANDA_ADMIN_SECRET", "MARAJANDA_ADMIN_HANDLE"} {
		unsetenv(t, name)
	}
	if err := run(t.Context(), []string{"--root", root, "--port", "0", "--timeout", "1ms"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestParseGameSeed(t *testing.T) {
	for _, test := range []struct {
		input string
		want  datastore.Game
	}{
		{input: "98374,-98", want: datastore.Game{Seed1: 98374, Seed2: -98}},
		{input: " -9223372036854775808 , 9223372036854775807 ", want: datastore.Game{Seed1: -9223372036854775808, Seed2: 9223372036854775807}},
		{input: "0,0", want: datastore.Game{}},
	} {
		game, err := parseGameSeed(test.input)
		if err != nil {
			t.Errorf("parseGameSeed(%q): %v", test.input, err)
			continue
		}
		if *game != test.want {
			t.Errorf("parseGameSeed(%q) = %#v, want %#v", test.input, *game, test.want)
		}
	}
}

func TestParseGameSeedRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "1", "1,2,3", "one,2", "1,two", "9223372036854775808,0", "0,-9223372036854775809"} {
		if _, err := parseGameSeed(input); err == nil {
			t.Errorf("parseGameSeed(%q) succeeded, want error", input)
		}
	}
}

func TestRunHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, &stdout); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--root", "--game-seed SEED1,SEED2", "--address", "--port", "8443", "--timeout"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help = %q, want %q", stdout.String(), want)
		}
	}
}

func unsetenv(t *testing.T, name string) {
	t.Helper()
	old, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

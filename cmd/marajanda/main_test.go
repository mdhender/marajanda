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
	t.Setenv("MARAJANDA_ADDRESS", "127.0.0.1")
	t.Setenv("MARAJANDA_PORT", "0")
	t.Setenv("MARAJANDA_TIMEOUT", "1ms")
	if err := run(t.Context(), nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, datastore.Filename)); err != nil {
		t.Fatalf("stat database: %v", err)
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
	} {
		unsetenv(t, name)
	}
	if err := run(t.Context(), []string{"--root", ":memory:", "--port", "0", "--timeout", "1ms"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, &stdout); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--root", "--address", "--port", "8443", "--timeout"} {
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

// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestOpenPersistentCreatesMigratesAndSeeds(t *testing.T) {
	root := t.TempDir()
	store, err := Open(t.Context(), root, SeedAccount{
		Email:  "ADMIN@EXAMPLE.COM",
		Secret: "temporary",
		Handle: "keeper",
	})
	if err != nil {
		t.Fatal(err)
	}

	account := readAccount(t, store, "admin@example.com")
	if account.handle != "keeper" || account.role != "admin" {
		t.Fatalf("account = %#v, want keeper admin", account)
	}
	if err := bcrypt.CompareHashAndPassword(account.hash, []byte("temporary")); err != nil {
		t.Fatalf("compare password hash: %v", err)
	}
	if cost, err := bcrypt.Cost(account.hash); err != nil || cost != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, %v; want %d", cost, err, bcrypt.MinCost)
	}
	assertPragma(t, store, "application_id", int64(ApplicationID))
	assertPragma(t, store, "user_version", int64(len(schema.Migrations)))
	assertPragma(t, store, "foreign_keys", 1)
	assertTextPragma(t, store, "journal_mode", "wal")
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, Filename)); err != nil {
		t.Fatalf("stat database: %v", err)
	}
	reopened, err := Open(t.Context(), root, SeedAccount{})
	if err != nil {
		t.Fatalf("reopen without seed configuration: %v", err)
	}
	defer reopened.Close()
	if got := accountCount(t, reopened); got != 1 {
		t.Fatalf("account count after reopen = %d, want 1", got)
	}
}

func TestOpenPersistentRejectsMissingRootAndMissingSeed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Open(t.Context(), missing, SeedAccount{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open missing root error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root was created: %v", err)
	}

	root := t.TempDir()
	if _, err := Open(t.Context(), root, SeedAccount{}); err == nil || !strings.Contains(err.Error(), "admin email is required") {
		t.Fatalf("Open missing seed error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, Filename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database created despite invalid seed: %v", err)
	}
}

func TestOpenPersistentRejectsNewerSchema(t *testing.T) {
	root := t.TempDir()
	store, err := Open(t.Context(), root, SeedAccount{
		Email: "admin@example.com", Secret: "temporary", Handle: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sqlite.OpenConn(filepath.Join(root, Filename), sqlite.OpenReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA user_version = 2;", nil); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(t.Context(), root, SeedAccount{})
	if err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("Open newer schema error = %v", err)
	}
}

func TestOpenMemorySeedsDefaults(t *testing.T) {
	store, err := OpenMemory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := accountCount(t, store); got != 2 {
		t.Fatalf("account count = %d, want 2", got)
	}
	for _, email := range []string{"admin@marajanda.com", "player@marajanda.com"} {
		account := readAccount(t, store, email)
		if err := bcrypt.CompareHashAndPassword(account.hash, []byte("good.luck")); err != nil {
			t.Fatalf("%s password: %v", email, err)
		}
	}
	assertPragma(t, store, "foreign_keys", 1)
	assertTextPragma(t, store, "journal_mode", "memory")
}

func TestAuthenticate(t *testing.T) {
	store, err := OpenMemory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, ok, err := store.Authenticate(t.Context(), " ADMIN@MARAJANDA.COM ", "good.luck")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || account.Handle != "admin" || account.Role != "admin" {
		t.Fatalf("Authenticate = %#v, %t; want admin account, true", account, ok)
	}

	for _, test := range []struct {
		name   string
		email  string
		secret string
	}{
		{name: "unknown account", email: "missing@marajanda.com", secret: "good.luck"},
		{name: "wrong secret", email: "admin@marajanda.com", secret: "not.right"},
	} {
		t.Run(test.name, func(t *testing.T) {
			account, ok, err := store.Authenticate(t.Context(), test.email, test.secret)
			if err != nil {
				t.Fatal(err)
			}
			if ok || account != (Account{}) {
				t.Fatalf("Authenticate = %#v, %t; want zero account, false", account, ok)
			}
		})
	}
}

func TestOpenSharedMemoryUsesNamedDatabase(t *testing.T) {
	first, err := OpenSharedMemory(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := OpenSharedMemory(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := accountCount(t, second); got != 2 {
		t.Fatalf("account count from second pool = %d, want 2", got)
	}
	assertPragma(t, second, "foreign_keys", 1)
}

type storedAccount struct {
	hash   []byte
	handle string
	role   string
}

func readAccount(t *testing.T, store *Store, email string) storedAccount {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var account storedAccount
	err = sqlitex.ExecuteTransient(conn, `
		SELECT secret_hash, handle, role FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{email},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.hash = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, account.hash)
			account.handle = stmt.ColumnText(1)
			account.role = stmt.ColumnText(2)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.hash == nil {
		t.Fatalf("account %q not found", email)
	}
	return account
}

func accountCount(t *testing.T, store *Store) int64 {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var count int64
	if err := sqlitex.ExecuteTransient(conn, "SELECT count(*) FROM accounts;", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertPragma(t *testing.T, store *Store, name string, want int64) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var got int64
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA "+name+";", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			got = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %d, want %d", name, got, want)
	}
}

func assertTextPragma(t *testing.T, store *Store, name, want string) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var got string
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA "+name+";", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			got = stmt.ColumnText(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %q, want %q", name, got, want)
	}
}

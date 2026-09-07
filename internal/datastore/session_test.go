// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func sessionToken(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, SessionTokenBytes)
}

func TestSessionLifecycleInEveryMemoryMode(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		token := sessionToken(0x42)
		account := Account{Email: "PLAYER@MARAJANDA.COM"}
		if err := store.CreateSession(t.Context(), token, account); err != nil {
			t.Fatal(err)
		}

		got, found, err := store.ResolveSession(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		if !found || got.Email != "player@marajanda.com" || got.Handle != "player" || got.Role != "player" {
			t.Fatalf("ResolveSession = %#v, %t; want seeded player", got, found)
		}
		if _, found, err := store.ResolveSession(t.Context(), sessionToken(0x43)); err != nil || found {
			t.Fatalf("wrong token resolved = %t, %v; want clean miss", found, err)
		}

		if err := store.DeleteSession(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		if _, found, err := store.ResolveSession(t.Context(), token); err != nil || found {
			t.Fatalf("revoked token resolved = %t, %v; want clean miss", found, err)
		}
		if err := store.DeleteSession(t.Context(), token); err != nil {
			t.Fatalf("second revoke: %v", err)
		}
	})
}

func TestSessionStoresOnlyAHashAndCascadesWithAccount(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		token := sessionToken(0x61)
		if err := store.CreateSession(t.Context(), token, Account{Email: "player@marajanda.com"}); err != nil {
			t.Fatal(err)
		}

		conn, release, err := store.take(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var stored []byte
		if err := sqlitex.ExecuteTransient(conn, `SELECT token_hash FROM sessions;`, &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				stored = make([]byte, stmt.ColumnLen(0))
				stmt.ColumnBytes(0, stored)
				return nil
			},
		}); err != nil {
			release()
			t.Fatal(err)
		}
		wantHash := sha256.Sum256(token)
		if !bytes.Equal(stored, wantHash[:]) || bytes.Equal(stored, token) {
			release()
			t.Fatalf("stored token = %x, want SHA-256 hash and not raw token", stored)
		}
		if err := sqlitex.ExecuteTransient(conn, `DELETE FROM accounts WHERE email = 'player@marajanda.com';`, nil); err != nil {
			release()
			t.Fatal(err)
		}
		release()

		if _, found, err := store.ResolveSession(t.Context(), token); err != nil || found {
			t.Fatalf("session after account deletion = %t, %v; want cascaded miss", found, err)
		}
	})
}

func TestDuplicateSessionTokenDoesNotChangeItsAccount(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		token := sessionToken(0x71)
		if err := store.CreateSession(t.Context(), token, Account{Email: "player@marajanda.com"}); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateSession(t.Context(), token, Account{Email: "admin@marajanda.com"}); sqlite.ErrCode(err).ToPrimary() != sqlite.ResultConstraint {
			t.Fatalf("duplicate CreateSession error = %v (%v), want constraint", err, sqlite.ErrCode(err))
		}
		got, found, err := store.ResolveSession(t.Context(), token)
		if err != nil || !found || got.Email != "player@marajanda.com" {
			t.Fatalf("session after collision = %#v, %t, %v; want original player", got, found, err)
		}
	})
}

func TestExistingSessionResolvesForDeactivatedAccount(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		token := sessionToken(0x51)
		if err := store.CreateSession(t.Context(), token, Account{Email: "player@marajanda.com"}); err != nil {
			t.Fatal(err)
		}
		conn, release, err := store.take(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		err = sqlitex.ExecuteTransient(conn, `UPDATE accounts SET is_active = 0 WHERE email = 'player@marajanda.com';`, nil)
		release()
		if err != nil {
			t.Fatal(err)
		}

		got, found, err := store.ResolveSession(t.Context(), token)
		if err != nil || !found || got.Active {
			t.Fatalf("deactivated session = %#v, %t, %v; want resolved inactive account", got, found, err)
		}
		if _, ok, err := store.Authenticate(t.Context(), "player@marajanda.com", "good.luck"); err == nil || ok {
			t.Fatalf("new authentication = %t, %v; want deactivation refusal", ok, err)
		}
	})
}

func TestConcurrentSessionLookupAndRevocation(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		token := sessionToken(0x81)
		if err := store.CreateSession(t.Context(), token, Account{Email: "player@marajanda.com"}); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		errs := make(chan error, 9)
		var ready sync.WaitGroup
		ready.Add(8)
		var done sync.WaitGroup
		for range 8 {
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				_, _, err := store.ResolveSession(t.Context(), token)
				errs <- err
			}()
		}
		ready.Wait()
		close(start)
		done.Add(1)
		go func() {
			defer done.Done()
			errs <- store.DeleteSession(t.Context(), token)
		}()
		done.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("concurrent operation: %v", err)
			}
		}
		if _, found, err := store.ResolveSession(t.Context(), token); err != nil || found {
			t.Fatalf("final resolution = %t, %v; want revoked miss", found, err)
		}
	})
}

func TestSessionSurvivesPersistentReopenUntilRevoked(t *testing.T) {
	root := t.TempDir()
	store, err := Open(t.Context(), root, SeedAccount{
		Email: "admin@example.com", Secret: "temporary", Handle: "keeper",
	}, new(testGame))
	if err != nil {
		t.Fatal(err)
	}
	token := sessionToken(0x91)
	if err := store.CreateSession(t.Context(), token, Account{Email: "admin@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(t.Context(), root, SeedAccount{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.ResolveSession(t.Context(), token); err != nil || !found || got.Email != "admin@example.com" {
		t.Fatalf("session after reopen = %#v, %t, %v", got, found, err)
	}
	if err := store.DeleteSession(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(t.Context(), root, SeedAccount{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, found, err := store.ResolveSession(t.Context(), token); err != nil || found {
		t.Fatalf("revoked session after reopen = %t, %v; want miss", found, err)
	}
}

func TestSessionRejectsInvalidCreationAndDoesNotAuthenticateOnStorageFailure(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(t.Context(), []byte("short"), Account{Email: "player@marajanda.com"}); err == nil {
		t.Fatal("CreateSession accepted a short token")
	}
	if _, found, err := store.ResolveSession(t.Context(), []byte("short")); err != nil || found {
		t.Fatalf("malformed token = %t, %v; want clean miss", found, err)
	}
	token := sessionToken(0xa1)
	if err := store.CreateSession(t.Context(), token, Account{Email: "player@marajanda.com"}); err != nil {
		t.Fatal(err)
	}
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = sqlitex.ExecuteTransient(conn, `DROP TABLE sessions;`, nil)
	release()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, found, err := store.ResolveSession(t.Context(), token); err == nil || found {
		t.Fatalf("resolution after storage failure = %t, %v; want error and no authentication", found, err)
	}
}

// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maloquacious/hexg"
	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var testGame = Game{Seed1: 98374, Seed2: -98}

func TestOpenPersistentCreatesMigratesAndSeeds(t *testing.T) {
	root := t.TempDir()
	store, err := Open(t.Context(), root, SeedAccount{
		Email:  "ADMIN@EXAMPLE.COM",
		Secret: "temporary",
		Handle: "keeper",
	}, new(testGame))
	if err != nil {
		t.Fatal(err)
	}

	account := readAccount(t, store, "admin@example.com")
	if account.handle != "keeper" || account.role != "admin" {
		t.Fatalf("account = %#v, want keeper admin", account)
	}
	if !account.origin.Equals(hexg.NewHex(0, 0)) || account.rotation != 0 {
		t.Fatalf("main admin origin = %v rotation = %d, want (0,0,0) rotation 0", account.origin, account.rotation)
	}
	if err := bcrypt.CompareHashAndPassword(account.hash, []byte("temporary")); err != nil {
		t.Fatalf("compare password hash: %v", err)
	}
	if cost, err := bcrypt.Cost(account.hash); err != nil || cost != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, %v; want %d", cost, err, bcrypt.MinCost)
	}
	game, err := store.Game(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if game != testGame {
		t.Fatalf("Game = %#v, want %#v", game, testGame)
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
	reopened, err := Open(t.Context(), root, SeedAccount{}, nil)
	if err != nil {
		t.Fatalf("reopen without seed configuration: %v", err)
	}
	defer reopened.Close()
	if got := accountCount(t, reopened); got != 1 {
		t.Fatalf("account count after reopen = %d, want 1", got)
	}
	game, err = reopened.Game(t.Context())
	if err != nil || game != testGame {
		t.Fatalf("Game after reopen = %#v, %v; want %#v", game, err, testGame)
	}
}

func TestOpenPersistentRejectsMissingRootAndMissingSeed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Open(t.Context(), missing, SeedAccount{}, nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open missing root error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root was created: %v", err)
	}

	root := t.TempDir()
	if _, err := Open(t.Context(), root, SeedAccount{}, new(testGame)); err == nil || !strings.Contains(err.Error(), "account email is required") {
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
	}, new(testGame))
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
	if err := sqlitex.ExecuteTransient(conn, fmt.Sprintf("PRAGMA user_version = %d;", len(schema.Migrations)+1), nil); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(t.Context(), root, SeedAccount{}, nil)
	if err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("Open newer schema error = %v", err)
	}
}

func TestOpenPersistentRejectsMissingGameSeed(t *testing.T) {
	root := t.TempDir()
	admin := SeedAccount{Email: "admin@example.com", Secret: "temporary", Handle: "admin"}
	if _, err := Open(t.Context(), root, admin, nil); err == nil || !strings.Contains(err.Error(), "game seed is required") {
		t.Fatalf("Open missing game seed error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, Filename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database created despite missing game seed: %v", err)
	}
}

func TestOpenMemorySeedsDefaults(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
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
	admin := readAccount(t, store, "admin@marajanda.com")
	if !admin.origin.Equals(hexg.NewHex(0, 0)) || admin.rotation != 0 {
		t.Fatalf("main admin origin = %v rotation = %d, want game origin and rotation 0", admin.origin, admin.rotation)
	}
	player := readAccount(t, store, "player@marajanda.com")
	if player.origin.Length() <= 15 || player.rotation < 0 || player.rotation > 5 {
		t.Fatalf("player origin = %v rotation = %d, want distance > 15 and rotation 0..5", player.origin, player.rotation)
	}
	game, err := store.Game(t.Context())
	if err != nil || game != testGame {
		t.Fatalf("Game = %#v, %v; want %#v", game, err, testGame)
	}
	assertPragma(t, store, "foreign_keys", 1)
	assertTextPragma(t, store, "journal_mode", "memory")
}

func TestAuthenticate(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
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

func TestFindOrCreateDevelopmentAccount(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	existing, err := store.FindOrCreateDevelopmentAccount(t.Context(), " ADMIN@MARAJANDA.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if existing != (Account{Email: "admin@marajanda.com", Handle: "admin", Role: "admin"}) {
		t.Fatalf("existing account = %#v, want admin", existing)
	}

	created, err := store.FindOrCreateDevelopmentAccount(t.Context(), "Agent@Example.Test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Handle, "agent-") || created.Role != "player" {
		t.Fatalf("created account = %#v, want generated player", created)
	}
	if created.Origin.Length() <= 15 || created.Rotation < 0 || created.Rotation > 5 {
		t.Fatalf("created origin = %v rotation = %d, want distance > 15 and rotation 0..5", created.Origin, created.Rotation)
	}
	again, err := store.FindOrCreateDevelopmentAccount(t.Context(), "agent@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if again != created {
		t.Fatalf("second lookup = %#v, want %#v", again, created)
	}
	if got := accountCount(t, store); got != 3 {
		t.Fatalf("account count = %d, want 3", got)
	}
}

func TestCreateAccountAssignsDeterministicOriginToEveryRole(t *testing.T) {
	first, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	seed := SeedAccount{
		Email: " ASSISTANT@EXAMPLE.COM ", Secret: "temporary", Handle: "assistant", Role: "admin",
	}
	firstAccount, err := first.CreateAccount(t.Context(), seed)
	if err != nil {
		t.Fatal(err)
	}
	secondAccount, err := second.CreateAccount(t.Context(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if firstAccount != secondAccount {
		t.Fatalf("deterministic accounts = %#v and %#v", firstAccount, secondAccount)
	}
	if firstAccount.Email != "assistant@example.com" || firstAccount.Role != "admin" {
		t.Fatalf("created account = %#v, want normalized assistant admin", firstAccount)
	}
	if firstAccount.Origin.Length() <= 15 || firstAccount.Rotation < 0 || firstAccount.Rotation > 5 {
		t.Fatalf("assistant origin = %v rotation = %d, want distance > 15 and rotation 0..5", firstAccount.Origin, firstAccount.Rotation)
	}
}

func TestAccountOriginAndRotationConstraints(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for _, test := range []struct {
		name string
		stmt string
		want sqlite.ResultCode
	}{
		{
			name: "required origin",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role)
				VALUES ('missing-origin@example.com', X'00', 'missing-origin', 'player');`,
			want: sqlite.ResultConstraintNotNull,
		},
		{
			name: "rotation range",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r, rotation)
				VALUES ('bad-rotation@example.com', X'00', 'bad-rotation', 'player', 100, 100, 6);`,
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "unique origin",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r, rotation)
				VALUES ('same-origin@example.com', X'00', 'same-origin', 'player', 0, 0, 1);`,
			want: sqlite.ResultConstraintUnique,
		},
		{
			name: "email conflict takes precedence over origin conflict",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r, rotation)
				VALUES ('admin@marajanda.com', X'00', 'same-email', 'admin', 0, 0, 0);`,
			want: sqlite.ResultConstraintUnique,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := sqlitex.ExecuteTransient(conn, test.stmt, nil)
			if got := sqlite.ErrCode(err); got != test.want {
				t.Fatalf("constraint error = %v (%v), want %v", err, got, test.want)
			}
			if test.name == "unique origin" && !strings.Contains(err.Error(), "accounts.origin_q, accounts.origin_r") {
				t.Fatalf("unique-origin error = %v, want origin constraint", err)
			}
			if test.name == "email conflict takes precedence over origin conflict" && !strings.Contains(err.Error(), "accounts.email") {
				t.Fatalf("duplicate-email error = %v, want email constraint", err)
			}
		})
	}
}

func TestCreateAccountConcurrentRaces(t *testing.T) {
	store, err := OpenSharedMemory(t.Context(), t.Name(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index := range 2 {
		wg.Go(func() {
			_, err := store.CreateAccount(t.Context(), SeedAccount{
				Email: "race@example.com", Secret: "temporary",
				Handle: fmt.Sprintf("racer-%d", index), Role: "player",
			})
			results <- err
		})
	}
	wg.Wait()
	close(results)

	succeeded, constrained := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case sqlite.ErrCode(err).ToPrimary() == sqlite.ResultConstraint && strings.Contains(err.Error(), "accounts.email"):
			constrained++
		default:
			t.Fatalf("concurrent same-email creation: %v", err)
		}
	}
	if succeeded != 1 || constrained != 1 {
		t.Fatalf("concurrent same-email creation = %d succeeded, %d constrained; want 1 and 1", succeeded, constrained)
	}

	results = make(chan error, 2)
	for index := range 2 {
		wg.Go(func() {
			_, err := store.CreateAccount(t.Context(), SeedAccount{
				Email: fmt.Sprintf("different-%d@example.com", index), Secret: "temporary",
				Handle: fmt.Sprintf("different-%d", index), Role: "player",
			})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent different-email creation: %v", err)
		}
	}
}

func TestOpenSharedMemoryUsesNamedDatabase(t *testing.T) {
	first, err := OpenSharedMemory(t.Context(), t.Name(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := OpenSharedMemory(t.Context(), t.Name(), Game{Seed1: 1, Seed2: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := accountCount(t, second); got != 2 {
		t.Fatalf("account count from second pool = %d, want 2", got)
	}
	game, err := second.Game(t.Context())
	if err != nil || game != testGame {
		t.Fatalf("shared Game = %#v, %v; want original %#v", game, err, testGame)
	}
	assertPragma(t, second, "foreign_keys", 1)
}

func TestFactionStartsAtOrigin(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if faction, found, err := store.Faction(t.Context(), "player@marajanda.com"); err != nil || found {
		t.Fatalf("Faction before save = %#v, %t, %v; want no faction", faction, found, err)
	}
	if err := store.SaveFaction(t.Context(), " PLAYER@MARAJANDA.COM ", "The Wayfarers"); err != nil {
		t.Fatal(err)
	}
	faction, found, err := store.Faction(t.Context(), "player@marajanda.com")
	if err != nil {
		t.Fatal(err)
	}
	if !found || !faction.Configured() || faction.Name != "The Wayfarers" || faction.Location.Q() != 0 || faction.Location.R() != 0 {
		t.Fatalf("Faction = %#v, %t; want configured faction at axial origin", faction, found)
	}
}

func TestFactionIsVisibleAcrossSharedMemoryConnections(t *testing.T) {
	first, err := OpenSharedMemory(t.Context(), t.Name(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenSharedMemory(t.Context(), t.Name(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if err := first.SaveFaction(t.Context(), "player@marajanda.com", "The Navigators"); err != nil {
		t.Fatal(err)
	}
	faction, found, err := second.Faction(t.Context(), "player@marajanda.com")
	if err != nil || !found || faction.Name != "The Navigators" {
		t.Fatalf("shared Faction = %#v, %t, %v; want The Navigators", faction, found, err)
	}
}

type storedAccount struct {
	hash     []byte
	handle   string
	role     string
	origin   hexg.Hex
	rotation int
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
		SELECT secret_hash, handle, role, origin_q, origin_r, rotation
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{email},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.hash = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, account.hash)
			account.handle = stmt.ColumnText(1)
			account.role = stmt.ColumnText(2)
			account.origin = hexg.NewHex(stmt.ColumnInt(3), stmt.ColumnInt(4))
			account.rotation = stmt.ColumnInt(5)
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

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
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// testGame uses the smallest legal world so the suite generates and inserts as
// little as possible while still exercising a world a player can be placed in.
var testGame = Game{Seed1: 98374, Seed2: -98, Radius: MinimumWorldRadius}

// testWorldHexes is how many hexes a world of the test radius holds.
var testWorldHexes = int64(3*testGame.Radius*(testGame.Radius+1) + 1)

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
	// The main admin sits on the game origin whatever the generator made of it.
	if terrain := readTerrain(t, store, account.origin); !terrain.Valid() {
		t.Fatalf("main admin terrain = %q, want a generated terrain", terrain)
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
	if terrain := readTerrain(t, store, player.origin); terrain.IsWater() {
		t.Fatalf("player origin %v is %q, want dry land", player.origin, terrain)
	}
	if got := hexCount(t, store); got != testWorldHexes {
		t.Fatalf("hex count = %d, want the whole world (%d)", got, testWorldHexes)
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
	// Creating an account no longer creates a hex: the world already holds it.
	if got := hexCount(t, store); got != testWorldHexes {
		t.Fatalf("hex count = %d, want the whole world (%d)", got, testWorldHexes)
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
	if terrain := readTerrain(t, first, firstAccount.Origin); terrain.IsWater() {
		t.Fatalf("assistant origin %v is %q, want dry land", firstAccount.Origin, terrain)
	}
}

// A new account keeps clear of the origins other accounts already hold.
//
// The exclusion set is those origins, read from accounts, and not the rows in
// hexes: the whole world is generated before the first account exists, so
// excluding every hex in the table would exclude everywhere.
func TestCreateAccountAvoidsAssignedOrigins(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	origins := []hexg.Hex{
		readAccount(t, store, "admin@marajanda.com").origin,
		readAccount(t, store, "player@marajanda.com").origin,
	}
	for index := range 3 {
		account, err := store.CreateAccount(t.Context(), SeedAccount{
			Email:  fmt.Sprintf("rival-%d@example.com", index),
			Secret: "temporary",
			Handle: fmt.Sprintf("rival-%d", index),
			Role:   "player",
		})
		if err != nil {
			t.Fatalf("create rival-%d: %v", index, err)
		}
		if account.Origin.Length() <= 15 {
			t.Fatalf("rival-%d origin %v is %d from the game origin, want > 15",
				index, account.Origin, account.Origin.Length())
		}
		if terrain := readTerrain(t, store, account.Origin); terrain.IsWater() {
			t.Fatalf("rival-%d origin %v is %q, want dry land", index, account.Origin, terrain)
		}
		for _, origin := range origins[1:] {
			if distance := account.Origin.Distance(origin); distance <= 15 {
				t.Fatalf("rival-%d origin %v is %d hexes from %v, want > 15",
					index, account.Origin, distance, origin)
			}
		}
		origins = append(origins, account.Origin)
	}
}

// The world is the game's terrain of record, so what a store hands back must be
// exactly what the generator produced for its seeds and radius.
func TestWorldMatchesTheGenerator(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	stored, err := store.World(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := game.GenerateWorld(testPRNGSeeds(), testGame.Radius)
	if stored.Radius() != want.Radius() || stored.Len() != want.Len() {
		t.Fatalf("World() = radius %d, %d hexes; want %d, %d",
			stored.Radius(), stored.Len(), want.Radius(), want.Len())
	}
	for _, hex := range want.Hexes() {
		got, ok := stored.At(hex.Coord)
		if !ok || got != hex {
			t.Fatalf("World() at %v = %+v ok=%v, want %+v", hex.Coord, got, ok, hex)
		}
	}
}

// The radius is persisted with the seeds and fixes the world's size forever, so
// a nonsensical one has to be refused when the database is created rather than
// discovered later by a player who cannot be placed.
func TestOpenMemoryRejectsAnUnusableWorldRadius(t *testing.T) {
	for _, radius := range []int{1, MinimumWorldRadius - 1, MaximumWorldRadius + 1} {
		if _, err := OpenMemory(t.Context(), Game{Seed1: 1, Seed2: 2, Radius: radius}); err == nil {
			t.Fatalf("OpenMemory(radius %d) succeeded, want an error", radius)
		}
	}
}

func TestOpenMemoryDefaultsTheWorldRadius(t *testing.T) {
	store, err := OpenMemory(t.Context(), Game{Seed1: 1, Seed2: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	record, err := store.Game(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if record.Radius != DefaultWorldRadius {
		t.Fatalf("Game().Radius = %d, want %d", record.Radius, DefaultWorldRadius)
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
		{
			name: "origin must be initialized",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r, rotation)
				VALUES ('uninitialized@example.com', X'00', 'uninitialized', 'player', 101, 101, 1);`,
			want: sqlite.ResultConstraintForeignKey,
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

func TestInitializedHexConstraints(t *testing.T) {
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
			name: "terrain required",
			stmt: `INSERT INTO hexes (q, r, elevation) VALUES (100, 100, 10);`,
			want: sqlite.ResultConstraintNotNull,
		},
		{
			name: "terrain constrained",
			stmt: `INSERT INTO hexes (q, r, terrain, elevation) VALUES (100, 100, 'desert', 10);`,
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "elevation required",
			stmt: `INSERT INTO hexes (q, r, terrain) VALUES (100, 100, 'grassland');`,
			want: sqlite.ResultConstraintNotNull,
		},
		{
			name: "coordinates primary key",
			stmt: `INSERT INTO hexes (q, r, terrain, elevation) VALUES (0, 0, 'grassland', 10);`,
			want: sqlite.ResultConstraintPrimaryKey,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := sqlitex.ExecuteTransient(conn, test.stmt, nil)
			if got := sqlite.ErrCode(err); got != test.want {
				t.Fatalf("constraint error = %v (%v), want %v", err, got, test.want)
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
	if accounts, hexes := accountCount(t, store), hexCount(t, store); accounts != 3 || hexes != testWorldHexes {
		t.Fatalf("after same-email race: %d accounts, %d hexes; want 3 and %d", accounts, hexes, testWorldHexes)
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
	if accounts, hexes := accountCount(t, store), hexCount(t, store); accounts != 5 || hexes != testWorldHexes {
		t.Fatalf("after different-email race: %d accounts, %d hexes; want 5 and %d", accounts, hexes, testWorldHexes)
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

func hexCount(t *testing.T, store *Store) int64 {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var count int64
	if err := sqlitex.ExecuteTransient(conn, "SELECT count(*) FROM hexes;", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func readTerrain(t *testing.T, store *Store, location hexg.Hex) game.Terrain {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var terrain game.Terrain
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT terrain FROM hexes WHERE q = ?1 AND r = ?2;`, &sqlitex.ExecOptions{
		Args: []any{location.Q(), location.R()},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			terrain = game.Terrain(stmt.ColumnText(0))
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if terrain == "" {
		t.Fatalf("initialized hex %v not found", location)
	}
	return terrain
}

func testPRNGSeeds() prng.Seeds {
	return prng.New(uint64(testGame.Seed1), uint64(testGame.Seed2))
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

func TestVisibleHexes(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// The main admin sits on the game origin, so its visible set is the one
	// place every account agrees about.
	admin := readAccount(t, store, "admin@marajanda.com")
	visible, err := store.VisibleHexes(t.Context(), "admin@marajanda.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || !visible[0].Equals(admin.origin) {
		t.Fatalf("admin visible hexes = %v, want [%v]", visible, admin.origin)
	}

	player := readAccount(t, store, "player@marajanda.com")
	visible, err = store.VisibleHexes(t.Context(), " PLAYER@MARAJANDA.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || !visible[0].Equals(player.origin) {
		t.Fatalf("player visible hexes = %v, want [%v]", visible, player.origin)
	}
	if visible[0].Equals(admin.origin) {
		t.Fatal("player sees the admin origin")
	}

	if _, err := store.VisibleHexes(t.Context(), "stranger@example.com"); err == nil {
		t.Fatal("VisibleHexes(unknown account) = nil error, want an error")
	}
}

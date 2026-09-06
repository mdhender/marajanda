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
	"github.com/mdhender/marajanda/internal/cylinder"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// testGame uses the smallest legal world so the suite generates and inserts as
// little as possible while still exercising a world a player can be placed in.
// wrappedOriginDistance measures how far an origin sits from the game origin
// the way the world does, going the short way around rather than across the map.
func wrappedOriginDistance(t *testing.T, origin hexg.Hex) int {
	t.Helper()
	cyl, err := cylinder.New(2*testGame.Width + 1)
	if err != nil {
		t.Fatalf("cylinder.New: %v", err)
	}
	return cyl.Distance(hexg.NewHex(0, 0), origin)
}

// wrappedDistance measures two origins against each other the same way.
func wrappedDistance(t *testing.T, a, b hexg.Hex) int {
	t.Helper()
	cyl, err := cylinder.New(2*testGame.Width + 1)
	if err != nil {
		t.Fatalf("cylinder.New: %v", err)
	}
	return cyl.Distance(a, b)
}

func readAccountExists(t *testing.T, store *Store, email string) bool {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, found, err := readAccountRecord(conn, email)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

var testGame = Game{Seed1: 98374, Seed2: -98, Width: MinimumWorldWidth, Height: MinimumWorldHeight}

// testWorldHexes is how many hexes a world of the test dimensions holds.
var testWorldHexes = int64((2*testGame.Width + 1) * (2*testGame.Height + 1))

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
	if !account.origin.Equals(hexg.NewHex(0, 0)) {
		t.Fatalf("main admin origin = %v, want (0,0,0)", account.origin)
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
	if !admin.origin.Equals(hexg.NewHex(0, 0)) {
		t.Fatalf("main admin origin = %v, want the game origin", admin.origin)
	}
	// A seeded player is created unseated. Placement needs the faction's race,
	// which nothing has chosen yet.
	if player := readAccount(t, store, "player@marajanda.com"); player.seated {
		t.Fatalf("seeded player holds origin %v, want no seat before its faction is configured", player.origin)
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
	if existing != (Account{Email: "admin@marajanda.com", Handle: "admin", Role: "admin", Seated: true, Active: true}) {
		t.Fatalf("existing account = %#v, want the seated main admin", existing)
	}

	created, err := store.FindOrCreateDevelopmentAccount(t.Context(), "Agent@Example.Test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Handle, "agent-") || created.Role != "player" {
		t.Fatalf("created account = %#v, want generated player", created)
	}
	if created.Seated {
		t.Fatalf("created player holds origin %v, want no seat before its faction is configured", created.Origin)
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
	if wrappedOriginDistance(t, firstAccount.Origin) <= 15 {
		t.Fatalf("assistant origin = %v, want wrapped distance > 15 from the game origin", firstAccount.Origin)
	}
	if terrain := readTerrain(t, first, firstAccount.Origin); terrain.IsWater() {
		t.Fatalf("assistant origin %v is %q, want dry land", firstAccount.Origin, terrain)
	}
}

// A newly seated account keeps clear of the origins other accounts already
// hold, the main admin's game origin included.
//
// The exclusion set is those origins, read from accounts, and not the rows in
// hexes: the whole world is generated before the first account exists, so
// excluding every hex in the table would exclude everywhere.
func TestSaveFactionAvoidsAssignedOrigins(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	origins := []hexg.Hex{readAccount(t, store, "admin@marajanda.com").origin}
	for index := range 3 {
		email := fmt.Sprintf("rival-%d@example.com", index)
		if _, err := store.CreateAccount(t.Context(), SeedAccount{
			Email: email, Secret: "temporary", Handle: fmt.Sprintf("rival-%d", index), Role: "player",
		}); err != nil {
			t.Fatalf("create rival-%d: %v", index, err)
		}
		account, err := store.SaveFaction(t.Context(), email, "Rival Company", game.RaceHuman)
		if err != nil {
			t.Fatalf("seat rival-%d: %v", index, err)
		}
		if !account.Seated {
			t.Fatalf("rival-%d is still unseated after configuring a faction", index)
		}
		if wrappedOriginDistance(t, account.Origin) <= 15 {
			t.Fatalf("rival-%d origin %v is too close to the game origin", index, account.Origin)
		}
		if terrain := readTerrain(t, store, account.Origin); !terrain.IsLand() {
			t.Fatalf("rival-%d origin %v is %q, want land", index, account.Origin, terrain)
		}
		for _, origin := range origins {
			// Every one of these is human, and the main admin counts as human
			// too, so the wider limit applies wherever the terrain matches.
			if distance := wrappedDistance(t, account.Origin, origin); distance < 8 {
				t.Fatalf("rival-%d origin %v is %d hexes from %v, want at least 8",
					index, account.Origin, distance, origin)
			}
		}
		origins = append(origins, account.Origin)
	}
}

// An admin is seated as it is created, because it configures no faction and so
// has no later moment to be placed.
func TestCreateAccountSeatsAdminsAndLeavesPlayersUnseated(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.CreateAccount(t.Context(), SeedAccount{
		Email: "assistant@example.com", Secret: "temporary", Handle: "assistant", Role: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !admin.Seated {
		t.Fatal("an assistant admin was created without a seat")
	}
	if terrain := readTerrain(t, store, admin.Origin); !terrain.IsLand() {
		t.Fatalf("assistant admin origin %v is %q, want land", admin.Origin, terrain)
	}

	player, err := store.CreateAccount(t.Context(), SeedAccount{
		Email: "recruit@example.com", Secret: "temporary", Handle: "recruit", Role: "player",
	})
	if err != nil {
		t.Fatal(err)
	}
	if player.Seated {
		t.Fatalf("a player was seated at %v before it configured a faction", player.Origin)
	}
	if stored := readAccount(t, store, "recruit@example.com"); stored.seated {
		t.Fatal("a player was stored with an origin before it configured a faction")
	}
}

// An admin that cannot be seated is refused outright: a half-built account with
// no place in the world is worse than no account.
func TestCreateAccountFailsWhenAnAdminCannotBeSeated(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fillWorld(t, store)

	if _, err := store.CreateAccount(t.Context(), SeedAccount{
		Email: "latecomer@example.com", Secret: "temporary", Handle: "latecomer", Role: "admin",
	}); !errors.Is(err, game.ErrNoOrigin) {
		t.Fatalf("CreateAccount(full world) error = %v, want %v", err, game.ErrNoOrigin)
	}
	if readAccountExists(t, store, "latecomer@example.com") {
		t.Fatal("a refused admin left an account behind")
	}
}

// A placement that fails leaves no faction row behind. A faction saved without
// an origin under it would be a player with a name and nowhere to stand.
func TestSaveFactionLeavesNoFactionWhenPlacementFails(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fillWorld(t, store)

	if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Homeless", game.RaceHuman); !errors.Is(err, game.ErrNoOrigin) {
		t.Fatalf("SaveFaction(full world) error = %v, want %v", err, game.ErrNoOrigin)
	}
	if faction, found, err := store.Faction(t.Context(), "player@marajanda.com"); err != nil || found {
		t.Fatalf("Faction after a failed placement = %#v, %t, %v; want none", faction, found, err)
	}
	if stored := readAccount(t, store, "player@marajanda.com"); stored.seated {
		t.Fatalf("a failed placement seated the account at %v", stored.origin)
	}
	// The founding entities are written in the same transaction, so a placement
	// that failed leaves no entity standing anywhere either.
	if count := entityCount(t, store); count != 0 {
		t.Fatalf("entity count after a failed placement = %d, want 0", count)
	}
}

// SaveFaction rejects a race the game does not know rather than storing it and
// letting the CHECK constraint decide.
func TestSaveFactionRejectsAnUnknownRace(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wyrms", game.Race("wyrm")); err == nil {
		t.Fatal("SaveFaction accepted an unregistered race")
	}
	if _, found, _ := store.Faction(t.Context(), "player@marajanda.com"); found {
		t.Fatal("a rejected race left a faction row behind")
	}
}

// fillWorld seats admin accounts until the world refuses another, so a test can
// see what happens to the account after the last one. Admins are used because
// they are seated as they are created, in one call.
func fillWorld(t *testing.T, store *Store) {
	t.Helper()
	for index := range 200 {
		_, err := store.CreateAccount(t.Context(), SeedAccount{
			Email:  fmt.Sprintf("filler-%d@example.com", index),
			Secret: "temporary",
			Handle: fmt.Sprintf("filler-%d", index),
			Role:   "admin",
		})
		if errors.Is(err, game.ErrNoOrigin) {
			if index == 0 {
				t.Fatal("the test world seated nobody at all")
			}
			return
		}
		if err != nil {
			t.Fatalf("create filler-%d: %v", index, err)
		}
	}
	t.Fatal("the test world never filled up")
}

// The world is the game's terrain of record, so what a store hands back must be
// exactly what the generator produced for its seeds and dimensions.
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
	want, err := game.GenerateWorld(testPRNGSeeds(), testGame.Width, testGame.Height)
	if err != nil {
		t.Fatalf("GenerateWorld: %v", err)
	}
	if stored.Width() != want.Width() || stored.Height() != want.Height() || stored.Len() != want.Len() {
		t.Fatalf("World() = width %d, %d hexes; want %d, %d",
			stored.Width(), stored.Len(), want.Width(), want.Len())
	}
	for _, hex := range want.Hexes() {
		got, ok := stored.At(hex.Coord)
		if !ok || got != hex {
			t.Fatalf("World() at %v = %+v ok=%v, want %+v", hex.Coord, got, ok, hex)
		}
	}
}

// The dimensions are persisted with the seeds and fix the world's size forever, so
// a nonsensical one has to be refused when the database is created rather than
// discovered later by a player who cannot be placed.
func TestOpenMemoryRejectsUnusableWorldDimensions(t *testing.T) {
	for _, record := range []Game{
		{Seed1: 1, Seed2: 2, Width: 1, Height: MinimumWorldHeight},
		{Seed1: 1, Seed2: 2, Width: MinimumWorldWidth - 1, Height: MinimumWorldHeight},
		{Seed1: 1, Seed2: 2, Width: MaximumWorldWidth + 1, Height: MinimumWorldHeight},
		{Seed1: 1, Seed2: 2, Width: MinimumWorldWidth, Height: MinimumWorldHeight - 1},
		{Seed1: 1, Seed2: 2, Width: MinimumWorldWidth, Height: MaximumWorldHeight + 1},
	} {
		if _, err := OpenMemory(t.Context(), record); err == nil {
			t.Fatalf("OpenMemory(%dx%d) succeeded, want an error", record.Width, record.Height)
		}
	}
}

func TestOpenMemoryDefaultsTheWorldDimensions(t *testing.T) {
	store, err := OpenMemory(t.Context(), Game{Seed1: 1, Seed2: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	record, err := store.Game(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if record.Width != DefaultWorldWidth || record.Height != DefaultWorldHeight {
		t.Fatalf("Game() = %dx%d, want %dx%d", record.Width, record.Height, DefaultWorldWidth, DefaultWorldHeight)
	}
}

func TestAccountOriginConstraints(t *testing.T) {
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
			// An account may hold no origin at all - a player is created before
			// it is seated - but never half of one.
			name: "origin columns are null together",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r)
				VALUES ('half-origin@example.com', X'00', 'half-origin', 'player', 0, NULL);`,
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "unique origin",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r)
				VALUES ('same-origin@example.com', X'00', 'same-origin', 'player', 0, 0);`,
			want: sqlite.ResultConstraintUnique,
		},
		{
			name: "email conflict takes precedence over origin conflict",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r)
				VALUES ('admin@marajanda.com', X'00', 'same-email', 'admin', 0, 0);`,
			want: sqlite.ResultConstraintUnique,
		},
		{
			name: "origin must be initialized",
			stmt: `INSERT INTO accounts (email, secret_hash, handle, role, origin_q, origin_r)
				VALUES ('uninitialized@example.com', X'00', 'uninitialized', 'player', 101, 101);`,
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

// The terrain CHECK and game.Terrains() are one contract written twice. A
// terrain the game can generate but the schema will not store is a world that
// cannot be saved, and it would only be found by generating one.
func TestInitializedHexTerrainCheckAcceptsEveryTerrain(t *testing.T) {
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

	for index, terrain := range game.Terrains() {
		t.Run(string(terrain), func(t *testing.T) {
			// Well outside the generated world, so nothing collides with a
			// real hex of it.
			stmt := fmt.Sprintf(
				`INSERT INTO hexes (q, r, terrain, elevation) VALUES (1000, %d, '%s', 10);`,
				1000+index, terrain)
			if err := sqlitex.ExecuteTransient(conn, stmt, nil); err != nil {
				t.Fatalf("INSERT %q: %v", terrain, err)
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

func TestSaveFactionSeatsAndFoundsAFaction(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if faction, found, err := store.Faction(t.Context(), "player@marajanda.com"); err != nil || found {
		t.Fatalf("Faction before save = %#v, %t, %v; want no faction", faction, found, err)
	}
	seated, err := store.SaveFaction(t.Context(), " PLAYER@MARAJANDA.COM ", "The Wayfarers", game.RaceElf)
	if err != nil {
		t.Fatal(err)
	}
	// Configuring a faction is what seats the account, because placement needs
	// the race the form has just supplied.
	if !seated.Seated {
		t.Fatal("SaveFaction returned an unseated account")
	}
	if stored := readAccount(t, store, "player@marajanda.com"); !stored.seated || !stored.origin.Equals(seated.Origin) {
		t.Fatalf("stored origin = %v (seated %t), want %v", stored.origin, stored.seated, seated.Origin)
	}
	// An elf takes forest first, and this world has forest in the belt.
	if terrain := readTerrain(t, store, seated.Origin); terrain != game.TerrainForest {
		t.Fatalf("elf origin %v is %q, want the favored forest", seated.Origin, terrain)
	}
	faction, found, err := store.Faction(t.Context(), "player@marajanda.com")
	if err != nil {
		t.Fatal(err)
	}
	if !found || !faction.Configured() || faction.Name != "The Wayfarers" || faction.Race != game.RaceElf {
		t.Fatalf("Faction = %#v, %t; want The Wayfarers of the elves", faction, found)
	}
	// A faction has no location. Its entities do, and configuring the faction
	// is what puts them on the map.
	entities, err := store.EntitiesAsOf(t.Context(), "player@marajanda.com", game.FirstTurn)
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) != 2 {
		t.Fatalf("founding entities = %#v, want a leader and a hamlet", entities)
	}
	for index, want := range []Entity{
		{Code: "LEADER-1", Name: "LEADER-1", Kind: game.EntityKindLeader, Location: seated.Origin},
		{Code: "HAMLET-1", Name: "HAMLET-1", Kind: game.EntityKindHamlet, Location: seated.Origin},
	} {
		got := entities[index]
		if got.ID == 0 || got.Code != want.Code || got.Name != want.Name || got.Kind != want.Kind || !got.Location.Equals(want.Location) {
			t.Fatalf("founding entity %d = %#v, want %#v on the faction origin", index, got, want)
		}
	}
}

// Reconfiguring a faction never moves a seat. An origin is immutable once taken,
// so a change of race renames the people without relocating them.
func TestSaveFactionKeepsAnExistingSeat(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wanderers", game.RaceDwarf)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Origin.Equals(first.Origin) {
		t.Fatalf("seat moved from %v to %v", first.Origin, second.Origin)
	}
	faction, _, err := store.Faction(t.Context(), "player@marajanda.com")
	if err != nil {
		t.Fatal(err)
	}
	if faction.Name != "The Wanderers" || faction.Race != game.RaceDwarf {
		t.Fatalf("Faction = %#v, want the renamed dwarves", faction)
	}
}

// A faction stored before race existed reads back as human, which is the column
// default and the race the exclusion set assumes for anyone without one.
func TestFactionDefaultsToHuman(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO factions (account_email, name) VALUES ('player@marajanda.com', 'The Unnamed');`, nil); err != nil {
		release()
		t.Fatal(err)
	}
	release()

	faction, found, err := store.Faction(t.Context(), "player@marajanda.com")
	if err != nil || !found {
		t.Fatalf("Faction = %#v, %t, %v", faction, found, err)
	}
	if faction.Race != game.RaceHuman {
		t.Fatalf("Faction race = %q, want %q", faction.Race, game.RaceHuman)
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

	if _, err := first.SaveFaction(t.Context(), "player@marajanda.com", "The Navigators", game.RaceHalfling); err != nil {
		t.Fatal(err)
	}
	faction, found, err := second.Faction(t.Context(), "player@marajanda.com")
	if err != nil || !found || faction.Name != "The Navigators" || faction.Race != game.RaceHalfling {
		t.Fatalf("shared Faction = %#v, %t, %v; want The Navigators of the halflings", faction, found, err)
	}
}

type storedAccount struct {
	hash   []byte
	handle string
	role   string
	origin hexg.Hex
	seated bool
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
		SELECT secret_hash, handle, role, origin_q, origin_r
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{email},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.hash = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, account.hash)
			account.handle = stmt.ColumnText(1)
			account.role = stmt.ColumnText(2)
			account.origin, account.seated = readOrigin(stmt, 3)
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

	// An unseated player has seen nothing at all. It is a floor rather than a
	// state the UI reaches: a player with no faction is sent to the faction
	// form, and the faction form is what seats them.
	visible, err = store.VisibleHexes(t.Context(), " PLAYER@MARAJANDA.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("unseated player visible hexes = %v, want none", visible)
	}

	seated, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	visible, err = store.VisibleHexes(t.Context(), " PLAYER@MARAJANDA.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || !visible[0].Equals(seated.Origin) {
		t.Fatalf("player visible hexes = %v, want [%v]", visible, seated.Origin)
	}
	if visible[0].Equals(admin.origin) {
		t.Fatal("player sees the admin origin")
	}

	if _, err := store.VisibleHexes(t.Context(), "stranger@example.com"); err == nil {
		t.Fatal("VisibleHexes(unknown account) = nil error, want an error")
	}
}

// Copyright (c) 2026 Michael D Henderson.

// Package datastore opens and migrates Marajanda SQLite databases.
package datastore

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"golang.org/x/crypto/bcrypt"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

const (
	// Filename is the database filename within a persistent server root.
	Filename = "marajanda.db"
	// ApplicationID is the ASCII encoding of "MRJ0".
	ApplicationID int32 = 0x4D524A30

	// DefaultWorldWidth and DefaultWorldHeight are the half-extents of a new
	// world: the columns either side of the origin, and the rows above and
	// below it. The world is 2*width+1 by 2*height+1, so the defaults give
	// 511 by 255. Its equatorial belt holds 47,689 land hexes, which seats
	// 402 factions of one race and some 650 when the six are spread evenly -
	// room for 256 players without pushing anyone past a second-choice
	// terrain.
	//
	// Both are half-extents so that the world always has an odd extent, which
	// is what puts the origin in its true centre and gives the wrap a window
	// with no aliased endpoint. See internal/cylinder.
	DefaultWorldWidth  = 255
	DefaultWorldHeight = 127

	// The bounds on each half-extent. The ceilings are set by what a single
	// core can generate at database creation without the request that
	// triggered it timing out, not by storage: the maxima are about half a
	// million hexes, which is some thirteen megabytes on disk.
	// The floors are set by placement rather than by geometry: origins sit
	// more than fifteen hexes from the game origin, inside an equatorial belt
	// two thirds of the way to each pole, so a world has to be wide and tall
	// enough to hold several such hexes and still have land among them.
	MinimumWorldWidth  = 20
	MaximumWorldWidth  = 511
	MinimumWorldHeight = 20
	MaximumWorldHeight = 255

	// MinimumWorldRadius is the smallest world that can hold a player. Origins
	// sit more than fifteen hexes from the game origin, so anything tighter
	// generates a world with nowhere to put anybody.
	MinimumWorldRadius = 20

	// MaximumWorldRadius bounds generation and the admin map, both of which
	// grow with the square of the radius.
	MaximumWorldRadius = 120
)

// The end-of-time sentinel is written into the schema from game.EndOfTimeTurn
// rather than typed out again beside it. It bounds the current turn and it is
// what the partial indexes below match on, so a schema naming one value while
// the code writes another would index no open period and close none either.
var schema = sqlitemigration.Schema{
	AppID:      ApplicationID,
	Migrations: []string{fmt.Sprintf(baselineMigration, game.EndOfTimeTurn)},
}

// baselineMigration is the entire schema. During beta it is a single squashed
// baseline: amend it and delete existing databases rather than appending a
// migration. Its only substitution is the end-of-time turn.
const baselineMigration = `CREATE TABLE game (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	seed1        INTEGER NOT NULL,
	seed2        INTEGER NOT NULL,
	width        INTEGER NOT NULL CHECK (width > 0),
	height       INTEGER NOT NULL CHECK (height > 0),
	-- There is one game per database, so there is one clock. A turn
	-- starts at 1 and only ever increases; it never reaches the
	-- end-of-time sentinel a period that has not ended runs to.
	current_turn INTEGER NOT NULL DEFAULT 1 CHECK (current_turn >= 1 AND current_turn < %[1]d)
) STRICT;

CREATE TABLE hexes (
	q         INTEGER NOT NULL,
	r         INTEGER NOT NULL,
	terrain   TEXT NOT NULL CHECK (terrain IN ('grassland', 'forest', 'hills', 'marsh', 'mountains', 'ocean', 'lake', 'ice')),
	elevation INTEGER NOT NULL,
	PRIMARY KEY (q, r)
) STRICT;

-- origin_q and origin_r are nullable because a player account exists
-- before it is seated: placement needs the faction's race, which is
-- chosen on the faction form, not at account creation. SQLite treats
-- NULLs as distinct in a UNIQUE constraint, so any number of unseated
-- accounts coexist while the constraint still rejects two accounts on
-- one hex.
--
-- The origin is the faction's permanent founding seat, which the
-- placement exclusion set depends on. It is not a current position:
-- nothing stands on the map but an entity, and an entity carries its
-- own location.
CREATE TABLE accounts (
	email       TEXT NOT NULL CHECK (email = lower(email)),
	secret_hash BLOB NOT NULL,
	handle      TEXT NOT NULL UNIQUE,
	role        TEXT NOT NULL CHECK (role IN ('admin', 'player')),
	origin_q    INTEGER,
	origin_r    INTEGER,
	CHECK ((origin_q IS NULL) = (origin_r IS NULL)),
	UNIQUE (origin_q, origin_r),
	UNIQUE (email),
	FOREIGN KEY (origin_q, origin_r) REFERENCES hexes (q, r) DEFERRABLE INITIALLY DEFERRED
) STRICT;

-- A faction has no coordinates. It owns entities, and they have the
-- locations.
CREATE TABLE factions (
	account_email TEXT PRIMARY KEY REFERENCES accounts (email) ON DELETE CASCADE,
	name          TEXT NOT NULL,
	race          TEXT NOT NULL DEFAULT 'human' CHECK (race IN ('human', 'elf', 'dwarf', 'orc', 'kobold', 'halfling'))
) STRICT;

-- An entity is anything that stands in the world. Its identity is this
-- integer primary key: immutable, never reused, and never a PRNG
-- instance key. Everything else about it is a fact dated in turns.
--
-- Ownership lives on the entity row rather than in a fact because
-- nothing transfers an entity between factions yet. It moves into a
-- fact the day something does.
CREATE TABLE entities (
	id            INTEGER PRIMARY KEY,
	faction_email TEXT NOT NULL REFERENCES factions (account_email) ON DELETE CASCADE,
	created_turn  INTEGER NOT NULL CHECK (created_turn >= 1)
) STRICT;

-- Code, name and kind share one fact table because they change rarely
-- and together. Every fact table carries the same half-open period
-- [effective_from, effective_through), read with one predicate:
--
--   effective_from <= :turn AND :turn < effective_through
CREATE TABLE entity_facts (
	entity_id         INTEGER NOT NULL REFERENCES entities (id) ON DELETE CASCADE,
	code              TEXT NOT NULL,
	name              TEXT NOT NULL,
	kind              TEXT NOT NULL CHECK (kind IN ('leader', 'hamlet')),
	effective_from    INTEGER NOT NULL CHECK (effective_from >= 0),
	effective_through INTEGER NOT NULL CHECK (effective_through > effective_from),
	PRIMARY KEY (entity_id, effective_from)
) STRICT;

-- Location is its own fact table from the start because it is the
-- attribute that changes every turn a leader moves.
CREATE TABLE entity_locations (
	entity_id         INTEGER NOT NULL REFERENCES entities (id) ON DELETE CASCADE,
	q                 INTEGER NOT NULL,
	r                 INTEGER NOT NULL,
	effective_from    INTEGER NOT NULL CHECK (effective_from >= 0),
	effective_through INTEGER NOT NULL CHECK (effective_through > effective_from),
	PRIMARY KEY (entity_id, effective_from),
	FOREIGN KEY (q, r) REFERENCES hexes (q, r)
) STRICT;

-- A unit is inventory: a quantity of a kind held by an entity. It has
-- no code, no name and no identity of its own, so merging two stacks is
-- addition. kind carries no CHECK: the list of unit kinds is a game
-- rule, and it arrives with the first rule that produces one rather
-- than being guessed at now.
CREATE TABLE units (
	entity_id         INTEGER NOT NULL REFERENCES entities (id) ON DELETE CASCADE,
	kind              TEXT NOT NULL,
	quantity          INTEGER NOT NULL CHECK (quantity > 0),
	effective_from    INTEGER NOT NULL CHECK (effective_from >= 0),
	effective_through INTEGER NOT NULL CHECK (effective_through > effective_from),
	PRIMARY KEY (entity_id, kind, effective_from)
) STRICT;

-- For one entity the periods of a fact table are contiguous and never
-- overlap, and exactly one of them runs to the end of time. These are
-- what hold the second half of that.
CREATE UNIQUE INDEX entity_facts_open ON entity_facts (entity_id) WHERE effective_through = %[1]d;
CREATE UNIQUE INDEX entity_locations_open ON entity_locations (entity_id) WHERE effective_through = %[1]d;
CREATE UNIQUE INDEX units_open ON units (entity_id, kind) WHERE effective_through = %[1]d;`

// SeedAccount contains the secret and public data needed to create an account.
type SeedAccount struct {
	Email  string
	Secret string
	Handle string
	Role   string
}

// Account contains the non-secret account data needed after authentication.
//
// Origin is meaningful only when Seated is true. A player account is created
// before it holds a hex: placement needs the faction's race, which is chosen on
// the faction form. An admin account is seated as it is created.
type Account struct {
	Email  string
	Handle string
	Role   string
	Origin hexg.Hex
	Seated bool
}

// Game contains the persisted state shared by the entire game. Like the seeds,
// the radius is fixed when the database is created: the world is generated once
// from all three, and changing any of them afterwards would describe a
// different world than the one on disk.
//
// The current turn is not here. It is the one column of the game row that
// moves, so it is read by CurrentTurn rather than carried in a value that
// callers compare whole against the values they created the database with.
type Game struct {
	Seed1  int64
	Seed2  int64
	Width  int
	Height int
}

// Seeds returns the game's PRNG seeds.
func (g Game) Seeds() prng.Seeds {
	return prng.New(uint64(g.Seed1), uint64(g.Seed2))
}

// Faction contains a player's faction metadata.
//
// A faction has no location. It owns entities, and they are what stand on the
// map; see Entity.
type Faction struct {
	Name string
	Race game.Race
}

// Configured reports whether all required faction metadata is present.
func (f Faction) Configured() bool {
	return f.Name != "" && f.Race.Valid()
}

// Store owns an open SQLite database.
type Store struct {
	conn *sqlite.Conn
	pool *sqlitemigration.Pool

	// The world is immutable from the moment the database is created, so it is
	// read once and kept. Reading it per request was invisible on a world of
	// a few thousand hexes and is not on one of a hundred and thirty thousand:
	// it is a full scan of the largest table, building the whole map, behind a
	// single held connection that every other request is queued on.
	worldOnce sync.Once
	world     game.World
	worldErr  error
}

// Open opens marajanda.db in root, migrates it, and seeds the game and admin if
// the file is new. game is required only for a new database. Open never creates
// root.
func Open(ctx context.Context, root string, admin SeedAccount, game *Game) (_ *Store, err error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("open datastore root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open datastore root: %q is not a directory", root)
	}

	databasePath := filepath.Join(root, Filename)
	newDatabase, err := isNewDatabase(databasePath)
	if err != nil {
		return nil, err
	}
	if newDatabase {
		admin.Role = "admin"
		if err := validateSeed(admin); err != nil {
			return nil, fmt.Errorf("seed persistent database: %w", err)
		}
		if game == nil {
			return nil, errors.New("seed persistent database: game seed is required")
		}
	}

	if newDatabase {
		defer func() {
			if err != nil {
				removeDatabaseFiles(databasePath)
			}
		}()
	}

	pool := sqlitemigration.NewPool(databasePath, schema, sqlitemigration.Options{
		Flags: sqlite.OpenReadWrite | sqlite.OpenCreate | sqlite.OpenWAL,
		PrepareConn: func(conn *sqlite.Conn) error {
			if err := prepareConnection(conn); err != nil {
				return err
			}
			return requireJournalMode(conn, "wal")
		},
	})
	store := &Store{pool: pool}
	if err := store.awaitReady(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("open persistent datastore: %w", err)
	}
	if newDatabase {
		if err := store.seedGame(ctx, *game); err != nil {
			store.Close()
			return nil, fmt.Errorf("seed persistent datastore: %w", err)
		}
		if err := store.seed(ctx, admin, true); err != nil {
			store.Close()
			return nil, fmt.Errorf("seed persistent datastore: %w", err)
		}
	} else if _, err := store.Game(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("open persistent datastore: %w", err)
	}
	return store, nil
}

// OpenMemory creates, migrates, and seeds a private in-memory database.
func OpenMemory(ctx context.Context, game Game) (*Store, error) {
	conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		return nil, fmt.Errorf("open in-memory datastore: %w", err)
	}
	store := &Store{conn: conn}
	if err := prepareAndMigrate(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open in-memory datastore: %w", err)
	}
	if err := store.seedDefaults(ctx, game); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed in-memory datastore: %w", err)
	}
	return store, nil
}

// OpenSharedMemory opens, migrates, and seeds a named shared in-memory database.
func OpenSharedMemory(ctx context.Context, name string, game Game) (*Store, error) {
	if name == "" {
		return nil, errors.New("open shared in-memory datastore: missing name")
	}
	uri := "file:marajanda-" + url.QueryEscape(name) + "?mode=memory&cache=shared"
	pool := sqlitemigration.NewPool(uri, schema, sqlitemigration.Options{
		Flags:       sqlite.OpenReadWrite | sqlite.OpenCreate | sqlite.OpenURI,
		PrepareConn: prepareConnection,
	})
	store := &Store{pool: pool}
	if err := store.awaitReady(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("open shared in-memory datastore: %w", err)
	}
	if err := store.seedDefaults(ctx, game); err != nil {
		store.Close()
		return nil, fmt.Errorf("seed shared in-memory datastore: %w", err)
	}
	return store, nil
}

// Close closes every database connection owned by the store.
func (s *Store) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return s.pool.Close()
}

// Authenticate verifies an account's credentials and returns its dashboard identity.
func (s *Store) Authenticate(ctx context.Context, email, secret string) (Account, bool, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return Account{}, false, err
	}
	defer release()

	var account Account
	var hash []byte
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT email, secret_hash, handle, role, origin_q, origin_r
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			hash = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, hash)
			account.Handle = stmt.ColumnText(2)
			account.Role = stmt.ColumnText(3)
			account.Origin, account.Seated = readOrigin(stmt, 4)
			return nil
		},
	}); err != nil {
		return Account{}, false, fmt.Errorf("look up account: %w", err)
	}
	if hash == nil {
		return Account{}, false, nil
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(secret)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return Account{}, false, nil
		}
		return Account{}, false, fmt.Errorf("verify account secret: %w", err)
	}
	return account, true, nil
}

// Game returns the game's persisted PRNG seeds.
func (s *Store) Game(ctx context.Context) (Game, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return Game{}, err
	}
	defer release()

	record, found, err := readGameRecord(conn)
	if err != nil {
		return Game{}, err
	}
	if !found {
		return Game{}, errors.New("game is not initialized")
	}
	return record, nil
}

// World returns the generated world.
//
// The world is immutable once created, so this is a pure read, and the first
// call is the only one that touches the database. It loads every hex: the admin
// map draws all of them, and a player map needs an arbitrary scattering of them
// as visibility spreads, which is not a shape a WHERE clause serves better than
// one sequential scan - but a scan that size belongs at startup, not on the
// path of every map request.
func (s *Store) World(ctx context.Context) (game.World, error) {
	return s.loadWorld(ctx)
}

func (s *Store) readWorldOnce(ctx context.Context) (game.World, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return game.World{}, err
	}
	defer release()

	record, found, err := readGameRecord(conn)
	if err != nil {
		return game.World{}, err
	}
	if !found {
		return game.World{}, errors.New("load world: game is not initialized")
	}
	return readWorld(conn, record.Width, record.Height)
}

// loadWorld reads the world once and hands out the same copy thereafter.
func (s *Store) loadWorld(ctx context.Context) (game.World, error) {
	s.worldOnce.Do(func() {
		s.world, s.worldErr = s.readWorldOnce(ctx)
	})
	return s.world, s.worldErr
}

func readWorld(conn *sqlite.Conn, width, height int) (game.World, error) {
	hexes := make([]game.Hex, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT q, r, terrain, elevation FROM hexes ORDER BY q, r;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hexes = append(hexes, game.Hex{
				Coord:     hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1)),
				Terrain:   game.Terrain(stmt.ColumnText(2)),
				Elevation: stmt.ColumnInt(3),
			})
			return nil
		},
	}); err != nil {
		return game.World{}, fmt.Errorf("load world: %w", err)
	}
	return game.NewWorld(width, height, hexes)
}

func readGameRecord(conn *sqlite.Conn) (Game, bool, error) {
	var record Game
	found := false
	if err := sqlitex.ExecuteTransient(conn, `SELECT seed1, seed2, width, height FROM game WHERE id = 1;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			record.Seed1 = stmt.ColumnInt64(0)
			record.Seed2 = stmt.ColumnInt64(1)
			record.Width = stmt.ColumnInt(2)
			record.Height = stmt.ColumnInt(3)
			found = true
			return nil
		},
	}); err != nil {
		return Game{}, false, fmt.Errorf("load game: %w", err)
	}
	return record, found, nil
}

// Faction returns the faction controlled by an account.
func (s *Store) Faction(ctx context.Context, email string) (Faction, bool, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return Faction{}, false, err
	}
	defer release()

	var faction Faction
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT name, race FROM factions WHERE account_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			faction.Name = stmt.ColumnText(0)
			faction.Race = game.Race(stmt.ColumnText(1))
			found = true
			return nil
		},
	}); err != nil {
		return Faction{}, false, fmt.Errorf("look up faction: %w", err)
	}
	return faction, found, nil
}

// SaveFaction creates or updates an account's faction metadata, seats the
// account if it is not seated yet, and founds the faction if it has not been
// founded yet.
//
// It returns the account as it now stands, so a caller holding a session can
// replace the unseated copy it started with.
//
// Placement and the faction row are written in one transaction. A player's
// race is what decides where they are seated, so the two cannot be separated:
// a placement that fails must leave no faction behind, and a faction that is
// saved must have an origin under it.
func (s *Store) SaveFaction(ctx context.Context, email, name string, race game.Race) (_ Account, err error) {
	if !race.Valid() {
		return Account{}, fmt.Errorf("save faction: invalid race %q", race)
	}
	email = normalizeEmail(email)

	conn, release, err := s.take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer release()

	account, found, err := readAccountRecord(conn, email)
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, errors.New("save faction: unknown account")
	}

	// Placement reads the world and every seated origin, so it runs before the
	// write transaction opens rather than holding one across a full map scan.
	if !account.Seated {
		origin, err := accountPlacement(conn, email, race)
		if err != nil {
			return Account{}, err
		}
		account.Origin, account.Seated = origin, true
	}

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Account{}, err
	}
	defer end(&err)

	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE accounts SET origin_q = ?2, origin_r = ?3
		WHERE email = ?1 AND origin_q IS NULL;`, &sqlitex.ExecOptions{
		Args: []any{email, account.Origin.Q(), account.Origin.R()},
	}); err != nil {
		return Account{}, fmt.Errorf("seat account: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO factions (account_email, name, race)
		VALUES (?1, ?2, ?3)
		ON CONFLICT (account_email) DO UPDATE SET name = excluded.name, race = excluded.race;`, &sqlitex.ExecOptions{
		Args: []any{email, name, string(race)},
	}); err != nil {
		return Account{}, fmt.Errorf("save faction: %w", err)
	}

	// Read the seat back rather than trusting the one just computed. The UPDATE
	// only seats an account that is still unseated, so a request that lost a
	// race to a concurrent one has to report the origin that won.
	seated, found, err := readAccountRecord(conn, email)
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, errors.New("save faction: account vanished while saving")
	}

	// Founding the faction is part of the same transaction. A faction that is
	// saved must have an origin under it and something standing on that origin,
	// so a placement or an insert that fails leaves no entity behind either.
	if err := foundFaction(conn, email, seated.Origin); err != nil {
		return Account{}, err
	}
	return seated, nil
}

// VisibleHexes returns the true map coordinates an account can currently see.
//
// Visibility is not visitation. A player will eventually see terrain in hexes
// they have never entered, so a map draws this set rather than a travel
// history. Today an account sees only its origin hex, which the account record
// already holds, so there is no visibility table to query yet.
func (s *Store) VisibleHexes(ctx context.Context, email string) ([]hexg.Hex, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	account, found, err := readAccountRecord(conn, normalizeEmail(email))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("look up visible hexes: unknown account")
	}
	// An account with no seat has seen nothing. It cannot reach a map page -
	// a player without a faction is sent to the faction form, and the faction
	// form is what seats them - so this is a floor, not a rendered state.
	if !account.Seated {
		return nil, nil
	}
	return []hexg.Hex{account.Origin}, nil
}

// FindOrCreateDevelopmentAccount returns an account for development-only sign-in.
// Accounts created by this method are players with generated handles and secrets.
func (s *Store) FindOrCreateDevelopmentAccount(ctx context.Context, email string) (account Account, err error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Account{}, fmt.Errorf("generate development account secret: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword(random, bcrypt.MinCost)
	if err != nil {
		return Account{}, fmt.Errorf("hash development account secret: %w", err)
	}
	if _, err := rand.Read(random[:8]); err != nil {
		return Account{}, fmt.Errorf("generate development account handle: %w", err)
	}
	handle := "agent-" + hex.EncodeToString(random[:8])

	return s.createAccount(ctx, SeedAccount{
		Email: email, Handle: handle, Role: "player",
	}, hash, false, true)
}

// CreateAccount creates an account. An admin is seated deterministically as it
// is created; a player is left unseated until it configures a faction. The main
// admin is created only while initializing a datastore.
func (s *Store) CreateAccount(ctx context.Context, account SeedAccount) (Account, error) {
	if err := validateSeed(account); err != nil {
		return Account{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(account.Secret), bcrypt.MinCost)
	if err != nil {
		return Account{}, fmt.Errorf("hash account secret: %w", err)
	}
	return s.createAccount(ctx, account, hash, false, false)
}

func (s *Store) awaitReady(ctx context.Context) error {
	_, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	release()
	return nil
}

func (s *Store) take(ctx context.Context) (*sqlite.Conn, func(), error) {
	if s.conn != nil {
		previousInterrupt := s.conn.SetInterrupt(ctx.Done())
		return s.conn, func() { s.conn.SetInterrupt(previousInterrupt) }, nil
	}
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() { s.pool.Put(conn) }, nil
}

func (s *Store) seedDefaults(ctx context.Context, game Game) error {
	if err := s.seedGame(ctx, game); err != nil {
		return err
	}
	for index, account := range []SeedAccount{
		{Email: "admin@marajanda.com", Secret: "good.luck", Handle: "admin", Role: "admin"},
		{Email: "player@marajanda.com", Secret: "good.luck", Handle: "player", Role: "player"},
	} {
		if err := s.seed(ctx, account, index == 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedGame(ctx context.Context, record Game) error {
	record.Width = cmp.Or(record.Width, DefaultWorldWidth)
	record.Height = cmp.Or(record.Height, DefaultWorldHeight)
	if record.Width < MinimumWorldWidth || record.Width > MaximumWorldWidth {
		return fmt.Errorf("seed game: world width %d is outside %d..%d",
			record.Width, MinimumWorldWidth, MaximumWorldWidth)
	}
	if record.Height < MinimumWorldHeight || record.Height > MaximumWorldHeight {
		return fmt.Errorf("seed game: world height %d is outside %d..%d",
			record.Height, MinimumWorldHeight, MaximumWorldHeight)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer release()

	inserted := false
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO game (id, seed1, seed2, width, height) VALUES (1, ?1, ?2, ?3, ?4)
		ON CONFLICT (id) DO NOTHING
		RETURNING 1;`, &sqlitex.ExecOptions{
		Args: []any{record.Seed1, record.Seed2, record.Width, record.Height},
		ResultFunc: func(*sqlite.Stmt) error {
			inserted = true
			return nil
		},
	}); err != nil {
		return fmt.Errorf("seed game: %w", err)
	}
	if !inserted {
		// The game already exists, and with it the world generated from it.
		return nil
	}
	return seedWorld(conn, record)
}

// seedWorld generates the world and writes it. It runs once, inside the same
// call that creates the game row, because the world is a function of the seeds
// and radius stored there and must never disagree with them.
func seedWorld(conn *sqlite.Conn, record Game) (err error) {
	world, err := game.GenerateWorld(record.Seeds(), record.Width, record.Height)
	if err != nil {
		return err
	}

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer end(&err)

	statement, err := conn.Prepare(`INSERT INTO hexes (q, r, terrain, elevation) VALUES (?1, ?2, ?3, ?4);`)
	if err != nil {
		return fmt.Errorf("prepare world insert: %w", err)
	}
	for _, hex := range world.Hexes() {
		statement.BindInt64(1, int64(hex.Coord.Q()))
		statement.BindInt64(2, int64(hex.Coord.R()))
		statement.BindText(3, string(hex.Terrain))
		statement.BindInt64(4, int64(hex.Elevation))
		if _, err := statement.Step(); err != nil {
			return fmt.Errorf("seed world: %w", err)
		}
		if err := statement.Reset(); err != nil {
			return fmt.Errorf("seed world: %w", err)
		}
	}
	if err := statement.Finalize(); err != nil {
		return fmt.Errorf("seed world: %w", err)
	}
	return nil
}

func (s *Store) seed(ctx context.Context, account SeedAccount, mainAdmin bool) error {
	if err := validateSeed(account); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(account.Secret), bcrypt.MinCost)
	if err != nil {
		return fmt.Errorf("hash account secret: %w", err)
	}

	_, err = s.createAccount(ctx, account, hash, mainAdmin, true)
	return err
}

func (s *Store) createAccount(ctx context.Context, seed SeedAccount, hash []byte, mainAdmin, ignoreExisting bool) (_ Account, err error) {
	seed.Email = normalizeEmail(seed.Email)
	conn, release, err := s.take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer release()

	if ignoreExisting {
		if account, found, err := readAccountRecord(conn, seed.Email); err != nil {
			return Account{}, err
		} else if found {
			return account, nil
		}
	}

	// An admin is seated as it is created: admins configure no faction, so
	// there is no later moment to place one. A player is created unseated and
	// takes its hex when it chooses a race on the faction form.
	//
	// The main admin is the one exception. It takes the game origin outright,
	// whatever terrain is there, and everyone else keeps clear of it.
	origin, seated := hexg.NewHex(0, 0), true
	switch {
	case mainAdmin:
		// The main admin takes the game origin as it stands.
	case seed.Role == "admin":
		origin, err = accountPlacement(conn, seed.Email, game.DefaultRace)
		if err != nil {
			return Account{}, err
		}
	default:
		seated = false
	}

	var originQ, originR any
	if seated {
		originQ, originR = origin.Q(), origin.R()
	}

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Account{}, err
	}
	defer end(&err)

	query := `INSERT INTO accounts
		(email, secret_hash, handle, role, origin_q, origin_r)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6);`
	inserted := true
	if ignoreExisting {
		inserted = false
		query = `INSERT INTO accounts
			(email, secret_hash, handle, role, origin_q, origin_r)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6)
			ON CONFLICT (email) DO NOTHING
			RETURNING 1;`
	}
	if err := sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
		Args: []any{seed.Email, hash, seed.Handle, seed.Role, originQ, originR},
		ResultFunc: func(*sqlite.Stmt) error {
			inserted = true
			return nil
		},
	}); err != nil {
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	if !inserted {
		account, found, err := readAccountRecord(conn, seed.Email)
		if err != nil {
			return Account{}, err
		}
		if !found {
			return Account{}, errors.New("create account: inserted account not found")
		}
		return account, nil
	}
	// The origin hex is not created here: the world already holds it. The
	// deferred foreign key from accounts to hexes now does real work, rejecting
	// an origin that is not a hex of this world.
	return Account{
		Email: seed.Email, Handle: seed.Handle, Role: seed.Role,
		Origin: origin, Seated: seated,
	}, nil
}

// accountPlacement chooses an account's origin hex.
//
// The exclusion set is the origins other accounts already hold, read from
// accounts. It is deliberately not every row in hexes: those are the world, and
// every hex of it exists before the first account does.
//
// Spacing now depends on who holds an origin as well as where it is, so each
// one is joined to its faction's race. The join is a LEFT JOIN defaulting to
// human because an admin holds an origin and controls no faction.
func accountPlacement(conn *sqlite.Conn, normalizedEmail string, race game.Race) (hexg.Hex, error) {
	record, foundGame, err := readGameRecord(conn)
	if err != nil {
		return hexg.Hex{}, fmt.Errorf("load game for account placement: %w", err)
	}
	if !foundGame {
		return hexg.Hex{}, errors.New("place account: game is not initialized")
	}

	world, err := readWorld(conn, record.Width, record.Height)
	if err != nil {
		return hexg.Hex{}, err
	}

	taken := make([]game.Placement, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT accounts.origin_q, accounts.origin_r, COALESCE(factions.race, 'human')
		FROM accounts LEFT JOIN factions ON factions.account_email = accounts.email
		WHERE accounts.origin_q IS NOT NULL;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			taken = append(taken, game.Placement{
				Coord: hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1)),
				Race:  game.Race(stmt.ColumnText(2)),
			})
			return nil
		},
	}); err != nil {
		return hexg.Hex{}, fmt.Errorf("load assigned origins: %w", err)
	}

	origin, err := game.AssignOrigin(record.Seeds(), normalizedEmail, race, world, taken)
	if err != nil {
		return hexg.Hex{}, fmt.Errorf("place account: %w", err)
	}
	return origin, nil
}

func readAccountRecord(conn *sqlite.Conn, normalizedEmail string) (Account, bool, error) {
	var account Account
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT email, handle, role, origin_q, origin_r
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			account.Handle = stmt.ColumnText(1)
			account.Role = stmt.ColumnText(2)
			account.Origin, account.Seated = readOrigin(stmt, 3)
			found = true
			return nil
		},
	}); err != nil {
		return Account{}, false, fmt.Errorf("look up account: %w", err)
	}
	return account, found, nil
}

// readOrigin reads an account's origin from two adjacent result columns,
// reporting whether the account is seated at all. The two columns are NULL
// together or not at all, which the accounts table asserts.
func readOrigin(stmt *sqlite.Stmt, column int) (hexg.Hex, bool) {
	if stmt.ColumnIsNull(column) {
		return hexg.Hex{}, false
	}
	return hexg.NewHex(stmt.ColumnInt(column), stmt.ColumnInt(column+1)), true
}

func prepareAndMigrate(ctx context.Context, conn *sqlite.Conn) error {
	if err := prepareConnection(conn); err != nil {
		return err
	}
	return sqlitemigration.Migrate(ctx, conn, schema)
}

func prepareConnection(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = ON;", nil); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	var version int64
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA user_version;", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			version = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > int64(len(schema.Migrations)) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, len(schema.Migrations))
	}
	return nil
}

func requireJournalMode(conn *sqlite.Conn, want string) error {
	var got string
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode;", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			got = stmt.ColumnText(0)
			return nil
		},
	}); err != nil {
		return fmt.Errorf("read journal mode: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("journal mode is %q, want %q", got, want)
	}
	return nil
}

func validateSeed(account SeedAccount) error {
	if strings.TrimSpace(account.Email) == "" {
		return errors.New("account email is required")
	}
	if account.Secret == "" {
		return errors.New("account secret is required")
	}
	if strings.TrimSpace(account.Handle) == "" {
		return errors.New("account handle is required")
	}
	if account.Role != "admin" && account.Role != "player" {
		return fmt.Errorf("invalid account role %q", account.Role)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isNewDatabase(path string) (bool, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("inspect database: %w", err)
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("inspect database: %q is not a regular file", path)
	default:
		return false, nil
	}
}

func removeDatabaseFiles(path string) {
	for _, suffix := range []string{"", "-shm", "-wal"} {
		_ = os.Remove(path + suffix)
	}
}

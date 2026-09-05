// Copyright (c) 2026 Michael D Henderson.

// Package datastore opens and migrates Marajanda SQLite databases.
package datastore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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

	// DefaultWorldRadius is how far a new world reaches from the game origin
	// when no radius is given.
	DefaultWorldRadius = 30

	// MinimumWorldRadius is the smallest world that can hold a player. Origins
	// sit more than fifteen hexes from the game origin, so anything tighter
	// generates a world with nowhere to put anybody.
	MinimumWorldRadius = 20

	// MaximumWorldRadius bounds generation and the admin map, both of which
	// grow with the square of the radius.
	MaximumWorldRadius = 120
)

var schema = sqlitemigration.Schema{
	AppID: ApplicationID,
	Migrations: []string{
		`CREATE TABLE game (
			id     INTEGER PRIMARY KEY CHECK (id = 1),
			seed1  INTEGER NOT NULL,
			seed2  INTEGER NOT NULL,
			radius INTEGER NOT NULL CHECK (radius > 0)
		) STRICT;

		CREATE TABLE hexes (
			q         INTEGER NOT NULL,
			r         INTEGER NOT NULL,
			terrain   TEXT NOT NULL CHECK (terrain IN ('grassland', 'forest', 'hills', 'marsh', 'mountains', 'ocean', 'lake')),
			elevation INTEGER NOT NULL,
			PRIMARY KEY (q, r)
		) STRICT;

		CREATE TABLE accounts (
			email       TEXT NOT NULL CHECK (email = lower(email)),
			secret_hash BLOB NOT NULL,
			handle      TEXT NOT NULL UNIQUE,
			role        TEXT NOT NULL CHECK (role IN ('admin', 'player')),
			origin_q    INTEGER NOT NULL,
			origin_r    INTEGER NOT NULL,
			rotation    INTEGER NOT NULL CHECK (rotation BETWEEN 0 AND 5),
			UNIQUE (origin_q, origin_r),
			UNIQUE (email),
			FOREIGN KEY (origin_q, origin_r) REFERENCES hexes (q, r) DEFERRABLE INITIALLY DEFERRED
		) STRICT;

		CREATE TABLE factions (
			account_email TEXT PRIMARY KEY REFERENCES accounts (email) ON DELETE CASCADE,
			name          TEXT NOT NULL,
			location_q    INTEGER NOT NULL DEFAULT 0,
			location_r    INTEGER NOT NULL DEFAULT 0
		) STRICT;`,
	},
}

// SeedAccount contains the secret and public data needed to create an account.
type SeedAccount struct {
	Email  string
	Secret string
	Handle string
	Role   string
}

// Account contains the non-secret account data needed after authentication.
type Account struct {
	Email    string
	Handle   string
	Role     string
	Origin   hexg.Hex
	Rotation int
}

// Game contains the persisted state shared by the entire game. Like the seeds,
// the radius is fixed when the database is created: the world is generated once
// from all three, and changing any of them afterwards would describe a
// different world than the one on disk.
type Game struct {
	Seed1  int64
	Seed2  int64
	Radius int
}

// Seeds returns the game's PRNG seeds.
func (g Game) Seeds() prng.Seeds {
	return prng.New(uint64(g.Seed1), uint64(g.Seed2))
}

// Faction contains a player's faction metadata.
type Faction struct {
	Name     string
	Location hexg.Hex
}

// Configured reports whether all required faction metadata is present.
func (f Faction) Configured() bool {
	return f.Name != ""
}

// Store owns an open SQLite database.
type Store struct {
	conn *sqlite.Conn
	pool *sqlitemigration.Pool
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
		SELECT email, secret_hash, handle, role, origin_q, origin_r, rotation
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			hash = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, hash)
			account.Handle = stmt.ColumnText(2)
			account.Role = stmt.ColumnText(3)
			account.Origin = hexg.NewHex(stmt.ColumnInt(4), stmt.ColumnInt(5))
			account.Rotation = stmt.ColumnInt(6)
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
// The world is immutable once created, so this is a pure read. It loads every
// hex: the admin map draws all of them, and a player map needs an arbitrary
// scattering of them as visibility spreads, which is not a shape a WHERE clause
// serves better than one sequential scan.
func (s *Store) World(ctx context.Context) (game.World, error) {
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
	return readWorld(conn, record.Radius)
}

func readWorld(conn *sqlite.Conn, radius int) (game.World, error) {
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
	return game.NewWorld(radius, hexes), nil
}

func readGameRecord(conn *sqlite.Conn) (Game, bool, error) {
	var record Game
	found := false
	if err := sqlitex.ExecuteTransient(conn, `SELECT seed1, seed2, radius FROM game WHERE id = 1;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			record.Seed1 = stmt.ColumnInt64(0)
			record.Seed2 = stmt.ColumnInt64(1)
			record.Radius = stmt.ColumnInt(2)
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
		SELECT name, location_q, location_r FROM factions WHERE account_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			faction.Name = stmt.ColumnText(0)
			faction.Location = hexg.NewHex(stmt.ColumnInt(1), stmt.ColumnInt(2))
			found = true
			return nil
		},
	}); err != nil {
		return Faction{}, false, fmt.Errorf("look up faction: %w", err)
	}
	return faction, found, nil
}

// SaveFaction creates or updates an account's faction metadata.
func (s *Store) SaveFaction(ctx context.Context, email, name string) error {
	conn, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO factions (account_email, name, location_q, location_r)
		VALUES (?1, ?2, 0, 0)
		ON CONFLICT (account_email) DO UPDATE SET name = excluded.name;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email), name},
	}); err != nil {
		return fmt.Errorf("save faction: %w", err)
	}
	return nil
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

// CreateAccount creates an account with a deterministic origin and map
// rotation. The main admin is created only while initializing a datastore.
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
	if record.Radius == 0 {
		record.Radius = DefaultWorldRadius
	}
	if record.Radius < MinimumWorldRadius || record.Radius > MaximumWorldRadius {
		return fmt.Errorf("seed game: world radius %d is outside %d..%d",
			record.Radius, MinimumWorldRadius, MaximumWorldRadius)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer release()

	inserted := false
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO game (id, seed1, seed2, radius) VALUES (1, ?1, ?2, ?3)
		ON CONFLICT (id) DO NOTHING
		RETURNING 1;`, &sqlitex.ExecOptions{
		Args: []any{record.Seed1, record.Seed2, record.Radius},
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
	world := game.GenerateWorld(record.Seeds(), record.Radius)

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

	origin := hexg.NewHex(0, 0)
	rotation := 0
	if !mainAdmin {
		origin, rotation, err = accountPlacement(conn, seed.Email)
		if err != nil {
			return Account{}, err
		}
	}

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Account{}, err
	}
	defer end(&err)

	query := `INSERT INTO accounts
		(email, secret_hash, handle, role, origin_q, origin_r, rotation)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7);`
	inserted := true
	if ignoreExisting {
		inserted = false
		query = `INSERT INTO accounts
			(email, secret_hash, handle, role, origin_q, origin_r, rotation)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
			ON CONFLICT (email) DO NOTHING
			RETURNING 1;`
	}
	if err := sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
		Args: []any{seed.Email, hash, seed.Handle, seed.Role, origin.Q(), origin.R(), rotation},
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
		Origin: origin, Rotation: rotation,
	}, nil
}

// accountPlacement chooses an account's origin hex and map rotation.
//
// The exclusion set is the origins other accounts already hold, read from
// accounts. It is deliberately not every row in hexes: those are the world, and
// every hex of it exists before the first account does.
func accountPlacement(conn *sqlite.Conn, normalizedEmail string) (hexg.Hex, int, error) {
	record, foundGame, err := readGameRecord(conn)
	if err != nil {
		return hexg.Hex{}, 0, fmt.Errorf("load game for account placement: %w", err)
	}
	if !foundGame {
		return hexg.Hex{}, 0, errors.New("place account: game is not initialized")
	}

	world, err := readWorld(conn, record.Radius)
	if err != nil {
		return hexg.Hex{}, 0, err
	}

	taken := make([]hexg.Hex, 0)
	if err := sqlitex.ExecuteTransient(conn, `SELECT origin_q, origin_r FROM accounts;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			taken = append(taken, hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1)))
			return nil
		},
	}); err != nil {
		return hexg.Hex{}, 0, fmt.Errorf("load assigned origins: %w", err)
	}

	seeds := record.Seeds()
	origin, err := game.AssignOrigin(seeds, normalizedEmail, world, taken)
	if err != nil {
		return hexg.Hex{}, 0, fmt.Errorf("place account: %w", err)
	}
	return origin, game.PlayerRotation(seeds, origin), nil
}

func readAccountRecord(conn *sqlite.Conn, normalizedEmail string) (Account, bool, error) {
	var account Account
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT email, handle, role, origin_q, origin_r, rotation
		FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			account.Handle = stmt.ColumnText(1)
			account.Role = stmt.ColumnText(2)
			account.Origin = hexg.NewHex(stmt.ColumnInt(3), stmt.ColumnInt(4))
			account.Rotation = stmt.ColumnInt(5)
			found = true
			return nil
		},
	}); err != nil {
		return Account{}, false, fmt.Errorf("look up account: %w", err)
	}
	return account, found, nil
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

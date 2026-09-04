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
)

var schema = sqlitemigration.Schema{
	AppID: ApplicationID,
	Migrations: []string{
		`CREATE TABLE accounts (
			email       TEXT PRIMARY KEY CHECK (email = lower(email)),
			secret_hash BLOB NOT NULL,
			handle      TEXT NOT NULL UNIQUE,
			role        TEXT NOT NULL CHECK (role IN ('admin', 'player'))
		) STRICT;`,
		`CREATE TABLE factions (
			account_email TEXT PRIMARY KEY REFERENCES accounts (email) ON DELETE CASCADE,
			name          TEXT NOT NULL,
			location_q    INTEGER NOT NULL DEFAULT 0,
			location_r    INTEGER NOT NULL DEFAULT 0
		) STRICT;`,
	},
}

// SeedAccount contains the initial account data for a new database.
type SeedAccount struct {
	Email  string
	Secret string
	Handle string
	Role   string
}

// Account contains the non-secret account data needed after authentication.
type Account struct {
	Email  string
	Handle string
	Role   string
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

// Open opens marajanda.db in root, migrates it, and seeds admin if the file is new.
// Open never creates root.
func Open(ctx context.Context, root string, admin SeedAccount) (_ *Store, err error) {
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
		if err := store.seed(ctx, admin); err != nil {
			store.Close()
			return nil, fmt.Errorf("seed persistent datastore: %w", err)
		}
	}
	return store, nil
}

// OpenMemory creates, migrates, and seeds a private in-memory database.
func OpenMemory(ctx context.Context) (*Store, error) {
	conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		return nil, fmt.Errorf("open in-memory datastore: %w", err)
	}
	store := &Store{conn: conn}
	if err := prepareAndMigrate(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open in-memory datastore: %w", err)
	}
	if err := store.seedDefaults(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed in-memory datastore: %w", err)
	}
	return store, nil
}

// OpenSharedMemory opens, migrates, and seeds a named shared in-memory database.
func OpenSharedMemory(ctx context.Context, name string) (*Store, error) {
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
	if err := store.seedDefaults(ctx); err != nil {
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
		SELECT email, secret_hash, handle, role FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			hash = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, hash)
			account.Handle = stmt.ColumnText(2)
			account.Role = stmt.ColumnText(3)
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

	conn, release, err := s.take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer release()
	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Account{}, err
	}
	defer end(&err)

	if err = sqlitex.ExecuteTransient(conn, `
		INSERT INTO accounts (email, secret_hash, handle, role)
		VALUES (?1, ?2, ?3, 'player')
		ON CONFLICT (email) DO NOTHING;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email), hash, handle},
	}); err != nil {
		return Account{}, err
	}
	if err = sqlitex.ExecuteTransient(conn, `
		SELECT email, handle, role FROM accounts WHERE email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(email)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			account.Handle = stmt.ColumnText(1)
			account.Role = stmt.ColumnText(2)
			return nil
		},
	}); err != nil {
		return Account{}, err
	}
	return account, nil
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

func (s *Store) seedDefaults(ctx context.Context) error {
	for _, account := range []SeedAccount{
		{Email: "admin@marajanda.com", Secret: "good.luck", Handle: "admin", Role: "admin"},
		{Email: "player@marajanda.com", Secret: "good.luck", Handle: "player", Role: "player"},
	} {
		if err := s.seed(ctx, account); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seed(ctx context.Context, account SeedAccount) (err error) {
	if err := validateSeed(account); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(account.Secret), bcrypt.MinCost)
	if err != nil {
		return fmt.Errorf("hash account secret: %w", err)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer release()
	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer end(&err)

	err = sqlitex.ExecuteTransient(conn, `
		INSERT INTO accounts (email, secret_hash, handle, role)
		VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT (email) DO NOTHING;`, &sqlitex.ExecOptions{
		Args: []any{normalizeEmail(account.Email), hash, account.Handle, account.Role},
	})
	return err
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
		return errors.New("admin email is required")
	}
	if account.Secret == "" {
		return errors.New("admin secret is required")
	}
	if strings.TrimSpace(account.Handle) == "" {
		return errors.New("admin handle is required")
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

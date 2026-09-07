// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"crypto/sha256"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// SessionTokenBytes is the required entropy in an opaque session token.
const SessionTokenBytes = 32

// CreateSession associates an opaque 32-byte bearer token with an account.
// Only the token's SHA-256 hash is persisted. A duplicate token is refused by
// the primary key rather than being allowed to move an existing session to a
// different account.
func (s *Store) CreateSession(ctx context.Context, token []byte, account Account) error {
	if len(token) != SessionTokenBytes {
		return fmt.Errorf("create session: token is %d bytes, want %d", len(token), SessionTokenBytes)
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer release()

	hash := sha256.Sum256(token)
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO sessions (token_hash, account_email) VALUES (?1, ?2);`, &sqlitex.ExecOptions{
		Args: []any{hash[:], normalizeEmail(account.Email)},
	}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// ResolveSession returns the account currently associated with token. It does
// not apply the account's active flag: deactivation prevents new
// authentication but deliberately leaves an already-issued session valid.
// An unknown or malformed token is a clean miss; a datastore failure is an
// error, so callers cannot accidentally authenticate through one.
func (s *Store) ResolveSession(ctx context.Context, token []byte) (Account, bool, error) {
	if len(token) != SessionTokenBytes {
		return Account{}, false, nil
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return Account{}, false, fmt.Errorf("resolve session: %w", err)
	}
	defer release()

	hash := sha256.Sum256(token)
	var account Account
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT a.email, a.handle, a.role, a.is_active, a.origin_q, a.origin_r
		FROM sessions AS s
		JOIN accounts AS a ON a.email = s.account_email
		WHERE s.token_hash = ?1;`, &sqlitex.ExecOptions{
		Args: []any{hash[:]},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			account.Email = stmt.ColumnText(0)
			account.Handle = stmt.ColumnText(1)
			account.Role = stmt.ColumnText(2)
			account.Active = stmt.ColumnInt(3) != 0
			account.Origin, account.Seated = readOrigin(stmt, 4)
			found = true
			return nil
		},
	}); err != nil {
		return Account{}, false, fmt.Errorf("resolve session: %w", err)
	}
	return account, found, nil
}

// DeleteSession revokes token. Revoking an unknown or malformed token is
// idempotent, as sign-out has no lookup detail to expose.
func (s *Store) DeleteSession(ctx context.Context, token []byte) error {
	if len(token) != SessionTokenBytes {
		return nil
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	defer release()

	hash := sha256.Sum256(token)
	if err := sqlitex.ExecuteTransient(conn, `DELETE FROM sessions WHERE token_hash = ?1;`, &sqlitex.ExecOptions{
		Args: []any{hash[:]},
	}); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

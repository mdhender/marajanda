// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Entity is one of a faction's entities as it stood on one turn.
//
// ID is the entity's identity: an integer that is immutable and never reused.
// Everything else on this struct is a fact read as of a turn, and asking for
// another turn may answer differently. The struct is a snapshot; the rows are
// the truth.
//
// ID is never a PRNG instance key. internal/prng forbids row ids as instance
// keys because they depend on insertion order, and that rule holds here: a rule
// needing per-entity randomness keys on values recorded in history - the
// faction, the creation turn, the code - never on this.
type Entity struct {
	ID       int64
	Code     string
	Name     string
	Kind     game.EntityKind
	Location hexg.Hex
}

// CurrentTurn returns the turn the game is on.
//
// There is one game per database, so there is one clock. Nothing advances it
// yet: a database sits on game.FirstTurn, which is what its factions' founding
// facts are effective from.
func (s *Store) CurrentTurn(ctx context.Context) (int, error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	return readCurrentTurn(conn)
}

// EntitiesAsOf returns the entities a faction held on turn, in the order they
// were created.
//
// The turn is the caller's, not the store's, because a report for turn 3 has to
// read turn 3 however far the game has moved on. "Where was LEADER-1 on turn 7"
// is this query, and it agrees with a replay of the orders because both
// describe the same rows.
func (s *Store) EntitiesAsOf(ctx context.Context, email string, turn int) ([]Entity, error) {
	if !game.ValidTurn(turn) {
		return nil, fmt.Errorf("look up entities: %d is not a turn", turn)
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return readEntities(conn, normalizeEmail(email), turn)
}

func readCurrentTurn(conn *sqlite.Conn) (int, error) {
	turn, found := 0, false
	if err := sqlitex.ExecuteTransient(conn, `SELECT current_turn FROM game WHERE id = 1;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			turn, found = stmt.ColumnInt(0), true
			return nil
		},
	}); err != nil {
		return 0, fmt.Errorf("read current turn: %w", err)
	}
	if !found {
		return 0, errors.New("read current turn: game is not initialized")
	}
	return turn, nil
}

// readEntities joins each entity to the one row of each fact table whose period
// contains the turn. Both joins carry the same between-test, and neither has a
// NULL to reason about: a period that has not ended runs to game.EndOfTimeTurn.
//
// The joins are inner. An entity with no fact covering the turn did not stand
// in the world on that turn, and a query for it should say so by omission.
func readEntities(conn *sqlite.Conn, normalizedEmail string, turn int) ([]Entity, error) {
	entities := make([]Entity, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT entities.id, entity_facts.code, entity_facts.name, entity_facts.kind,
		       entity_locations.q, entity_locations.r
		FROM entities
		JOIN entity_facts ON entity_facts.entity_id = entities.id
			AND entity_facts.effective_from <= ?2 AND ?2 < entity_facts.effective_through
		JOIN entity_locations ON entity_locations.entity_id = entities.id
			AND entity_locations.effective_from <= ?2 AND ?2 < entity_locations.effective_through
		WHERE entities.faction_email = ?1
		ORDER BY entities.id;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			entities = append(entities, Entity{
				ID:       stmt.ColumnInt64(0),
				Code:     stmt.ColumnText(1),
				Name:     stmt.ColumnText(2),
				Kind:     game.EntityKind(stmt.ColumnText(3)),
				Location: hexg.NewHex(stmt.ColumnInt(4), stmt.ColumnInt(5)),
			})
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up entities: %w", err)
	}
	return entities, nil
}

// foundFaction creates the entities a faction starts with, on its origin hex
// and effective from the current turn. Nothing is waiting on a turn to be
// processed, so the founding facts are effective from the turn the faction was
// configured rather than from the one after it.
//
// A faction that already holds an entity has been founded, so this does
// nothing: reconfiguring a faction renames its people, it does not raise a
// second leader.
//
// It runs inside the caller's transaction.
func foundFaction(conn *sqlite.Conn, normalizedEmail string, origin hexg.Hex) error {
	founded, err := factionHasEntities(conn, normalizedEmail)
	if err != nil {
		return err
	}
	if founded {
		return nil
	}
	turn, err := readCurrentTurn(conn)
	if err != nil {
		return err
	}
	for _, kind := range game.FoundingEntityKinds() {
		if _, err := createEntity(conn, normalizedEmail, kind, origin, turn); err != nil {
			return err
		}
	}
	return nil
}

func factionHasEntities(conn *sqlite.Conn, normalizedEmail string) (bool, error) {
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT 1 FROM entities WHERE faction_email = ?1 LIMIT 1;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail},
		ResultFunc: func(*sqlite.Stmt) error {
			found = true
			return nil
		},
	}); err != nil {
		return false, fmt.Errorf("look up faction entities: %w", err)
	}
	return found, nil
}

// createEntity adds one entity to a faction, standing on location and effective
// from turn. Its name defaults to its code; changing it is an order, not a form.
//
// It runs inside the caller's transaction: an entity without the facts that say
// what and where it is has never existed in the world.
func createEntity(conn *sqlite.Conn, normalizedEmail string, kind game.EntityKind, location hexg.Hex, turn int) (Entity, error) {
	if !kind.Valid() {
		return Entity{}, fmt.Errorf("create entity: invalid kind %q", kind)
	}
	if !game.ValidTurn(turn) {
		return Entity{}, fmt.Errorf("create entity: %d is not a turn", turn)
	}
	code, err := nextEntityCode(conn, normalizedEmail, kind)
	if err != nil {
		return Entity{}, err
	}

	entity := Entity{Code: code, Name: code, Kind: kind, Location: location}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entities (faction_email, created_turn) VALUES (?1, ?2) RETURNING id;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			entity.ID = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		return Entity{}, fmt.Errorf("create entity: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6);`, &sqlitex.ExecOptions{
		Args: []any{entity.ID, entity.Code, entity.Name, string(entity.Kind), turn, game.EndOfTimeTurn},
	}); err != nil {
		return Entity{}, fmt.Errorf("create entity facts: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_locations (entity_id, q, r, effective_from, effective_through)
		VALUES (?1, ?2, ?3, ?4, ?5);`, &sqlitex.ExecOptions{
		Args: []any{entity.ID, location.Q(), location.R(), turn, game.EndOfTimeTurn},
	}); err != nil {
		return Entity{}, fmt.Errorf("create entity location: %w", err)
	}
	return entity, nil
}

// nextEntityCode returns the code the faction's next entity of this kind takes.
//
// The sequence is read from the codes a faction has already spent rather than
// from its entities' current kinds. A code is frozen at creation and a kind is
// not, so a hamlet that has grown into a village is still HAMLET-1 and still
// holds hamlet number one.
func nextEntityCode(conn *sqlite.Conn, normalizedEmail string, kind game.EntityKind) (string, error) {
	highest := 0
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT DISTINCT entity_facts.code FROM entity_facts
		JOIN entities ON entities.id = entity_facts.entity_id
		WHERE entities.faction_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			if sequence, ok := game.EntityCodeSequence(kind, stmt.ColumnText(0)); ok && sequence > highest {
				highest = sequence
			}
			return nil
		},
	}); err != nil {
		return "", fmt.Errorf("read entity codes: %w", err)
	}
	return game.EntityCode(kind, highest+1), nil
}

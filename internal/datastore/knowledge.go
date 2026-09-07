// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/cylinder"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// ErrUnknownFaction reports a knowledge read or write for an account that
// controls no faction.
var ErrUnknownFaction = errors.New("account controls no faction")

// KnowledgeAsOf returns what a faction knew about the world on a turn.
//
// It is one read for a faction and a turn rather than a lookup per hex, because
// of how it is used: the orders page prices every one of an entity's orders on
// every write, for every player editing orders, so a per-hex query in a loop is
// the wrong shape. See docs/reference/knowledge.md.
func (s *Store) KnowledgeAsOf(ctx context.Context, email string, turn int) (game.KnowledgeSet, error) {
	if !game.ValidTurn(turn) {
		return nil, fmt.Errorf("read knowledge: %d is not a turn the game can be on", turn)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return readKnowledge(conn, normalizeEmail(email), turn)
}

// MarkEntered records that one of the faction's entities stood in a hex.
//
// The hex becomes explored and its six neighbours become observed, all
// effective from turn+1, because a reveal is a consequence of the turn being
// processed and everything a turn produces is dated from the turn after it.
//
// It is monotone and idempotent. Two entities entering the same hex in one turn
// leave what one entity would have left, and a neighbour sweep never writes a
// hex an entity stood in back down to observed. Turn processing may therefore
// walk a faction's entities in any order and reach the same record, which is
// what keeps a replay honest.
func (s *Store) MarkEntered(ctx context.Context, email string, turn int, entered hexg.Hex) error {
	if !game.ValidTurn(turn) {
		return fmt.Errorf("mark entered: %d is not a turn the game can be on", turn)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer release()

	normalizedEmail := normalizeEmail(email)
	found, err := factionExists(conn, normalizedEmail)
	if err != nil {
		return err
	}
	if !found {
		return ErrUnknownFaction
	}

	cyl, err := readCylinder(conn)
	if err != nil {
		return err
	}

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return fmt.Errorf("mark entered: %w", err)
	}
	defer end(&err)

	err = markEntered(conn, normalizedEmail, turn, entered, cyl)
	return err
}

// markEntered writes one entity's arrival. It runs inside a transaction its
// caller opened, so turn processing can walk a whole turn's movement without
// leaving the record half written.
func markEntered(conn *sqlite.Conn, normalizedEmail string, turn int, entered hexg.Hex, cyl cylinder.Cylinder) error {
	return learnAll(conn, normalizedEmail, turn+1, game.Reveals(cyl, entered))
}

// learnAll writes a list of sightings, effective from a turn.
//
// What a hex reveals is game.Reveals's rule and not this file's. Founding and a
// step reveal the same seven hexes and differ only in the turn they are
// effective from, so they are one write with two callers.
func learnAll(conn *sqlite.Conn, normalizedEmail string, effectiveFrom int, seen []game.Observation) error {
	for _, observation := range seen {
		if err := learn(conn, normalizedEmail, effectiveFrom, observation.Hex, observation.State); err != nil {
			return err
		}
	}
	return nil
}

// foundKnowledge writes what a faction knows the day it is seated: its origin
// hex explored, and the six hexes around it observed.
//
// The facts are effective from the founding turn rather than from the turn
// after it. Nothing about founding is waiting on a turn to be processed, which
// is the same exception the founding entity facts take. A faction therefore
// opens its first turn knowing seven hexes.
func foundKnowledge(conn *sqlite.Conn, normalizedEmail string, origin hexg.Hex, turn int) error {
	cyl, err := readCylinder(conn)
	if err != nil {
		return err
	}

	return learnAll(conn, normalizedEmail, turn, game.Reveals(cyl, origin))
}

// learn records that a faction knows a hex in a state, effective from a turn.
//
// Knowledge is monotone, so this only ever writes upwards. What it compares
// against is the state open at effectiveFrom rather than the state on the turn
// being processed, because every write of one turn lands on the same turn and
// the later writes have to see the earlier ones.
//
// A hex outside the world is not written. The insert selects its coordinates
// from hexes, so the world filters its own neighbours and a step off a pole
// writes nothing rather than failing.
func learn(conn *sqlite.Conn, normalizedEmail string, effectiveFrom int, coord hexg.Hex, state game.Knowledge) error {
	current, openedAt, known, err := readOpenKnowledge(conn, normalizedEmail, coord, effectiveFrom)
	if err != nil {
		return err
	}

	if known {
		if current.AtLeast(state) {
			return nil
		}
		// A row that opened on the turn being written never described a turn
		// boundary: it was written earlier in this same turn's processing.
		// Closing it would leave an empty period, so it goes instead.
		if openedAt == effectiveFrom {
			if err := sqlitex.ExecuteTransient(conn, `
				DELETE FROM faction_knowledge
				WHERE faction_email = ?1 AND q = ?2 AND r = ?3 AND effective_from = ?4;`,
				&sqlitex.ExecOptions{Args: []any{normalizedEmail, coord.Q(), coord.R(), effectiveFrom}},
			); err != nil {
				return fmt.Errorf("replace knowledge: %w", err)
			}
		} else if err := sqlitex.ExecuteTransient(conn, `
			UPDATE faction_knowledge SET effective_through = ?4
			WHERE faction_email = ?1 AND q = ?2 AND r = ?3 AND effective_through = ?5;`,
			&sqlitex.ExecOptions{Args: []any{normalizedEmail, coord.Q(), coord.R(), effectiveFrom, game.EndOfTimeTurn}},
		); err != nil {
			return fmt.Errorf("close knowledge: %w", err)
		}
	}

	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO faction_knowledge (faction_email, q, r, state, effective_from, effective_through)
		SELECT ?1, q, r, ?4, ?5, ?6 FROM hexes WHERE q = ?2 AND r = ?3;`,
		&sqlitex.ExecOptions{Args: []any{
			normalizedEmail, coord.Q(), coord.R(), string(state), effectiveFrom, game.EndOfTimeTurn,
		}},
	); err != nil {
		return fmt.Errorf("record knowledge: %w", err)
	}
	return nil
}

// readOpenKnowledge reads the state of one hex as it stands on a turn, and the
// turn that state opened on. The period test is the one every fact table uses.
func readOpenKnowledge(conn *sqlite.Conn, normalizedEmail string, coord hexg.Hex, turn int) (game.Knowledge, int, bool, error) {
	var state game.Knowledge
	openedAt, found := 0, false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT state, effective_from FROM faction_knowledge
		WHERE faction_email = ?1 AND q = ?2 AND r = ?3
		  AND effective_from <= ?4 AND ?4 < effective_through;`,
		&sqlitex.ExecOptions{
			Args: []any{normalizedEmail, coord.Q(), coord.R(), turn},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				state = game.Knowledge(stmt.ColumnText(0))
				openedAt = stmt.ColumnInt(1)
				found = true
				return nil
			},
		}); err != nil {
		return "", 0, false, fmt.Errorf("read knowledge: %w", err)
	}
	return state, openedAt, found, nil
}

// readKnowledge reads a faction's whole knowledge set as of a turn.
func readKnowledge(conn *sqlite.Conn, normalizedEmail string, turn int) (game.KnowledgeSet, error) {
	found, err := factionExists(conn, normalizedEmail)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUnknownFaction
	}

	known := make(game.KnowledgeSet)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT q, r, state FROM faction_knowledge
		WHERE faction_email = ?1
		  AND effective_from <= ?2 AND ?2 < effective_through;`,
		&sqlitex.ExecOptions{
			Args: []any{normalizedEmail, turn},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				known[hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1))] = game.Knowledge(stmt.ColumnText(2))
				return nil
			},
		}); err != nil {
		return nil, fmt.Errorf("read knowledge: %w", err)
	}
	return known, nil
}

func factionExists(conn *sqlite.Conn, normalizedEmail string) (bool, error) {
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT 1 FROM factions WHERE account_email = ?1;`, &sqlitex.ExecOptions{
		Args:       []any{normalizedEmail},
		ResultFunc: func(stmt *sqlite.Stmt) error { found = true; return nil },
	}); err != nil {
		return false, fmt.Errorf("look up faction: %w", err)
	}
	return found, nil
}

// readCylinder builds the world's east-west wrap from the stored width.
//
// It reads the game record rather than the world: normalizing a coordinate
// needs the number of columns and nothing else, and loading every hex of the
// world to wrap six neighbours would be a poor trade.
func readCylinder(conn *sqlite.Conn) (cylinder.Cylinder, error) {
	record, found, err := readGameRecord(conn)
	if err != nil {
		return cylinder.Cylinder{}, err
	}
	if !found {
		return cylinder.Cylinder{}, errors.New("read world shape: game is not initialized")
	}
	cyl, err := cylinder.New(2*record.Width + 1)
	if err != nil {
		return cylinder.Cylinder{}, fmt.Errorf("read world shape: %w", err)
	}
	return cyl, nil
}

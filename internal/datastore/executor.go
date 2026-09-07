// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"fmt"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/cylinder"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Turn processing: what the stored orders of the turn being closed do to the
// world.
//
// It is the executor half of the pair docs/reference/action-points.md
// describes. The rules are game.Execute's and this file is what feeds them
// rows and writes back what they decided: locations and knowledge, effective
// from turn+1, so a turn-N report still describes the world as it was while
// turn N happened. See docs/reference/turn-processing.md.
//
// It writes no orders. The orders of the closed turn stay exactly as the player
// wrote them, because they and the seeds are the replay.

// processTurn walks every active faction's orders for the turn and applies
// what they did.
//
// It reads only the stored orders and facts dated at or before the turn, so a
// replay of the same rows reaches the same world. Nothing here draws on a
// roller: the movement rule introduces no randomness.
//
// Knowledge is frozen at the turn boundary and every reveal is written
// effective from turn+1, so no entity's march prices another entity's march and
// the walk order does not matter. It is still deterministic - factions by
// email, entities in creation order - because a replay comparing writes should
// not have to sort them first.
//
// It runs inside the caller's transaction. A turn is processed whole or not at
// all: a world half advanced is one no report can describe.
func processTurn(conn *sqlite.Conn, turn int) error {
	cyl, err := readCylinder(conn)
	if err != nil {
		return err
	}
	factions, err := readProcessableFactions(conn)
	if err != nil {
		return err
	}
	world := &worldTerrain{conn: conn, cache: make(map[hexg.Hex]game.Terrain)}
	for _, email := range factions {
		if err := processFaction(conn, email, turn, cyl, world); err != nil {
			return err
		}
	}
	return nil
}

// processFaction walks one faction's entities.
//
// The knowledge set is read once for the faction rather than once per entity.
// It is the set effective when the turn opened, and it stays that set for the
// whole faction: every reveal this turn produces is dated from turn+1, so a
// re-read would answer the same rows anyway, and reading once says so.
func processFaction(conn *sqlite.Conn, normalizedEmail string, turn int, cyl cylinder.Cylinder, world *worldTerrain) error {
	known, err := readKnowledge(conn, normalizedEmail, turn)
	if err != nil {
		return err
	}
	entities, err := readEntities(conn, normalizedEmail, turn)
	if err != nil {
		return err
	}
	orders, err := readFactionOrders(conn, normalizedEmail, turn)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		// An entity nobody gave orders to is walked anyway. It does nothing
		// and reveals nothing, but its whole allowance lapses, and that is a
		// line a player asks about: the empty turn is recorded rather than
		// left to be inferred from a missing row.
		outcome := game.Execute(game.Plan{
			Sight:     game.GroundTruth(world.at),
			World:     cyl,
			Knowledge: known,
			Start:     entity.Location,
			Allowance: entity.Allowance,
			Orders:    orders[entity.ID],
		})
		if err := world.err; err != nil {
			// A costing that could not read the world decided a step on a hex
			// it never saw. Nothing it produced can be written.
			return err
		}
		if err := applyOutcome(conn, normalizedEmail, turn, entity, outcome, cyl); err != nil {
			return err
		}
	}
	return nil
}

// applyOutcome writes what one entity's turn produced: the record of it, and
// the facts it changed.
//
// The result is written first and it is written whole. It is the reason the
// facts that follow exist, so the two are one transaction and a report can
// always answer why a leader is where it is.
//
// The order of the knowledge writes does not matter and neither does the order
// of the entities: knowledge is monotone and idempotent, and a location is
// written once per entity per turn. What matters is that all of it is dated
// from turn+1.
func applyOutcome(conn *sqlite.Conn, normalizedEmail string, turn int, entity Entity, outcome game.Outcome, cyl cylinder.Cylinder) error {
	if err := writeResult(conn, turn, entity, outcome); err != nil {
		return err
	}
	for _, observation := range outcome.Observations {
		// The sightings the turn produced are what the faction's knowledge is
		// written from, so the record of the turn and the record of what the
		// faction knows are one list read twice rather than two rules.
		if err := learn(conn, normalizedEmail, turn+1, observation.Hex, observation.State); err != nil {
			return err
		}
	}
	if outcome.End == entity.Location {
		// An entity that ended where it started has the same location fact it
		// had, and rewriting it would close a period at the turn it describes.
		return nil
	}
	return moveEntity(conn, entity.ID, turn, outcome.End)
}

// moveEntity dates the entity's old location to turn+1 and opens its new one
// there.
//
// This is the effective-dating rule and not an update: the row that said where
// the entity was during the turn stays, closed at the turn after it, so a
// turn-N report reads a turn-N location. See docs/reference/entities.md.
func moveEntity(conn *sqlite.Conn, entityID int64, turn int, to hexg.Hex) error {
	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE entity_locations SET effective_through = ?2
		WHERE entity_id = ?1 AND effective_through = ?3;`, &sqlitex.ExecOptions{
		Args: []any{entityID, turn + 1, game.EndOfTimeTurn},
	}); err != nil {
		return fmt.Errorf("close entity location: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_locations (entity_id, q, r, effective_from, effective_through)
		VALUES (?1, ?2, ?3, ?4, ?5);`, &sqlitex.ExecOptions{
		Args: []any{entityID, to.Q(), to.R(), turn + 1, game.EndOfTimeTurn},
	}); err != nil {
		return fmt.Errorf("move entity: %w", err)
	}
	return nil
}

// readProcessableFactions returns the factions a turn is processed for, by
// email and in a stable order.
//
// A deactivated faction is not one of them. It gives no orders, so the orders
// it wrote while it was active are history rather than instructions: they stay
// exactly as they were written and nothing carries them out. See
// docs/reference/turn-processing.md#inactive-factions.
func readProcessableFactions(conn *sqlite.Conn) ([]string, error) {
	emails := make([]string, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT account_email FROM factions WHERE is_active = 1 ORDER BY account_email;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			emails = append(emails, stmt.ColumnText(0))
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up factions: %w", err)
	}
	return emails, nil
}

// worldTerrain reads the terrain of one hex at a time, remembering what it
// read.
//
// The executor needs ground truth for the handful of hexes a turn's steps aim
// at, not the whole map: loading a hundred and thirty thousand hexes to walk
// six of them is the wrong trade, and the read has to run on the connection
// holding the transaction so it sees what this turn has already written.
//
// The lookup a game.Sight takes cannot report an error, so a failed read is
// kept here and the caller asks for it before writing anything the costing
// decided.
type worldTerrain struct {
	conn  *sqlite.Conn
	cache map[hexg.Hex]game.Terrain
	err   error
}

// at answers with the terrain of a hex, reporting false for a coordinate the
// world does not have.
//
// A hex the world does not have is cached as the empty terrain, which is not a
// terrain the game knows, so a miss is remembered as cheaply as a hit. Rows do
// not wrap, so a step off a pole is exactly that miss.
func (w *worldTerrain) at(coord hexg.Hex) (game.Terrain, bool) {
	if terrain, cached := w.cache[coord]; cached {
		return terrain, terrain != ""
	}
	var terrain game.Terrain
	if err := sqlitex.ExecuteTransient(w.conn, `
		SELECT terrain FROM hexes WHERE q = ?1 AND r = ?2;`, &sqlitex.ExecOptions{
		Args: []any{coord.Q(), coord.R()},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			terrain = game.Terrain(stmt.ColumnText(0))
			return nil
		},
	}); err != nil {
		if w.err == nil {
			w.err = fmt.Errorf("read terrain: %w", err)
		}
		return "", false
	}
	w.cache[coord] = terrain
	return terrain, terrain != ""
}

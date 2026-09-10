// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"fmt"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// The order pre-processor: what a faction's orders cost it during entry, and
// how many action points they leave idle.
//
// It is one of two engines and the distinction is the point. This one serves
// order entry, it prices what the player wrote, and it binds nothing.
// The executor serves turn processing, writes no orders, and decides what
// happened. Its numbers are an aid to order entry, not a quote: see
// docs/reference/action-points.md#the-two-engines.
//
// Both price through one function, game.Price, so the two agree exactly over
// ground the faction knows. Where they differ the difference is exploration and
// nothing else, and it comes out of the entity's remaining orders.

// EstimateOrders prices every one of a faction's entities' orders on a turn.
//
// The estimate is fogged: unknown ground is priced at the flat exploration cost
// whatever is actually there, and every order is assumed to land. That is not a
// limitation of this method, it is the rule - a cost that varied with terrain a
// faction has not seen would tell a player what is there. The accuracy level is
// never read from a request.
//
// Every stored order is one the player authored, and all of them are priced.
// What the allowance leaves over is reported as game.Estimate.Residue - a
// number, not an order. Nothing here rests an entity that did not ask to: a
// player may be spending an entity to exhaustion on purpose, and idle points
// are theirs to leave idle.
//
// An entity that takes no orders has no allowance and prices to a zero
// estimate. It is in the map so a caller can render its section without asking
// whether it is there.
func (s *Store) EstimateOrders(ctx context.Context, email string, turn int) (map[int64]game.Estimate, error) {
	if !game.ValidTurn(turn) {
		return nil, fmt.Errorf("price orders: %d is not a turn", turn)
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	normalizedEmail := normalizeEmail(email)
	known, err := readKnowledge(conn, normalizedEmail, turn)
	if err != nil {
		return nil, err
	}
	cyl, err := readCylinder(conn)
	if err != nil {
		return nil, err
	}
	entities, err := readEntities(conn, normalizedEmail, turn)
	if err != nil {
		return nil, err
	}
	orders, err := readFactionOrders(conn, normalizedEmail, turn)
	if err != nil {
		return nil, err
	}

	estimates := make(map[int64]game.Estimate, len(entities))
	for _, entity := range entities {
		estimates[entity.ID] = game.Price(game.Plan{
			Sight:     game.Fogged(),
			Kind:      entity.Kind,
			World:     cyl,
			Knowledge: known,
			Start:     entity.Location,
			Allowance: entity.Allowance,
			Orders:    orders[entity.ID],
		})
	}
	return estimates, nil
}

// readEntityStanding reads what and where one of the faction's entities was on
// a turn and what it had to spend, reporting whether it stood in the world.
//
// The joins are the ones readEntities makes, for one entity: the location is a
// fact and so is the allowance, and an entity with no location fact covering
// the turn was not in the world on it.
func readEntityStanding(conn *sqlite.Conn, normalizedEmail string, entityID int64, turn int) (hexg.Hex, game.EntityKind, int, bool, error) {
	var location hexg.Hex
	var kind game.EntityKind
	allowance, standing := 0, false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT entity_locations.q, entity_locations.r, entity_facts.kind,
		       COALESCE(entity_allowances.points, 0)
		FROM entities
		JOIN entity_facts ON entity_facts.entity_id = entities.id
			AND entity_facts.effective_from <= ?3 AND ?3 < entity_facts.effective_through
		JOIN entity_locations ON entity_locations.entity_id = entities.id
			AND entity_locations.effective_from <= ?3 AND ?3 < entity_locations.effective_through
		LEFT JOIN entity_allowances ON entity_allowances.entity_id = entities.id
			AND entity_allowances.effective_from <= ?3 AND ?3 < entity_allowances.effective_through
		WHERE entities.id = ?1 AND entities.faction_email = ?2;`, &sqlitex.ExecOptions{
		Args: []any{entityID, normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			location = hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1))
			kind = game.EntityKind(stmt.ColumnText(2))
			allowance = stmt.ColumnInt(3)
			standing = true
			return nil
		},
	}); err != nil {
		return hexg.Hex{}, "", 0, false, fmt.Errorf("look up entity standing: %w", err)
	}
	return location, kind, allowance, standing, nil
}

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
// the trailing Rest that shows what they leave unspent.
//
// It is one of two engines and the distinction is the point. This one serves
// order entry, it writes orders on the player's behalf, and it binds nothing.
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
// The orders priced are the ones the player authored. A trailing Rest is the
// residue rather than an order in its own right, so it is left out of the list
// and reported as game.Estimate.Residue.
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
		authored, _ := game.SplitTrailingRest(orders[entity.ID])
		estimates[entity.ID] = game.Price(game.Plan{
			Sight:     game.Fogged(),
			World:     cyl,
			Knowledge: known,
			Start:     entity.Location,
			Allowance: entity.Allowance,
			Orders:    authored,
		})
	}
	return estimates, nil
}

// stripTrailingRest takes the residue off an entity's list before a write.
//
// The list a write addresses is the list the player authored: an append goes on
// the end of that, and a position is the position the page showed. Taking the
// Rest off renumbers nothing, because it is the last order there is.
//
// It runs inside the caller's transaction.
func stripTrailingRest(conn *sqlite.Conn, turn int, entityID int64) error {
	orders, err := readEntityOrders(conn, entityID, turn)
	if err != nil {
		return err
	}
	if _, trailing := game.SplitTrailingRest(orders); !trailing {
		return nil
	}
	// The detail row goes with it: rest_orders cascades from orders.
	if err := sqlitex.ExecuteTransient(conn, `
		DELETE FROM orders WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, orders[len(orders)-1].Seq},
	}); err != nil {
		return fmt.Errorf("remove trailing rest: %w", err)
	}
	return nil
}

// syncTrailingRest puts the residue back after a write, as a Rest whose count
// is what the orders before it leave unspent.
//
// The line is rendered always and the row is stored only when the count is at
// least one. A Rest x0 is an order that costs nothing and does nothing, and
// turn processing would have to walk it and record a result for it.
//
// A Rest the player placed last is the residue. There is nothing to tell it
// apart from one written here, and nothing that needs to be: unspent points at
// the end of a turn are what a trailing Rest means, and a rest a player wants
// to keep at a length of their own goes somewhere other than the end.
//
// It runs inside the caller's transaction.
func syncTrailingRest(conn *sqlite.Conn, normalizedEmail string, turn int, entityID int64) error {
	location, allowance, standing, err := readEntityStanding(conn, normalizedEmail, entityID, turn)
	if err != nil {
		return err
	}
	// An entity that did not stand in the world on the turn has no plan to
	// price, and one with no allowance takes no orders at all.
	if !standing || allowance < 1 {
		return nil
	}
	orders, err := readEntityOrders(conn, entityID, turn)
	if err != nil {
		return err
	}
	authored, trailing := game.SplitTrailingRest(orders)

	known, err := readKnowledge(conn, normalizedEmail, turn)
	if err != nil {
		return err
	}
	cyl, err := readCylinder(conn)
	if err != nil {
		return err
	}
	residue := game.Price(game.Plan{
		Sight:     game.Fogged(),
		World:     cyl,
		Knowledge: known,
		Start:     location,
		Allowance: allowance,
		Orders:    authored,
	}).Residue

	switch {
	case residue < 1 && trailing:
		return stripTrailingRest(conn, turn, entityID)
	case residue < 1:
		return nil
	case trailing:
		last := orders[len(orders)-1]
		last.Detail.Count = residue
		return writeOrderDetail(conn, turn, entityID, last)
	case len(orders) >= MaxOrdersPerEntity:
		// A list already at the storage bound has no room for the residue. It
		// is overspending long before this, so there is nothing to show.
		return nil
	default:
		return insertOrder(conn, turn, entityID, game.Order{
			Seq:    len(orders) + 1,
			Kind:   game.OrderKindRest,
			Detail: game.OrderDetail{Count: residue},
		})
	}
}

// readEntityStanding reads where one of the faction's entities stood on a turn
// and what it had to spend, reporting whether it stood in the world at all.
//
// The joins are the ones readEntities makes, for one entity: the location is a
// fact and so is the allowance, and an entity with no location fact covering
// the turn was not in the world on it.
func readEntityStanding(conn *sqlite.Conn, normalizedEmail string, entityID int64, turn int) (hexg.Hex, int, bool, error) {
	var location hexg.Hex
	allowance, standing := 0, false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT entity_locations.q, entity_locations.r, COALESCE(entity_allowances.points, 0)
		FROM entities
		JOIN entity_locations ON entity_locations.entity_id = entities.id
			AND entity_locations.effective_from <= ?3 AND ?3 < entity_locations.effective_through
		LEFT JOIN entity_allowances ON entity_allowances.entity_id = entities.id
			AND entity_allowances.effective_from <= ?3 AND ?3 < entity_allowances.effective_through
		WHERE entities.id = ?1 AND entities.faction_email = ?2;`, &sqlitex.ExecOptions{
		Args: []any{entityID, normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			location = hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1))
			allowance = stmt.ColumnInt(2)
			standing = true
			return nil
		},
	}); err != nil {
		return hexg.Hex{}, 0, false, fmt.Errorf("look up entity standing: %w", err)
	}
	return location, allowance, standing, nil
}

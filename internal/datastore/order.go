// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// The reasons an order is refused. They are returned to a player, so a handler
// tells them apart to answer with the right status and the right sentence.
var (
	// ErrTurnClosed reports a write aimed at a turn that is not the one the
	// game is on. Only the current turn's orders are writable: advancing the
	// turn is what freezes the turn before it.
	ErrTurnClosed = errors.New("only the current turn's orders can be changed")

	// ErrUnknownEntity reports an entity that is not the faction's, or that did
	// not stand in the world on the turn.
	ErrUnknownEntity = errors.New("that is not one of the faction's entities")

	// ErrOrderKindRefused reports an order kind the entity's kind does not
	// accept. Which kinds an entity accepts is a game rule; see
	// game.EntityKind.Accepts.
	ErrOrderKindRefused = errors.New("that entity does not take that order")

	// ErrUnknownOrder reports an order the entity does not have. An insert
	// addresses the position an order is to take, so for it the position one
	// past the end is a place and anything beyond that is not.
	ErrUnknownOrder = errors.New("that order is not one of the entity's")

	// ErrTooManyOrders reports an entity given more orders in a turn than
	// storage allows. It is what keeps a tolerated overspend bounded and not
	// the movement allowance; see MaxOrdersPerEntity.
	ErrTooManyOrders = fmt.Errorf("an entity carries at most %d orders in a turn", MaxOrdersPerEntity)

	// ErrFactionInactive reports a write by a faction that has been
	// deactivated. A deactivated faction cannot give orders; its player can
	// still sign in and look at their game.
	ErrFactionInactive = errors.New("that faction is not active")
)

// Order is one of an entity's orders for a turn: an order kind and whatever
// that kind needs to be carried out.
//
// An order is one action. A move goes one way, so "move nw ne e" is three
// orders and not one order carrying three directions.
type Order struct {
	Seq  int
	Kind game.OrderKind
	// Direction is the way a move goes. The zero value is not a compass point,
	// so it is a move a player has added and not yet said the direction of,
	// and it is what an order of a kind that has no direction carries.
	Direction compass.Point
}

// OrderDirection addresses one order's direction, for a save that carries a
// whole page of them.
type OrderDirection struct {
	EntityID  int64
	Seq       int
	Direction compass.Point
}

// OrdersAsOf returns the orders a faction's entities carry on turn, keyed by
// entity and in sequence order.
//
// The turn is the caller's, the way it is for EntitiesAsOf. Past turns are
// stored and are read by exactly this query; only the current turn is writable.
func (s *Store) OrdersAsOf(ctx context.Context, email string, turn int) (map[int64][]Order, error) {
	if !game.ValidTurn(turn) {
		return nil, fmt.Errorf("look up orders: %d is not a turn", turn)
	}
	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return readFactionOrders(conn, normalizeEmail(email), turn)
}

// AddOrder appends an order to an entity's list for the turn and returns its
// sequence number.
//
// The direction arrives with the kind, because an order is one action and a
// move that goes nowhere is not one. The blank direction is still allowed: it
// is what the add control on the page sends, and it means an order a player
// has added and not yet filled in.
//
// The entity's kind decides which order kinds it accepts, so a kind it does not
// accept is refused here as well as omitted from the form. A hand-built request
// cannot do what the form declines to show.
func (s *Store) AddOrder(ctx context.Context, email string, turn int, entityID int64, kind game.OrderKind, direction compass.Point) (_ int, err error) {
	seq := 0
	if err := s.writeOrders(ctx, email, turn, func(conn *sqlite.Conn) error {
		orders, err := readOrderableEntityOrders(conn, "add order", email, turn, entityID, kind)
		if err != nil {
			return err
		}
		seq = len(orders) + 1
		return insertOrder(conn, turn, entityID, Order{Seq: seq, Kind: kind, Direction: direction})
	}); err != nil {
		return 0, err
	}
	return seq, nil
}

// InsertOrder puts an order at a position in an entity's list, shifting the
// orders from that position on up by one.
//
// seq is the position the new order takes, from 1 to one past the end. One
// past the end is an append, which is what the control that inserts after the
// last order asks for.
func (s *Store) InsertOrder(ctx context.Context, email string, turn int, entityID int64, seq int, kind game.OrderKind, direction compass.Point) error {
	return s.writeOrders(ctx, email, turn, func(conn *sqlite.Conn) error {
		orders, err := readOrderableEntityOrders(conn, "insert order", email, turn, entityID, kind)
		if err != nil {
			return err
		}
		if seq < 1 || seq > len(orders)+1 {
			return fmt.Errorf("insert order: %w: %d", ErrUnknownOrder, seq)
		}
		inserted := make([]Order, 0, len(orders)+1)
		inserted = append(inserted, orders[:seq-1]...)
		inserted = append(inserted, Order{Kind: kind, Direction: direction})
		inserted = append(inserted, orders[seq-1:]...)
		return rewriteEntityOrders(conn, turn, entityID, inserted)
	})
}

// SetOrderDirection sets which way one order goes.
//
// An invalid direction - the compass point's zero value - is the blank option,
// and it leaves the order in place with nothing said about where it goes.
// Removing the order is RemoveOrder's work, not the blank option's.
func (s *Store) SetOrderDirection(ctx context.Context, email string, turn int, entityID int64, seq int, direction compass.Point) error {
	return s.SetOrderDirections(ctx, email, turn, []OrderDirection{
		{EntityID: entityID, Seq: seq, Direction: direction},
	})
}

// SetOrderDirections sets the direction of every order it is given, in one
// transaction.
//
// It is what the script-free page's one Save button saves: a whole page of
// selects, applied together or not at all.
func (s *Store) SetOrderDirections(ctx context.Context, email string, turn int, directions []OrderDirection) error {
	if len(directions) == 0 {
		return nil
	}
	return s.writeOrders(ctx, email, turn, func(conn *sqlite.Conn) error {
		for _, wanted := range directions {
			if err := requireEntity(conn, normalizeEmail(email), wanted.EntityID); err != nil {
				return err
			}
			orders, err := readEntityOrders(conn, wanted.EntityID, turn)
			if err != nil {
				return err
			}
			index := indexOfOrder(orders, wanted.Seq)
			if index < 0 {
				return fmt.Errorf("set order direction: %w: %d", ErrUnknownOrder, wanted.Seq)
			}
			orders[index].Direction = wanted.Direction
			if err := writeOrderDetail(conn, turn, wanted.EntityID, orders[index]); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveOrder deletes an order and renumbers the ones after it, so the
// sequence stays 1..N with no gap.
//
// Only the open turn is touched. Nothing removes an order from a turn that has
// been advanced past.
func (s *Store) RemoveOrder(ctx context.Context, email string, turn int, entityID int64, seq int) error {
	return s.writeOrders(ctx, email, turn, func(conn *sqlite.Conn) error {
		if err := requireEntity(conn, normalizeEmail(email), entityID); err != nil {
			return err
		}
		orders, err := readEntityOrders(conn, entityID, turn)
		if err != nil {
			return err
		}
		index := indexOfOrder(orders, seq)
		if index < 0 {
			return fmt.Errorf("remove order: %w: %d", ErrUnknownOrder, seq)
		}
		return rewriteEntityOrders(conn, turn, entityID, append(orders[:index:index], orders[index+1:]...))
	})
}

// AdvanceTurn moves the game's clock on by one and returns the turn it now
// sits on.
//
// Advancing is what freezes the orders of the turn left behind: every write
// checks the current turn, so the rows of a turn the game has moved past are
// read-only from here on. Processing those orders is a separate piece of work.
func (s *Store) AdvanceTurn(ctx context.Context) (_ int, err error) {
	conn, release, err := s.take(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return 0, err
	}
	defer end(&err)

	turn, err := readCurrentTurn(conn)
	if err != nil {
		return 0, err
	}
	next := turn + 1
	if !game.ValidTurn(next) {
		return 0, fmt.Errorf("advance turn: %d is not a turn", next)
	}
	if err := sqlitex.ExecuteTransient(conn, `UPDATE game SET current_turn = ?1 WHERE id = 1;`, &sqlitex.ExecOptions{
		Args: []any{next},
	}); err != nil {
		return 0, fmt.Errorf("advance turn: %w", err)
	}
	return next, nil
}

// writeOrders runs one order write inside a transaction, after the two checks
// every order write starts with.
//
// The gates are the store's own invariants rather than checks a caller can
// arrange to pass, so they belong to the write rather than to each method that
// makes one.
func (s *Store) writeOrders(ctx context.Context, email string, turn int, write func(*sqlite.Conn) error) (err error) {
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

	if err := requireActiveFaction(conn, normalizeEmail(email)); err != nil {
		return err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return err
	}
	return write(conn)
}

// requireOpenTurn refuses a write aimed at any turn but the one the game is on.
//
// This is the whole of the history rule, and it is the store's own invariant
// rather than a check a caller can arrange to pass: whatever turn a caller
// hands in, only the open one is written. A handler reads the current turn and
// writes for that turn, so what this catches is the clock moving between the
// two - the admin advancing the turn while a request is in flight - and every
// later attempt to edit a turn that has been closed.
func requireOpenTurn(conn *sqlite.Conn, turn int) error {
	current, err := readCurrentTurn(conn)
	if err != nil {
		return err
	}
	if turn != current {
		return fmt.Errorf("%w: turn %d, and the game is on turn %d", ErrTurnClosed, turn, current)
	}
	return nil
}

// requireActiveFaction refuses a write by a faction that has been deactivated.
//
// It sits beside requireOpenTurn and does the same kind of work: it is the
// store's own invariant rather than a check a caller can arrange to pass. The
// page declines to show the controls, and this is what a hand-built request
// meets - the rule order legality already follows.
//
// A missing faction is refused too. Nothing without a faction row owns an
// entity, so this is a floor rather than a rendered state.
func requireActiveFaction(conn *sqlite.Conn, normalizedEmail string) error {
	active := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT is_active FROM factions WHERE account_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			active = stmt.ColumnInt(0) != 0
			return nil
		},
	}); err != nil {
		return fmt.Errorf("look up faction: %w", err)
	}
	if !active {
		return fmt.Errorf("%w: %s", ErrFactionInactive, normalizedEmail)
	}
	return nil
}

// requireEntity confirms an entity belongs to the faction.
//
// Ownership is on the entity row rather than in a fact, because nothing
// transfers an entity between factions, so this asks no turn.
func requireEntity(conn *sqlite.Conn, normalizedEmail string, entityID int64) error {
	found := false
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT 1 FROM entities WHERE id = ?1 AND faction_email = ?2;`, &sqlitex.ExecOptions{
		Args: []any{entityID, normalizedEmail},
		ResultFunc: func(*sqlite.Stmt) error {
			found = true
			return nil
		},
	}); err != nil {
		return fmt.Errorf("look up entity: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: %d", ErrUnknownEntity, entityID)
	}
	return nil
}

// readEntityKind returns the kind an entity held on turn, confirming that it is
// the faction's and that it stood in the world on that turn.
func readEntityKind(conn *sqlite.Conn, normalizedEmail string, entityID int64, turn int) (game.EntityKind, error) {
	var kind game.EntityKind
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT entity_facts.kind FROM entities
		JOIN entity_facts ON entity_facts.entity_id = entities.id
			AND entity_facts.effective_from <= ?3 AND ?3 < entity_facts.effective_through
		WHERE entities.id = ?1 AND entities.faction_email = ?2;`, &sqlitex.ExecOptions{
		Args: []any{entityID, normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			kind = game.EntityKind(stmt.ColumnText(0))
			return nil
		},
	}); err != nil {
		return "", fmt.Errorf("look up entity kind: %w", err)
	}
	if kind == "" {
		return "", fmt.Errorf("%w: %d", ErrUnknownEntity, entityID)
	}
	return kind, nil
}

// readOrderableEntityOrders is what the two writes that lengthen a list start
// with: it confirms the entity is the faction's, that it stood in the world on
// the turn and that its kind takes the order kind, and reads the list the new
// order joins.
func readOrderableEntityOrders(conn *sqlite.Conn, what, email string, turn int, entityID int64, kind game.OrderKind) ([]Order, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("%s: %w: %q", what, ErrOrderKindRefused, kind)
	}
	entityKind, err := readEntityKind(conn, normalizeEmail(email), entityID, turn)
	if err != nil {
		return nil, err
	}
	if !entityKind.Accepts(kind) {
		return nil, fmt.Errorf("%s: %w: %s takes no %s", what, ErrOrderKindRefused, entityKind, kind)
	}
	orders, err := readEntityOrders(conn, entityID, turn)
	if err != nil {
		return nil, err
	}
	if len(orders) >= MaxOrdersPerEntity {
		return nil, fmt.Errorf("%s: %w", what, ErrTooManyOrders)
	}
	return orders, nil
}

// readFactionOrders reads every order the faction's entities carry on turn.
//
// The join to the detail table is outer: an order with no direction yet is one
// a player has added and not filled in, and it has to come back so the page can
// draw its blank select.
func readFactionOrders(conn *sqlite.Conn, normalizedEmail string, turn int) (map[int64][]Order, error) {
	orders := make(map[int64][]Order)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT orders.entity_id, orders.seq, orders.kind, move_orders.direction
		FROM orders
		JOIN entities ON entities.id = orders.entity_id
		LEFT JOIN move_orders ON move_orders.turn = orders.turn
			AND move_orders.entity_id = orders.entity_id AND move_orders.seq = orders.seq
		WHERE entities.faction_email = ?1 AND orders.turn = ?2
		ORDER BY orders.entity_id, orders.seq;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			entityID := stmt.ColumnInt64(0)
			order, err := scanOrder(stmt, 1)
			if err != nil {
				return fmt.Errorf("entity %d: %w", entityID, err)
			}
			orders[entityID] = append(orders[entityID], order)
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up orders: %w", err)
	}
	return orders, nil
}

// readEntityOrders reads one entity's orders for a turn, in sequence order.
// It runs inside the caller's transaction.
func readEntityOrders(conn *sqlite.Conn, entityID int64, turn int) ([]Order, error) {
	orders := make([]Order, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT orders.seq, orders.kind, move_orders.direction
		FROM orders
		LEFT JOIN move_orders ON move_orders.turn = orders.turn
			AND move_orders.entity_id = orders.entity_id AND move_orders.seq = orders.seq
		WHERE orders.turn = ?1 AND orders.entity_id = ?2
		ORDER BY orders.seq;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			order, err := scanOrder(stmt, 0)
			if err != nil {
				return err
			}
			orders = append(orders, order)
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up orders: %w", err)
	}
	return orders, nil
}

// scanOrder reads a sequence, a kind and a direction from three columns
// starting at first. An empty direction column is the outer join finding no
// detail row, which is an order with nothing said about where it goes.
func scanOrder(stmt *sqlite.Stmt, first int) (Order, error) {
	order := Order{Seq: stmt.ColumnInt(first), Kind: game.OrderKind(stmt.ColumnText(first + 1))}
	if direction := stmt.ColumnText(first + 2); direction != "" {
		point, err := compass.Parse(direction)
		if err != nil {
			return Order{}, fmt.Errorf("order %d: %w", order.Seq, err)
		}
		order.Direction = point
	}
	return order, nil
}

func indexOfOrder(orders []Order, seq int) int {
	for index, order := range orders {
		if order.Seq == seq {
			return index
		}
	}
	return -1
}

// insertOrder writes one order and its detail row. It runs inside the caller's
// transaction.
func insertOrder(conn *sqlite.Conn, turn int, entityID int64, order Order) error {
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO orders (turn, entity_id, seq, kind) VALUES (?1, ?2, ?3, ?4);`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, order.Seq, string(order.Kind)},
	}); err != nil {
		return fmt.Errorf("add order: %w", err)
	}
	return writeOrderDetail(conn, turn, entityID, order)
}

// writeOrderDetail replaces what an order's kind stores beyond its kind.
//
// A move with a direction is one row in move_orders; a move without one is no
// row at all, which is how an order a player has added and not filled in is
// stored. It runs inside the caller's transaction.
func writeOrderDetail(conn *sqlite.Conn, turn int, entityID int64, order Order) error {
	if err := sqlitex.ExecuteTransient(conn, `
		DELETE FROM move_orders WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, order.Seq},
	}); err != nil {
		return fmt.Errorf("clear order direction: %w", err)
	}
	if order.Direction == 0 {
		return nil
	}
	if !order.Direction.IsValid() {
		return fmt.Errorf("set order direction: %w: %s", compass.ErrUnknownPoint, order.Direction)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO move_orders (turn, entity_id, seq, direction) VALUES (?1, ?2, ?3, ?4);`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, order.Seq, storedDirection(order.Direction)},
	}); err != nil {
		return fmt.Errorf("set order direction: %w", err)
	}
	return nil
}

// rewriteEntityOrders replaces an entity's orders for a turn with the list it
// is given, renumbering them 1..N.
//
// Delete and reinsert rather than an UPDATE that shifts sequence numbers: the
// detail rows hang off the sequence number, and a bulk shift would depend on
// the order SQLite happened to walk the rows in to avoid colliding with a
// number that has not moved yet. An entity's orders are a handful of rows.
// It runs inside the caller's transaction.
func rewriteEntityOrders(conn *sqlite.Conn, turn int, entityID int64, orders []Order) error {
	if len(orders) > MaxOrdersPerEntity {
		return fmt.Errorf("write orders: %w", ErrTooManyOrders)
	}
	// The detail rows go with them: move_orders cascades from orders.
	if err := sqlitex.ExecuteTransient(conn, `
		DELETE FROM orders WHERE turn = ?1 AND entity_id = ?2;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID},
	}); err != nil {
		return fmt.Errorf("remove orders: %w", err)
	}
	for index, order := range orders {
		order.Seq = index + 1
		if err := insertOrder(conn, turn, entityID, order); err != nil {
			return err
		}
	}
	return nil
}

// storedDirection is the form a direction takes on disk: the lowercase
// abbreviation, which is what compass.Parse accepts and what terrain and race
// already do.
func storedDirection(point compass.Point) string {
	return strings.ToLower(point.String())
}

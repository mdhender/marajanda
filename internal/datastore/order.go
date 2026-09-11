// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

	// ErrOrderCountRefused reports a rest whose count is not one the schema
	// admits. A rest lasts at least one action point, because a Rest x0 is an
	// order that costs nothing and does nothing, and at most the order limit,
	// which is what keeps a tolerated overspend bounded.
	ErrOrderCountRefused = fmt.Errorf("a rest lasts from 1 to %d action points", MaxOrdersPerEntity)

	// ErrOrderDetailRefused reports detail that belongs to a different order
	// kind. A move carries only a direction and a rest carries only a count.
	ErrOrderDetailRefused = errors.New("that detail does not belong to the order")

	// ErrFactionInactive reports a write by a faction that has been
	// deactivated. A deactivated faction cannot give orders; its player can
	// still sign in and look at their game.
	ErrFactionInactive = errors.New("that faction is not active")

	// ErrOrdersChanged reports a write that named the order list it expected to
	// be writing to, and found another. Somebody else wrote in between; the
	// caller's view is stale and the write did nothing. See ExpectOrders.
	ErrOrdersChanged = errors.New("the orders changed since they were read")
)

// OrderWriteOption conditions one order write. It is variadic on every write so
// that a caller with nothing to say passes nothing: the page, which is the only
// client of a turn when it is the only client of a turn, is unchanged by the
// existence of this.
type OrderWriteOption func(*orderWriteOptions)

type orderWriteOptions struct {
	expect    string
	expectSet bool
}

// ExpectOrders makes a write conditional on the faction's orders for the turn
// still hashing to etag, which is what OrdersETag returned when the caller read
// them. A write whose expectation does not hold fails with ErrOrdersChanged and
// writes nothing.
//
// The comparison happens inside the write's own transaction, so this is a real
// precondition rather than a look before a leap: no other write can land
// between the check and the write it guards.
func ExpectOrders(etag string) OrderWriteOption {
	return func(options *orderWriteOptions) { options.expect, options.expectSet = etag, true }
}

// OrderExpectation reports the tag a set of write options expects, and whether
// they expect one at all.
//
// The option itself is opaque so that what it carries can change. This is the
// way to read it, and it exists because *Store is not the only implementation
// of the order writes: the server takes an interface, and a fake standing in
// for this store has to honour a precondition rather than drop it, or a handler
// that forgets to pass the header through passes its tests.
func OrderExpectation(opts []OrderWriteOption) (string, bool) {
	options := newOrderWriteOptions(opts)
	return options.expect, options.expectSet
}

func newOrderWriteOptions(opts []OrderWriteOption) orderWriteOptions {
	var options orderWriteOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// Order is one of an entity's orders for a turn: an order kind and whatever
// that kind needs to be carried out.
//
// The shape is a game rule and lives in internal/game; this store stores it.
// An order is one action, so "move nw ne e" is three orders and not one order
// carrying three directions.
type Order = game.Order

// OrderUpdate addresses one order's detail, for a save that carries a whole
// page of them.
//
// Which half of the detail is applied is decided by the order's stored kind: a
// move takes the direction and a rest takes the count. A caller cannot give a
// rest a direction by naming one.
type OrderUpdate struct {
	EntityID int64
	Seq      int
	Detail   game.OrderDetail
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
// The detail arrives with the kind, because an order is one action and a move
// that goes nowhere is not one. A blank direction is still allowed: it is what
// the add control on the page sends, and it means an order a player has added
// and not yet filled in. A rest has no blank; it lasts at least one point.
//
// The end of the list is the end of what the player has authored. A trailing
// Rest is not an order the new one goes after: it is the residue, it is taken
// off before the write and put back after it, so an added order always lands in
// front of it. See docs/reference/action-points.md#the-trailing-rest.
//
// The entity's kind decides which order kinds it accepts, so a kind it does not
// accept is refused here as well as omitted from the form. A hand-built request
// cannot do what the form declines to show.
func (s *Store) AddOrder(ctx context.Context, email string, turn int, entityID int64, kind game.OrderKind, detail game.OrderDetail, opts ...OrderWriteOption) (_ int, err error) {
	seq := 0
	if err := s.writeOrders(ctx, email, turn, []int64{entityID}, opts, func(conn *sqlite.Conn) error {
		orders, err := readOrderableEntityOrders(conn, "add order", email, turn, entityID, kind)
		if err != nil {
			return err
		}
		seq = len(orders) + 1
		return insertOrder(conn, turn, entityID, Order{Seq: seq, Kind: kind, Detail: detail})
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
func (s *Store) InsertOrder(ctx context.Context, email string, turn int, entityID int64, seq int, kind game.OrderKind, detail game.OrderDetail, opts ...OrderWriteOption) error {
	return s.writeOrders(ctx, email, turn, []int64{entityID}, opts, func(conn *sqlite.Conn) error {
		orders, err := readOrderableEntityOrders(conn, "insert order", email, turn, entityID, kind)
		if err != nil {
			return err
		}
		if seq < 1 || seq > len(orders)+1 {
			return fmt.Errorf("insert order: %w: %d", ErrUnknownOrder, seq)
		}
		inserted := make([]Order, 0, len(orders)+1)
		inserted = append(inserted, orders[:seq-1]...)
		inserted = append(inserted, Order{Kind: kind, Detail: detail})
		inserted = append(inserted, orders[seq-1:]...)
		return rewriteEntityOrders(conn, turn, entityID, inserted)
	})
}

// ReplaceOrders declares an entity's orders for a turn: the list it is given
// becomes the list, renumbered 1..N, and whatever was there is gone.
//
// It is the operation the incremental controls cannot express. Appending is
// right for building a plan an order at a time, which is what the page does and
// what POST is for, but it cannot say "these are my orders" - and so it cannot
// be retried. A client whose append timed out does not know whether it landed,
// and sending it again appends a second copy. Sending this again sends the same
// list, which is the same list.
//
// Every order is checked the way one added order is: the kind has to be one the
// game has and one the entity's kind accepts, the detail has to suit the kind,
// and the list has to fit inside MaxOrdersPerEntity. Nothing is written unless
// all of it can be.
//
// An empty list is a legal declaration. It means the entity has no orders this
// turn, which is a thing a player may mean and had no way to say in one request.
func (s *Store) ReplaceOrders(ctx context.Context, email string, turn int, entityID int64, orders []Order, opts ...OrderWriteOption) error {
	return s.writeOrders(ctx, email, turn, []int64{entityID}, opts, func(conn *sqlite.Conn) error {
		entityKind, err := readEntityKind(conn, normalizeEmail(email), entityID, turn)
		if err != nil {
			return err
		}
		replacement := make([]Order, 0, len(orders))
		for index, order := range orders {
			if !order.Kind.Valid() {
				return fmt.Errorf("replace orders: %w: %q", ErrOrderKindRefused, order.Kind)
			}
			if !entityKind.Accepts(order.Kind) {
				return fmt.Errorf("replace orders: %w: %s takes no %s", ErrOrderKindRefused, entityKind, order.Kind)
			}
			order.Seq = index + 1
			replacement = append(replacement, order)
		}
		return rewriteEntityOrders(conn, turn, entityID, replacement)
	})
}

// SetOrderDetail sets what one order carries beyond its kind: which way a move
// goes, or how long a rest lasts.
//
// An invalid direction - the compass point's zero value - is the blank option,
// and it leaves the order in place with nothing said about where it goes.
// Removing the order is RemoveOrder's work, not the blank option's.
func (s *Store) SetOrderDetail(ctx context.Context, email string, turn int, entityID int64, seq int, detail game.OrderDetail, opts ...OrderWriteOption) error {
	return s.SetOrderDetails(ctx, email, turn, []OrderUpdate{
		{EntityID: entityID, Seq: seq, Detail: detail},
	}, opts...)
}

// SetOrderDetails sets the detail of every order it is given, in one
// transaction.
//
// It is what the script-free page's one Save button saves: a whole page of
// controls, applied together or not at all.
//
// The order's stored kind decides which half of the detail is used, so naming a
// direction for a rest changes nothing about the rest. An order's kind is not
// editable; a player who wants a different kind removes the order and adds one.
func (s *Store) SetOrderDetails(ctx context.Context, email string, turn int, updates []OrderUpdate, opts ...OrderWriteOption) error {
	if len(updates) == 0 {
		return nil
	}
	entities := make([]int64, 0, len(updates))
	for _, wanted := range updates {
		if !slices.Contains(entities, wanted.EntityID) {
			entities = append(entities, wanted.EntityID)
		}
	}
	return s.writeOrders(ctx, email, turn, entities, opts, func(conn *sqlite.Conn) error {
		for _, wanted := range updates {
			orders, err := readEntityOrders(conn, wanted.EntityID, turn)
			if err != nil {
				return err
			}
			index := indexOfOrder(orders, wanted.Seq)
			if index < 0 {
				return fmt.Errorf("set order detail: %w: %d", ErrUnknownOrder, wanted.Seq)
			}
			switch orders[index].Kind {
			case game.OrderKindMove:
				if wanted.Detail.Count != 0 {
					return fmt.Errorf("set order detail: %w", ErrOrderDetailRefused)
				}
				orders[index].Detail.Direction = wanted.Detail.Direction
			case game.OrderKindRest:
				if wanted.Detail.Direction != 0 {
					return fmt.Errorf("set order detail: %w", ErrOrderDetailRefused)
				}
				orders[index].Detail.Count = wanted.Detail.Count
			}
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
func (s *Store) RemoveOrder(ctx context.Context, email string, turn int, entityID int64, seq int, opts ...OrderWriteOption) error {
	return s.writeOrders(ctx, email, turn, []int64{entityID}, opts, func(conn *sqlite.Conn) error {
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

// AdvanceTurn processes the orders of the turn the game is on, then moves the
// clock on by one and returns the turn it now sits on.
//
// The two halves are one transaction, so a turn is processed whole or not at
// all. Processing walks every active faction's stored orders and writes what
// they did - locations, knowledge - effective from turn+1, which is why a
// turn-N report still describes the world as it was while turn N happened. See
// docs/reference/turn-processing.md.
//
// Advancing is also what freezes the orders of the turn left behind: every
// write checks the current turn, so the rows of a turn the game has moved past
// are read-only from here on. Nothing rewrites them - processing writes no
// orders, and the stored orders with the seeds are the replay.
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
	if err := processTurn(conn, turn); err != nil {
		return 0, fmt.Errorf("process turn %d: %w", turn, err)
	}
	if err := sqlitex.ExecuteTransient(conn, `UPDATE game SET current_turn = ?1 WHERE id = 1;`, &sqlitex.ExecOptions{
		Args: []any{next},
	}); err != nil {
		return 0, fmt.Errorf("advance turn: %w", err)
	}
	return next, nil
}

// writeOrders runs one order write inside a transaction, between the checks
// every order write starts with and the trailing Rest every order write leaves
// correct.
//
// The gates are the store's own invariants rather than checks a caller can
// arrange to pass, so they belong to the write rather than to each method that
// makes one. A request naming an entity that is not the faction's is refused
// before anything is touched, not by the write it would have reached.
//
// The list is the player's and nothing here adds to it. What the orders leave
// unspent is a number the pre-processor reports, not an order stored on the
// end, so a write touches the orders it was asked to touch and no others. That
// is not only tidier: a stored residue would have to be sized from an estimate,
// and an estimate is exactly what a durable row must not be sized from.
func (s *Store) writeOrders(ctx context.Context, email string, turn int, entityIDs []int64, opts []OrderWriteOption, write func(*sqlite.Conn) error) (err error) {
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

	normalizedEmail := normalizeEmail(email)
	if err := requireActiveFaction(conn, normalizedEmail); err != nil {
		return err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return err
	}
	for _, entityID := range entityIDs {
		if err := requireEntity(conn, normalizedEmail, entityID); err != nil {
			return err
		}
	}
	// The precondition is the last gate before the write and is inside the
	// same transaction, so the list it compares against is the list the write
	// is about to change.
	if options := newOrderWriteOptions(opts); options.expectSet {
		current, err := ordersETag(conn, normalizedEmail, turn)
		if err != nil {
			return err
		}
		if current != options.expect {
			return fmt.Errorf("%w: orders are %s, not the expected %s", ErrOrdersChanged, current, options.expect)
		}
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
		SELECT orders.entity_id, orders.seq, orders.kind,
		       move_orders.direction, COALESCE(rest_orders.count, 0)
		FROM orders
		JOIN entities ON entities.id = orders.entity_id
		LEFT JOIN move_orders ON move_orders.turn = orders.turn
			AND move_orders.entity_id = orders.entity_id AND move_orders.seq = orders.seq
		LEFT JOIN rest_orders ON rest_orders.turn = orders.turn
			AND rest_orders.entity_id = orders.entity_id AND rest_orders.seq = orders.seq
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
		SELECT orders.seq, orders.kind,
		       move_orders.direction, COALESCE(rest_orders.count, 0)
		FROM orders
		LEFT JOIN move_orders ON move_orders.turn = orders.turn
			AND move_orders.entity_id = orders.entity_id AND move_orders.seq = orders.seq
		LEFT JOIN rest_orders ON rest_orders.turn = orders.turn
			AND rest_orders.entity_id = orders.entity_id AND rest_orders.seq = orders.seq
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

// scanOrder reads a sequence, a kind and both details from four columns
// starting at first. An empty direction column is the outer join finding no
// detail row, which is an order with nothing said about where it goes; a zero
// count is the same join finding no rest.
func scanOrder(stmt *sqlite.Stmt, first int) (Order, error) {
	order := Order{
		Seq:    stmt.ColumnInt(first),
		Kind:   game.OrderKind(stmt.ColumnText(first + 1)),
		Detail: game.OrderDetail{Count: stmt.ColumnInt(first + 3)},
	}
	if direction := stmt.ColumnText(first + 2); direction != "" {
		point, err := compass.Parse(direction)
		if err != nil {
			return Order{}, fmt.Errorf("order %d: %w", order.Seq, err)
		}
		order.Detail.Direction = point
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
// stored. A rest is always one row in rest_orders, because a rest with no count
// is not an order anybody has half-written - a rest lasts at least one point.
// It runs inside the caller's transaction.
func writeOrderDetail(conn *sqlite.Conn, turn int, entityID int64, order Order) error {
	if order.Kind == game.OrderKindMove && order.Detail.Count != 0 ||
		order.Kind == game.OrderKindRest && order.Detail.Direction != 0 {
		return fmt.Errorf("set order detail: %w", ErrOrderDetailRefused)
	}
	for _, table := range []string{"move_orders", "rest_orders"} {
		if err := sqlitex.ExecuteTransient(conn, `
			DELETE FROM `+table+` WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3;`, &sqlitex.ExecOptions{
			Args: []any{turn, entityID, order.Seq},
		}); err != nil {
			return fmt.Errorf("clear order detail: %w", err)
		}
	}
	switch order.Kind {
	case game.OrderKindMove:
		if order.Detail.Direction == 0 {
			return nil
		}
		if !order.Detail.Direction.IsValid() {
			return fmt.Errorf("set order direction: %w: %s", compass.ErrUnknownPoint, order.Detail.Direction)
		}
		if err := sqlitex.ExecuteTransient(conn, `
			INSERT INTO move_orders (turn, entity_id, seq, direction) VALUES (?1, ?2, ?3, ?4);`, &sqlitex.ExecOptions{
			Args: []any{turn, entityID, order.Seq, storedDirection(order.Detail.Direction)},
		}); err != nil {
			return fmt.Errorf("set order direction: %w", err)
		}
	case game.OrderKindRest:
		if order.Detail.Count < 1 || order.Detail.Count > MaxOrdersPerEntity {
			return fmt.Errorf("set rest count: %w: %d", ErrOrderCountRefused, order.Detail.Count)
		}
		if err := sqlitex.ExecuteTransient(conn, `
			INSERT INTO rest_orders (turn, entity_id, seq, count) VALUES (?1, ?2, ?3, ?4);`, &sqlitex.ExecOptions{
			Args: []any{turn, entityID, order.Seq, order.Detail.Count},
		}); err != nil {
			return fmt.Errorf("set rest count: %w", err)
		}
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

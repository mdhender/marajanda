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

	// ErrUnknownOrder reports a stanza the entity does not have.
	ErrUnknownOrder = errors.New("that order is not one of the entity's")

	// ErrUnknownStep reports a step outside the boxes an order shows: its
	// stored steps, plus the one blank box on the end.
	ErrUnknownStep = errors.New("that step is not one of the order's boxes")

	// ErrTooManySteps reports an order longer than storage allows. It is a
	// sanity limit and not the movement allowance; see MaxOrderSteps.
	ErrTooManySteps = fmt.Errorf("an order carries at most %d steps", MaxOrderSteps)

	// ErrFactionInactive reports a write by a faction that has been
	// deactivated. A deactivated faction cannot give orders; its player can
	// still sign in and look at their game.
	ErrFactionInactive = errors.New("that faction is not active")
)

// Order is one stanza of an entity's orders for a turn: an order kind and, for
// a move, the list of directions it walks.
//
// Steps are contiguous. A stanza is stored compacted, so the list is the whole
// order and its position in the list is the step number.
type Order struct {
	Seq   int
	Kind  game.OrderKind
	Steps []compass.Point
}

// OrderSteps addresses one stanza's steps, for a save that carries a whole
// page of them.
type OrderSteps struct {
	EntityID int64
	Seq      int
	Steps    []compass.Point
}

// OrdersAsOf returns the orders a faction's entities carry on turn, keyed by
// entity and in stanza order.
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

// AddOrder appends a stanza to an entity's orders for the turn and returns its
// sequence number.
//
// The entity's kind decides which order kinds it accepts, so a kind it does not
// accept is refused here as well as omitted from the form. A hand-built request
// cannot do what the form declines to show.
func (s *Store) AddOrder(ctx context.Context, email string, turn int, entityID int64, kind game.OrderKind) (_ int, err error) {
	if !kind.Valid() {
		return 0, fmt.Errorf("add order: %w: %q", ErrOrderKindRefused, kind)
	}
	email = normalizeEmail(email)

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

	if err := requireActiveFaction(conn, email); err != nil {
		return 0, err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return 0, err
	}
	entityKind, err := readEntityKind(conn, email, entityID, turn)
	if err != nil {
		return 0, err
	}
	if !entityKind.Accepts(kind) {
		return 0, fmt.Errorf("add order: %w: %s takes no %s", ErrOrderKindRefused, entityKind, kind)
	}
	orders, err := readEntityOrders(conn, entityID, turn)
	if err != nil {
		return 0, err
	}
	seq := len(orders) + 1
	if err := insertOrder(conn, turn, entityID, Order{Seq: seq, Kind: kind}); err != nil {
		return 0, err
	}
	return seq, nil
}

// SetOrderStep sets one step box of one stanza.
//
// step addresses a box the page showed: one of the stored steps, or the blank
// box on the end, which is step count plus one. An invalid direction - the
// compass point's zero value - is the blank option, and it clears the box.
//
// Clearing a box in the middle compacts the rest, so an order has exactly one
// stored form. Clearing the trailing blank box changes nothing.
func (s *Store) SetOrderStep(ctx context.Context, email string, turn int, entityID int64, seq, step int, direction compass.Point) (err error) {
	email = normalizeEmail(email)

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

	if err := requireActiveFaction(conn, email); err != nil {
		return err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return err
	}
	if err := requireEntity(conn, email, entityID); err != nil {
		return err
	}
	orders, err := readEntityOrders(conn, entityID, turn)
	if err != nil {
		return err
	}
	index := indexOfOrder(orders, seq)
	if index < 0 {
		return fmt.Errorf("set order step: %w: %d", ErrUnknownOrder, seq)
	}
	steps := orders[index].Steps
	switch {
	case step < 1 || step > len(steps)+1:
		return fmt.Errorf("set order step: %w: %d", ErrUnknownStep, step)
	case step == len(steps)+1:
		// The blank box on the end. A direction appends a step; the blank
		// option leaves an order that already ends where it ended.
		if !direction.IsValid() {
			return nil
		}
		steps = append(steps, direction)
	case !direction.IsValid():
		steps = append(steps[:step-1:step-1], steps[step:]...)
	default:
		steps[step-1] = direction
	}
	orders[index].Steps = steps
	return writeOrderSteps(conn, turn, entityID, orders[index])
}

// SetOrderSteps replaces the steps of every stanza it is given, in one
// transaction.
//
// It is what the script-free page's one Save button saves: a whole page of
// boxes, applied together or not at all. Blank boxes are the caller's to drop -
// what arrives here is the compacted list the stanza is to have.
func (s *Store) SetOrderSteps(ctx context.Context, email string, turn int, stanzas []OrderSteps) (err error) {
	if len(stanzas) == 0 {
		return nil
	}
	email = normalizeEmail(email)

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

	if err := requireActiveFaction(conn, email); err != nil {
		return err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return err
	}
	for _, stanza := range stanzas {
		if err := requireEntity(conn, email, stanza.EntityID); err != nil {
			return err
		}
		orders, err := readEntityOrders(conn, stanza.EntityID, turn)
		if err != nil {
			return err
		}
		index := indexOfOrder(orders, stanza.Seq)
		if index < 0 {
			return fmt.Errorf("set order steps: %w: %d", ErrUnknownOrder, stanza.Seq)
		}
		orders[index].Steps = stanza.Steps
		if err := writeOrderSteps(conn, turn, stanza.EntityID, orders[index]); err != nil {
			return err
		}
	}
	return nil
}

// RemoveOrder deletes a stanza and renumbers the ones after it, so the
// sequence stays 1..N with no gap.
//
// Only the open turn is touched. Nothing removes an order from a turn that has
// been advanced past.
func (s *Store) RemoveOrder(ctx context.Context, email string, turn int, entityID int64, seq int) (err error) {
	email = normalizeEmail(email)

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

	if err := requireActiveFaction(conn, email); err != nil {
		return err
	}
	if err := requireOpenTurn(conn, turn); err != nil {
		return err
	}
	if err := requireEntity(conn, email, entityID); err != nil {
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

// readFactionOrders reads every stanza the faction's entities carry on turn.
//
// The join to the steps is outer: a stanza with no steps yet is an order a
// player has added and not filled in, and it has to come back so the page can
// draw its blank box.
func readFactionOrders(conn *sqlite.Conn, normalizedEmail string, turn int) (map[int64][]Order, error) {
	orders := make(map[int64][]Order)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT orders.entity_id, orders.seq, orders.kind, order_steps.direction
		FROM orders
		JOIN entities ON entities.id = orders.entity_id
		LEFT JOIN order_steps ON order_steps.turn = orders.turn
			AND order_steps.entity_id = orders.entity_id AND order_steps.seq = orders.seq
		WHERE entities.faction_email = ?1 AND orders.turn = ?2
		ORDER BY orders.entity_id, orders.seq, order_steps.step;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			entityID, seq := stmt.ColumnInt64(0), stmt.ColumnInt(1)
			stanzas := orders[entityID]
			if len(stanzas) == 0 || stanzas[len(stanzas)-1].Seq != seq {
				stanzas = append(stanzas, Order{Seq: seq, Kind: game.OrderKind(stmt.ColumnText(2))})
			}
			if direction := stmt.ColumnText(3); direction != "" {
				point, err := compass.Parse(direction)
				if err != nil {
					return fmt.Errorf("entity %d order %d: %w", entityID, seq, err)
				}
				stanzas[len(stanzas)-1].Steps = append(stanzas[len(stanzas)-1].Steps, point)
			}
			orders[entityID] = stanzas
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up orders: %w", err)
	}
	return orders, nil
}

// readEntityOrders reads one entity's stanzas for a turn, in sequence order.
// It runs inside the caller's transaction.
func readEntityOrders(conn *sqlite.Conn, entityID int64, turn int) ([]Order, error) {
	orders := make([]Order, 0)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT orders.seq, orders.kind, order_steps.direction
		FROM orders
		LEFT JOIN order_steps ON order_steps.turn = orders.turn
			AND order_steps.entity_id = orders.entity_id AND order_steps.seq = orders.seq
		WHERE orders.turn = ?1 AND orders.entity_id = ?2
		ORDER BY orders.seq, order_steps.step;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			seq := stmt.ColumnInt(0)
			if len(orders) == 0 || orders[len(orders)-1].Seq != seq {
				orders = append(orders, Order{Seq: seq, Kind: game.OrderKind(stmt.ColumnText(1))})
			}
			if direction := stmt.ColumnText(2); direction != "" {
				point, err := compass.Parse(direction)
				if err != nil {
					return fmt.Errorf("order %d: %w", seq, err)
				}
				orders[len(orders)-1].Steps = append(orders[len(orders)-1].Steps, point)
			}
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("look up orders: %w", err)
	}
	return orders, nil
}

func indexOfOrder(orders []Order, seq int) int {
	for index, order := range orders {
		if order.Seq == seq {
			return index
		}
	}
	return -1
}

// insertOrder writes one stanza and its steps. It runs inside the caller's
// transaction.
func insertOrder(conn *sqlite.Conn, turn int, entityID int64, order Order) error {
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO orders (turn, entity_id, seq, kind) VALUES (?1, ?2, ?3, ?4);`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, order.Seq, string(order.Kind)},
	}); err != nil {
		return fmt.Errorf("add order: %w", err)
	}
	return writeOrderSteps(conn, turn, entityID, order)
}

// writeOrderSteps replaces a stanza's steps with the list it carries,
// numbering them 1..N.
//
// Replacing rather than patching is what compacts: the stored steps are
// whatever the caller's list holds, in its order, with no gap to leave behind.
// It runs inside the caller's transaction.
func writeOrderSteps(conn *sqlite.Conn, turn int, entityID int64, order Order) error {
	if len(order.Steps) > MaxOrderSteps {
		return fmt.Errorf("set order steps: %w", ErrTooManySteps)
	}
	for _, point := range order.Steps {
		if !point.IsValid() {
			return fmt.Errorf("set order steps: %w: %s", compass.ErrUnknownPoint, point)
		}
	}
	if err := sqlitex.ExecuteTransient(conn, `
		DELETE FROM order_steps WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, order.Seq},
	}); err != nil {
		return fmt.Errorf("clear order steps: %w", err)
	}
	for index, point := range order.Steps {
		if err := sqlitex.ExecuteTransient(conn, `
			INSERT INTO order_steps (turn, entity_id, seq, step, direction)
			VALUES (?1, ?2, ?3, ?4, ?5);`, &sqlitex.ExecOptions{
			Args: []any{turn, entityID, order.Seq, index + 1, storedDirection(point)},
		}); err != nil {
			return fmt.Errorf("set order steps: %w", err)
		}
	}
	return nil
}

// rewriteEntityOrders replaces an entity's stanzas for a turn with the list it
// is given, renumbering them 1..N.
//
// Delete and reinsert rather than an UPDATE that shifts sequence numbers: the
// steps hang off the sequence number, and a bulk shift would depend on the
// order SQLite happened to walk the rows in to avoid colliding with a number
// that has not moved yet. An entity's orders are a handful of rows.
// It runs inside the caller's transaction.
func rewriteEntityOrders(conn *sqlite.Conn, turn int, entityID int64, orders []Order) error {
	// The steps go with them: order_steps cascades from orders.
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

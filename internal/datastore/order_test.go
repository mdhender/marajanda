// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"testing"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

const orderPlayer = "player@marajanda.com"

// foundedFaction seats the standard test player and returns its leader and its
// hamlet, which is every entity a faction is founded with.
func foundedFaction(t *testing.T, store *Store) (leader, hamlet Entity) {
	t.Helper()
	if _, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
		t.Fatal(err)
	}
	entities := entitiesNow(t, store, orderPlayer)
	if len(entities) != 2 {
		t.Fatalf("founding entities = %#v, want a leader and a hamlet", entities)
	}
	return entities[0], entities[1]
}

// moving is the detail a move carries: which way it goes.
func moving(direction compass.Point) game.OrderDetail {
	return game.OrderDetail{Direction: direction}
}

// resting is the detail a rest carries: how long it lasts.
func resting(count int) game.OrderDetail {
	return game.OrderDetail{Count: count}
}

// storedOrdersNow reads everything one entity carries as of the turn the game
// is on, the trailing Rest included.
func storedOrdersNow(t *testing.T, store *Store, entityID int64) []Order {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	orders, err := store.OrdersAsOf(t.Context(), orderPlayer, turn)
	if err != nil {
		t.Fatal(err)
	}
	return orders[entityID]
}

// ordersNow reads the orders one entity's player authored, which is its stored
// list without the trailing Rest the pre-processor keeps at the end of it.
func ordersNow(t *testing.T, store *Store, entityID int64) []Order {
	t.Helper()
	authored, _ := game.SplitTrailingRest(storedOrdersNow(t, store, entityID))
	return authored
}

// trailingRest is the count of the Rest an entity's list ends with, or zero
// when it ends with something else. Nothing stores a Rest x0.
func trailingRest(t *testing.T, store *Store, entityID int64) int {
	t.Helper()
	orders := storedOrdersNow(t, store, entityID)
	if _, trailing := game.SplitTrailingRest(orders); !trailing {
		return 0
	}
	return orders[len(orders)-1].Detail.Count
}

// march names an entity's orders in one string, so a test can say what it
// wanted in one line and read what it got in another. An order with no
// direction yet is a dash, because it is a row on the page either way.
func march(orders []Order) string {
	names := ""
	for _, order := range orders {
		if names != "" {
			names += " "
		}
		if order.Detail.Direction.IsValid() {
			names += order.Detail.Direction.String()
		} else {
			names += "-"
		}
	}
	return names
}

func addMove(t *testing.T, store *Store, entityID int64, direction compass.Point) int {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seq, err := store.AddOrder(t.Context(), orderPlayer, turn, entityID, game.OrderKindMove, moving(direction))
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func setDirection(t *testing.T, store *Store, entityID int64, seq int, direction compass.Point) {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetOrderDetail(t.Context(), orderPlayer, turn, entityID, seq, moving(direction)); err != nil {
		t.Fatal(err)
	}
}

// An order is one action. It can be added with its direction, and a row added
// blank is filled in afterwards; either way one order holds one direction.
func TestAnOrderCarriesOneDirection(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		// The add control on the page sends no direction, so a new row is a
		// move with nothing yet said about where it goes.
		seq := addMove(t, store, leader.ID, 0)
		if seq != 1 {
			t.Fatalf("first order = %d, want 1", seq)
		}
		orders := ordersNow(t, store, leader.ID)
		if len(orders) != 1 || orders[0].Kind != game.OrderKindMove || orders[0].Detail.Direction.IsValid() {
			t.Fatalf("orders = %#v, want one move with no direction", orders)
		}

		// "move nw ne e" is three orders, not one order with three steps.
		setDirection(t, store, leader.ID, seq, compass.NW)
		addMove(t, store, leader.ID, compass.NE)
		addMove(t, store, leader.ID, compass.E)
		if got := march(ordersNow(t, store, leader.ID)); got != "NW NE E" {
			t.Fatalf("orders = %q, want NW NE E", got)
		}

		// A direction is replaced in place, and the blank option leaves the
		// order standing with nothing said about where it goes. Emptying a row
		// is not removing it.
		setDirection(t, store, leader.ID, 2, compass.SE)
		if got := march(ordersNow(t, store, leader.ID)); got != "NW SE E" {
			t.Fatalf("orders = %q, want NW SE E", got)
		}
		setDirection(t, store, leader.ID, 2, 0)
		if got := march(ordersNow(t, store, leader.ID)); got != "NW - E" {
			t.Fatalf("orders = %q, want the second order emptied, not removed", got)
		}
		if got := storedMove(t, store, leader.ID, 2); got != "" {
			t.Fatalf("order 2 still stores %q", got)
		}
	})
}

// Inserting an order shifts the ones from that position on up by one, so a
// list can be corrected in the middle without retyping its tail.
func TestInsertingAnOrderShiftsTheRestUp(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		addMove(t, store, leader.ID, compass.NW)
		addMove(t, store, leader.ID, compass.E)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 2, game.OrderKindMove, moving(compass.NE)); err != nil {
			t.Fatal(err)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "NW NE E" {
			t.Fatalf("orders = %q, want NE inserted between NW and E", got)
		}

		// The numbering is contiguous from 1 and each direction went with the
		// order it belongs to rather than staying under the number it had.
		orders := ordersNow(t, store, leader.ID)
		for index, order := range orders {
			if order.Seq != index+1 {
				t.Fatalf("orders = %#v, want sequences 1..%d", orders, len(orders))
			}
		}
		if got := storedMove(t, store, leader.ID, 3); got != "e" {
			t.Fatalf("order 3 stores %q, want e", got)
		}

		// One past the end is a place: it is what "insert after the last
		// order" asks for.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 4, game.OrderKindMove, moving(compass.SW)); err != nil {
			t.Fatal(err)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "NW NE E SW" {
			t.Fatalf("orders = %q, want SW appended", got)
		}
		// Two past the end is not.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 6, game.OrderKindMove, moving(compass.W)); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("insert at 6 = %v, want %v", err, ErrUnknownOrder)
		}
	})
}

// Removing an order renumbers the ones after it, and takes their directions
// with them rather than leaving one behind under a number that has moved.
func TestRemovingAnOrderRenumbersTheRest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		for _, point := range []compass.Point{compass.NW, compass.E, compass.SW} {
			addMove(t, store, leader.ID, point)
		}

		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RemoveOrder(t.Context(), orderPlayer, turn, leader.ID, 1); err != nil {
			t.Fatal(err)
		}
		orders := ordersNow(t, store, leader.ID)
		if len(orders) != 2 {
			t.Fatalf("orders = %#v, want two", orders)
		}
		if orders[0].Seq != 1 || orders[0].Detail.Direction != compass.E {
			t.Fatalf("first order = %#v, want the old second renumbered to 1", orders[0])
		}
		if orders[1].Seq != 2 || orders[1].Detail.Direction != compass.SW {
			t.Fatalf("second order = %#v, want the old third renumbered to 2", orders[1])
		}
		// No direction is left under a sequence number that no longer exists.
		if got := storedMove(t, store, leader.ID, 3); got != "" {
			t.Fatalf("order 3 still stores %q", got)
		}
	})
}

// Which order kinds an entity accepts is a game rule, and the store enforces it
// as well as the form. A hamlet accepts none today.
func TestAnEntityRefusesAnOrderItsKindDoesNotAccept(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		_, hamlet := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, hamlet.ID, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrOrderKindRefused) {
			t.Fatalf("hamlet move = %v, want %v", err, ErrOrderKindRefused)
		}
		// An order kind the game does not know is refused the same way, and so
		// is an insert of one.
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, hamlet.ID, game.OrderKind("besiege"), game.OrderDetail{}); !errors.Is(err, ErrOrderKindRefused) {
			t.Fatalf("unknown kind = %v, want %v", err, ErrOrderKindRefused)
		}
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, hamlet.ID, 1, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrOrderKindRefused) {
			t.Fatalf("hamlet insert = %v, want %v", err, ErrOrderKindRefused)
		}
		if orders := ordersNow(t, store, hamlet.ID); len(orders) != 0 {
			t.Fatalf("the hamlet holds %#v, want nothing", orders)
		}
	})
}

// An entity that is not the faction's is not the faction's to order.
func TestOrdersAreRefusedForAnotherFactionsEntity(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		foundedFaction(t, store)
		if _, err := store.CreateAccount(t.Context(), SeedAccount{
			Email: "rival@example.com", Secret: "good.luck", Handle: "rival", Role: "player",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SaveFaction(t.Context(), "rival@example.com", "The Rivals", game.RaceOrc); err != nil {
			t.Fatal(err)
		}
		rival, err := store.EntitiesAsOf(t.Context(), "rival@example.com", game.FirstTurn)
		if err != nil {
			t.Fatal(err)
		}
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, rival[0].ID, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrUnknownEntity) {
			t.Fatalf("ordering a rival's leader = %v, want %v", err, ErrUnknownEntity)
		}
	})
}

// Only the current turn is writable. Every write carries the turn the page was
// rendered from, so a page that was open while the clock moved is refused.
func TestOnlyTheCurrentTurnIsWritable(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID, compass.NW)

		closed := game.FirstTurn
		next, err := store.AdvanceTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if next != closed+1 {
			t.Fatalf("AdvanceTurn = %d, want %d", next, closed+1)
		}

		for name, write := range map[string]func() error{
			"add": func() error {
				_, err := store.AddOrder(t.Context(), orderPlayer, closed, leader.ID, game.OrderKindMove, moving(compass.E))
				return err
			},
			"insert": func() error {
				return store.InsertOrder(t.Context(), orderPlayer, closed, leader.ID, seq, game.OrderKindMove, moving(compass.E))
			},
			"set a direction": func() error {
				return store.SetOrderDetail(t.Context(), orderPlayer, closed, leader.ID, seq, moving(compass.E))
			},
			"save directions": func() error {
				return store.SetOrderDetails(t.Context(), orderPlayer, closed, []OrderUpdate{
					{EntityID: leader.ID, Seq: seq, Detail: moving(compass.E)},
				})
			},
			"remove": func() error {
				return store.RemoveOrder(t.Context(), orderPlayer, closed, leader.ID, seq)
			},
		} {
			if err := write(); !errors.Is(err, ErrTurnClosed) {
				t.Fatalf("%s on turn %d = %v, want %v", name, closed, err, ErrTurnClosed)
			}
		}

		// A turn ahead of the clock is refused for the same reason.
		if _, err := store.AddOrder(t.Context(), orderPlayer, next+1, leader.ID, game.OrderKindMove, game.OrderDetail{}); !errors.Is(err, ErrTurnClosed) {
			t.Fatalf("add on turn %d = %v, want %v", next+1, err, ErrTurnClosed)
		}
	})
}

// Advancing the turn freezes what came before it: the previous turn's rows are
// exactly as they were, and the new turn starts with nothing in it.
func TestAdvancingTheTurnLeavesThePreviousTurnAlone(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		for _, point := range []compass.Point{compass.NW, compass.E} {
			addMove(t, store, leader.ID, point)
		}
		before, err := store.OrdersAsOf(t.Context(), orderPlayer, game.FirstTurn)
		if err != nil {
			t.Fatal(err)
		}

		next, err := store.AdvanceTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		after, err := store.OrdersAsOf(t.Context(), orderPlayer, game.FirstTurn)
		if err != nil {
			t.Fatal(err)
		}
		if march(after[leader.ID]) != march(before[leader.ID]) {
			t.Fatalf("turn %d orders = %#v, want the frozen %#v", game.FirstTurn, after, before)
		}

		// The new turn is empty. Orders are issued for a turn, not carried
		// into the next one.
		open, err := store.OrdersAsOf(t.Context(), orderPlayer, next)
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 0 {
			t.Fatalf("turn %d orders = %#v, want none", next, open)
		}

		// And the new turn takes its own orders, numbered from one.
		if got := addMove(t, store, leader.ID, compass.SW); got != 1 {
			t.Fatalf("first order of turn %d = %d, want 1", next, got)
		}
		if got := len(ordersNow(t, store, leader.ID)); got != 1 {
			t.Fatalf("turn %d holds %d orders, want 1", next, got)
		}
	})
}

// The script-free page saves a whole page of selects at once, and a blank one
// is a direction cleared rather than a row dropped.
func TestSavingAWholePageOfDirections(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		first, second := addMove(t, store, leader.ID, 0), addMove(t, store, leader.ID, 0)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetOrderDetails(t.Context(), orderPlayer, turn, []OrderUpdate{
			{EntityID: leader.ID, Seq: first, Detail: moving(compass.NW)},
			{EntityID: leader.ID, Seq: second, Detail: moving(compass.E)},
		}); err != nil {
			t.Fatal(err)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "NW E" {
			t.Fatalf("orders = %q, want NW E", got)
		}

		// A blank select empties its own row and leaves every other row alone.
		if err := store.SetOrderDetails(t.Context(), orderPlayer, turn, []OrderUpdate{
			{EntityID: leader.ID, Seq: first, Detail: moving(0)},
		}); err != nil {
			t.Fatal(err)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "- E" {
			t.Fatalf("orders = %q, want the first emptied and the second untouched", got)
		}
	})
}

// An order that is not there is refused rather than quietly created, and an
// entity is not given more orders in a turn than storage allows.
func TestOrderWritesRefuseWhatThePageNeverShowed(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID, compass.NW)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetOrderDetail(t.Context(), orderPlayer, turn, leader.ID, seq+1, moving(compass.E)); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("a direction on a missing order = %v, want %v", err, ErrUnknownOrder)
		}
		if err := store.RemoveOrder(t.Context(), orderPlayer, turn, leader.ID, seq+1); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("removing a missing order = %v, want %v", err, ErrUnknownOrder)
		}

		// The cap is on orders per entity per turn. The one already there
		// counts towards it.
		for range MaxOrdersPerEntity - 1 {
			addMove(t, store, leader.ID, compass.E)
		}
		if got := len(ordersNow(t, store, leader.ID)); got != MaxOrdersPerEntity {
			t.Fatalf("orders = %d, want the cap of %d", got, MaxOrdersPerEntity)
		}
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, leader.ID, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrTooManyOrders) {
			t.Fatalf("order %d = %v, want %v", MaxOrdersPerEntity+1, err, ErrTooManyOrders)
		}
		// An insert is bounded by the same cap: it lengthens the list too.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrTooManyOrders) {
			t.Fatalf("insert at the cap = %v, want %v", err, ErrTooManyOrders)
		}
		if got := len(ordersNow(t, store, leader.ID)); got != MaxOrdersPerEntity {
			t.Fatalf("orders = %d, want the cap of %d", got, MaxOrdersPerEntity)
		}
	})
}

// An order the store refuses is an order the database does not hold. The write
// runs in a transaction, so a save that fails part way leaves nothing.
func TestAFailedSaveWritesNothing(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID, 0)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		err = store.SetOrderDetails(t.Context(), orderPlayer, turn, []OrderUpdate{
			{EntityID: leader.ID, Seq: seq, Detail: moving(compass.NW)},
			{EntityID: leader.ID, Seq: seq + 1, Detail: moving(compass.E)},
		})
		if !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("save = %v, want %v", err, ErrUnknownOrder)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "-" {
			t.Fatalf("orders = %q, want the save rolled back", got)
		}
	})
}

// storedMove reads one order's direction straight out of the detail table,
// so a test can see what is stored rather than the order the store rebuilt from
// it. An order with no direction has no row, and comes back empty.
func storedMove(t *testing.T, store *Store, entityID int64, seq int) string {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	stored := ""
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT direction FROM move_orders
		WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, seq},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			stored = stmt.ColumnText(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return stored
}

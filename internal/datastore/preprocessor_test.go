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

// estimateNow prices one entity's orders as of the turn the game is on.
func estimateNow(t *testing.T, store *Store, entityID int64) game.Estimate {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	estimates, err := store.EstimateOrders(t.Context(), orderPlayer, turn)
	if err != nil {
		t.Fatal(err)
	}
	return estimates[entityID]
}

// countAllowanceRows counts the allowance facts a database holds, which is one
// per entity that takes orders and none for the rest.
func countAllowanceRows(t *testing.T, store *Store) (rows, points int) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := sqlitex.ExecuteTransient(conn, `SELECT points FROM entity_allowances;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rows, points = rows+1, stmt.ColumnInt(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return rows, points
}

func removeOrder(t *testing.T, store *Store, entityID int64, seq int) {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveOrder(t.Context(), orderPlayer, turn, entityID, seq); err != nil {
		t.Fatal(err)
	}
}

func addRest(t *testing.T, store *Store, entityID int64, count int) int {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seq, err := store.AddOrder(t.Context(), orderPlayer, turn, entityID, game.OrderKindRest, resting(count))
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// An allowance is written when an entity is created, and an entity kind that
// accepts no orders has none. Having none is the absence of a row, so a hamlet
// reads as zero rather than as a leader with nothing to spend.
func TestFoundingWritesTheAllowanceOfEveryEntityThatTakesOrders(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, hamlet := foundedFaction(t, store)

		if leader.Allowance != game.LeaderAllowance {
			t.Fatalf("leader allowance = %d, want %d", leader.Allowance, game.LeaderAllowance)
		}
		if hamlet.Allowance != 0 {
			t.Fatalf("hamlet allowance = %d, want none", hamlet.Allowance)
		}
		rows, points := countAllowanceRows(t, store)
		if rows != 1 || points != game.LeaderAllowance {
			t.Fatalf("%d allowance rows holding %d, want one holding %d", rows, points, game.LeaderAllowance)
		}
	})
}

// A leader opens the turn with its whole allowance in a trailing Rest, and the
// Rest decrements as orders are added. The homeland ring buys one cheap step
// and the ring beyond it costs three.
func TestTheTrailingRestIsTheResidue(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		// Nothing has been written yet, so nothing is stored - but the page
		// still has a residue to draw, and it is the whole allowance.
		if got := storedOrdersNow(t, store, leader.ID); len(got) != 0 {
			t.Fatalf("a leader that has been given nothing carries %#v", got)
		}
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance {
			t.Fatalf("residue before any write = %d, want %d", got, game.LeaderAllowance)
		}

		addMove(t, store, leader.ID, compass.NE)
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance-1 {
			t.Fatalf("rest after one cheap step = %d, want %d", got, game.LeaderAllowance-1)
		}
		addMove(t, store, leader.ID, compass.NE)
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance-4 {
			t.Fatalf("rest after a step and an exploration = %d, want %d", got, game.LeaderAllowance-4)
		}
		// The Rest is a stored order, so the whole list is one step, one
		// exploration and the residue.
		stored := storedOrdersNow(t, store, leader.ID)
		if len(stored) != 3 || stored[2].Kind != game.OrderKindRest || stored[2].Seq != 3 {
			t.Fatalf("stored orders = %#v, want two moves and a rest", stored)
		}

		// When the orders reach the allowance the row goes, rather than being
		// left as a Rest x0 for turn processing to walk.
		addMove(t, store, leader.ID, compass.NE)
		if got := trailingRest(t, store, leader.ID); got != 0 {
			t.Fatalf("rest after overspending = %d, want none", got)
		}
		if got := storedOrdersNow(t, store, leader.ID); len(got) != 3 {
			t.Fatalf("stored orders = %#v, want three moves and no rest", got)
		}
		if estimate := estimateNow(t, store, leader.ID); estimate.Overspend != 1 || estimate.ExhaustsAt != 3 {
			t.Fatalf("estimate = %#v, want an overspend of 1 crossing at order 3", estimate)
		}

		// Removing an order puts the residue back.
		removeOrder(t, store, leader.ID, 3)
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance-4 {
			t.Fatalf("rest after removing a step = %d, want %d", got, game.LeaderAllowance-4)
		}
	})
}

// An added order goes on the end of what the player wrote, not after the
// residue. The trailing Rest comes off before every write and goes back after
// it, so the list a write addresses is the list the page showed.
func TestAnAddedOrderLandsInFrontOfTheTrailingRest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		if seq := addMove(t, store, leader.ID, compass.NE); seq != 1 {
			t.Fatalf("first order = %d, want 1", seq)
		}
		// The residue is stored as order 2 now, and the next add is still
		// order 2.
		if seq := addMove(t, store, leader.ID, compass.E); seq != 2 {
			t.Fatalf("second order = %d, want 2", seq)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "NE E" {
			t.Fatalf("orders = %q, want NE E", got)
		}
		stored := storedOrdersNow(t, store, leader.ID)
		if len(stored) != 3 || stored[2].Kind != game.OrderKindRest {
			t.Fatalf("stored orders = %#v, want the rest last", stored)
		}
	})
}

// Inserting an order changes where the entity stands for every order after it,
// so the whole list is re-priced. A step that was onto known ground may now be
// onto unknown ground, and the residue moves with it.
func TestInsertingAnOrderRepricesEverythingAfterIt(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		// A step onto the homeland ring and one beyond it: one point and three,
		// with two left over.
		addMove(t, store, leader.ID, compass.NE)
		addMove(t, store, leader.ID, compass.NE)
		before := estimateNow(t, store, leader.ID)
		if before.Orders[1].Cost != game.UnknownStepCost || before.Residue != 2 {
			t.Fatalf("estimate = %#v, want the second step at %d and two over", before, game.UnknownStepCost)
		}

		// Put another step in front of them. The order that was the first step
		// off the ring is now the second, walked from a hex the faction has
		// never seen, and its price changes even though nothing about the
		// order itself did.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindMove, moving(compass.NE)); err != nil {
			t.Fatal(err)
		}
		after := estimateNow(t, store, leader.ID)
		if len(after.Orders) != 3 {
			t.Fatalf("priced %d orders, want 3", len(after.Orders))
		}
		if after.Orders[1].Cost != game.UnknownStepCost {
			t.Fatalf("the middle step costs %d, want %d now that it starts elsewhere",
				after.Orders[1].Cost, game.UnknownStepCost)
		}
		if after.Total != 7 || after.ExhaustsAt != 3 {
			t.Fatalf("estimate = %#v, want 7 points crossing at order 3", after)
		}
		if got := trailingRest(t, store, leader.ID); got != 0 {
			t.Fatalf("rest = %d, want none: 1 + 3 + 3 is over six", got)
		}
	})
}

// A rest a player places is an order like any other. One at the end is the
// residue by definition, so the pre-processor sets its count; one anywhere else
// is theirs, and is priced and left alone.
func TestAPlayerCanPlaceARest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		// A rest added on its own is the trailing one, so it holds the whole
		// allowance rather than the one point the add control asked for.
		if seq := addRest(t, store, leader.ID, 1); seq != 1 {
			t.Fatalf("first order = %d, want 1", seq)
		}
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance {
			t.Fatalf("rest = %d, want the whole allowance of %d", got, game.LeaderAllowance)
		}

		// A rest before a move is the player's. It spends its points where it
		// was asked for and leaves the move to be paid for afterwards.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindMove, moving(compass.NE)); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindRest, resting(2)); err != nil {
			t.Fatal(err)
		}
		authored := ordersNow(t, store, leader.ID)
		if len(authored) != 2 || authored[0].Kind != game.OrderKindRest || authored[0].Detail.Count != 2 {
			t.Fatalf("orders = %#v, want a rest of two then a move", authored)
		}
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance-3 {
			t.Fatalf("rest = %d, want %d", got, game.LeaderAllowance-3)
		}

		// A count is set the way a direction is, and the residue follows it.
		if err := store.SetOrderDetail(t.Context(), orderPlayer, turn, leader.ID, 1, resting(4)); err != nil {
			t.Fatal(err)
		}
		if got := ordersNow(t, store, leader.ID)[0].Detail.Count; got != 4 {
			t.Fatalf("rest count = %d, want 4", got)
		}
		if got := trailingRest(t, store, leader.ID); got != game.LeaderAllowance-5 {
			t.Fatalf("rest = %d, want %d", got, game.LeaderAllowance-5)
		}
	})
}

// A rest lasts at least one action point and at most the order limit. Nothing
// stores a Rest x0: it is an order that costs nothing and does nothing.
func TestARestCountIsBounded(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// The rest under test has to be the player's rather than the trailing
		// one, whose count the pre-processor decides, so it goes in front of a
		// move rather than on the end.
		addMove(t, store, leader.ID, compass.NE)
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindRest, resting(2)); err != nil {
			t.Fatal(err)
		}

		for _, count := range []int{0, -1, MaxOrdersPerEntity + 1} {
			err := store.SetOrderDetail(t.Context(), orderPlayer, turn, leader.ID, 1, resting(count))
			if !errors.Is(err, ErrOrderCountRefused) {
				t.Fatalf("a rest of %d was refused with %v, want ErrOrderCountRefused", count, err)
			}
		}
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, leader.ID, game.OrderKindRest, resting(0)); !errors.Is(err, ErrOrderCountRefused) {
			t.Fatalf("adding a rest of nothing gave %v, want ErrOrderCountRefused", err)
		}
	})
}

// The estimate is what the orders page draws, and it prices the orders the
// player authored. The trailing Rest is the residue rather than a row in the
// list, so it is reported as one and left out of the other.
func TestEstimateOrdersPricesTheAuthoredOrders(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, hamlet := foundedFaction(t, store)
		addMove(t, store, leader.ID, compass.NE)

		estimate := estimateNow(t, store, leader.ID)
		if len(estimate.Orders) != 1 || estimate.Orders[0].Kind != game.OrderKindMove {
			t.Fatalf("priced %#v, want the one move alone", estimate.Orders)
		}
		if estimate.Allowance != game.LeaderAllowance || estimate.Total != game.KnownStepCost {
			t.Fatalf("estimate = %#v, want %d of %d spent", estimate, game.KnownStepCost, game.LeaderAllowance)
		}
		if estimate.Residue != trailingRest(t, store, leader.ID) {
			t.Fatal("the residue and the stored trailing rest disagree")
		}

		// An entity that takes no orders has no allowance and no plan, and it
		// is still in the map so a page can draw its section.
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		estimates, err := store.EstimateOrders(t.Context(), orderPlayer, turn)
		if err != nil {
			t.Fatal(err)
		}
		if _, found := estimates[hamlet.ID]; !found {
			t.Fatal("the hamlet has no estimate")
		}
		if got := estimates[hamlet.ID]; got.Allowance != 0 || len(got.Orders) != 0 {
			t.Fatalf("hamlet estimate = %#v, want an empty one", got)
		}
	})
}

// A faction that has been given orders and then deactivated keeps them, and
// every write is still refused. The trailing Rest is maintained by the write
// path, so a refused write leaves it exactly as it was.
func TestARefusedWriteLeavesTheTrailingRestAlone(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		addMove(t, store, leader.ID, compass.NE)
		before := storedOrdersNow(t, store, leader.ID)

		// An entity that is not the faction's is refused before anything is
		// touched, which is what keeps the strip from running on it.
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, leader.ID+9999, game.OrderKindMove, moving(compass.E)); !errors.Is(err, ErrUnknownEntity) {
			t.Fatalf("adding to a stranger's entity gave %v, want ErrUnknownEntity", err)
		}
		if err := store.RemoveOrder(t.Context(), orderPlayer, turn, leader.ID, 99); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("removing an order that is not there gave %v, want ErrUnknownOrder", err)
		}
		after := storedOrdersNow(t, store, leader.ID)
		if len(after) != len(before) {
			t.Fatalf("orders after refused writes = %#v, want %#v", after, before)
		}
		for index := range before {
			if after[index] != before[index] {
				t.Fatalf("order %d = %#v, want %#v", index+1, after[index], before[index])
			}
		}
	})
}

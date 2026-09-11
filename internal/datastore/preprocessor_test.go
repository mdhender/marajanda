// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"testing"

	"github.com/maloquacious/hexg"
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

// Idle points are reported, never stored. The number falls as orders are added
// and rises as they are removed, and the stored list is only ever what the
// player wrote. The homeland ring buys one cheap step and the ring beyond it
// costs three.
func TestIdlePointsAreReportedRatherThanStored(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		// Nothing has been written, so nothing is stored - and the whole
		// allowance is idle.
		if got := storedOrdersNow(t, store, leader.ID); len(got) != 0 {
			t.Fatalf("a leader that has been given nothing carries %#v", got)
		}
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance {
			t.Fatalf("idle points before any write = %d, want %d", got, game.LeaderAllowance)
		}

		addMove(t, store, leader.ID, compass.NE)
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-1 {
			t.Fatalf("idle after one cheap step = %d, want %d", got, game.LeaderAllowance-1)
		}
		addMove(t, store, leader.ID, compass.NE)
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-4 {
			t.Fatalf("idle after a step and an exploration = %d, want %d", got, game.LeaderAllowance-4)
		}
		// Two moves were written, so two orders are stored. The idle points
		// are not among them.
		stored := storedOrdersNow(t, store, leader.ID)
		if len(stored) != 2 || stored[0].Kind != game.OrderKindMove || stored[1].Kind != game.OrderKindMove {
			t.Fatalf("stored orders = %#v, want two moves and nothing else", stored)
		}

		// Orders that reach past the allowance leave nothing idle, and still
		// nothing is added to or taken from the list.
		addMove(t, store, leader.ID, compass.NE)
		if got := estimateNow(t, store, leader.ID).Residue; got != 0 {
			t.Fatalf("idle after overspending = %d, want none", got)
		}
		if got := storedOrdersNow(t, store, leader.ID); len(got) != 3 {
			t.Fatalf("stored orders = %#v, want three moves", got)
		}
		if estimate := estimateNow(t, store, leader.ID); estimate.Overspend != 1 || estimate.ExhaustsAt != 3 {
			t.Fatalf("estimate = %#v, want an overspend of 1 crossing at order 3", estimate)
		}

		// Removing an order gives the points back.
		removeOrder(t, store, leader.ID, 3)
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-4 {
			t.Fatalf("idle after removing a step = %d, want %d", got, game.LeaderAllowance-4)
		}
	})
}

// An added order goes on the end of the list, and the list is only ever what
// the player wrote. Nothing is stored behind the last order for an add to have
// to step around.
func TestAnAddedOrderLandsOnTheEndOfTheList(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		if seq := addMove(t, store, leader.ID, compass.NE); seq != 1 {
			t.Fatalf("first order = %d, want 1", seq)
		}
		if seq := addMove(t, store, leader.ID, compass.E); seq != 2 {
			t.Fatalf("second order = %d, want 2", seq)
		}
		if got := march(ordersNow(t, store, leader.ID)); got != "NE E" {
			t.Fatalf("orders = %q, want NE E", got)
		}
		if stored := storedOrdersNow(t, store, leader.ID); len(stored) != 2 {
			t.Fatalf("stored orders = %#v, want exactly the two moves", stored)
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
		if after.Residue != 0 {
			t.Fatalf("idle points = %d, want none: 1 + 3 + 3 is over six", after.Residue)
		}
	})
}

// A Rest appended to the end of a list survives, and it survives at the length
// it was given.
//
// It did not, once: the residue was stored as a trailing Rest, an append landed
// exactly where that Rest went, and the write that followed replaced it. The
// order was accepted, discarded, and reported as created. See #56.
func TestARestAppendedToTheEndIsKept(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		addMove(t, store, leader.ID, compass.NE)

		seq := addRest(t, store, leader.ID, 2)
		if seq != 2 {
			t.Fatalf("appended rest = order %d, want 2", seq)
		}
		stored := ordersNow(t, store, leader.ID)
		if len(stored) != 2 {
			t.Fatalf("orders = %#v, want the move and the rest", stored)
		}
		last := stored[1]
		if last.Seq != seq || last.Kind != game.OrderKindRest || last.Detail.Count != 2 {
			t.Fatalf("last order = %#v, want the rest of two that was appended", last)
		}
		// Two resting points and one cheap step, and what is left is idle
		// rather than swallowed by the rest.
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-3 {
			t.Fatalf("idle points = %d, want %d", got, game.LeaderAllowance-3)
		}

		// A second append lands after it rather than on top of it.
		if got := addRest(t, store, leader.ID, 1); got != 3 {
			t.Fatalf("second appended rest = order %d, want 3", got)
		}
		if got := ordersNow(t, store, leader.ID); len(got) != 3 {
			t.Fatalf("orders = %#v, want three", got)
		}
	})
}

// A rest a player places is an order like any other, wherever it sits. One on
// the end is not the residue and is not resized: it costs what it says, and
// what is left over after it is idle.
func TestAPlayerCanPlaceARest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		// A rest added on its own is stored at the length the add asked for.
		// It is the only order there is, so the rest of the allowance is idle.
		if seq := addRest(t, store, leader.ID, 1); seq != 1 {
			t.Fatalf("first order = %d, want 1", seq)
		}
		stored := ordersNow(t, store, leader.ID)
		if len(stored) != 1 || stored[0].Kind != game.OrderKindRest || stored[0].Detail.Count != 1 {
			t.Fatalf("orders = %#v, want one rest of one point", stored)
		}
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-1 {
			t.Fatalf("idle points = %d, want %d", got, game.LeaderAllowance-1)
		}

		// A rest before a move spends its points where it was asked for and
		// leaves the move to be paid for afterwards.
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindMove, moving(compass.NE)); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertOrder(t.Context(), orderPlayer, turn, leader.ID, 1, game.OrderKindRest, resting(2)); err != nil {
			t.Fatal(err)
		}
		authored := ordersNow(t, store, leader.ID)
		if len(authored) != 3 || authored[0].Kind != game.OrderKindRest || authored[0].Detail.Count != 2 {
			t.Fatalf("orders = %#v, want a rest of two, a move, then the first rest", authored)
		}
		// Two resting points, one cheap step, and the rest of one added first.
		if got := estimateNow(t, store, leader.ID).Residue; got != game.LeaderAllowance-4 {
			t.Fatalf("idle points = %d, want %d", got, game.LeaderAllowance-4)
		}

		// A count is set the way a direction is, and the idle points follow it.
		if err := store.SetOrderDetail(t.Context(), orderPlayer, turn, leader.ID, 1, resting(4)); err != nil {
			t.Fatal(err)
		}
		if got := ordersNow(t, store, leader.ID)[0].Detail.Count; got != 4 {
			t.Fatalf("rest count = %d, want 4", got)
		}
		if got := estimateNow(t, store, leader.ID).Residue; got != 0 {
			t.Fatalf("idle points = %d, want none: 4 + 1 + 1 is the whole allowance", got)
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
		if estimate.Residue != game.LeaderAllowance-game.KnownStepCost {
			t.Fatalf("idle points = %d, want %d", estimate.Residue, game.LeaderAllowance-game.KnownStepCost)
		}
		if stored := storedOrdersNow(t, store, leader.ID); len(stored) != 1 {
			t.Fatalf("stored orders = %#v, want the one move: idle points are not stored", stored)
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

// wallToWalkUpTo finds a hex a leader can stand on that has impassable ground
// beside it, together with a passable neighbour to arrive from. Walking in from
// that neighbour is what puts the wall on the faction's map.
func wallToWalkUpTo(t *testing.T, store *Store) (from hexg.Hex, approach, into compass.Point) {
	t.Helper()
	world, err := store.World(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cyl := testCylinder(t)
	for _, hex := range world.Hexes() {
		if !hex.Terrain.Passable(game.EntityKindLeader) {
			continue
		}
		var wall, open compass.Point
		for _, point := range compass.Points() {
			if world.IsPassable(compass.Neighbor(cyl, hex.Coord, point), game.EntityKindLeader) {
				if open == 0 {
					open = point
				}
			} else if wall == 0 {
				wall = point
			}
		}
		if wall == 0 || open == 0 {
			continue
		}
		return compass.Neighbor(cyl, hex.Coord, open), open.Opposite(), wall
	}
	t.Fatal("the test world has no shoreline to walk up to")
	return hexg.Hex{}, 0, 0
}

// The estimate warns about a step into ground the faction has already seen it
// cannot enter, and it warns without changing what the step costs or where the
// walk goes on from. This is what a player was not told before #57: the leader
// was priced into a lake that had been on their map for a turn.
func TestTheEstimateWarnsAboutWaterTheFactionHasSeen(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		from, approach, into := wallToWalkUpTo(t, store)
		standAt(t, store, leader.ID, from)

		// Walk up to the shore. Entering the hex observes the ring around it,
		// so the wall is on the faction's map from the next turn.
		addMove(t, store, leader.ID, approach)
		advanceTurn(t, store)

		stand := compass.Neighbor(testCylinder(t), from, approach)
		wall := compass.Neighbor(testCylinder(t), stand, into)
		addMove(t, store, leader.ID, into)

		estimate := estimateNow(t, store, leader.ID)
		step := estimate.Orders[0]
		if step.Warning != game.FailureTerrain {
			t.Fatalf("warning = %q, want %q for %v, which the faction has seen", step.Warning, game.FailureTerrain, wall)
		}
		// Priced as a step onto known ground, and the walk still ends there:
		// the warning is advice and nothing downstream of it moves.
		if step.Cost != game.KnownStepCost || step.To != wall || estimate.End != wall {
			t.Fatalf("step = %#v ending %v, want it priced and walked as though it lands", step, estimate.End)
		}
	})
}

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

// ordersNow reads one entity's stanzas as of the turn the game is on.
func ordersNow(t *testing.T, store *Store, entityID int64) []Order {
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

// steps names a stanza's directions, so a test can say what it wanted in one
// line and read what it got in another.
func steps(order Order) string {
	names := ""
	for _, point := range order.Steps {
		if names != "" {
			names += " "
		}
		names += point.String()
	}
	return names
}

func addMove(t *testing.T, store *Store, entityID int64) int {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seq, err := store.AddOrder(t.Context(), orderPlayer, turn, entityID, game.OrderKindMove)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func setStep(t *testing.T, store *Store, entityID int64, seq, step int, direction compass.Point) {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetOrderStep(t.Context(), orderPlayer, turn, entityID, seq, step, direction); err != nil {
		t.Fatal(err)
	}
}

// A stanza is added empty and filled a box at a time. The box on the end is the
// one that appends, and there is never an "add a box" control to press.
func TestAnOrderIsBuiltOneBoxAtATime(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)

		seq := addMove(t, store, leader.ID)
		if seq != 1 {
			t.Fatalf("first stanza = %d, want 1", seq)
		}
		orders := ordersNow(t, store, leader.ID)
		if len(orders) != 1 || orders[0].Kind != game.OrderKindMove || len(orders[0].Steps) != 0 {
			t.Fatalf("orders = %#v, want one empty move", orders)
		}

		// Each write addresses the blank box on the end, which is one past
		// what is stored.
		for step, point := range []compass.Point{compass.NW, compass.NE, compass.E} {
			setStep(t, store, leader.ID, seq, step+1, point)
		}
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "NW NE E" {
			t.Fatalf("steps = %q, want NW NE E", got)
		}

		// A filled box takes a new direction in place.
		setStep(t, store, leader.ID, seq, 2, compass.SE)
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "NW SE E" {
			t.Fatalf("steps = %q, want NW SE E", got)
		}
	})
}

// Clearing a middle box shifts the later ones left. One order has exactly one
// stored form, which is what replay depends on.
func TestClearingAStepCompactsTheRest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID)
		for step, point := range []compass.Point{compass.NW, compass.NE, compass.E} {
			setStep(t, store, leader.ID, seq, step+1, point)
		}

		// The blank option on the second box: "move nw <blank> e" is stored
		// and re-rendered as "move nw e".
		setStep(t, store, leader.ID, seq, 2, 0)
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "NW E" {
			t.Fatalf("steps = %q, want NW E", got)
		}
		// The stored numbering is contiguous, not a 1 and a 3.
		if got := storedSteps(t, store, leader.ID, seq); got != "1:nw 2:e" {
			t.Fatalf("stored steps = %q, want 1:nw 2:e", got)
		}

		// Blanking the box on the end is not a change at all.
		if err := storeSetStep(store, t, leader.ID, seq, 3, 0); err != nil {
			t.Fatalf("clearing the blank box: %v", err)
		}
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "NW E" {
			t.Fatalf("steps = %q, want NW E", got)
		}
	})
}

// Removing a stanza renumbers the ones after it, and takes their steps with it
// rather than leaving them behind under a number that has moved.
func TestRemovingAnOrderRenumbersTheRest(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		for range 3 {
			addMove(t, store, leader.ID)
		}
		setStep(t, store, leader.ID, 1, 1, compass.NW)
		setStep(t, store, leader.ID, 2, 1, compass.E)
		setStep(t, store, leader.ID, 3, 1, compass.SW)

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
		if orders[0].Seq != 1 || steps(orders[0]) != "E" {
			t.Fatalf("first stanza = %#v, want the old second renumbered to 1", orders[0])
		}
		if orders[1].Seq != 2 || steps(orders[1]) != "SW" {
			t.Fatalf("second stanza = %#v, want the old third renumbered to 2", orders[1])
		}
		// No step is left under a sequence number that no longer exists.
		if got := storedSteps(t, store, leader.ID, 3); got != "" {
			t.Fatalf("stanza 3 still holds %q", got)
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
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, hamlet.ID, game.OrderKindMove); !errors.Is(err, ErrOrderKindRefused) {
			t.Fatalf("hamlet move = %v, want %v", err, ErrOrderKindRefused)
		}
		// An order kind the game does not know is refused the same way.
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, hamlet.ID, game.OrderKind("besiege")); !errors.Is(err, ErrOrderKindRefused) {
			t.Fatalf("unknown kind = %v, want %v", err, ErrOrderKindRefused)
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
		if _, err := store.AddOrder(t.Context(), orderPlayer, turn, rival[0].ID, game.OrderKindMove); !errors.Is(err, ErrUnknownEntity) {
			t.Fatalf("ordering a rival's leader = %v, want %v", err, ErrUnknownEntity)
		}
	})
}

// Only the current turn is writable. Every write carries the turn the page was
// rendered from, so a page that was open while the clock moved is refused.
func TestOnlyTheCurrentTurnIsWritable(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID)
		setStep(t, store, leader.ID, seq, 1, compass.NW)

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
				_, err := store.AddOrder(t.Context(), orderPlayer, closed, leader.ID, game.OrderKindMove)
				return err
			},
			"set a step": func() error {
				return store.SetOrderStep(t.Context(), orderPlayer, closed, leader.ID, seq, 1, compass.E)
			},
			"save steps": func() error {
				return store.SetOrderSteps(t.Context(), orderPlayer, closed, []OrderSteps{{EntityID: leader.ID, Seq: seq, Steps: []compass.Point{compass.E}}})
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
		if _, err := store.AddOrder(t.Context(), orderPlayer, next+1, leader.ID, game.OrderKindMove); !errors.Is(err, ErrTurnClosed) {
			t.Fatalf("add on turn %d = %v, want %v", next+1, err, ErrTurnClosed)
		}
	})
}

// Advancing the turn freezes what came before it: the previous turn's rows are
// exactly as they were, and the new turn starts with nothing in it.
func TestAdvancingTheTurnLeavesThePreviousTurnAlone(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID)
		for step, point := range []compass.Point{compass.NW, compass.E} {
			setStep(t, store, leader.ID, seq, step+1, point)
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
		if len(after[leader.ID]) != len(before[leader.ID]) || steps(after[leader.ID][0]) != steps(before[leader.ID][0]) {
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
		if got := addMove(t, store, leader.ID); got != 1 {
			t.Fatalf("first stanza of turn %d = %d, want 1", next, got)
		}
		if got := len(ordersNow(t, store, leader.ID)); got != 1 {
			t.Fatalf("turn %d holds %d stanzas, want 1", next, got)
		}
	})
}

// The script-free page saves a whole page of boxes at once. Blanks are dropped
// on the way in, so a save compacts exactly as a box-at-a-time edit does.
func TestSavingAWholePageOfStepsCompacts(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		first, second := addMove(t, store, leader.ID), addMove(t, store, leader.ID)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetOrderSteps(t.Context(), orderPlayer, turn, []OrderSteps{
			{EntityID: leader.ID, Seq: first, Steps: []compass.Point{compass.NW, compass.E}},
			{EntityID: leader.ID, Seq: second, Steps: []compass.Point{compass.SW}},
		}); err != nil {
			t.Fatal(err)
		}
		orders := ordersNow(t, store, leader.ID)
		if len(orders) != 2 || steps(orders[0]) != "NW E" || steps(orders[1]) != "SW" {
			t.Fatalf("orders = %#v, want NW E then SW", orders)
		}

		// A save is a replacement, so a shorter list leaves nothing behind.
		if err := store.SetOrderSteps(t.Context(), orderPlayer, turn, []OrderSteps{
			{EntityID: leader.ID, Seq: first, Steps: []compass.Point{compass.E}},
		}); err != nil {
			t.Fatal(err)
		}
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "E" {
			t.Fatalf("steps = %q, want E", got)
		}
	})
}

// A stanza that is not there, and a box the page never showed, are refused
// rather than quietly creating one.
func TestOrderWritesRefuseWhatThePageNeverShowed(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetOrderStep(t.Context(), orderPlayer, turn, leader.ID, seq+1, 1, compass.E); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("step on a missing stanza = %v, want %v", err, ErrUnknownOrder)
		}
		if err := store.RemoveOrder(t.Context(), orderPlayer, turn, leader.ID, seq+1); !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("removing a missing stanza = %v, want %v", err, ErrUnknownOrder)
		}
		// The stanza is empty, so it shows one box. Two is not a box.
		for _, step := range []int{0, 2, MaxOrderSteps + 1} {
			if err := store.SetOrderStep(t.Context(), orderPlayer, turn, leader.ID, seq, step, compass.E); !errors.Is(err, ErrUnknownStep) {
				t.Fatalf("step %d = %v, want %v", step, err, ErrUnknownStep)
			}
		}
		// The storage limit is the storage limit, whatever a save asks for.
		long := make([]compass.Point, MaxOrderSteps+1)
		for index := range long {
			long[index] = compass.E
		}
		if err := store.SetOrderSteps(t.Context(), orderPlayer, turn, []OrderSteps{{EntityID: leader.ID, Seq: seq, Steps: long}}); !errors.Is(err, ErrTooManySteps) {
			t.Fatalf("a %d step order = %v, want %v", len(long), err, ErrTooManySteps)
		}
	})
}

// A stanza the store refuses is a stanza the database does not hold. The write
// runs in a transaction, so a save that fails part way leaves nothing.
func TestAFailedSaveWritesNothing(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		seq := addMove(t, store, leader.ID)
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		err = store.SetOrderSteps(t.Context(), orderPlayer, turn, []OrderSteps{
			{EntityID: leader.ID, Seq: seq, Steps: []compass.Point{compass.NW}},
			{EntityID: leader.ID, Seq: seq + 1, Steps: []compass.Point{compass.E}},
		})
		if !errors.Is(err, ErrUnknownOrder) {
			t.Fatalf("save = %v, want %v", err, ErrUnknownOrder)
		}
		if got := steps(ordersNow(t, store, leader.ID)[0]); got != "" {
			t.Fatalf("steps = %q, want the save rolled back", got)
		}
	})
}

// storeSetStep is SetOrderStep on the current turn, returning the error instead
// of failing the test with it.
func storeSetStep(store *Store, t *testing.T, entityID int64, seq, step int, direction compass.Point) error {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return store.SetOrderStep(t.Context(), orderPlayer, turn, entityID, seq, step, direction)
}

// storedSteps reads a stanza's steps straight out of the table, as
// "<step>:<direction>" pairs, so a test can see the numbering rather than the
// list the store rebuilt from it.
func storedSteps(t *testing.T, store *Store, entityID int64, seq int) string {
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
		SELECT step, direction FROM order_steps
		WHERE turn = ?1 AND entity_id = ?2 AND seq = ?3 ORDER BY step;`, &sqlitex.ExecOptions{
		Args: []any{turn, entityID, seq},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			if stored != "" {
				stored += " "
			}
			stored += stmt.ColumnText(0) + ":" + stmt.ColumnText(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return stored
}

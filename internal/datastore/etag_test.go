// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"testing"

	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
)

func tagFor(t *testing.T, store *Store, email string, turn int) string {
	t.Helper()
	orders, err := store.OrdersAsOf(t.Context(), email, turn)
	if err != nil {
		t.Fatal(err)
	}
	tag := OrdersTag(email, turn, orders)
	if tag == "" {
		t.Fatal("OrdersTag returned an empty tag")
	}
	return tag
}

func orderTag(t *testing.T, store *Store, turn int) string {
	t.Helper()
	return tagFor(t, store, orderPlayer, turn)
}

// The tag is a function of the list and nothing else. Reading twice without
// writing gives the same tag, and every kind of write gives a different one -
// including the ones that leave the list the same length.
func TestOrdersETagMovesWithTheOrdersAndNotOtherwise(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	leader, _ := foundedFaction(t, store)

	empty := orderTag(t, store, game.FirstTurn)
	if again := orderTag(t, store, game.FirstTurn); again != empty {
		t.Fatalf("two reads of one list tagged %s and %s", empty, again)
	}

	seen := map[string]string{empty: "the empty list"}
	note := func(what string) {
		tag := orderTag(t, store, game.FirstTurn)
		if where, ok := seen[tag]; ok {
			t.Fatalf("%s tags the same as %s", what, where)
		}
		seen[tag] = what
	}

	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.NE)); err != nil {
		t.Fatal(err)
	}
	note("one move")
	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.E)); err != nil {
		t.Fatal(err)
	}
	note("two moves")
	// A detail change keeps the length and the kinds, so a tag over the shape
	// alone would miss it.
	if err := store.SetOrderDetail(t.Context(), orderPlayer, game.FirstTurn, leader.ID, 2, moving(compass.W)); err != nil {
		t.Fatal(err)
	}
	note("the second move turned west")
	if err := store.RemoveOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, 1); err != nil {
		t.Fatal(err)
	}
	note("one move again, but the other one")
}

// A different turn is a different list, so it tags differently even when the
// orders read the same.
func TestOrdersETagSeparatesTurns(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	foundedFaction(t, store)

	first := orderTag(t, store, game.FirstTurn)
	second := orderTag(t, store, game.FirstTurn+1)
	if first == second {
		t.Fatalf("two empty turns tag alike: %s", first)
	}
}

// The precondition is the point: a write that names a list it is not writing to
// fails, and leaves the list alone.
func TestExpectOrdersRefusesAWriteAgainstAStaleList(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	leader, _ := foundedFaction(t, store)

	stale := orderTag(t, store, game.FirstTurn)
	// Somebody else writes.
	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindRest, resting(3)); err != nil {
		t.Fatal(err)
	}
	fresh := orderTag(t, store, game.FirstTurn)

	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.NE),
		ExpectOrders(stale)); !errors.Is(err, ErrOrdersChanged) {
		t.Fatalf("append against a stale list = %v, want %v", err, ErrOrdersChanged)
	}
	if now := orderTag(t, store, game.FirstTurn); now != fresh {
		t.Fatal("the refused write changed the orders anyway")
	}

	// The same write against the list as it now stands goes through.
	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.NE),
		ExpectOrders(fresh)); err != nil {
		t.Fatalf("append against the current list = %v", err)
	}
	if orderTag(t, store, game.FirstTurn) == fresh {
		t.Fatal("the accepted write left the tag alone")
	}
}

// Every write honours the expectation, not just the one the first test reached
// for. A precondition a route quietly dropped is worse than none.
func TestExpectOrdersGuardsEveryWrite(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	leader, _ := foundedFaction(t, store)
	for range 2 {
		if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.NE)); err != nil {
			t.Fatal(err)
		}
	}
	const stale = "0000000000000000000000000000000000000000000000000000000000000000"

	for name, write := range map[string]func() error{
		"add": func() error {
			_, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.E), ExpectOrders(stale))
			return err
		},
		"insert": func() error {
			return store.InsertOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, 1, game.OrderKindMove, moving(compass.E), ExpectOrders(stale))
		},
		"set one detail": func() error {
			return store.SetOrderDetail(t.Context(), orderPlayer, game.FirstTurn, leader.ID, 1, moving(compass.E), ExpectOrders(stale))
		},
		"set details": func() error {
			return store.SetOrderDetails(t.Context(), orderPlayer, game.FirstTurn,
				[]OrderUpdate{{EntityID: leader.ID, Seq: 1, Detail: moving(compass.E)}}, ExpectOrders(stale))
		},
		"remove": func() error {
			return store.RemoveOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, 1, ExpectOrders(stale))
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := orderTag(t, store, game.FirstTurn)
			if err := write(); !errors.Is(err, ErrOrdersChanged) {
				t.Fatalf("%s against a stale list = %v, want %v", name, err, ErrOrdersChanged)
			}
			if after := orderTag(t, store, game.FirstTurn); after != before {
				t.Fatalf("%s changed the orders despite being refused", name)
			}
		})
	}
}

// One faction's writes do not move another's tag. The tag is scoped to the
// faction that reads it.
func TestOrdersETagIsPerFaction(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	leader, _ := foundedFaction(t, store)

	other := "admin@marajanda.com"
	before := tagFor(t, store, other, game.FirstTurn)
	if _, err := store.AddOrder(t.Context(), orderPlayer, game.FirstTurn, leader.ID, game.OrderKindMove, moving(compass.NE)); err != nil {
		t.Fatal(err)
	}
	after := tagFor(t, store, other, game.FirstTurn)
	if before != after {
		t.Fatal("one faction's write moved another faction's tag")
	}
	if before == orderTag(t, store, game.FirstTurn) {
		t.Fatal("two factions with different orders tag alike")
	}
}

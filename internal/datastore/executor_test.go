// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// advanceTurn processes the turn the game is on and returns the turn it moves
// to, which is what the admin's control does.
func advanceTurn(t *testing.T, store *Store) int {
	t.Helper()
	next, err := store.AdvanceTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestConcurrentAdvancesReturnTheTurnsTheyCommitted(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		results := make(chan int, 2)
		errors := make(chan error, 2)
		start := make(chan struct{})
		var ready sync.WaitGroup
		ready.Add(2)
		for range 2 {
			go func() {
				ready.Done()
				<-start
				turn, err := store.AdvanceTurn(t.Context())
				results <- turn
				errors <- err
			}()
		}
		ready.Wait()
		close(start)

		got := []int{<-results, <-results}
		for range 2 {
			if err := <-errors; err != nil {
				t.Fatalf("concurrent AdvanceTurn: %v", err)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, []int{game.FirstTurn + 1, game.FirstTurn + 2}) {
			t.Fatalf("concurrent AdvanceTurn results = %v, want [2 3]", got)
		}
		if current, err := store.CurrentTurn(t.Context()); err != nil || current != game.FirstTurn+2 {
			t.Fatalf("CurrentTurn = %d, %v; want 3", current, err)
		}
	})
}

func TestAdvanceTurnRefusesTheEndOfTime(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		conn, release, err := store.take(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlitex.ExecuteTransient(conn, `UPDATE game SET current_turn = ?1 WHERE id = 1;`, &sqlitex.ExecOptions{
			Args: []any{game.EndOfTimeTurn - 1},
		}); err != nil {
			release()
			t.Fatal(err)
		}
		release()

		if next, err := store.AdvanceTurn(t.Context()); err == nil || next != 0 {
			t.Fatalf("AdvanceTurn = %d, %v; want 0 and an error", next, err)
		}
		if current, err := store.CurrentTurn(t.Context()); err != nil || current != game.EndOfTimeTurn-1 {
			t.Fatalf("CurrentTurn = %d, %v; want %d", current, err, game.EndOfTimeTurn-1)
		}
	})
}

// locationAsOf reads where an entity stood on a turn, reporting whether it
// stood in the world at all.
func locationAsOf(t *testing.T, store *Store, email string, entityID int64, turn int) (hexg.Hex, bool) {
	t.Helper()
	entities, err := store.EntitiesAsOf(t.Context(), email, turn)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range entities {
		if entity.ID == entityID {
			return entity.Location, true
		}
	}
	return hexg.Hex{}, false
}

// locationRows renders every location fact one entity holds, periods and all.
func locationRows(t *testing.T, store *Store, entityID int64) []string {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var rows []string
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT q, r, effective_from, effective_through FROM entity_locations WHERE entity_id = ?1;`,
		&sqlitex.ExecOptions{
			Args: []any{entityID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rows = append(rows, fmt.Sprintf("(%d,%d) [%d,%d)",
					stmt.ColumnInt(0), stmt.ColumnInt(1), stmt.ColumnInt(2), stmt.ColumnInt(3)))
				return nil
			},
		}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(rows)
	return rows
}

// walkableFrom picks a direction out of a hex that a leader can actually walk,
// so a test says "step onto real ground" without hard-coding a generated map.
func walkableFrom(t *testing.T, store *Store, from hexg.Hex) compass.Point {
	t.Helper()
	world, err := store.World(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range compass.Points() {
		if world.IsPassable(compass.Neighbor(testCylinder(t), from, point), game.EntityKindLeader) {
			return point
		}
	}
	t.Fatalf("no passable neighbour of %v", from)
	return 0
}

// standAt moves an entity's open location fact to a hex without processing a
// turn, which is how a test puts a leader somewhere the generated world has
// something worth walking into.
func standAt(t *testing.T, store *Store, entityID int64, coord hexg.Hex) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE entity_locations SET q = ?2, r = ?3
		WHERE entity_id = ?1 AND effective_through = ?4;`, &sqlitex.ExecOptions{
		Args: []any{entityID, coord.Q(), coord.R(), game.EndOfTimeTurn},
	}); err != nil {
		t.Fatal(err)
	}
}

// impassableEdge finds a land hex with an impassable neighbour, and the
// direction that walks into it.
func impassableEdge(t *testing.T, store *Store) (stand hexg.Hex, into compass.Point) {
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
		for _, point := range compass.Points() {
			if !world.IsPassable(compass.Neighbor(cyl, hex.Coord, point), game.EntityKindLeader) {
				return hex.Coord, point
			}
		}
	}
	t.Fatal("the test world has no impassable ground")
	return hexg.Hex{}, 0
}

// Advancing the turn carries out the orders of the turn it closes. The entity
// is somewhere new, and the fact that says so is effective from the turn after
// the one it moved on: a turn-N report still describes the world as it was
// while turn N happened.
func TestAdvancingTheTurnMovesAnEntityAndDatesTheFact(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		origin := leader.Location
		direction := walkableFrom(t, store, origin)
		destination := compass.Neighbor(testCylinder(t), origin, direction)
		addMove(t, store, leader.ID, direction)

		closed := game.FirstTurn
		if next := advanceTurn(t, store); next != closed+1 {
			t.Fatalf("AdvanceTurn = %d, want %d", next, closed+1)
		}

		// The turn that was processed still reads as it was while it happened.
		if got, _ := locationAsOf(t, store, orderPlayer, leader.ID, closed); got != origin {
			t.Fatalf("location as of turn %d = %v, want the origin %v", closed, got, origin)
		}
		if got, _ := locationAsOf(t, store, orderPlayer, leader.ID, closed+1); got != destination {
			t.Fatalf("location as of turn %d = %v, want %v", closed+1, got, destination)
		}
		want := []string{
			fmt.Sprintf("(%d,%d) [%d,%d)", origin.Q(), origin.R(), closed, closed+1),
			fmt.Sprintf("(%d,%d) [%d,%d)", destination.Q(), destination.R(), closed+1, game.EndOfTimeTurn),
		}
		got := locationRows(t, store, leader.ID)
		slices.Sort(want)
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("location rows = %v, want %v", got, want)
		}
	})
}

// A step is what a faction learns from. The hex the entity entered is explored
// and its six neighbours are observed, all effective from the turn after the
// one it walked on.
func TestProcessingWritesWhatTheTurnRevealed(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		cyl := testCylinder(t)
		direction := walkableFrom(t, store, leader.Location)
		destination := compass.Neighbor(cyl, leader.Location, direction)
		addMove(t, store, leader.ID, direction)

		beyond := compass.Neighbor(cyl, destination, direction)
		if knowledgeAsOf(t, store, orderPlayer, game.FirstTurn).Knows(beyond) {
			t.Skip("the generated world put the second ring inside the homeland")
		}

		advanceTurn(t, store)

		// Nothing a turn reveals prices a step in that turn, so the turn that
		// was processed knows exactly what it knew when it opened.
		if knowledgeAsOf(t, store, orderPlayer, game.FirstTurn).Knows(beyond) {
			t.Fatal("a hex revealed during turn 1 is known as of turn 1")
		}
		known := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1)
		if got := known.State(destination); got != game.KnowledgeExplored {
			t.Fatalf("the hex the leader entered is %q, want %q", got, game.KnowledgeExplored)
		}
		if got := known.State(beyond); got != game.KnowledgeObserved {
			t.Fatalf("a neighbour of the hex entered is %q, want %q", got, game.KnowledgeObserved)
		}
	})
}

// A step into ground that cannot be entered is paid for and moves nothing. The
// exploration happened, so the hex becomes known; the entity is standing where
// it started.
func TestProcessingChargesAStepIntoImpassableGround(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		stand, into := impassableEdge(t, store)
		standAt(t, store, leader.ID, stand)
		wall := compass.Neighbor(testCylinder(t), stand, into)

		addMove(t, store, leader.ID, into)
		advanceTurn(t, store)

		if got, _ := locationAsOf(t, store, orderPlayer, leader.ID, game.FirstTurn+1); got != stand {
			t.Fatalf("a leader that walked into a wall is at %v, want %v", got, stand)
		}
		if got := locationRows(t, store, leader.ID); len(got) != 1 {
			t.Fatalf("location rows = %v, want the one it never left", got)
		}
		// The wall is known now, and known as observed: the leader saw it and
		// did not stand in it.
		if got := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1).State(wall); got != game.KnowledgeObserved {
			t.Fatalf("the hex the step failed into is %q, want %q", got, game.KnowledgeObserved)
		}
	})
}

// Orders are the history a replay reads, so processing leaves them exactly as
// the player wrote them. The executor writes no orders.
func TestProcessingLeavesTheOrdersOfTheClosedTurnAlone(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		direction := walkableFrom(t, store, leader.Location)
		addMove(t, store, leader.ID, direction)
		before := storedOrdersNow(t, store, leader.ID)

		advanceTurn(t, store)

		after, err := store.OrdersAsOf(t.Context(), orderPlayer, game.FirstTurn)
		if err != nil {
			t.Fatal(err)
		}
		if len(after[leader.ID]) != len(before) {
			t.Fatalf("orders of the closed turn = %#v, want %#v", after[leader.ID], before)
		}
		for index := range before {
			if after[leader.ID][index] != before[index] {
				t.Fatalf("order %d = %#v, want %#v", index+1, after[leader.ID][index], before[index])
			}
		}
		// The turn that opened carries nothing. Orders belong to the turn they
		// were written for.
		if got := storedOrdersNow(t, store, leader.ID); len(got) != 0 {
			t.Fatalf("the new turn opened carrying %#v", got)
		}
	})
}

// A deactivated faction gives no orders, so the orders it wrote while it was
// active are history rather than instructions. Nothing carries them out.
func TestProcessingSkipsAnInactiveFaction(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		origin := leader.Location
		addMove(t, store, leader.ID, walkableFrom(t, store, origin))
		before := knowledgeRows(t, store, orderPlayer)

		deactivate(t, store, "factions", orderPlayer)
		advanceTurn(t, store)

		if got, _ := locationAsOf(t, store, orderPlayer, leader.ID, game.FirstTurn+1); got != origin {
			t.Fatalf("an inactive faction's leader moved to %v", got)
		}
		if got := knowledgeRows(t, store, orderPlayer); len(got) != len(before) {
			t.Fatalf("an inactive faction learnt %d rows, had %d", len(got), len(before))
		}
	})
}

// An entity walked until it runs out stops where it stopped. Six points buy one
// cheap step onto the homeland ring and one exploration beyond it, and the
// orders after that exhaust: the leader is two hexes out, not four.
func TestProcessingStopsWhereTheEntityRunsOut(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		cyl := testCylinder(t)
		world, err := store.World(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// A direction with four passable hexes in a row, so nothing but the
		// allowance stops the march.
		direction := compass.Point(0)
		for _, point := range compass.Points() {
			walked, clear := leader.Location, true
			for range 4 {
				walked = compass.Neighbor(cyl, walked, point)
				clear = clear && world.IsPassable(walked, game.EntityKindLeader)
			}
			if clear {
				direction = point
				break
			}
		}
		if !direction.IsValid() {
			t.Skip("the generated world has no four passable hexes in a line from the origin")
		}
		for range 4 {
			addMove(t, store, leader.ID, direction)
		}

		advanceTurn(t, store)

		stopped := compass.Steps(cyl, leader.Location, direction, 2)
		if got, _ := locationAsOf(t, store, orderPlayer, leader.ID, game.FirstTurn+1); got != stopped {
			t.Fatalf("leader stopped at %v, want %v: one cheap step and one exploration", got, stopped)
		}
	})
}

// Processing reads only the stored orders and facts dated at or before the
// turn, so the same rows produce the same world. Two databases with the same
// seeds, given the same orders, hold the same knowledge and the same locations.
func TestProcessingIsAReplay(t *testing.T) {
	walk := func(t *testing.T, store *Store) ([]string, hexg.Hex) {
		leader, _ := foundedFaction(t, store)
		direction := walkableFrom(t, store, leader.Location)
		addMove(t, store, leader.ID, direction)
		advanceTurn(t, store)
		addMove(t, store, leader.ID, direction.Opposite())
		advanceTurn(t, store)
		at, _ := locationAsOf(t, store, orderPlayer, leader.ID, game.FirstTurn+2)
		return knowledgeRows(t, store, orderPlayer), at
	}

	first, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	firstRows, firstAt := walk(t, first)
	secondRows, secondAt := walk(t, second)

	if firstAt != secondAt {
		t.Fatalf("the same orders left the leader at %v and at %v", firstAt, secondAt)
	}
	if len(firstRows) != len(secondRows) {
		t.Fatalf("%d knowledge rows against %d", len(firstRows), len(secondRows))
	}
	for index := range firstRows {
		if firstRows[index] != secondRows[index] {
			t.Fatalf("row %d = %q, want %q", index, secondRows[index], firstRows[index])
		}
	}
}

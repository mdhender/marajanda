// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"fmt"
	"slices"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// resultsAsOf reads what a faction's entities did on a turn.
func resultsAsOf(t *testing.T, store *Store, email string, turn int) []TurnResult {
	t.Helper()
	results, err := store.ResultsAsOf(t.Context(), email, turn)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

// resultFor picks one entity's result out of a turn's, failing when the turn
// recorded nothing for it. Every entity that stood in the world has one.
func resultFor(t *testing.T, store *Store, entityID int64, turn int) TurnResult {
	t.Helper()
	for _, result := range resultsAsOf(t, store, orderPlayer, turn) {
		if result.EntityID == entityID {
			return result
		}
	}
	t.Fatalf("turn %d recorded nothing for entity %d", turn, entityID)
	return TurnResult{}
}

// resultRows renders every stored result row, all three grains, in a stable
// order. Comparing two of these compares the whole record rather than a
// summary of it, which is what a replay has to do.
func resultRows(t *testing.T, store *Store) []string {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var rows []string
	read := func(query string, render func(*sqlite.Stmt) string) {
		if err := sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rows = append(rows, render(stmt))
				return nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	read(`SELECT turn, entity_id, allowance, spent, lapsed, start_q, start_r, end_q, end_r FROM turn_results;`,
		func(stmt *sqlite.Stmt) string {
			return fmt.Sprintf("turn %d entity %d: %d ap, spent %d, lapsed %d, (%d,%d) to (%d,%d)",
				stmt.ColumnInt(0), stmt.ColumnInt64(1), stmt.ColumnInt(2), stmt.ColumnInt(3),
				stmt.ColumnInt(4), stmt.ColumnInt(5), stmt.ColumnInt(6), stmt.ColumnInt(7), stmt.ColumnInt(8))
		})
	read(`SELECT turn, entity_id, seq, kind, cost, carried, coalesce(reason, ''),
	             from_q, from_r, target_q, target_r, to_q, to_r FROM turn_result_orders;`,
		func(stmt *sqlite.Stmt) string {
			return fmt.Sprintf("turn %d entity %d order %d: %s cost %d carried %d %q (%d,%d)->(%d,%d) at (%d,%d)",
				stmt.ColumnInt(0), stmt.ColumnInt64(1), stmt.ColumnInt(2), stmt.ColumnText(3),
				stmt.ColumnInt(4), stmt.ColumnInt(5), stmt.ColumnText(6),
				stmt.ColumnInt(7), stmt.ColumnInt(8), stmt.ColumnInt(9), stmt.ColumnInt(10),
				stmt.ColumnInt(11), stmt.ColumnInt(12))
		})
	read(`SELECT turn, entity_id, seq, q, r, state FROM turn_result_observations;`,
		func(stmt *sqlite.Stmt) string {
			return fmt.Sprintf("turn %d entity %d order %d saw (%d,%d) %s",
				stmt.ColumnInt(0), stmt.ColumnInt64(1), stmt.ColumnInt(2),
				stmt.ColumnInt(3), stmt.ColumnInt(4), stmt.ColumnText(5))
		})
	slices.Sort(rows)
	return rows
}

// observationsOf returns the hexes one order of a result revealed in a state.
func observationsOf(result TurnResult, seq int, state game.Knowledge) []hexg.Hex {
	hexes := make([]hexg.Hex, 0, len(result.Observations))
	for _, observation := range result.Observations {
		if observation.Seq == seq && observation.State == state {
			hexes = append(hexes, observation.Hex)
		}
	}
	return hexes
}

// A step that lands is recorded on both grains: one order outcome saying it was
// carried and what it was charged, and one observation per hex it revealed.
func TestProcessingRecordsAStepThatLanded(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		origin := leader.Location
		direction := walkableFrom(t, store, origin)
		destination := compass.Neighbor(testCylinder(t), origin, direction)
		seq := addMove(t, store, leader.ID, direction)

		closed := game.FirstTurn
		advanceTurn(t, store)

		result := resultFor(t, store, leader.ID, closed)
		// The ledger accounts for six points: one cheap step onto the homeland
		// ring, and the trailing Rest the pre-processor sized to what the step
		// left over. Nothing lapses, because the player agreed to the residue.
		if result.Allowance != game.LeaderAllowance || result.Spent != game.LeaderAllowance || result.Lapsed != 0 {
			t.Fatalf("ledger = %d of %d spent, %d lapsed, want the allowance accounted for",
				result.Spent, result.Allowance, result.Lapsed)
		}
		if result.Start != origin || result.End != destination {
			t.Fatalf("recorded a walk from %v to %v, want %v to %v", result.Start, result.End, origin, destination)
		}
		if len(result.Orders) != 2 {
			t.Fatalf("order results = %#v, want the step and the trailing Rest", result.Orders)
		}
		if rest := result.Orders[1]; rest.Kind != game.OrderKindRest || rest.Cost != game.LeaderAllowance-game.KnownStepCost {
			t.Fatalf("trailing order = %#v, want a Rest of %d", rest, game.LeaderAllowance-game.KnownStepCost)
		}
		order := result.Orders[0]
		if order.Seq != seq || order.Kind != game.OrderKindMove || !order.Carried || order.Reason != "" {
			t.Fatalf("order result = %#v, want a carried move", order)
		}
		if order.Cost != game.KnownStepCost || order.From != origin || order.Target != destination || order.To != destination {
			t.Fatalf("order result = %#v, want %d AP from %v into %v", order, game.KnownStepCost, origin, destination)
		}
		// The step explored the hex it entered and observed the ring around it.
		explored := observationsOf(result, seq, game.KnowledgeExplored)
		if len(explored) != 1 || explored[0] != destination {
			t.Fatalf("explored %v, want just %v", explored, destination)
		}
		if observed := observationsOf(result, seq, game.KnowledgeObserved); len(observed) != len(compass.Points()) {
			t.Fatalf("observed %v, want the six hexes around %v", observed, destination)
		}
	})
}

// A result is the record of what one entity's turn was, so an entity nobody
// gave orders to has one. Its whole allowance lapsed, which is a line a player
// asks about rather than a missing row for them to infer it from.
func TestProcessingRecordsAnEntityThatWasGivenNothingToDo(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, hamlet := foundedFaction(t, store)

		closed := game.FirstTurn
		advanceTurn(t, store)

		idle := resultFor(t, store, leader.ID, closed)
		if len(idle.Orders) != 0 || len(idle.Observations) != 0 {
			t.Fatalf("an entity with no orders recorded %#v", idle)
		}
		if idle.Spent != 0 || idle.Lapsed != game.LeaderAllowance {
			t.Fatalf("spent %d and lapsed %d, want the whole allowance lapsed", idle.Spent, idle.Lapsed)
		}
		if idle.Start != leader.Location || idle.End != leader.Location {
			t.Fatalf("an entity with no orders moved from %v to %v", idle.Start, idle.End)
		}
		// An entity kind that accepts no orders has no allowance, so its turn
		// accounts for nothing. It still has a turn.
		settled := resultFor(t, store, hamlet.ID, closed)
		if settled.Allowance != 0 || settled.Spent != 0 || settled.Lapsed != 0 {
			t.Fatalf("a hamlet's ledger = %#v, want nothing to account for", settled)
		}
	})
}

// A step into ground that cannot be entered says so by name. It was charged, it
// carried nothing, and the hex it walked into is on the record as seen.
func TestProcessingRecordsWhyAStepFailed(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		stand, into := iceEdge(t, store)
		standAt(t, store, leader.ID, stand)
		wall := compass.Neighbor(testCylinder(t), stand, into)
		seq := addMove(t, store, leader.ID, into)

		closed := game.FirstTurn
		advanceTurn(t, store)

		result := resultFor(t, store, leader.ID, closed)
		order := result.Orders[0]
		if order.Carried || order.Reason != game.FailureTerrain {
			t.Fatalf("order result = %#v, want %q", order, game.FailureTerrain)
		}
		// The step is paid for as what it was when it was ordered, and it left
		// the entity standing where it started.
		if order.Cost != game.UnknownStepCost || order.From != stand || order.To != stand {
			t.Fatalf("order result = %#v, want %d AP and no movement", order, game.UnknownStepCost)
		}
		if order.Target != wall {
			t.Fatalf("the failed step aimed at %v, want %v", order.Target, wall)
		}
		// The step was charged in full and moved nothing, and the trailing
		// Rest the pre-processor sized against it spent the remainder.
		if result.Spent != game.LeaderAllowance || result.End != stand {
			t.Fatalf("ledger = %#v, want the step charged and the entity where it was", result)
		}
		// The exploration happened and the entity did not, so the wall is
		// observed and nothing around it was revealed.
		observed := observationsOf(result, seq, game.KnowledgeObserved)
		if len(observed) != 1 || observed[0] != wall {
			t.Fatalf("observed %v, want just the wall %v", observed, wall)
		}
		if len(observationsOf(result, seq, game.KnowledgeExplored)) != 0 {
			t.Fatalf("a step that failed explored %v", result.Observations)
		}
	})
}

// An order the entity could not afford is recorded rather than dropped. It
// carries the reason, it charges nothing, and every order after it carries the
// same reason.
func TestProcessingRecordsWhatTheEntityCouldNotAfford(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		cyl := testCylinder(t)
		world, err := store.World(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// Three passable hexes in a line: one cheap step onto the ring, one
		// exploration beyond it, and a third the six points cannot reach.
		direction := compass.Point(0)
		for _, point := range compass.Points() {
			walked, clear := leader.Location, true
			for range 3 {
				walked = compass.Neighbor(cyl, walked, point)
				clear = clear && world.IsPassable(walked)
			}
			if clear {
				direction = point
				break
			}
		}
		if !direction.IsValid() {
			t.Skip("the generated world has no three passable hexes in a line from the origin")
		}
		for range 3 {
			addMove(t, store, leader.ID, direction)
		}

		closed := game.FirstTurn
		advanceTurn(t, store)

		result := resultFor(t, store, leader.ID, closed)
		if len(result.Orders) != 3 {
			t.Fatalf("order results = %#v, want one per order", result.Orders)
		}
		exhausted := result.Orders[2]
		if exhausted.Carried || exhausted.Reason != game.FailureExhaust || exhausted.Cost != 0 {
			t.Fatalf("third order = %#v, want %q charging nothing", exhausted, game.FailureExhaust)
		}
		// An exhausted order moves nothing, so it starts and ends where the
		// entity stopped.
		stopped := compass.Steps(cyl, leader.Location, direction, 2)
		if exhausted.From != stopped || exhausted.To != stopped {
			t.Fatalf("third order = %#v, want it resolved from %v", exhausted, stopped)
		}
		if len(observationsOf(result, exhausted.Seq, game.KnowledgeObserved)) != 0 {
			t.Fatalf("an order that never happened revealed something: %#v", result.Observations)
		}
		if result.Spent != game.KnownStepCost+game.UnknownStepCost || result.End != stopped {
			t.Fatalf("ledger = %#v, want the two steps it could afford", result)
		}
	})
}

// The result record and the knowledge record are written from one list, so what
// a report says an entity saw is what the faction ends the turn knowing.
func TestRecordedObservationsAgreeWithWhatTheFactionKnows(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		direction := walkableFrom(t, store, leader.Location)
		addMove(t, store, leader.ID, direction)

		closed := game.FirstTurn
		advanceTurn(t, store)

		known := knowledgeAsOf(t, store, orderPlayer, closed+1)
		for _, observation := range resultFor(t, store, leader.ID, closed).Observations {
			if !known.State(observation.Hex).AtLeast(observation.State) {
				t.Fatalf("the turn recorded %v as %q, and the faction knows it as %q",
					observation.Hex, observation.State, known.State(observation.Hex))
			}
		}
	})
}

// Nothing carries out a deactivated faction's orders, so nothing records a turn
// for its entities either.
func TestProcessingRecordsNothingForAnInactiveFaction(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		addMove(t, store, leader.ID, walkableFrom(t, store, leader.Location))

		deactivate(t, store, "factions", orderPlayer)
		closed := game.FirstTurn
		advanceTurn(t, store)

		if results := resultsAsOf(t, store, orderPlayer, closed); len(results) != 0 {
			t.Fatalf("an inactive faction recorded %#v", results)
		}
	})
}

// The record of what happened is the thing a replay is checked against. Two
// databases with the same seeds, given the same orders, record the same rows -
// every ledger, every order outcome and every observation.
func TestRecordedResultsAreAReplay(t *testing.T) {
	walk := func(t *testing.T, store *Store) []string {
		leader, _ := foundedFaction(t, store)
		direction := walkableFrom(t, store, leader.Location)
		addMove(t, store, leader.ID, direction)
		advanceTurn(t, store)
		addMove(t, store, leader.ID, direction.Opposite())
		advanceTurn(t, store)
		return resultRows(t, store)
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

	firstRows, secondRows := walk(t, first), walk(t, second)
	if len(firstRows) == 0 {
		t.Fatal("a processed turn recorded nothing")
	}
	if !slices.Equal(firstRows, secondRows) {
		t.Fatalf("the same orders recorded\n%v\nand\n%v", firstRows, secondRows)
	}
}

// Results belong to the turn they were recorded on. A later turn does not
// answer with an earlier turn's rows, and a turn the game cannot be on is
// refused rather than answered with nothing.
func TestResultsAreReadForOneTurn(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		leader, _ := foundedFaction(t, store)
		addMove(t, store, leader.ID, walkableFrom(t, store, leader.Location))
		advanceTurn(t, store)

		if results := resultsAsOf(t, store, orderPlayer, game.FirstTurn+1); len(results) != 0 {
			t.Fatalf("the turn that opened has already recorded %#v", results)
		}
		if _, err := store.ResultsAsOf(t.Context(), orderPlayer, 0); err == nil {
			t.Fatal("ResultsAsOf(turn 0) = nil error, want an error")
		}
		if _, err := store.ResultsAsOf(t.Context(), "admin@marajanda.com", game.FirstTurn); err == nil {
			t.Fatal("ResultsAsOf(admin) = nil error, want ErrUnknownFaction")
		}
	})
}

// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"context"
	"fmt"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// The result record: what turn processing decided, kept apart from what the
// player asked for.
//
// Orders are intent and results are consequence, and the two are separate
// tables sharing the key (turn, entity_id, seq). That is what keeps the freeze
// #29 rests on honest - nothing ever writes back onto an order row - and it
// buys the check #33 was written for: a replay derives results from the stored
// orders and the seeds, and compares them against the rows already recorded.
// See docs/reference/turn-results.md.

// TurnResult is one entity's turn as it was recorded.
//
// The three parts are three grains. The ledger is of the entity's turn, an
// order outcome is of one order, and an observation is of one hex an order
// revealed. Nothing collapses them: a step that failed on terrain charged its
// points, carried nothing and still revealed a hex, which is a row on each.
type TurnResult struct {
	// Turn is the turn that was processed, not the turn its facts are
	// effective from.
	Turn int
	// EntityID is whose turn it was.
	EntityID int64
	// Allowance, Spent and Lapsed are the action point ledger: what the entity
	// had, what its orders were charged, and what nothing reached.
	Allowance, Spent, Lapsed int
	// Start and End are where the entity stood when the turn opened and where
	// it stopped.
	Start, End hexg.Hex
	// Orders is one outcome per order the entity carried, in sequence order.
	// It is empty for an entity that was given nothing to do.
	Orders []OrderResult
	// Observations are the hexes the turn revealed, in the order they were
	// revealed.
	Observations []game.Observation
}

// OrderResult is what one order did.
//
// Cost is what the entity was charged rather than what the order would have
// cost: an order it could not afford charges nothing, and a step that failed on
// terrain is charged in full. Reason is empty when the order was carried out.
type OrderResult struct {
	Seq     int
	Kind    game.OrderKind
	Cost    int
	Carried bool
	Reason  game.FailureReason
	// From is where the entity stood when the order was resolved, which is not
	// where it stood when the turn opened: a step that failed does not move
	// it, so the order after it resolves from the hex it did not leave.
	From hexg.Hex
	// Target is where a step was aimed, which a failed step has to record or
	// nothing can say what it walked into. It is From for an order that aims
	// nowhere.
	Target hexg.Hex
	// To is where the order left the entity. It is From for everything that
	// did not move it, a failed step included.
	To hexg.Hex
}

// ResultsAsOf returns what a faction's entities did on a turn, by entity in
// creation order.
//
// The turn is the caller's. A turn-3 report reads turn 3 however far the game
// has moved on, and it reads the rows processing wrote rather than deriving
// them again: the derivation is the replay, and a record that is only ever
// derived is nothing to compare a replay against.
//
// An entity that was given nothing to do has a result. Its allowance lapsed,
// which is a line a player asks about, so the absence of orders is recorded
// rather than left to be inferred from the absence of a row.
func (s *Store) ResultsAsOf(ctx context.Context, email string, turn int) ([]TurnResult, error) {
	if !game.ValidTurn(turn) {
		return nil, fmt.Errorf("read results: %d is not a turn the game can be on", turn)
	}

	conn, release, err := s.take(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return readResults(conn, normalizeEmail(email), turn)
}

// writeResult records one entity's turn.
//
// It runs inside the caller's transaction, with the knowledge and location
// writes the same outcome produces. A result the world does not agree with is
// worse than no result at all, so the three are written together or not at all.
func writeResult(conn *sqlite.Conn, turn int, entity Entity, outcome game.Outcome) error {
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO turn_results (turn, entity_id, allowance, spent, lapsed, start_q, start_r, end_q, end_r)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, &sqlitex.ExecOptions{
		Args: []any{
			turn, entity.ID, outcome.Allowance, outcome.Spent, outcome.Lapsed,
			outcome.Start.Q(), outcome.Start.R(), outcome.End.Q(), outcome.End.R(),
		},
	}); err != nil {
		return fmt.Errorf("record turn result: %w", err)
	}
	for _, order := range outcome.Orders {
		// A carried order has no reason, and the column says so with a NULL
		// rather than with an empty string: the check on the table holds the
		// two in step.
		var reason any
		if order.Reason != "" {
			reason = string(order.Reason)
		}
		if err := sqlitex.ExecuteTransient(conn, `
			INSERT INTO turn_result_orders
				(turn, entity_id, seq, kind, cost, carried, reason,
				 from_q, from_r, target_q, target_r, to_q, to_r)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13);`, &sqlitex.ExecOptions{
			Args: []any{
				turn, entity.ID, order.Seq, string(order.Kind), order.Cost, boolToInt(order.Carried), reason,
				order.From.Q(), order.From.R(), order.Target.Q(), order.Target.R(), order.To.Q(), order.To.R(),
			},
		}); err != nil {
			return fmt.Errorf("record order result: %w", err)
		}
	}
	for _, observation := range outcome.Observations {
		// The insert selects its coordinates from hexes, so a ring that runs
		// off a pole records fewer than seven rows rather than naming a row
		// the world does not have. It is the knowledge write's clip, and for
		// the same reason.
		//
		// A hex may be revealed twice in a turn - a step observes what a later
		// step explores - but never twice by one order, so the key holds and
		// nothing here has to reconcile the two. What a faction ends the turn
		// knowing is faction_knowledge's answer.
		if err := sqlitex.ExecuteTransient(conn, `
			INSERT INTO turn_result_observations (turn, entity_id, seq, q, r, state)
			SELECT ?1, ?2, ?3, q, r, ?6 FROM hexes WHERE q = ?4 AND r = ?5;`, &sqlitex.ExecOptions{
			Args: []any{
				turn, entity.ID, observation.Seq,
				observation.Hex.Q(), observation.Hex.R(), string(observation.State),
			},
		}); err != nil {
			return fmt.Errorf("record observation: %w", err)
		}
	}
	return nil
}

// readResults reads a faction's results for one turn.
//
// Three reads rather than one join: the grains are different, and a join across
// them would multiply an entity's orders by its observations and leave the
// caller to undo it.
func readResults(conn *sqlite.Conn, normalizedEmail string, turn int) ([]TurnResult, error) {
	found, err := factionExists(conn, normalizedEmail)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUnknownFaction
	}

	results := make([]TurnResult, 0)
	at := make(map[int64]int)
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT turn_results.entity_id, allowance, spent, lapsed, start_q, start_r, end_q, end_r
		FROM turn_results
		JOIN entities ON entities.id = turn_results.entity_id
		WHERE entities.faction_email = ?1 AND turn_results.turn = ?2
		ORDER BY turn_results.entity_id;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			at[stmt.ColumnInt64(0)] = len(results)
			results = append(results, TurnResult{
				Turn:      turn,
				EntityID:  stmt.ColumnInt64(0),
				Allowance: stmt.ColumnInt(1),
				Spent:     stmt.ColumnInt(2),
				Lapsed:    stmt.ColumnInt(3),
				Start:     hexg.NewHex(stmt.ColumnInt(4), stmt.ColumnInt(5)),
				End:       hexg.NewHex(stmt.ColumnInt(6), stmt.ColumnInt(7)),
			})
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("read turn results: %w", err)
	}

	if err := sqlitex.ExecuteTransient(conn, `
		SELECT turn_result_orders.entity_id, seq, kind, cost, carried, reason,
		       from_q, from_r, target_q, target_r, to_q, to_r
		FROM turn_result_orders
		JOIN entities ON entities.id = turn_result_orders.entity_id
		WHERE entities.faction_email = ?1 AND turn_result_orders.turn = ?2
		ORDER BY turn_result_orders.entity_id, seq;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			index, known := at[stmt.ColumnInt64(0)]
			if !known {
				return fmt.Errorf("order result for entity %d has no turn result", stmt.ColumnInt64(0))
			}
			results[index].Orders = append(results[index].Orders, OrderResult{
				Seq:     stmt.ColumnInt(1),
				Kind:    game.OrderKind(stmt.ColumnText(2)),
				Cost:    stmt.ColumnInt(3),
				Carried: stmt.ColumnInt(4) == 1,
				Reason:  game.FailureReason(stmt.ColumnText(5)),
				From:    hexg.NewHex(stmt.ColumnInt(6), stmt.ColumnInt(7)),
				Target:  hexg.NewHex(stmt.ColumnInt(8), stmt.ColumnInt(9)),
				To:      hexg.NewHex(stmt.ColumnInt(10), stmt.ColumnInt(11)),
			})
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("read order results: %w", err)
	}

	if err := sqlitex.ExecuteTransient(conn, `
		SELECT turn_result_observations.entity_id, seq, q, r, state
		FROM turn_result_observations
		JOIN entities ON entities.id = turn_result_observations.entity_id
		WHERE entities.faction_email = ?1 AND turn_result_observations.turn = ?2
		ORDER BY turn_result_observations.entity_id, seq, q, r;`, &sqlitex.ExecOptions{
		Args: []any{normalizedEmail, turn},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			index, known := at[stmt.ColumnInt64(0)]
			if !known {
				return fmt.Errorf("observation for entity %d has no turn result", stmt.ColumnInt64(0))
			}
			results[index].Observations = append(results[index].Observations, game.Observation{
				Seq:   stmt.ColumnInt(1),
				Hex:   hexg.NewHex(stmt.ColumnInt(2), stmt.ColumnInt(3)),
				State: game.Knowledge(stmt.ColumnText(4)),
			})
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("read observations: %w", err)
	}
	return results, nil
}

// boolToInt is the STRICT tables' boolean. They have none, so a flag is an
// integer with a check, exactly as is_active is.
func boolToInt(flag bool) int {
	if flag {
		return 1
	}
	return 0
}

// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/cylinder"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// A faction opens its first turn knowing seven hexes: the origin it stands on,
// and the six around it. Those facts are effective from the founding turn
// rather than from the turn after it, because nothing about founding is
// waiting on a turn to be processed.
func TestFoundingKnowsTheHomelandRing(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		known := knowledgeNow(t, store, orderPlayer)
		if len(known) != 7 {
			t.Fatalf("founding knowledge = %d hexes, want 7", len(known))
		}
		if got := known.State(seated.Origin); got != game.KnowledgeExplored {
			t.Fatalf("origin state = %q, want %q", got, game.KnowledgeExplored)
		}
		for _, neighbour := range compass.Neighbors(testCylinder(t), seated.Origin) {
			if got := known.State(neighbour); got != game.KnowledgeObserved {
				t.Fatalf("neighbour %v state = %q, want %q", neighbour, got, game.KnowledgeObserved)
			}
		}

		for _, from := range knowledgeStarts(t, store, orderPlayer) {
			if from != game.FirstTurn {
				t.Fatalf("founding knowledge effective from %d, want %d", from, game.FirstTurn)
			}
		}
	})
}

// Every neighbour a founding writes is a canonical coordinate of the world.
// The ring around an origin near the meridian wraps rather than naming a column
// the world does not have.
func TestKnowledgeHoldsCanonicalCoordinates(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
			t.Fatal(err)
		}

		cyl := testCylinder(t)
		for hex := range knowledgeNow(t, store, orderPlayer) {
			if !cyl.IsCanonical(hex) {
				t.Fatalf("known hex %v is not canonical", hex)
			}
		}
	})
}

// Entering a hex explores it and observes the six around it, all effective from
// the turn after the one being processed. Every reveal is a consequence of a
// turn, and everything a turn produces is dated from the turn after it.
func TestEnteringAHexExploresItFromTheTurnAfter(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		// The hex due east of the origin is observed at founding, so entering
		// it is the upgrade this test is about.
		entered := compass.Neighbor(testCylinder(t), seated.Origin, compass.E)
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, entered); err != nil {
			t.Fatal(err)
		}

		// Turn 1 still says what it said. A report of the turn that was
		// processed does not change underneath itself.
		if got := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn).State(entered); got != game.KnowledgeObserved {
			t.Fatalf("state on turn %d = %q, want %q", game.FirstTurn, got, game.KnowledgeObserved)
		}

		next := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1)
		if got := next.State(entered); got != game.KnowledgeExplored {
			t.Fatalf("state on turn %d = %q, want %q", game.FirstTurn+1, got, game.KnowledgeExplored)
		}
		for _, neighbour := range compass.Neighbors(testCylinder(t), entered) {
			if !next.Knows(neighbour) {
				t.Fatalf("neighbour %v of the entered hex is unknown", neighbour)
			}
		}
	})
}

// Two entities entering the same hex in one turn leave what one entity would
// have left. The second write finds the state it was going to write and stops,
// so nothing is duplicated and no period is split.
func TestEnteringOneHexTwiceInATurnIsIdempotent(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		entered := compass.Neighbor(testCylinder(t), seated.Origin, compass.E)
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, entered); err != nil {
			t.Fatal(err)
		}
		once := knowledgeRows(t, store, orderPlayer)

		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, entered); err != nil {
			t.Fatal(err)
		}
		if twice := knowledgeRows(t, store, orderPlayer); !slices.Equal(once, twice) {
			t.Fatalf("a second entry changed the record:\n once = %v\ntwice = %v", once, twice)
		}
	})
}

// A hex explored during a turn is not written back down to observed by another
// entity that only saw it from next door. Knowledge is monotone, and the
// comparison is against the state as it stands on the turn being written, so
// the writes of one turn see each other.
func TestASightingDoesNotUnexploreAHex(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		cyl := testCylinder(t)
		entered := compass.Neighbor(cyl, seated.Origin, compass.E)

		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, entered); err != nil {
			t.Fatal(err)
		}
		// Standing in the origin observes the entered hex, which is already
		// explored on the turn being written.
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, seated.Origin); err != nil {
			t.Fatal(err)
		}

		if got := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1).State(entered); got != game.KnowledgeExplored {
			t.Fatalf("state after a sighting = %q, want %q", got, game.KnowledgeExplored)
		}
	})
}

// The record does not depend on the order a turn's entities are walked in. No
// entity's march changes what another entity's march writes, so a replay that
// walks a turn's entities in a different order reaches the same rows.
func TestKnowledgeDoesNotDependOnTheOrderEntitiesAreWalked(t *testing.T) {
	// Two stores of the same world, seated from the same seeds, so the only
	// difference between them is the order their entries are made in.
	walk := func(t *testing.T, name string, reverse bool) []string {
		t.Helper()
		store, err := OpenSharedMemory(t.Context(), name, testGame)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		cyl := testCylinder(t)
		east := compass.Neighbor(cyl, seated.Origin, compass.E)
		steps := []hexg.Hex{east, seated.Origin, compass.Neighbor(cyl, east, compass.E)}
		if reverse {
			slices.Reverse(steps)
		}
		for _, hex := range steps {
			if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, hex); err != nil {
				t.Fatal(err)
			}
		}
		return knowledgeRows(t, store, orderPlayer)
	}

	forwards := walk(t, t.Name()+"-forwards", false)
	backwards := walk(t, t.Name()+"-backwards", true)

	if !slices.Equal(forwards, backwards) {
		t.Fatalf("walk order changed the record:\n forwards = %v\nbackwards = %v", forwards, backwards)
	}
}

// A period that opened on the turn being written is replaced rather than
// closed. It never described a turn boundary, so closing it would leave an
// empty period and a row nobody meant to keep.
func TestAnUpgradeWithinATurnLeavesOnePeriod(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		cyl := testCylinder(t)
		// Two steps out, so the far hex is unknown at founding: entering the
		// near hex observes it, and entering it explores it, both in one turn.
		near := compass.Neighbor(cyl, seated.Origin, compass.E)
		far := compass.Neighbor(cyl, near, compass.E)

		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, near); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, far); err != nil {
			t.Fatal(err)
		}

		if got := periodsFor(t, store, orderPlayer, far); got != 1 {
			t.Fatalf("periods for a hex learnt twice in one turn = %d, want 1", got)
		}
		if got := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1).State(far); got != game.KnowledgeExplored {
			t.Fatalf("state = %q, want %q", got, game.KnowledgeExplored)
		}
		// Turn 1 never knew it at all, so the replaced row left nothing behind.
		if knowledgeAsOf(t, store, orderPlayer, game.FirstTurn).Knows(far) {
			t.Fatal("a hex first learnt on turn 1 is known as of turn 1")
		}
	})
}

// Knowledge reads as of a turn, like every other fact. A hex learnt later is
// unknown earlier, which is what lets a report of an old turn be a report of
// that turn.
func TestKnowledgeReadsAsOfATurn(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		cyl := testCylinder(t)
		near := compass.Neighbor(cyl, seated.Origin, compass.E)
		far := compass.Neighbor(cyl, near, compass.E)

		// Walk out one hex a turn, advancing the clock between the two.
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, near); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AdvanceTurn(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn+1, far); err != nil {
			t.Fatal(err)
		}

		for _, test := range []struct {
			turn int
			want game.Knowledge
		}{
			{game.FirstTurn, ""},
			{game.FirstTurn + 1, game.KnowledgeObserved},
			{game.FirstTurn + 2, game.KnowledgeExplored},
		} {
			if got := knowledgeAsOf(t, store, orderPlayer, test.turn).State(far); got != test.want {
				t.Fatalf("state of %v on turn %d = %q, want %q", far, test.turn, got, test.want)
			}
		}
	})
}

// A neighbour beyond a pole is not a hex of the world, so nothing records it.
// The world filters its own neighbours: the write selects the coordinates it
// inserts from hexes, so a ring that runs off the top writes fewer than six.
func TestKnowledgeStopsAtThePole(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
			t.Fatal(err)
		}

		// The northernmost row is ice, and the row above it is not the world.
		pole := hexg.NewHex(0, -testGame.Height)
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, pole); err != nil {
			t.Fatal(err)
		}

		known := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1)
		if !known.Knows(pole) {
			t.Fatal("the entered hex was not recorded")
		}

		// Two of the six lie on the row above the pole, which is not the
		// world. The other four are hexes and are observed.
		dropped := 0
		for _, neighbour := range compass.Neighbors(testCylinder(t), pole) {
			if neighbour.R() < -testGame.Height {
				dropped++
				if known.Knows(neighbour) {
					t.Fatalf("hex %v lies outside the world and was recorded", neighbour)
				}
				continue
			}
			if !known.Knows(neighbour) {
				t.Fatalf("neighbour %v of the entered hex is unknown", neighbour)
			}
		}
		if dropped != 2 {
			t.Fatalf("neighbours beyond the pole = %d, want 2; this test is not exercising the clip", dropped)
		}
	})
}

// The ring around a hex on the eastern edge wraps to the western one. The world
// has no eastern or western edge, so a neighbour is named by the canonical
// coordinate every other account means rather than by a column off the end.
func TestKnowledgeWrapsAtTheMeridian(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
			t.Fatal(err)
		}

		// The easternmost column of the middle row, whose eastern neighbour is
		// the westernmost column.
		edge := hexg.NewHex(testGame.Width, 0)
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, edge); err != nil {
			t.Fatal(err)
		}

		known := knowledgeAsOf(t, store, orderPlayer, game.FirstTurn+1)
		wrapped := hexg.NewHex(-testGame.Width, 0)
		if !known.Knows(wrapped) {
			t.Fatalf("the eastern neighbour of %v was not recorded as %v", edge, wrapped)
		}
		if len(known) != 7+7 {
			t.Fatalf("known hexes = %d, want the founding ring and the seam ring", len(known))
		}
	})
}

// One hex has one open period per faction, the same rule every fact table
// carries and the same partial unique index that holds it.
func TestOnlyOnePeriodPerKnownHexRunsToTheEndOfTime(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		seated, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman)
		if err != nil {
			t.Fatal(err)
		}

		conn, release, err := store.take(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer release()

		err = sqlitex.ExecuteTransient(conn, `
			INSERT INTO faction_knowledge (faction_email, q, r, state, effective_from, effective_through)
			VALUES (?1, ?2, ?3, 'explored', ?4, ?5);`, &sqlitex.ExecOptions{
			Args: []any{orderPlayer, seated.Origin.Q(), seated.Origin.R(), game.FirstTurn + 1, game.EndOfTimeTurn},
		})
		if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintUnique {
			t.Fatalf("second open period = %v, want %v", got, sqlite.ResultConstraintUnique)
		}
	})
}

// Knowledge belongs to a faction. An unconfigured player controls none, and
// asking for it says so rather than answering with an empty world.
func TestKnowledgeRequiresAFaction(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.KnowledgeAsOf(t.Context(), orderPlayer, game.FirstTurn); !errors.Is(err, ErrUnknownFaction) {
			t.Fatalf("KnowledgeAsOf(unconfigured player) = %v, want %v", err, ErrUnknownFaction)
		}
		if err := store.MarkEntered(t.Context(), orderPlayer, game.FirstTurn, hexg.NewHex(0, 0)); !errors.Is(err, ErrUnknownFaction) {
			t.Fatalf("MarkEntered(unconfigured player) = %v, want %v", err, ErrUnknownFaction)
		}
	})
}

// Neither sentinel is a turn the game can be on, so neither reads nor writes
// against one.
func TestKnowledgeRejectsATurnTheGameCannotBeOn(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.SaveFaction(t.Context(), orderPlayer, "The Wayfarers", game.RaceHuman); err != nil {
			t.Fatal(err)
		}
		for _, turn := range []int{game.StartOfTimeTurn, game.EndOfTimeTurn, -1} {
			if _, err := store.KnowledgeAsOf(t.Context(), orderPlayer, turn); err == nil {
				t.Fatalf("KnowledgeAsOf(turn %d) = nil error, want an error", turn)
			}
			if err := store.MarkEntered(t.Context(), orderPlayer, turn, hexg.NewHex(0, 0)); err == nil {
				t.Fatalf("MarkEntered(turn %d) = nil error, want an error", turn)
			}
		}
	})
}

// knowledgeNow reads a faction's knowledge as of the turn the game is on.
func knowledgeNow(t *testing.T, store *Store, email string) game.KnowledgeSet {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return knowledgeAsOf(t, store, email, turn)
}

func knowledgeAsOf(t *testing.T, store *Store, email string, turn int) game.KnowledgeSet {
	t.Helper()
	known, err := store.KnowledgeAsOf(t.Context(), email, turn)
	if err != nil {
		t.Fatal(err)
	}
	return known
}

// knowledgeRows renders every stored row, periods and all, in a stable order.
// Comparing two of these compares the whole record rather than a summary of it.
func knowledgeRows(t *testing.T, store *Store, email string) []string {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var rows []string
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT q, r, state, effective_from, effective_through FROM faction_knowledge
		WHERE faction_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{email},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rows = append(rows, fmt.Sprintf("(%d,%d) %s [%d,%d)",
				stmt.ColumnInt(0), stmt.ColumnInt(1), stmt.ColumnText(2),
				stmt.ColumnInt(3), stmt.ColumnInt(4)))
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(rows)
	return rows
}

// knowledgeStarts reads the turn every one of a faction's knowledge rows opens on.
func knowledgeStarts(t *testing.T, store *Store, email string) []int {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var starts []int
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT effective_from FROM faction_knowledge WHERE faction_email = ?1;`, &sqlitex.ExecOptions{
		Args: []any{email},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			starts = append(starts, stmt.ColumnInt(0))
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return starts
}

// periodsFor counts how many rows one faction holds for one hex.
func periodsFor(t *testing.T, store *Store, email string, coord hexg.Hex) int64 {
	t.Helper()
	return countQuery(t, store,
		`SELECT count(*) FROM faction_knowledge WHERE faction_email = ?1 AND q = ?2 AND r = ?3;`,
		email, coord.Q(), coord.R())
}

func testCylinder(t *testing.T) cylinder.Cylinder {
	t.Helper()
	cyl, err := cylinder.New(2*testGame.Width + 1)
	if err != nil {
		t.Fatalf("cylinder.New: %v", err)
	}
	return cyl
}

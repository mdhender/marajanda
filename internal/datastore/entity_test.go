// Copyright (c) 2026 Michael D Henderson.

package datastore

import (
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// eachMemoryMode runs a test against a private in-memory database and against a
// named shared one. The temporal tables are read and written on whatever
// connection the pool hands out, so both modes are exercised wherever the
// answer could depend on which connection asked.
func eachMemoryMode(t *testing.T, test func(t *testing.T, store *Store)) {
	t.Helper()
	for name, open := range map[string]func(*testing.T) (*Store, error){
		"memory": func(t *testing.T) (*Store, error) {
			return OpenMemory(t.Context(), testGame)
		},
		"shared memory": func(t *testing.T) (*Store, error) {
			return OpenSharedMemory(t.Context(), t.Name(), testGame)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, err := open(t)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			test(t, store)
		})
	}
}

// A new database sits on the first turn, which is what the founding facts of
// every faction configured in it are effective from.
func TestCurrentTurnStartsAtTheFirstTurn(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		turn, err := store.CurrentTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if turn != game.FirstTurn {
			t.Fatalf("CurrentTurn = %d, want %d", turn, game.FirstTurn)
		}
	})
}

// The clock only ever increases and never reaches the end of time, so the
// column refuses anything below the first turn and anything at the sentinel a
// period that has not ended runs to.
func TestCurrentTurnRefusesATurnOutsideTheClock(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		conn, release, err := store.take(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer release()

		for _, turn := range []int{game.StartOfTimeTurn, -1, game.EndOfTimeTurn, game.EndOfTimeTurn + 1} {
			err := sqlitex.ExecuteTransient(conn, `UPDATE game SET current_turn = ?1 WHERE id = 1;`, &sqlitex.ExecOptions{
				Args: []any{turn},
			})
			if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintCheck {
				t.Fatalf("current_turn = %d: error = %v (%v), want %v", turn, err, got, sqlite.ResultConstraintCheck)
			}
		}
		// A turn the game can actually be on is accepted.
		if err := sqlitex.ExecuteTransient(conn, `UPDATE game SET current_turn = 2 WHERE id = 1;`, nil); err != nil {
			t.Fatalf("advance to turn 2: %v", err)
		}
	})
}

// A faction is founded once. Reconfiguring it renames its people; it does not
// raise a second leader, and it does not move the codes the first one holds.
func TestFoundingAFactionHappensOnce(t *testing.T) {
	eachMemoryMode(t, func(t *testing.T, store *Store) {
		if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman); err != nil {
			t.Fatal(err)
		}
		first := entitiesNow(t, store, "player@marajanda.com")
		if len(first) != 2 {
			t.Fatalf("founding entities = %#v, want two", first)
		}
		if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wanderers", game.RaceDwarf); err != nil {
			t.Fatal(err)
		}
		second := entitiesNow(t, store, "player@marajanda.com")
		if len(second) != len(first) {
			t.Fatalf("entities after reconfiguring = %#v, want the founding %#v", second, first)
		}
		for index, want := range first {
			if second[index] != want {
				t.Fatalf("entity %d = %#v, want %#v", index, second[index], want)
			}
		}
	})
}

// Founding entities stand on the account's origin from the turn the faction was
// configured. Nothing about them is waiting on a turn to be processed, so they
// are not effective from the turn after it.
func TestFoundingEntitiesStandOnTheOriginFromTheCurrentTurn(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seated, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range entitiesNow(t, store, "player@marajanda.com") {
		if !entity.Location.Equals(seated.Origin) {
			t.Fatalf("%s stands at %v, want the origin %v", entity.Code, entity.Location, seated.Origin)
		}
	}
	if turns := createdTurns(t, store); len(turns) != 2 || turns[0] != game.FirstTurn || turns[1] != game.FirstTurn {
		t.Fatalf("created turns = %v, want both on turn %d", turns, game.FirstTurn)
	}
	// The founding facts are open: each runs to the end of time.
	if open := openPeriodCount(t, store, "entity_facts"); open != 2 {
		t.Fatalf("open fact periods = %d, want 2", open)
	}
	if open := openPeriodCount(t, store, "entity_locations"); open != 2 {
		t.Fatalf("open location periods = %d, want 2", open)
	}
}

// Codes are a per-faction sequence for a kind. Two factions each hold a
// LEADER-1, and a faction's second leader is LEADER-2.
func TestEntityCodesAreASequenceWithinAFaction(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAccount(t.Context(), SeedAccount{
		Email: "rival@example.com", Secret: "good.luck", Handle: "rival", Role: "player",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFaction(t.Context(), "rival@example.com", "The Rivals", game.RaceOrc); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"player@marajanda.com", "rival@example.com"} {
		if codes := entityCodes(t, store, email); len(codes) != 2 || codes[0] != "LEADER-1" || codes[1] != "HAMLET-1" {
			t.Fatalf("%s codes = %v, want LEADER-1 and HAMLET-1", email, codes)
		}
	}

	// A second leader for the first faction takes the next number in that
	// faction's sequence, whatever the rival holds.
	second := createEntityForTest(t, store, "player@marajanda.com", game.EntityKindLeader, first.Origin, game.FirstTurn)
	if second.Code != "LEADER-2" || second.Name != second.Code {
		t.Fatalf("second leader = %#v, want LEADER-2 named after its code", second)
	}
	if codes := entityCodes(t, store, "rival@example.com"); len(codes) != 2 {
		t.Fatalf("rival codes = %v, want its own two", codes)
	}
}

// A code is frozen at creation and a kind is not, so a change of kind leaves
// the code alone - and leaves the number it holds spent.
func TestAKindChangeLeavesTheCodeAlone(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seated, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	hamlet := entitiesNow(t, store, "player@marajanda.com")[1]

	// The kinds this schema knows are leader and hamlet, so this is the change
	// available to make. A hamlet growing into a village is the same two
	// statements once village is a kind.
	closeAndReopenFact(t, store, hamlet.ID, hamlet.Code, "Smirnopolis", game.EntityKindLeader, 4)

	before, err := store.EntitiesAsOf(t.Context(), "player@marajanda.com", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := before[1]; got.Kind != game.EntityKindHamlet || got.Name != "HAMLET-1" {
		t.Fatalf("entity on turn 3 = %#v, want the unchanged hamlet", got)
	}
	after, err := store.EntitiesAsOf(t.Context(), "player@marajanda.com", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := after[1]; got.Kind != game.EntityKindLeader || got.Name != "Smirnopolis" || got.Code != "HAMLET-1" {
		t.Fatalf("entity on turn 4 = %#v, want the changed fact under the frozen code HAMLET-1", got)
	}
	if got, want := after[1].ID, hamlet.ID; got != want {
		t.Fatalf("entity id = %d, want the same entity %d", got, want)
	}

	// The number is spent: the faction's next hamlet is HAMLET-2 even though
	// nothing is a hamlet any more.
	next := createEntityForTest(t, store, "player@marajanda.com", game.EntityKindHamlet, seated.Origin, 4)
	if next.Code != "HAMLET-2" {
		t.Fatalf("next hamlet code = %q, want HAMLET-2", next.Code)
	}
}

// Location is its own fact table because it is what changes when a leader
// moves, and a move is read back the same way every other fact is.
func TestEntityLocationReadsAsOfATurn(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seated, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	leader := entitiesNow(t, store, "player@marajanda.com")[0]
	// Somewhere else in the world for the leader to stand. Which hex it is does
	// not matter here; that it is a hex of the world does, because a location
	// references one.
	moved := otherHex(t, store, seated.Origin)

	// An order issued during turn 3 is processed at the end of it, so the move
	// it writes is effective from turn 4.
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE entity_locations SET effective_through = 4
		WHERE entity_id = ?1 AND effective_through = ?2;`, &sqlitex.ExecOptions{
		Args: []any{leader.ID, game.EndOfTimeTurn},
	}); err != nil {
		release()
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_locations (entity_id, q, r, effective_from, effective_through)
		VALUES (?1, ?2, ?3, 4, ?4);`, &sqlitex.ExecOptions{
		Args: []any{leader.ID, moved.Q(), moved.R(), game.EndOfTimeTurn},
	}); err != nil {
		release()
		t.Fatal(err)
	}
	release()

	for _, test := range []struct {
		turn int
		want hexg.Hex
	}{
		{turn: game.FirstTurn, want: seated.Origin},
		{turn: 3, want: seated.Origin},
		{turn: 4, want: moved},
		{turn: 5, want: moved},
	} {
		entities, err := store.EntitiesAsOf(t.Context(), "player@marajanda.com", test.turn)
		if err != nil {
			t.Fatal(err)
		}
		if got := entities[0].Location; !got.Equals(test.want) {
			t.Fatalf("location on turn %d = %v, want %v", test.turn, got, test.want)
		}
	}
}

// EntitiesAsOf answers for a turn, so it refuses a number that is not one.
// Neither sentinel is a turn the game can be on.
func TestEntitiesAsOfRejectsATurnTheGameCannotBeOn(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, turn := range []int{game.StartOfTimeTurn, -1, game.EndOfTimeTurn} {
		if _, err := store.EntitiesAsOf(t.Context(), "player@marajanda.com", turn); err == nil {
			t.Fatalf("EntitiesAsOf(%d) was accepted", turn)
		}
	}
}

// The periods of one entity's fact table never overlap and exactly one of them
// runs to the end of time. The partial unique indexes hold the second half of
// that, and they hold it against the value game.EndOfTimeTurn names: a period
// closed one turn earlier is closed, and the index has room for an open row
// beside it.
func TestOnlyOnePeriodPerEntityRunsToTheEndOfTime(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seated, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman)
	if err != nil {
		t.Fatal(err)
	}
	leader := entitiesNow(t, store, "player@marajanda.com")[0]

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// A second open period for the same entity is refused by each fact table.
	err = sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
		VALUES (?1, 'LEADER-1', 'Second', 'leader', 2, ?2);`, &sqlitex.ExecOptions{
		Args: []any{leader.ID, game.EndOfTimeTurn},
	})
	if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintUnique {
		t.Fatalf("second open fact period: error = %v (%v), want %v", err, got, sqlite.ResultConstraintUnique)
	}
	err = sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_locations (entity_id, q, r, effective_from, effective_through)
		VALUES (?1, ?2, ?3, 2, ?4);`, &sqlitex.ExecOptions{
		Args: []any{leader.ID, seated.Origin.Q(), seated.Origin.R(), game.EndOfTimeTurn},
	})
	if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintUnique {
		t.Fatalf("second open location period: error = %v (%v), want %v", err, got, sqlite.ResultConstraintUnique)
	}

	// A period that ends one turn before the end of time is a closed period.
	// The index does not hold it, so an open row still fits beside it.
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
		VALUES (?1, 'LEADER-1', 'Almost forever', 'leader', 2, ?2);`, &sqlitex.ExecOptions{
		Args: []any{leader.ID, game.EndOfTimeTurn - 1},
	}); err != nil {
		t.Fatalf("closed period ending before the end of time: %v", err)
	}
}

func TestEntityPeriodConstraints(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman); err != nil {
		t.Fatal(err)
	}
	leader := entitiesNow(t, store, "player@marajanda.com")[0]

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for _, test := range []struct {
		name string
		stmt string
		args []any
		want sqlite.ResultCode
	}{
		{
			// A period missing its end is a constraint violation rather than an
			// open period nobody meant to write. That is what the sentinel buys.
			name: "a period must have an end",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from)
				VALUES (?1, 'LEADER-9', 'Endless', 'leader', 2);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintNotNull,
		},
		{
			name: "a period must end after it starts",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
				VALUES (?1, 'LEADER-9', 'Backwards', 'leader', 5, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "a period cannot start before the start of time",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
				VALUES (?1, 'LEADER-9', 'Before', 'leader', -1, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "a kind must be one the game knows",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
				VALUES (?1, 'DRAGON-1', 'Dragon', 'dragon', 2, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "facts belong to an entity that exists",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
				VALUES (9999, 'LEADER-9', 'Nobody', 'leader', 2, 5);`,
			want: sqlite.ResultConstraintForeignKey,
		},
		{
			name: "one fact per entity per starting turn",
			stmt: `INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
				VALUES (?1, 'LEADER-1', 'Twice', 'leader', 1, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintPrimaryKey,
		},
		{
			// An entity cannot stand on a coordinate the world does not hold.
			name: "a location must be a hex of the world",
			stmt: `INSERT INTO entity_locations (entity_id, q, r, effective_from, effective_through)
				VALUES (?1, 1000, 1000, 2, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintForeignKey,
		},
		{
			name: "a unit holds a positive quantity",
			stmt: `INSERT INTO units (entity_id, kind, quantity, effective_from, effective_through)
				VALUES (?1, 'archers', 0, 1, 5);`,
			args: []any{leader.ID},
			want: sqlite.ResultConstraintCheck,
		},
		{
			name: "units belong to an entity that exists",
			stmt: `INSERT INTO units (entity_id, kind, quantity, effective_from, effective_through)
				VALUES (9999, 'archers', 40, 1, 5);`,
			want: sqlite.ResultConstraintForeignKey,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := sqlitex.ExecuteTransient(conn, test.stmt, &sqlitex.ExecOptions{Args: test.args})
			if got := sqlite.ErrCode(err); got != test.want {
				t.Fatalf("constraint error = %v (%v), want %v", err, got, test.want)
			}
		})
	}
}

// A unit is a quantity of a kind held by an entity, with no identity of its
// own: merging two stacks is addition, so one entity holds one open row per
// kind. The kinds themselves carry no constraint - the list of them is a game
// rule that arrives with the first rule that produces one.
func TestUnitsAreOneOpenStackPerKind(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman); err != nil {
		t.Fatal(err)
	}
	entities := entitiesNow(t, store, "player@marajanda.com")

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	insert := `INSERT INTO units (entity_id, kind, quantity, effective_from, effective_through) VALUES (?1, ?2, ?3, ?4, ?5);`
	for _, kind := range []string{"archers", "cattle", "gold"} {
		if err := sqlitex.ExecuteTransient(conn, insert, &sqlitex.ExecOptions{
			Args: []any{entities[0].ID, kind, 40, game.FirstTurn, game.EndOfTimeTurn},
		}); err != nil {
			t.Fatalf("hold %s: %v", kind, err)
		}
	}
	// Another entity holds its own stack of the same kind.
	if err := sqlitex.ExecuteTransient(conn, insert, &sqlitex.ExecOptions{
		Args: []any{entities[1].ID, "archers", 12, game.FirstTurn, game.EndOfTimeTurn},
	}); err != nil {
		t.Fatalf("second entity holding archers: %v", err)
	}
	// The same entity does not hold two open stacks of one kind.
	err = sqlitex.ExecuteTransient(conn, insert, &sqlitex.ExecOptions{
		Args: []any{entities[0].ID, "archers", 12, 2, game.EndOfTimeTurn},
	})
	if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintUnique {
		t.Fatalf("second open stack: error = %v (%v), want %v", err, got, sqlite.ResultConstraintUnique)
	}
	// A closed stack and an open one coexist, which is what a change of
	// quantity from one turn to the next looks like.
	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE units SET effective_through = 4 WHERE entity_id = ?1 AND kind = 'archers' AND effective_through = ?2;`,
		&sqlitex.ExecOptions{Args: []any{entities[0].ID, game.EndOfTimeTurn}}); err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, insert, &sqlitex.ExecOptions{
		Args: []any{entities[0].ID, "archers", 52, 4, game.EndOfTimeTurn},
	}); err != nil {
		t.Fatalf("reopen the stack: %v", err)
	}
}

// An entity belongs to a faction, and a faction belongs to an account. Deleting
// the account takes the whole chain with it rather than leaving facts about
// something nobody owns.
func TestDeletingAnAccountRemovesItsEntitiesAndTheirFacts(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.SaveFaction(t.Context(), "player@marajanda.com", "The Wayfarers", game.RaceHuman); err != nil {
		t.Fatal(err)
	}
	if entityCount(t, store) != 2 {
		t.Fatalf("entity count = %d, want the founding two", entityCount(t, store))
	}

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, `DELETE FROM accounts WHERE email = 'player@marajanda.com';`, nil); err != nil {
		release()
		t.Fatal(err)
	}
	release()

	if count := entityCount(t, store); count != 0 {
		t.Fatalf("entity count after deleting the account = %d, want 0", count)
	}
	if count := tableCount(t, store, "entity_facts"); count != 0 {
		t.Fatalf("entity_facts after deleting the account = %d, want 0", count)
	}
	if count := tableCount(t, store, "entity_locations"); count != 0 {
		t.Fatalf("entity_locations after deleting the account = %d, want 0", count)
	}
}

// entitiesNow reads a faction's entities as of the current turn.
func entitiesNow(t *testing.T, store *Store, email string) []Entity {
	t.Helper()
	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entities, err := store.EntitiesAsOf(t.Context(), email, turn)
	if err != nil {
		t.Fatal(err)
	}
	return entities
}

func entityCodes(t *testing.T, store *Store, email string) []string {
	t.Helper()
	var codes []string
	for _, entity := range entitiesNow(t, store, email) {
		codes = append(codes, entity.Code)
	}
	return codes
}

// createEntityForTest adds one entity the way a game rule will, in its own
// transaction.
func createEntityForTest(t *testing.T, store *Store, email string, kind game.EntityKind, location hexg.Hex, turn int) Entity {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		t.Fatal(err)
	}
	entity, err := createEntity(conn, email, kind, location, turn)
	end(&err)
	if err != nil {
		t.Fatal(err)
	}
	return entity
}

// closeAndReopenFact is what turn processing does to a fact: close the open row
// at the turn the change takes effect, and open its replacement there.
func closeAndReopenFact(t *testing.T, store *Store, id int64, code, name string, kind game.EntityKind, turn int) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if err := sqlitex.ExecuteTransient(conn, `
		UPDATE entity_facts SET effective_through = ?2
		WHERE entity_id = ?1 AND effective_through = ?3;`, &sqlitex.ExecOptions{
		Args: []any{id, turn, game.EndOfTimeTurn},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, `
		INSERT INTO entity_facts (entity_id, code, name, kind, effective_from, effective_through)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6);`, &sqlitex.ExecOptions{
		Args: []any{id, code, name, string(kind), turn, game.EndOfTimeTurn},
	}); err != nil {
		t.Fatal(err)
	}
}

// otherHex returns some hex of the world that is not the given one.
func otherHex(t *testing.T, store *Store, not hexg.Hex) hexg.Hex {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var found hexg.Hex
	if err := sqlitex.ExecuteTransient(conn, `
		SELECT q, r FROM hexes WHERE q <> ?1 OR r <> ?2 ORDER BY q, r LIMIT 1;`, &sqlitex.ExecOptions{
		Args: []any{not.Q(), not.R()},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			found = hexg.NewHex(stmt.ColumnInt(0), stmt.ColumnInt(1))
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

func createdTurns(t *testing.T, store *Store) []int {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var turns []int
	if err := sqlitex.ExecuteTransient(conn, `SELECT created_turn FROM entities ORDER BY id;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			turns = append(turns, stmt.ColumnInt(0))
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return turns
}

func openPeriodCount(t *testing.T, store *Store, table string) int64 {
	t.Helper()
	return countQuery(t, store, `SELECT count(*) FROM `+table+` WHERE effective_through = ?1;`, game.EndOfTimeTurn)
}

func entityCount(t *testing.T, store *Store) int64 {
	t.Helper()
	return tableCount(t, store, "entities")
}

func tableCount(t *testing.T, store *Store, table string) int64 {
	t.Helper()
	return countQuery(t, store, `SELECT count(*) FROM `+table+`;`)
}

func countQuery(t *testing.T, store *Store, query string, args ...any) int64 {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var count int64
	if err := sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = stmt.ColumnInt64(0)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

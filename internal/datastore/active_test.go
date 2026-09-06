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

// deactivate clears the flag on one row of one table by hand, which is how
// both flags are set during beta.
func deactivate(t *testing.T, store *Store, table, email string) {
	t.Helper()
	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	column := "email"
	if table == "factions" {
		column = "account_email"
	}
	if err := sqlitex.ExecuteTransient(conn,
		"UPDATE "+table+" SET is_active = 0 WHERE "+column+" = ?1;",
		&sqlitex.ExecOptions{Args: []any{email}}); err != nil {
		t.Fatal(err)
	}
}

// TestNewRowsAreActive covers both in-memory modes: a flag whose default did
// not survive a shared open would take away every account the second
// connection created.
func TestNewRowsAreActive(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(*testing.T) *Store
	}{
		{name: "memory", open: func(t *testing.T) *Store {
			store, err := OpenMemory(t.Context(), testGame)
			if err != nil {
				t.Fatal(err)
			}
			return store
		}},
		{name: "shared memory", open: func(t *testing.T) *Store {
			store, err := OpenSharedMemory(t.Context(), t.Name(), testGame)
			if err != nil {
				t.Fatal(err)
			}
			return store
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := test.open(t)
			defer store.Close()

			created, err := store.CreateAccount(t.Context(), SeedAccount{
				Email: "recruit@example.com", Secret: "good.luck", Handle: "recruit", Role: "player",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !created.Active {
				t.Fatalf("created account = %#v, want an active account", created)
			}
			// The seeded accounts predate the flag's default as much as a new
			// row does, so the seed is checked as well as the insert.
			seeded, ok, err := store.Authenticate(t.Context(), "admin@marajanda.com", "good.luck")
			if err != nil || !ok || !seeded.Active {
				t.Fatalf("seeded admin = %#v, %t, %v; want an active admin", seeded, ok, err)
			}

			if _, err := store.SaveFaction(t.Context(), "recruit@example.com", "The Wayfarers", game.RaceHuman); err != nil {
				t.Fatal(err)
			}
			faction, found, err := store.Faction(t.Context(), "recruit@example.com")
			if err != nil || !found {
				t.Fatalf("Faction = %#v, %t, %v; want the saved faction", faction, found, err)
			}
			if !faction.Active {
				t.Fatalf("saved faction = %#v, want an active faction", faction)
			}
			// Inactive is not unconfigured. Nothing may read the flag as a
			// reason to send a player back to the faction form.
			if !faction.Configured() {
				t.Fatalf("saved faction = %#v, want it configured", faction)
			}
		})
	}
}

func TestAuthenticateRefusesAnInactiveAccount(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	deactivate(t, store, "accounts", "player@marajanda.com")

	account, ok, err := store.Authenticate(t.Context(), " PLAYER@MARAJANDA.COM ", "good.luck")
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("Authenticate = %#v, %t, %v; want ErrAccountInactive", account, ok, err)
	}
	if ok || account != (Account{}) {
		t.Fatalf("Authenticate = %#v, %t; want zero account, false", account, ok)
	}

	// A wrong passphrase is still the one indistinguishable refusal, active or
	// not: an account nobody holds the passphrase to gives nothing away.
	account, ok, err = store.Authenticate(t.Context(), "player@marajanda.com", "not.right")
	if err != nil || ok || account != (Account{}) {
		t.Fatalf("Authenticate = %#v, %t, %v; want zero account, false, no error", account, ok, err)
	}

	// The flags are independent: the player's faction says nothing about this.
	other, ok, err := store.Authenticate(t.Context(), "admin@marajanda.com", "good.luck")
	if err != nil || !ok || !other.Active {
		t.Fatalf("Authenticate admin = %#v, %t, %v; want the active admin", other, ok, err)
	}
}

func TestDevelopmentSignInRefusesAnInactiveAccount(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	deactivate(t, store, "accounts", "player@marajanda.com")

	account, err := store.FindOrCreateDevelopmentAccount(t.Context(), "Player@Marajanda.com")
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("FindOrCreateDevelopmentAccount = %#v, %v; want ErrAccountInactive", account, err)
	}
	if account != (Account{}) {
		t.Fatalf("FindOrCreateDevelopmentAccount = %#v, want no account", account)
	}

	// An account the route creates is active, like any other new account.
	created, err := store.FindOrCreateDevelopmentAccount(t.Context(), "agent@example.test")
	if err != nil || !created.Active {
		t.Fatalf("created development account = %#v, %v; want an active account", created, err)
	}
}

// TestAnInactiveFactionGivesNoOrders walks every order write. The page declines
// to show the controls; this is what a hand-built request meets.
func TestAnInactiveFactionGivesNoOrders(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	leader, _ := foundedFaction(t, store)
	seq := addMove(t, store, leader.ID)
	setStep(t, store, leader.ID, seq, 1, compass.NE)

	turn, err := store.CurrentTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writes := []struct {
		name  string
		write func() error
	}{
		{name: "AddOrder", write: func() error {
			_, err := store.AddOrder(t.Context(), orderPlayer, turn, leader.ID, game.OrderKindMove)
			return err
		}},
		{name: "SetOrderStep", write: func() error {
			return store.SetOrderStep(t.Context(), orderPlayer, turn, leader.ID, seq, 1, compass.E)
		}},
		{name: "SetOrderSteps", write: func() error {
			return store.SetOrderSteps(t.Context(), orderPlayer, turn, []OrderSteps{
				{EntityID: leader.ID, Seq: seq, Steps: []compass.Point{compass.E}},
			})
		}},
		{name: "RemoveOrder", write: func() error {
			return store.RemoveOrder(t.Context(), orderPlayer, turn, leader.ID, seq)
		}},
	}

	// Every one of them works while the faction is active, so the refusals
	// below are the flag rather than the request.
	for _, write := range writes[:len(writes)-1] {
		if err := write.write(); err != nil {
			t.Fatalf("%s while active = %v, want it to land", write.name, err)
		}
	}

	deactivate(t, store, "factions", orderPlayer)

	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			if err := write.write(); !errors.Is(err, ErrFactionInactive) {
				t.Fatalf("%s = %v, want ErrFactionInactive", write.name, err)
			}
		})
	}

	// The orders are still there to read. Deactivating a faction stops it
	// acting; it does not take its game away.
	orders := ordersNow(t, store, leader.ID)
	if len(orders) == 0 {
		t.Fatalf("orders after deactivation = %#v, want the stored orders", orders)
	}
	if faction, found, err := store.Faction(t.Context(), orderPlayer); err != nil || !found || faction.Active {
		t.Fatalf("Faction = %#v, %t, %v; want a found, inactive faction", faction, found, err)
	}
}

func TestActiveFlagConstraints(t *testing.T) {
	store, err := OpenMemory(t.Context(), testGame)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	conn, release, err := store.take(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for _, test := range []struct {
		name string
		stmt string
	}{
		{
			name: "an account is active or it is not",
			stmt: `UPDATE accounts SET is_active = 2 WHERE email = 'admin@marajanda.com';`,
		},
		{
			name: "a faction is active or it is not",
			stmt: `INSERT INTO factions (account_email, name, race, is_active)
				VALUES ('player@marajanda.com', 'The Wayfarers', 'human', -1);`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := sqlitex.ExecuteTransient(conn, test.stmt, nil)
			if got := sqlite.ErrCode(err); got != sqlite.ResultConstraintCheck {
				t.Fatalf("constraint error = %v (%v), want %v", err, got, sqlite.ResultConstraintCheck)
			}
		})
	}
}

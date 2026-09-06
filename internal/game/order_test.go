// Copyright (c) 2026 Michael D Henderson.

package game

import "testing"

func TestOrderKindsAreTheKindsTheGameKnows(t *testing.T) {
	kinds := OrderKinds()
	if len(kinds) != 1 || kinds[0] != OrderKindMove {
		t.Fatalf("OrderKinds() = %v, want move alone", kinds)
	}
	for _, kind := range kinds {
		if !kind.Valid() {
			t.Fatalf("%q is listed and not valid", kind)
		}
	}
	// The stored form is the lowercase word, which is what the schema checks
	// and what a form posts.
	if OrderKindMove != "move" {
		t.Fatalf("OrderKindMove = %q, want move", OrderKindMove)
	}
	for _, kind := range []OrderKind{"", "Move", "MOVE", "attack"} {
		if OrderKind(kind).Valid() {
			t.Fatalf("%q is not an order kind and reports valid", kind)
		}
	}
}

// Which orders reach an entity is what separates a hamlet from a leader.
func TestEntityKindsAcceptTheirOwnOrders(t *testing.T) {
	for _, test := range []struct {
		kind  EntityKind
		wants []OrderKind
	}{
		{EntityKindLeader, []OrderKind{OrderKindMove}},
		{EntityKindHamlet, nil},
		{EntityKind("village"), nil},
	} {
		got := test.kind.OrderKinds()
		if len(got) != len(test.wants) {
			t.Fatalf("%s accepts %v, want %v", test.kind, got, test.wants)
		}
		for index, want := range test.wants {
			if got[index] != want {
				t.Fatalf("%s order kind %d = %q, want %q", test.kind, index, got[index], want)
			}
		}
		for _, kind := range OrderKinds() {
			if want := len(test.wants) > 0; test.kind.Accepts(kind) != want {
				t.Fatalf("%s.Accepts(%q) = %v, want %v", test.kind, kind, !want, want)
			}
		}
	}
}

// The slice is the caller's. Handing out the rule itself would let one page
// reorder what every other page offers.
func TestOrderKindsForAnEntityAreACopy(t *testing.T) {
	kinds := EntityKindLeader.OrderKinds()
	if len(kinds) == 0 {
		t.Fatal("a leader accepts no orders")
	}
	kinds[0] = "attack"
	if again := EntityKindLeader.OrderKinds(); again[0] != OrderKindMove {
		t.Fatalf("a leader now accepts %q, want %q", again[0], OrderKindMove)
	}
}

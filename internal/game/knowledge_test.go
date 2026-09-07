// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
)

// The two states are the two states. Anything else is not a state a hex is in,
// including the empty value a hex the faction knows nothing about reads back as.
func TestKnowledgeStatesAreTheTwoStates(t *testing.T) {
	for _, test := range []struct {
		state Knowledge
		want  bool
	}{
		{KnowledgeObserved, true},
		{KnowledgeExplored, true},
		{"", false},
		{"unknown", false},
		{"OBSERVED", false},
	} {
		if got := test.state.Valid(); got != test.want {
			t.Errorf("Knowledge(%q).Valid() = %v, want %v", test.state, got, test.want)
		}
	}

	if got := KnowledgeStates(); len(got) != 2 || got[0] != KnowledgeObserved || got[1] != KnowledgeExplored {
		t.Fatalf("KnowledgeStates() = %v, want observed then explored", got)
	}
}

// The states are ordered, and the order is the whole of the monotonicity rule:
// explored outranks observed, and a hex an entity stood in is never written
// back down by a later sighting of it.
func TestExploredOutranksObserved(t *testing.T) {
	for _, test := range []struct {
		state, other Knowledge
		want         bool
	}{
		{KnowledgeExplored, KnowledgeObserved, true},
		{KnowledgeExplored, KnowledgeExplored, true},
		{KnowledgeObserved, KnowledgeObserved, true},
		{KnowledgeObserved, KnowledgeExplored, false},
		{KnowledgeObserved, "", true},
		{"", KnowledgeObserved, false},
	} {
		if got := test.state.AtLeast(test.other); got != test.want {
			t.Errorf("Knowledge(%q).AtLeast(%q) = %v, want %v", test.state, test.other, got, test.want)
		}
	}
}

// A hex the faction knows nothing about is absent from the set rather than
// present in an unknown state, so Knows reports what a movement cost asks.
func TestKnowledgeSetAnswersForAHexItDoesNotHold(t *testing.T) {
	known := KnowledgeSet{
		hexg.NewHex(0, 0): KnowledgeExplored,
		hexg.NewHex(1, 0): KnowledgeObserved,
	}

	for _, test := range []struct {
		hex   hexg.Hex
		knows bool
		state Knowledge
	}{
		{hexg.NewHex(0, 0), true, KnowledgeExplored},
		{hexg.NewHex(1, 0), true, KnowledgeObserved},
		{hexg.NewHex(9, 9), false, ""},
	} {
		if got := known.Knows(test.hex); got != test.knows {
			t.Errorf("Knows(%v) = %v, want %v", test.hex, got, test.knows)
		}
		if got := known.State(test.hex); got != test.state {
			t.Errorf("State(%v) = %q, want %q", test.hex, got, test.state)
		}
	}

	if got := known.Hexes(); len(got) != len(known) {
		t.Fatalf("Hexes() = %d coordinates, want %d", len(got), len(known))
	}
}

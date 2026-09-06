// Copyright (c) 2026 Michael D Henderson.

package game

import "testing"

func TestEntityKindValid(t *testing.T) {
	for _, kind := range EntityKinds() {
		if !kind.Valid() {
			t.Errorf("EntityKinds names %q, which is not valid", kind)
		}
	}
	for _, kind := range []EntityKind{"", "village", "LEADER", "Hamlet"} {
		if EntityKind(kind).Valid() {
			t.Errorf("%q is valid, want rejected", kind)
		}
	}
}

// A faction is founded with a leader to give orders to and a hamlet to give
// them from, and the founding kinds have to be kinds the game knows.
func TestFoundingEntityKindsAreKindsTheGameKnows(t *testing.T) {
	founding := FoundingEntityKinds()
	if len(founding) != 2 || founding[0] != EntityKindLeader || founding[1] != EntityKindHamlet {
		t.Fatalf("FoundingEntityKinds = %v, want a leader then a hamlet", founding)
	}
	for _, kind := range founding {
		if !kind.Valid() {
			t.Errorf("a faction is founded with %q, which is not a kind", kind)
		}
	}
}

// A code is a kind and a sequence, and reading it back gives the sequence
// again. Players read these off reports and a map, so the pair has to agree.
func TestEntityCodeRoundTrips(t *testing.T) {
	for _, kind := range EntityKinds() {
		for _, sequence := range []int{1, 2, 10, 4096} {
			code := EntityCode(kind, sequence)
			got, ok := EntityCodeSequence(kind, code)
			if !ok || got != sequence {
				t.Errorf("EntityCodeSequence(%q, %q) = %d, %t; want %d, true", kind, code, got, ok, sequence)
			}
		}
	}
	if code := EntityCode(EntityKindLeader, 1); code != "LEADER-1" {
		t.Errorf("EntityCode(leader, 1) = %q, want LEADER-1", code)
	}
	if code := EntityCode(EntityKindHamlet, 1); code != "HAMLET-1" {
		t.Errorf("EntityCode(hamlet, 1) = %q, want HAMLET-1", code)
	}
}

// A code belongs to exactly one kind, and the sequence it carries starts at
// one. Anything else is not a code of that kind, and reading a sequence out of
// it would spend a number nobody assigned.
func TestEntityCodeSequenceRejectsWhatIsNotThatKindsCode(t *testing.T) {
	for _, test := range []struct {
		kind EntityKind
		code string
	}{
		{kind: EntityKindLeader, code: "HAMLET-1"},
		{kind: EntityKindLeader, code: "LEADER-0"},
		{kind: EntityKindLeader, code: "LEADER--1"},
		{kind: EntityKindLeader, code: "LEADER-"},
		{kind: EntityKindLeader, code: "LEADER-1a"},
		{kind: EntityKindLeader, code: "leader-1"},
		{kind: EntityKindLeader, code: "LEADER1"},
		{kind: EntityKindLeader, code: ""},
		{kind: EntityKindHamlet, code: "LEADER-1"},
	} {
		if sequence, ok := EntityCodeSequence(test.kind, test.code); ok {
			t.Errorf("EntityCodeSequence(%q, %q) = %d, true; want rejected", test.kind, test.code, sequence)
		}
	}
}

// Neither sentinel is a turn the game can be on, so neither can collide with a
// real one however far the game runs.
func TestValidTurnExcludesBothSentinels(t *testing.T) {
	for _, turn := range []int{StartOfTimeTurn, -1, EndOfTimeTurn, EndOfTimeTurn + 1} {
		if ValidTurn(turn) {
			t.Errorf("ValidTurn(%d) = true, want false", turn)
		}
	}
	for _, turn := range []int{FirstTurn, 2, EndOfTimeTurn - 1} {
		if !ValidTurn(turn) {
			t.Errorf("ValidTurn(%d) = false, want true", turn)
		}
	}
	if StartOfTimeTurn >= FirstTurn || FirstTurn >= EndOfTimeTurn {
		t.Fatalf("turn bounds are out of order: %d, %d, %d", StartOfTimeTurn, FirstTurn, EndOfTimeTurn)
	}
}

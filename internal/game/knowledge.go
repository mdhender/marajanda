// Copyright (c) 2026 Michael D Henderson.

package game

import "github.com/maloquacious/hexg"

// Knowledge is what a faction knows about one hex.
//
// The two values are ordered and monotone. Unknown becomes observed, observed
// becomes explored, and nothing goes backwards: a faction does not forget
// terrain. Unknown is not a value here because it is the absence of a record
// rather than a state something is in.
//
// Knowledge is not a rendering detail. A step onto ground the faction knew when
// the turn opened costs less than a step onto ground it did not, so this is a
// rules input, and a replay that does not reproduce it reproduces the wrong
// costs. See docs/reference/knowledge.md and docs/reference/action-points.md.
type Knowledge string

const (
	// KnowledgeObserved means the faction knows the hex's terrain attributes:
	// its type, its elevation, and whether it can be entered. Nothing about
	// what stands in it.
	KnowledgeObserved Knowledge = "observed"

	// KnowledgeExplored means the faction knows the above, and what was
	// standing in the hex when its entity was there.
	KnowledgeExplored Knowledge = "explored"
)

// KnowledgeStates lists every state a hex may be known in, least first. The
// order is the rule: it is what AtLeast compares on.
func KnowledgeStates() []Knowledge {
	return []Knowledge{KnowledgeObserved, KnowledgeExplored}
}

// Valid reports whether the state is one this game knows.
func (k Knowledge) Valid() bool {
	return k == KnowledgeObserved || k == KnowledgeExplored
}

// AtLeast reports whether k is other or better.
//
// This is the whole of the monotonicity rule. A hex an entity stood in is
// explored, and a later sighting of it from a neighbouring hex must not write
// it back down to observed, so every write asks this before it writes.
func (k Knowledge) AtLeast(other Knowledge) bool {
	return k.rank() >= other.rank()
}

// rank orders the states. An unknown value ranks below every real one, so a
// state read from somewhere that should not have produced it never outranks a
// state a rule meant to write.
func (k Knowledge) rank() int {
	switch k {
	case KnowledgeExplored:
		return 2
	case KnowledgeObserved:
		return 1
	default:
		return 0
	}
}

// KnowledgeSet is what a faction knew about the world on one turn, keyed by the
// hex's canonical coordinate. A hex the faction did not know is absent.
//
// It is a set rather than a query because of how it is read. The orders page
// prices every one of an entity's orders on every write, for every player
// editing orders, so the cost rule wants one read for a faction and a turn
// rather than a lookup per hex in a loop.
type KnowledgeSet map[hexg.Hex]Knowledge

// Knows reports whether the faction knew the hex at all. It is the question the
// movement cost asks: a step onto ground the faction knew costs the cheap
// price whether that ground was walked or only seen.
func (s KnowledgeSet) Knows(hex hexg.Hex) bool {
	return s[hex].Valid()
}

// State returns what the faction knew about the hex, or the empty state if it
// knew nothing.
func (s KnowledgeSet) State(hex hexg.Hex) Knowledge {
	return s[hex]
}

// Hexes returns the coordinates the faction knew, in no particular order. A
// caller that needs a stable order sorts what it gets.
func (s KnowledgeSet) Hexes() []hexg.Hex {
	hexes := make([]hexg.Hex, 0, len(s))
	for hex := range s {
		hexes = append(hexes, hex)
	}
	return hexes
}

// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/cylinder"
)

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

// Observation is one hex a turn revealed to a faction, and how well.
//
// It is the grain a reveal is recorded on, which is not the grain a step is
// recorded on: one step reveals the hex it entered and as many as six around
// it, so a step outcome is one row and the observations it caused are up to
// seven. See docs/reference/turn-results.md.
//
// Seq is the order that revealed the hex. It is zero when no order did, which
// is what founding is: a faction knows its homeland ring before it has been
// given anything to do.
type Observation struct {
	Seq   int
	Hex   hexg.Hex
	State Knowledge
}

// Reveals returns what standing in a hex reveals: that hex explored, and the
// six around it observed.
//
// This is the rule knowledge is written from, wherever the standing came from -
// a step the executor carried out, or the founding of a faction on its origin.
// It lives here rather than in the store because what a hex reveals is a game
// rule, and two copies of it would be two rules.
//
// Every coordinate it answers with is canonical. A hex beyond a pole is still
// answered, because whether the world has that hex is the world's question: the
// store inserts by selecting from the hexes it has, so a ring that runs off a
// pole records fewer than seven rows rather than naming a row the world does
// not have.
func Reveals(world cylinder.Cylinder, entered hexg.Hex) []Observation {
	entered = world.Normalize(entered)
	seen := make([]Observation, 0, 1+len(compass.Points()))
	seen = append(seen, Observation{Hex: entered, State: KnowledgeExplored})
	for _, neighbour := range compass.Neighbors(world, entered) {
		seen = append(seen, Observation{Hex: neighbour, State: KnowledgeObserved})
	}
	return seen
}

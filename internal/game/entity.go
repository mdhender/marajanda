// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"slices"
	"strconv"
	"strings"
)

// EntityKind is what an entity is. An entity is anything that stands in the
// world: it has a location, a code, a name, and a kind. Orders are issued to
// entities, and an entity's kind decides which order kinds are legal for it.
//
// A settlement is an entity rather than inventory because it has every property
// an entity has - a location, a mutable kind, a name a player will want to
// change, and inventory of its own. What separates a hamlet from a leader is
// not what it is, it is which orders reach it. Today a hamlet accepts none.
//
// Kind is mutable and tracked, so growth from hamlet to village is a new fact
// about one entity rather than a new entity.
type EntityKind string

const (
	EntityKindLeader EntityKind = "leader"
	EntityKindHamlet EntityKind = "hamlet"
)

// EntityKinds lists every kind an entity may hold.
func EntityKinds() []EntityKind {
	return []EntityKind{EntityKindLeader, EntityKindHamlet}
}

// FoundingEntityKinds are the entities a faction is founded with, in the order
// they are created: a leader to give orders to, and a hamlet to give them from.
// Both stand on the faction's origin hex.
//
// It happens to list the same kinds as EntityKinds today. They are different
// rules - what a kind may be, and what a faction starts with - and they part
// company as soon as either grows.
func FoundingEntityKinds() []EntityKind {
	return []EntityKind{EntityKindLeader, EntityKindHamlet}
}

// Valid reports whether the kind is one this game knows.
func (k EntityKind) Valid() bool {
	return slices.Contains(EntityKinds(), k)
}

// EntityCode is an entity's code: its kind, then a per-faction sequence for
// that kind. LEADER-1, HAMLET-1.
//
// A code is assigned at creation and then frozen. A hamlet that grows into a
// village is still HAMLET-1: players read codes on reports and off a map, and a
// code moving under them is worse than a code that no longer describes its
// entity. Codes are scoped to a faction, so two factions each have a LEADER-1.
func EntityCode(kind EntityKind, sequence int) string {
	return EntityCodePrefix(kind) + strconv.Itoa(sequence)
}

// EntityCodePrefix is the part of a code that names the kind, sequence
// excluded.
func EntityCodePrefix(kind EntityKind) string {
	return strings.ToUpper(string(kind)) + "-"
}

// EntityCodeSequence returns the sequence a code carries for kind, reporting
// whether the code is one of that kind's at all.
//
// It reads the code rather than the entity's current kind on purpose. Kind
// changes and the code does not, so a faction whose HAMLET-1 has grown into a
// village has still spent hamlet number one.
func EntityCodeSequence(kind EntityKind, code string) (int, bool) {
	suffix, found := strings.CutPrefix(code, EntityCodePrefix(kind))
	if !found {
		return 0, false
	}
	sequence, err := strconv.Atoi(suffix)
	if err != nil || sequence < 1 {
		return 0, false
	}
	return sequence, true
}

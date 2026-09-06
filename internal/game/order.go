// Copyright (c) 2026 Michael D Henderson.

package game

import "slices"

// OrderKind is what an order tells an entity to do.
//
// An order is issued to an entity rather than to a faction: the faction is
// reached through the entity, and a faction with two leaders has to say which
// one is moving.
type OrderKind string

const (
	// OrderKindMove walks an entity a list of compass points, one step per
	// point, in the order they are given.
	OrderKindMove OrderKind = "move"
)

// OrderKinds lists every order kind the game knows, in the order a form offers
// them.
func OrderKinds() []OrderKind {
	return []OrderKind{OrderKindMove}
}

// Valid reports whether the order kind is one this game knows.
func (k OrderKind) Valid() bool {
	return slices.Contains(OrderKinds(), k)
}

// entityOrderKinds is which order kinds each entity kind accepts.
//
// This is a game rule, so it lives here beside Race and Terrain rather than in
// a template or a handler. The form offers only the kinds an entity accepts and
// the server refuses the rest, so a hand-built request cannot do what the form
// declines to show.
//
// A hamlet accepts nothing today. That is a rule with no orders in it yet, not
// a gap: what separates a hamlet from a leader is which orders reach it.
var entityOrderKinds = map[EntityKind][]OrderKind{
	EntityKindLeader: {OrderKindMove},
	EntityKindHamlet: {},
}

// OrderKinds returns the order kinds an entity of this kind accepts, in the
// order a form offers them. A kind the game does not know accepts nothing.
//
// The result is a fresh slice, so a caller that sorts or appends to it cannot
// change what the next caller sees.
func (k EntityKind) OrderKinds() []OrderKind {
	kinds := entityOrderKinds[k]
	return append(make([]OrderKind, 0, len(kinds)), kinds...)
}

// Accepts reports whether an entity of this kind may be given that order.
func (k EntityKind) Accepts(order OrderKind) bool {
	return slices.Contains(entityOrderKinds[k], order)
}

// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"slices"

	"github.com/mdhender/marajanda/internal/compass"
)

// OrderKind is what an order tells an entity to do.
//
// An order is issued to an entity rather than to a faction: the faction is
// reached through the entity, and a faction with two leaders has to say which
// one is moving.
type OrderKind string

const (
	// OrderKindMove walks an entity one hex, in the direction the order names.
	// An order is one action, so "move nw ne e" is three orders.
	OrderKindMove OrderKind = "move"

	// OrderKindRest spends action points and moves nothing. It carries a
	// count, so a Rest xN is one order with one cost rather than N rows.
	//
	// What a rest recovers is open (#36). Today it costs its points, records
	// that it happened, and changes no state. Its cost and its place among the
	// kinds do not change when that answer arrives.
	OrderKindRest OrderKind = "rest"
)

// OrderKinds lists every order kind the game knows, in the order a form offers
// them.
func OrderKinds() []OrderKind {
	return []OrderKind{OrderKindMove, OrderKindRest}
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
	EntityKindLeader: {OrderKindMove, OrderKindRest},
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

// OrderDetail is what an order carries beyond its kind: the direction a move
// goes, the count a rest lasts.
//
// The two live on one struct here and in two tables on disk, and both are
// right. A caller writing an order says one thing about it, so one argument is
// the shape that call wants; a column that had to mean "not applicable to this
// order kind" is what the detail tables exist to avoid. See
// docs/reference/orders.md#storage.
type OrderDetail struct {
	// Direction is the way a move goes. The zero value is not a compass point,
	// so it is a move a player has added and not yet said the direction of.
	Direction compass.Point
	// Count is how many points a rest lasts. A rest lasts at least one.
	Count int
}

// Order is one instruction issued to one entity for one turn, and one action.
//
// The shape is a game rule rather than a storage detail, so it lives here and
// the store stores it. Seq is the order's position in its entity's list,
// contiguous from 1.
type Order struct {
	Seq    int
	Kind   OrderKind
	Detail OrderDetail
}

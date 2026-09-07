// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/cylinder"
)

// What an entity may spend in a turn, and what each order costs it.
//
// These are the placeholder numbers docs/reference/action-points.md records.
// The flat pair stands in for a cost that will be a function of the moving
// entity's race, the terrain it leaves and the terrain it enters; #37 replaces
// the numbers and not the shape of the rule.
const (
	// KnownStepCost is a step onto ground the faction knew when the turn
	// opened.
	KnownStepCost = 1

	// UnknownStepCost is a step onto ground it did not. The 3 is the price of
	// walking into ground the faction cannot describe, and it is flat
	// regardless of what is actually there: a cost that varied with the
	// terrain would tell a player what they were walking into.
	UnknownStepCost = 3

	// RestPointCost is what one point of a rest costs. A Rest xN costs N.
	RestPointCost = 1

	// LeaderAllowance is the allowance a leader is created with. Six points
	// buy one cheap step and one exploration with 2 AP left: enough to walk
	// back onto known ground, not enough to explore again.
	LeaderAllowance = 6
)

// FoundingAllowance is the action point allowance an entity of this kind is
// created with.
//
// It is the value written into the entity's allowance fact, not a substitute
// for reading that fact: the allowance is effective-dated like the entity's
// location, and a rule that priced from the kind would price turn 3 from
// whatever the kind means today. An entity kind that accepts no orders has no
// allowance, so a hamlet and Marajanda have none.
func FoundingAllowance(kind EntityKind) int {
	if kind == EntityKindLeader {
		return LeaderAllowance
	}
	return 0
}

// StepCost is what one step onto a hex costs.
//
// The question is whether the faction knew the hex, not whether it walked it:
// a step costs the same onto ground the faction has only seen as onto ground it
// has stood in. The knowledge set is the one effective when the turn opened, so
// nothing an entity reveals during a turn prices a step in that turn.
func StepCost(known KnowledgeSet, destination hexg.Hex) int {
	if known.Knows(destination) {
		return KnownStepCost
	}
	return UnknownStepCost
}

// Sight is what a costing may see.
//
// There is one cost function and two callers, and this is the whole of what
// separates them. A fogged costing prices from what the faction knows and
// assumes every order lands. A ground-truth costing may also read the world, so
// a step that will fail is shown as failing and everything after it is priced
// from the hex the entity did not leave.
//
// The zero value is fogged, which is the safe one: a costing that says nothing
// about what it may see sees nothing. The level is set from the session's role
// and never from anything in a request, because pricing unknown ground from
// what is actually there tells a player what is there. See
// docs/reference/action-points.md.
type Sight struct {
	// terrain reads the world. It is nil for a fogged costing, which is what
	// makes the zero value the fogged one and what makes a ground-truth
	// costing impossible to ask for without handing over a world to read.
	terrain func(hexg.Hex) (Terrain, bool)
}

// Fogged prices from the faction's knowledge alone. It is what the orders page
// runs for a player.
func Fogged() Sight {
	return Sight{}
}

// GroundTruth prices from the world as it is. It is what the executor and the
// admin path run; terrain answers with the terrain of a hex, reporting false
// for a coordinate the world does not have.
func GroundTruth(terrain func(hexg.Hex) (Terrain, bool)) Sight {
	return Sight{terrain: terrain}
}

// blocks reports whether a step into destination fails outright. A fogged
// costing assumes every order lands and never asks.
//
// A coordinate the world does not have blocks as impassable ground does. Rows
// do not wrap, so a step off a pole names a row the world has no hex in, and
// the world is the filter that says so.
func (s Sight) blocks(destination hexg.Hex) bool {
	if s.terrain == nil {
		return false
	}
	terrain, found := s.terrain(destination)
	return !found || !terrain.Passable()
}

// Plan is one entity's orders and everything needed to price them.
//
// It carries the knowledge set rather than a way to look one up. The orders
// page prices every row of an entity's list on every write, for every player
// editing orders, so the read wants to be one set read for a faction and a turn
// and not a query per hex in a loop. See docs/reference/knowledge.md.
type Plan struct {
	// Sight is what this costing may see. The zero value is fogged.
	Sight Sight
	// World is the wrap. Every hex a step lands on is normalized through it,
	// because on a world that wraps the alternative is a coordinate that is
	// right about where it is and wrong about what it is called.
	World cylinder.Cylinder
	// Knowledge is what the faction knew when the turn opened.
	Knowledge KnowledgeSet
	// Start is where the entity stands.
	Start hexg.Hex
	// Allowance is the entity's action point allowance for the turn. It is a
	// fact of the entity read as of the turn, not a running balance.
	Allowance int
	// Orders are the entity's orders in sequence order.
	Orders []Order
}

// Estimate is what a Plan costs.
//
// It is an estimate and the word is the point. Every row is priced from the
// position the rows before it would leave the entity in, so a step that fails
// leaves every later row priced from a hex the entity never reached, and an
// unknown hex is priced at the flat exploration cost whatever is actually
// there. The executor charges the real cost, and the difference comes out of
// the entity's remaining orders. Nothing here commits the executor to anything.
type Estimate struct {
	// Allowance is the allowance the orders were priced against.
	Allowance int
	// Orders is one entry per order, in the order they were given.
	Orders []OrderCost
	// Committed is what the orders that fit inside the allowance cost.
	Committed int
	// Total is what every order costs, whether or not it can be paid for.
	Total int
	// Residue is what the allowance leaves unspent, never negative. It is the
	// count of the trailing Rest.
	Residue int
	// Overspend is what the orders cost beyond the allowance, never negative.
	Overspend int
	// ExhaustsAt is the sequence number of the first order the entity cannot
	// afford, or zero when it can afford them all. That order and every order
	// after it will exhaust.
	ExhaustsAt int
	// End is where the walk left the entity.
	End hexg.Hex
}

// OrderCost is what one order costs, and what pricing it assumed.
type OrderCost struct {
	// Seq is the order's position in the entity's list.
	Seq int
	// Kind is what the order tells the entity to do.
	Kind OrderKind
	// Cost is what the order costs in action points.
	Cost int
	// Running is what the orders up to and including this one cost.
	Running int
	// Priced reports whether the order could be priced at all. A move a player
	// has added and not yet said the direction of has no destination, so it
	// has no price; it is not an order that costs nothing.
	Priced bool
	// Exhausts reports that the entity cannot afford this order. It is true of
	// the order where the running total crosses the allowance and of every
	// order after it.
	Exhausts bool
	// Failed reports a step that a ground-truth costing can see will not land.
	// A fogged costing assumes every order lands, so it never sets this.
	Failed bool
	// From and To are where the order starts and where it leaves the entity.
	// They are equal for an order that moves nothing and for a step that
	// failed: a step is paid for whether or not it lands.
	From, To hexg.Hex
	// Target is where a step was aimed, which is not where it left the entity.
	// A step that failed has to say what it walked into, or nothing downstream
	// can record the hex the attempt revealed. It is From for an order that
	// aims nowhere: a rest, and a move with no direction.
	Target hexg.Hex
}

// Price prices an entity's orders.
//
// It walks the orders from where the entity stands, priced against the
// knowledge effective when the turn opened. A fogged costing assumes every
// order lands, so row n is priced from the position rows 1..n-1 would leave the
// entity in; a ground-truth costing walks the world instead, and a step that
// fails leaves the entity where it was.
//
// Exhaustion marks orders, it does not stop the walk. The entity an executor
// walks stops where it runs out; this one keeps going, because the tail of a
// plan priced from where the plan puts it is what a player is editing. Every
// order from the crossing on carries Exhausts, and Committed stops counting
// there, so nothing pretends the tail will happen.
func Price(plan Plan) Estimate {
	at := plan.World.Normalize(plan.Start)
	estimate := Estimate{Allowance: plan.Allowance, End: at}
	for _, order := range plan.Orders {
		cost := OrderCost{Seq: order.Seq, Kind: order.Kind, Priced: true, From: at, Target: at, To: at}
		switch order.Kind {
		case OrderKindMove:
			if !order.Detail.Direction.IsValid() {
				// A move a player has added and not yet said the direction of.
				// It has nowhere to go, so there is nothing to price.
				cost.Priced = false
				break
			}
			destination := compass.Neighbor(plan.World, at, order.Detail.Direction)
			cost.Target = destination
			cost.Cost = StepCost(plan.Knowledge, destination)
			if plan.Sight.blocks(destination) {
				// The step is paid for as what it was when it was ordered and
				// the entity stays where it started. See
				// docs/reference/action-points.md#impassable-destinations.
				cost.Failed = true
				break
			}
			cost.To = destination
		case OrderKindRest:
			cost.Cost = order.Detail.Count * RestPointCost
		default:
			// An order kind this build does not price. It is not free, it is
			// unpriced, and saying so is better than reporting a zero.
			cost.Priced = false
		}
		estimate.Total += cost.Cost
		cost.Running = estimate.Total
		if estimate.ExhaustsAt == 0 && estimate.Total > plan.Allowance {
			estimate.ExhaustsAt = order.Seq
		}
		cost.Exhausts = estimate.ExhaustsAt != 0
		if !cost.Exhausts {
			estimate.Committed = estimate.Total
		}
		at = cost.To
		estimate.Orders = append(estimate.Orders, cost)
	}
	estimate.End = at
	estimate.Residue = max(0, plan.Allowance-estimate.Total)
	estimate.Overspend = max(0, estimate.Total-plan.Allowance)
	return estimate
}

// SplitTrailingRest separates an entity's list into the orders a player
// authored and the count of the Rest that trails them.
//
// A Rest at the end of a list is the residue by definition: it is what the
// entity's unspent action points become, and the pre-processor keeps its count
// equal to what everything before it leaves over. A Rest anywhere else is an
// order the player placed, and it is priced and rendered like any other.
//
// The second result reports whether there was a trailing Rest at all, which is
// not the same as a count of zero: nothing stores a Rest x0.
func SplitTrailingRest(orders []Order) ([]Order, bool) {
	if len(orders) == 0 || orders[len(orders)-1].Kind != OrderKindRest {
		return orders, false
	}
	return orders[:len(orders)-1], true
}

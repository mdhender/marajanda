// Copyright (c) 2026 Michael D Henderson.

package game

import "github.com/maloquacious/hexg"

// The executor: what an entity's orders actually did when the turn was
// processed.
//
// It is the second of the two engines docs/reference/action-points.md names.
// The pre-processor serves order entry, writes orders on the player's behalf,
// and binds nothing; this one serves turn processing, writes no orders, and
// decides what happened. It prices through the same function the pre-processor
// does, Price, so the two agree exactly over ground the faction knows and
// differ only where exploration turned out to cost what the estimate could not
// see.
//
// Nothing here reads a database or a clock. It is handed an entity's orders,
// its allowance, and a Sight that can read the world, and it answers with what
// the turn did; applying that to the facts is the store's work.

// FailureReason says why an order was not carried out. An order that was
// carried out has none.
//
// The vocabulary is the one #33 records. Two of its words are produced today.
// `blocked` is not: nothing blocks a hex, because there is no stacking rule and
// no zone of control, so the word arrives with the rule that earns it.
type FailureReason string

const (
	// FailureTerrain is a step into ground that cannot be entered - polar ice,
	// or a coordinate the world does not have. The step is paid for as what it
	// was when it was ordered and the entity stays where it started. See
	// docs/reference/action-points.md#impassable-destinations.
	FailureTerrain FailureReason = "terrain"

	// FailureExhaust is an order the entity could not afford. It carries no
	// charge: an entity is walked until it cannot afford its next order, and
	// what it cannot afford does not happen.
	FailureExhaust FailureReason = "exhaust"

	// FailureUnknown is an order that does not say what to do. A move a player
	// added and never gave a direction is the one order that reaches
	// processing in that state; it costs nothing, does nothing, and is
	// reported rather than skipped in silence.
	FailureUnknown FailureReason = "unknown"
)

// StepOutcome is what one order did.
//
// Cost is what the entity was charged, which is not what the order would have
// cost: an order that exhausts is not carried out and charges nothing, while a
// step that fails on terrain is charged in full. Carried is the plain question
// of whether the order happened, and Reason answers it when it did not.
type StepOutcome struct {
	// Seq is the order's position in the entity's list.
	Seq int
	// Kind is what the order told the entity to do.
	Kind OrderKind
	// Cost is the action points charged for it.
	Cost int
	// Carried reports that the order was carried out.
	Carried bool
	// Reason says why it was not. It is empty when Carried is true.
	Reason FailureReason
	// From is where the entity stood when the order was resolved. Orders are
	// resolved against where the entity actually stands, so a step that failed
	// leaves the next order starting here.
	From hexg.Hex
	// Target is where a step was aimed. It is From for an order that aims
	// nowhere: a rest, and a move with no direction.
	Target hexg.Hex
	// To is where the order left the entity. It is From for everything that
	// did not move it, a failed step included.
	To hexg.Hex
}

// Outcome is one entity's whole turn: what it was charged, where it ended, and
// what the turn revealed.
//
// Orders and Observations are two grains and not one list. An order produces
// one outcome; the step it carried out produces up to seven observations, one
// per hex it revealed. Both hang off the order that caused them, which is what
// lets a report say that the second step was what showed the mountains. See
// docs/reference/turn-results.md.
type Outcome struct {
	// Allowance is what the entity had to spend on the turn.
	Allowance int
	// Spent is what its orders were charged.
	Spent int
	// Lapsed is what the allowance left unspent. Processing appends nothing to
	// an entity's orders, so points its orders did not reach simply lapse.
	Lapsed int
	// Start and End are where the entity stood when the turn opened and where
	// it stopped.
	Start, End hexg.Hex
	// Orders is one outcome per order, in the order they were given.
	Orders []StepOutcome
	// Observations are the hexes the turn revealed, in the order they were
	// revealed. A step that landed explores the hex it entered and observes
	// the six around it; a step that failed on terrain observes the hex it
	// walked into and nothing else, because the exploration happened and the
	// entity did not.
	Observations []Observation
}

// Entered are the hexes the entity stood in, in the order it entered them.
//
// It is read off the orders rather than carried beside them: the hexes an
// entity entered are exactly the destinations of the steps it carried out, and
// a second list of them is a second thing to keep true.
func (o Outcome) Entered() []hexg.Hex {
	entered := make([]hexg.Hex, 0, len(o.Orders))
	for _, order := range o.Orders {
		if order.Carried && order.Kind == OrderKindMove {
			entered = append(entered, order.To)
		}
	}
	return entered
}

// Execute walks an entity's orders and answers with what they did.
//
// The walk is Price's, read as a result rather than as an estimate. Every order
// from the first the entity cannot afford is exhausted: it is not carried out,
// it charges nothing, and the entity stops where it stopped. Orders before that
// are charged what they cost, whether or not they landed - a step into ground
// that turns out to be impassable is paid for as what it was when it was
// ordered.
//
// The plan's Sight decides whether a step can fail at all. A fogged plan
// assumes every order lands, so executing one produces a turn in which nothing
// ever meets terrain; the store hands in a ground-truth Sight, which is what
// makes this the executor rather than a second pre-processor.
func Execute(plan Plan) Outcome {
	estimate := Price(plan)
	at := plan.World.Normalize(plan.Start)
	outcome := Outcome{Allowance: plan.Allowance, Start: at, End: at}
	for _, priced := range estimate.Orders {
		step := StepOutcome{Seq: priced.Seq, Kind: priced.Kind, From: at, Target: at, To: at}
		switch {
		case priced.Exhausts:
			// The entity is walked until it cannot afford its next order.
			// Nothing after that happens, so nothing after that is charged.
			step.Reason = FailureExhaust
		case !priced.Priced:
			step.Reason = FailureUnknown
		case priced.Failed:
			step.Cost, step.Target, step.Reason = priced.Cost, priced.Target, FailureTerrain
			outcome.observe(priced.Seq, Observation{Hex: priced.Target, State: KnowledgeObserved})
		default:
			step.Cost, step.Target, step.To, step.Carried = priced.Cost, priced.Target, priced.To, true
			if priced.Kind == OrderKindMove {
				outcome.observe(priced.Seq, Reveals(plan.World, priced.To)...)
			}
		}
		outcome.Spent += step.Cost
		at = step.To
		outcome.Orders = append(outcome.Orders, step)
	}
	outcome.End = at
	outcome.Lapsed = max(0, plan.Allowance-outcome.Spent)
	return outcome
}

// observe records what one order revealed, stamping each observation with the
// order that caused it.
//
// The stamp is done here rather than by Reveals because a reveal is not always
// an order's doing: a faction knows its homeland ring from founding, which is
// the same rule with nothing to hang it on.
func (o *Outcome) observe(seq int, seen ...Observation) {
	for _, observation := range seen {
		observation.Seq = seq
		o.Observations = append(o.Observations, observation)
	}
}

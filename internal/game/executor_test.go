// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"slices"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
)

// grasslandEverywhere is a world with nothing in it to walk into.
func grasslandEverywhere(hexg.Hex) (Terrain, bool) { return TerrainGrassland, true }

// outcomes renders one entity's turn in one line per order, so a test says what
// it wanted in a table and reads what it got in the same shape.
func outcomes(outcome Outcome) []string {
	lines := make([]string, 0, len(outcome.Orders))
	for _, step := range outcome.Orders {
		reason := string(step.Reason)
		if step.Carried {
			reason = "carried"
		}
		lines = append(lines, reason)
	}
	return lines
}

// revealed returns the hexes a turn recorded in one knowledge state, in the
// order they were revealed. Observations are the grain the record of a turn
// keeps its sightings on, so a test that asks what a step showed asks here.
func revealed(outcome Outcome, state Knowledge) []hexg.Hex {
	hexes := make([]hexg.Hex, 0, len(outcome.Observations))
	for _, observation := range outcome.Observations {
		if observation.State == state {
			hexes = append(hexes, observation.Hex)
		}
	}
	return hexes
}

// A turn an entity can afford in full is carried out in full: every step lands,
// the entity ends where its orders take it, and what the allowance did not
// reach lapses.
func TestExecuteWalksOrdersItCanAfford(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	known := homeland(world, origin)

	outcome := Execute(Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: known,
		Start: origin, Allowance: LeaderAllowance, Orders: moves(compass.E),
	})

	if got := outcomes(outcome); !slices.Equal(got, []string{"carried"}) {
		t.Fatalf("outcomes = %v, want one carried step", got)
	}
	east := compass.Neighbor(world, origin, compass.E)
	if outcome.End != east {
		t.Fatalf("ended at %v, want %v", outcome.End, east)
	}
	if entered := outcome.Entered(); len(entered) != 1 || entered[0] != east {
		t.Fatalf("entered %v, want just %v", entered, east)
	}
	// A step that lands explores the hex it entered and observes the six
	// around it: one row on the step grain, seven on the observation grain.
	if explored := revealed(outcome, KnowledgeExplored); len(explored) != 1 || explored[0] != east {
		t.Fatalf("explored %v, want just %v", explored, east)
	}
	if observed := revealed(outcome, KnowledgeObserved); len(observed) != len(compass.Points()) {
		t.Fatalf("observed %v, want the six hexes around %v", observed, east)
	}
	for _, observation := range outcome.Observations {
		if observation.Seq != 1 {
			t.Fatalf("observation %#v, want it hung off the step that caused it", observation)
		}
	}
	// The ring is known, so the step is the cheap one and the rest of the
	// allowance lapses: processing appends nothing to an entity's orders.
	if outcome.Spent != KnownStepCost || outcome.Lapsed != LeaderAllowance-KnownStepCost {
		t.Fatalf("spent %d and lapsed %d, want %d and %d",
			outcome.Spent, outcome.Lapsed, KnownStepCost, LeaderAllowance-KnownStepCost)
	}
}

// An entity is walked until it cannot afford its next order. The orders it
// could afford stand, it stops where it stopped, and every order from the
// crossing on is exhausted: not carried out, and not charged.
func TestExecuteStopsWhereTheEntityRunsOut(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	known := homeland(world, origin)

	// One cheap step onto the known ring, then three explorations at 3 AP. Six
	// points pay for the step and the first exploration and no more.
	outcome := Execute(Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: known,
		Start: origin, Allowance: LeaderAllowance,
		Orders: moves(compass.E, compass.E, compass.E, compass.E),
	})

	want := []string{"carried", "carried", "exhaust", "exhaust"}
	if got := outcomes(outcome); !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
	stopped := compass.Neighbor(world, compass.Neighbor(world, origin, compass.E), compass.E)
	if outcome.End != stopped {
		t.Fatalf("ended at %v, want %v", outcome.End, stopped)
	}
	if entered := outcome.Entered(); len(entered) != 2 {
		t.Fatalf("entered %v, want the two hexes it could afford", entered)
	}
	if outcome.Spent != KnownStepCost+UnknownStepCost || outcome.Lapsed != LeaderAllowance-outcome.Spent {
		t.Fatalf("spent %d and lapsed %d of %d", outcome.Spent, outcome.Lapsed, outcome.Allowance)
	}
	// An exhausted order charges nothing, whatever it would have cost.
	for _, step := range outcome.Orders[2:] {
		if step.Cost != 0 || step.Carried {
			t.Fatalf("exhausted order %d = %#v, want nothing charged", step.Seq, step)
		}
	}
}

// A step into ground that cannot be entered is paid for and moves nothing. The
// hex it walked into is revealed, because the exploration happened; the entity
// did not.
func TestExecuteChargesAStepIntoImpassableGround(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	wall := compass.Neighbor(world, origin, compass.NE)
	terrain := func(coord hexg.Hex) (Terrain, bool) {
		if coord == wall {
			return TerrainIce, true
		}
		return TerrainGrassland, true
	}

	// The wall is unknown, so walking into it costs the exploration price.
	outcome := Execute(Plan{
		Sight: GroundTruth(terrain), World: world, Knowledge: KnowledgeSet{origin: KnowledgeExplored},
		Start: origin, Allowance: LeaderAllowance, Orders: moves(compass.NE, compass.E),
	})

	want := []string{"terrain", "carried"}
	if got := outcomes(outcome); !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
	failed := outcome.Orders[0]
	if failed.Cost != UnknownStepCost || failed.Target != wall || failed.To != origin {
		t.Fatalf("failed step = %#v, want %d AP, aimed at %v, moving nothing", failed, UnknownStepCost, wall)
	}
	// The exploration happened and the entity did not, so the step that failed
	// observed the wall and revealed nothing around it.
	if observed := revealed(outcome, KnowledgeObserved); len(observed) == 0 || observed[0] != wall {
		t.Fatalf("observed %v, want the wall first", observed)
	}
	for _, observation := range outcome.Observations {
		if observation.Seq == failed.Seq && observation.Hex != wall {
			t.Fatalf("the failed step revealed %v as well as the wall", observation.Hex)
		}
	}
	// The second order is walked from the hex the entity did not leave.
	if outcome.Orders[1].From != origin {
		t.Fatalf("second order started at %v, want the origin", outcome.Orders[1].From)
	}
	if entered := outcome.Entered(); len(entered) != 1 || entered[0] != outcome.End {
		t.Fatalf("entered %v and ended at %v", entered, outcome.End)
	}
}

// A rest spends its points and moves nothing. What it recovers is #36; today
// it is a charge and a line in the record.
func TestExecuteChargesARestAndMovesNothing(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	outcome := Execute(Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: homeland(world, origin),
		Start: origin, Allowance: LeaderAllowance,
		Orders: []Order{{Seq: 1, Kind: OrderKindRest, Detail: OrderDetail{Count: LeaderAllowance}}},
	})

	if got := outcomes(outcome); !slices.Equal(got, []string{"carried"}) {
		t.Fatalf("outcomes = %v, want a carried rest", got)
	}
	if outcome.End != origin || len(outcome.Entered()) != 0 {
		t.Fatalf("a rest moved the entity to %v", outcome.End)
	}
	// A trailing Rest sized to the whole allowance spends it, so nothing
	// lapses. That is the residue the player agreed to, not an engine write.
	if outcome.Spent != LeaderAllowance || outcome.Lapsed != 0 {
		t.Fatalf("spent %d and lapsed %d, want the whole allowance spent", outcome.Spent, outcome.Lapsed)
	}
}

// A move a player added and never gave a direction reaches processing as an
// order that does not say what to do. It costs nothing, does nothing, and is
// reported rather than passed over in silence.
func TestExecuteReportsAnOrderThatSaysNothing(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	outcome := Execute(Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: homeland(world, origin),
		Start: origin, Allowance: LeaderAllowance,
		Orders: []Order{{Seq: 1, Kind: OrderKindMove}, {Seq: 2, Kind: OrderKindMove, Detail: OrderDetail{Direction: compass.E}}},
	})

	want := []string{"unknown", "carried"}
	if got := outcomes(outcome); !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
	if outcome.Orders[0].Cost != 0 || outcome.Orders[0].Target != origin {
		t.Fatalf("an order with nothing said = %#v, want nothing charged and nowhere aimed", outcome.Orders[0])
	}
	if outcome.Spent != KnownStepCost {
		t.Fatalf("spent %d, want the one step that said where it was going", outcome.Spent)
	}
}

// The executor charges what the pre-processor committed. They are one walk read
// two ways: what an entity is charged is what the estimate said its affordable
// orders cost, so the two engines cannot drift.
func TestExecuteChargesWhatPricingCommitted(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	plan := Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: homeland(world, origin),
		Start: origin, Allowance: LeaderAllowance,
		Orders: moves(compass.E, compass.NE, compass.SE, compass.W),
	}

	estimate, outcome := Price(plan), Execute(plan)
	if outcome.Spent != estimate.Committed {
		t.Fatalf("charged %d against a committed cost of %d", outcome.Spent, estimate.Committed)
	}
	if outcome.Lapsed != max(0, plan.Allowance-estimate.Committed) {
		t.Fatalf("lapsed %d against a committed cost of %d", outcome.Lapsed, estimate.Committed)
	}
}

// An entity with no orders does nothing and reveals nothing. Its whole
// allowance lapses.
func TestExecuteWalksNothingWhenThereAreNoOrders(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	outcome := Execute(Plan{
		Sight: GroundTruth(grasslandEverywhere), World: world, Knowledge: homeland(world, origin),
		Start: origin, Allowance: LeaderAllowance,
	})

	if len(outcome.Orders) != 0 || len(outcome.Entered()) != 0 || len(outcome.Observations) != 0 {
		t.Fatalf("an entity with no orders produced %#v", outcome)
	}
	if outcome.Start != origin || outcome.End != origin {
		t.Fatalf("an entity with no orders moved from %v to %v", outcome.Start, outcome.End)
	}
	if outcome.Lapsed != LeaderAllowance {
		t.Fatalf("lapsed %d, want the whole allowance", outcome.Lapsed)
	}
}

// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/cylinder"
)

// testWorld is a cylinder wide enough that nothing in these tests wraps by
// accident.
func testCylinder(t *testing.T) cylinder.Cylinder {
	t.Helper()
	world, err := cylinder.New(41)
	if err != nil {
		t.Fatal(err)
	}
	return world
}

// homeland is what a faction knows the day it is seated: its origin explored
// and the six hexes around it observed.
func homeland(world cylinder.Cylinder, origin hexg.Hex) KnowledgeSet {
	known := KnowledgeSet{world.Normalize(origin): KnowledgeExplored}
	for _, neighbour := range compass.Neighbors(world, origin) {
		known[neighbour] = KnowledgeObserved
	}
	return known
}

func moves(directions ...compass.Point) []Order {
	orders := make([]Order, 0, len(directions))
	for index, direction := range directions {
		orders = append(orders, Order{Seq: index + 1, Kind: OrderKindMove, Detail: OrderDetail{Direction: direction}})
	}
	return orders
}

// An allowance is written at creation from the entity's kind, and an entity
// kind that accepts no orders has none.
func TestFoundingAllowance(t *testing.T) {
	if got := FoundingAllowance(EntityKindLeader); got != LeaderAllowance {
		t.Fatalf("leader allowance = %d, want %d", got, LeaderAllowance)
	}
	for _, kind := range []EntityKind{EntityKindHamlet, EntityKindMarajanda, EntityKind("village")} {
		if got := FoundingAllowance(kind); got != 0 {
			t.Fatalf("%s allowance = %d, want none", kind, got)
		}
	}
	// Every kind that accepts an order has something to spend on it, and every
	// kind that accepts none has nothing.
	for _, kind := range EntityKinds() {
		if spends, acts := FoundingAllowance(kind) > 0, len(kind.OrderKinds()) > 0; spends != acts {
			t.Fatalf("%s has allowance %v and accepts orders %v", kind, spends, acts)
		}
	}
}

// A step is priced by whether the faction knew the hex, and by nothing else.
// Terrain does not enter into it, which is what keeps the price from telling a
// player what they are walking into.
func TestStepCostAsksOnlyWhetherTheFactionKnew(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	known := homeland(world, origin)

	if got := StepCost(known, origin); got != KnownStepCost {
		t.Fatalf("step onto the origin = %d, want %d", got, KnownStepCost)
	}
	for _, neighbour := range compass.Neighbors(world, origin) {
		if got := StepCost(known, neighbour); got != KnownStepCost {
			t.Fatalf("step onto an observed neighbour = %d, want %d", got, KnownStepCost)
		}
	}
	// A hex the faction has only seen costs what a hex it has walked costs.
	if known[compass.Neighbor(world, origin, compass.NE)] != KnowledgeObserved {
		t.Fatal("the ring should be observed, not explored")
	}
	beyond := compass.Steps(world, origin, compass.NE, 2)
	if got := StepCost(known, beyond); got != UnknownStepCost {
		t.Fatalf("step onto unknown ground = %d, want %d", got, UnknownStepCost)
	}
}

// The homeland ring buys one cheap step. The ring beyond it was not known when
// the turn opened, so the next step costs the exploration price whatever the
// first step revealed, and six points buy one cheap step and one exploration
// with two left over.
func TestTheSecondRingAlwaysCostsThree(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	estimate := Price(Plan{
		World:     world,
		Knowledge: homeland(world, origin),
		Start:     origin,
		Allowance: LeaderAllowance,
		Orders:    moves(compass.NE, compass.NE),
	})
	if len(estimate.Orders) != 2 {
		t.Fatalf("priced %d orders, want 2", len(estimate.Orders))
	}
	if estimate.Orders[0].Cost != KnownStepCost {
		t.Fatalf("first step = %d, want %d", estimate.Orders[0].Cost, KnownStepCost)
	}
	if estimate.Orders[1].Cost != UnknownStepCost {
		t.Fatalf("second step = %d, want %d", estimate.Orders[1].Cost, UnknownStepCost)
	}
	if estimate.Total != 4 || estimate.Committed != 4 {
		t.Fatalf("total %d, committed %d, want 4 and 4", estimate.Total, estimate.Committed)
	}
	if estimate.Residue != 2 || estimate.Overspend != 0 || estimate.ExhaustsAt != 0 {
		t.Fatalf("residue %d, overspend %d, exhausts at %d, want 2, 0 and nothing",
			estimate.Residue, estimate.Overspend, estimate.ExhaustsAt)
	}
	if want := compass.Steps(world, origin, compass.NE, 2); estimate.End != want {
		t.Fatalf("walk ended at %v, want %v", estimate.End, want)
	}
	// Retracing this turn's own new ground costs three again: nothing learned
	// during a turn prices a step in that turn.
	back := Price(Plan{
		World:     world,
		Knowledge: homeland(world, origin),
		Start:     origin,
		Allowance: LeaderAllowance,
		Orders:    moves(compass.NE, compass.NE, compass.SW, compass.NE),
	})
	// Stepping back onto the ring is cheap - it was known when the turn opened
	// - and stepping out again onto the hex this turn explored is not.
	if back.Orders[2].Cost != KnownStepCost {
		t.Fatalf("stepping back onto the ring = %d, want %d", back.Orders[2].Cost, KnownStepCost)
	}
	if back.Orders[3].Cost != UnknownStepCost {
		t.Fatalf("retracing this turn's own new ground = %d, want %d", back.Orders[3].Cost, UnknownStepCost)
	}
}

// A move a player has added and not yet said the direction of has nowhere to
// go. It is unpriced rather than free, and it leaves the entity where it was.
func TestAMoveWithNoDirectionIsUnpricedRatherThanFree(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	estimate := Price(Plan{
		World:     world,
		Knowledge: homeland(world, origin),
		Start:     origin,
		Allowance: LeaderAllowance,
		Orders: []Order{
			{Seq: 1, Kind: OrderKindMove},
			{Seq: 2, Kind: OrderKindMove, Detail: OrderDetail{Direction: compass.E}},
		},
	})
	if estimate.Orders[0].Priced || estimate.Orders[0].Cost != 0 {
		t.Fatalf("blank move = %#v, want unpriced", estimate.Orders[0])
	}
	if estimate.Orders[0].From != estimate.Orders[0].To {
		t.Fatal("a blank move moved the entity")
	}
	// The step after it is still priced from the origin, so the blank row has
	// not shifted anything.
	if estimate.Orders[1].Cost != KnownStepCost || estimate.Total != KnownStepCost {
		t.Fatalf("orders = %#v, want one step of %d", estimate.Orders, KnownStepCost)
	}
}

// A rest costs its count and moves nothing, and it may sit anywhere in a list.
func TestARestCostsItsCountAndMovesNothing(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	estimate := Price(Plan{
		World:     world,
		Knowledge: homeland(world, origin),
		Start:     origin,
		Allowance: LeaderAllowance,
		Orders: []Order{
			{Seq: 1, Kind: OrderKindRest, Detail: OrderDetail{Count: 3}},
			{Seq: 2, Kind: OrderKindMove, Detail: OrderDetail{Direction: compass.E}},
		},
	})
	if estimate.Orders[0].Cost != 3 || estimate.Orders[0].From != estimate.Orders[0].To {
		t.Fatalf("rest = %#v, want 3 AP and no movement", estimate.Orders[0])
	}
	if estimate.Total != 4 || estimate.Residue != 2 {
		t.Fatalf("total %d, residue %d, want 4 and 2", estimate.Total, estimate.Residue)
	}
	if want := compass.Neighbor(world, origin, compass.E); estimate.End != want {
		t.Fatalf("walk ended at %v, want %v", estimate.End, want)
	}
}

// Nothing is rejected for being too long. The entity is walked until it cannot
// afford its next order; that order and every order after it exhausts, and the
// running total says by how much the plan overspends.
func TestOverspendMarksTheCrossingAndEverythingAfterIt(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	// One cheap step, then three explorations: 1, 3, 3, 3 against six points.
	estimate := Price(Plan{
		World:     world,
		Knowledge: homeland(world, origin),
		Start:     origin,
		Allowance: LeaderAllowance,
		Orders:    moves(compass.E, compass.E, compass.E, compass.E),
	})
	if estimate.Total != 10 || estimate.Committed != 4 {
		t.Fatalf("total %d, committed %d, want 10 and 4", estimate.Total, estimate.Committed)
	}
	if estimate.ExhaustsAt != 3 {
		t.Fatalf("exhausts at order %d, want 3", estimate.ExhaustsAt)
	}
	if estimate.Overspend != 4 || estimate.Residue != 0 {
		t.Fatalf("overspend %d, residue %d, want 4 and 0", estimate.Overspend, estimate.Residue)
	}
	for index, cost := range estimate.Orders {
		if want := index >= 2; cost.Exhausts != want {
			t.Fatalf("order %d exhausts = %v, want %v", cost.Seq, cost.Exhausts, want)
		}
	}
}

// The residue is what the allowance leaves over, and an overspend leaves
// nothing over. The trailing Rest is that number, so it never goes negative.
func TestResidueAndOverspendAreNeverBothSet(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	known := homeland(world, origin)

	for _, test := range []struct {
		orders          []Order
		residue, spends int
	}{
		{nil, LeaderAllowance, 0},
		{moves(compass.E), LeaderAllowance - 1, 0},
		{moves(compass.E, compass.W), LeaderAllowance - 2, 0},
		{moves(compass.E, compass.E), LeaderAllowance - 4, 0},
		{moves(compass.E, compass.E, compass.E), 0, 1},
	} {
		estimate := Price(Plan{
			World: world, Knowledge: known, Start: origin,
			Allowance: LeaderAllowance, Orders: test.orders,
		})
		if estimate.Residue != test.residue || estimate.Overspend != test.spends {
			t.Fatalf("%d orders: residue %d and overspend %d, want %d and %d",
				len(test.orders), estimate.Residue, estimate.Overspend, test.residue, test.spends)
		}
		if estimate.Residue > 0 && estimate.Overspend > 0 {
			t.Fatal("a plan cannot both leave points over and want more")
		}
	}
}

// The test that falls out of one cost function and two callers: over ground the
// faction knows in full, the fogged estimate and the ground truth agree exactly.
// Where they differ, the difference is exploration and nothing else.
func TestFoggedAndGroundTruthAgreeOverKnownGround(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)

	// A patch the faction has walked in full, and grassland everywhere.
	known := KnowledgeSet{}
	for _, coord := range world.Spiral(origin, 3) {
		known[coord] = KnowledgeExplored
	}
	grassland := func(hexg.Hex) (Terrain, bool) { return TerrainGrassland, true }

	plan := Plan{
		World: world, Knowledge: known, Start: origin,
		Allowance: LeaderAllowance, Orders: moves(compass.NE, compass.E, compass.SE),
	}
	fogged := Price(plan)
	plan.Sight = GroundTruth(grassland)
	truth := Price(plan)

	if fogged.Total != truth.Total || fogged.Committed != truth.Committed {
		t.Fatalf("fogged %d/%d against ground truth %d/%d",
			fogged.Total, fogged.Committed, truth.Total, truth.Committed)
	}
	if fogged.End != truth.End {
		t.Fatalf("fogged ended at %v, ground truth at %v", fogged.End, truth.End)
	}
	for index := range fogged.Orders {
		if fogged.Orders[index] != truth.Orders[index] {
			t.Fatalf("order %d priced %#v fogged and %#v true", index+1, fogged.Orders[index], truth.Orders[index])
		}
	}
}

// A fogged costing assumes every order lands, whatever is actually there. A
// ground-truth costing knows better: the step is paid for as what it was when
// it was ordered, the entity stays where it started, and every later order is
// priced from the hex it did not leave.
func TestGroundTruthSeesAStepThatWillNotLand(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	wall := compass.Neighbor(world, origin, compass.NE)

	known := homeland(world, origin)
	terrain := func(coord hexg.Hex) (Terrain, bool) {
		if coord == wall {
			return TerrainIce, true
		}
		return TerrainGrassland, true
	}

	plan := Plan{
		Kind: EntityKindLeader, World: world, Knowledge: known, Start: origin,
		Allowance: LeaderAllowance, Orders: moves(compass.NE, compass.E),
	}
	fogged := Price(plan)
	if fogged.Orders[0].Failed || fogged.Orders[0].To != wall {
		t.Fatalf("fogged first step = %#v, want a step that lands", fogged.Orders[0])
	}

	plan.Sight = GroundTruth(terrain)
	truth := Price(plan)
	if !truth.Orders[0].Failed || truth.Orders[0].To != origin {
		t.Fatalf("ground truth first step = %#v, want a failure that moves nothing", truth.Orders[0])
	}
	// The failed step is still paid for, and at the price the faction knew.
	if truth.Orders[0].Cost != KnownStepCost {
		t.Fatalf("failed step cost %d, want %d", truth.Orders[0].Cost, KnownStepCost)
	}
	// The second order is priced from the origin rather than from the hex the
	// plan assumed, so the two costings disagree about it.
	if truth.Orders[1].From != origin {
		t.Fatalf("second order started at %v, want the origin", truth.Orders[1].From)
	}
	if fogged.Orders[1].Cost == truth.Orders[1].Cost && fogged.Orders[1].From == truth.Orders[1].From {
		t.Fatal("the two costings agreed about a step walked from somewhere else")
	}
	// A coordinate the world does not have blocks the way impassable ground
	// does: the world is its own filter.
	nowhere := Price(Plan{
		Kind: EntityKindLeader, World: world, Knowledge: known, Start: origin, Allowance: LeaderAllowance,
		Sight:  GroundTruth(func(hexg.Hex) (Terrain, bool) { return "", false }),
		Orders: moves(compass.E),
	})
	if !nowhere.Orders[0].Failed {
		t.Fatal("a step onto a coordinate the world does not have landed")
	}
}

// Water and ice stop ordinary entity kinds, while Marajanda is not stopped by
// terrain. The destination is what is tested; the terrain under the entity's
// starting position is irrelevant.
func TestGroundTruthPassabilityDependsOnEntityKind(t *testing.T) {
	world := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	destination := compass.Neighbor(world, origin, compass.E)

	for _, terrain := range []Terrain{TerrainOcean, TerrainLake, TerrainIce} {
		sight := GroundTruth(func(hexg.Hex) (Terrain, bool) { return terrain, true })
		for _, kind := range []EntityKind{EntityKindLeader, EntityKindHamlet, EntityKindMarajanda} {
			estimate := Price(Plan{
				Sight: sight, Kind: kind, World: world, Start: origin,
				Allowance: LeaderAllowance, Orders: moves(compass.E),
			})
			wantFailed := kind != EntityKindMarajanda
			if estimate.Orders[0].Failed != wantFailed {
				t.Fatalf("%s entering %s failed = %v, want %v", kind, terrain, estimate.Orders[0].Failed, wantFailed)
			}
			if kind == EntityKindMarajanda && estimate.End != destination {
				t.Fatalf("Marajanda entering %s ended at %v, want %v", terrain, estimate.End, destination)
			}
		}
	}
}

// The zero value of a sight is the fogged one. A costing that says nothing
// about what it may see sees nothing.
func TestTheZeroSightIsFogged(t *testing.T) {
	if Fogged().terrain != nil {
		t.Fatal("the fogged sight carries a world to read")
	}
	if (Sight{}).blocks(EntityKindLeader, hexg.NewHex(0, 0)) {
		t.Fatal("a fogged sight read the world")
	}
	ice := GroundTruth(func(hexg.Hex) (Terrain, bool) { return TerrainIce, true })
	if !ice.blocks(EntityKindLeader, hexg.NewHex(0, 0)) {
		t.Fatal("a ground-truth sight walked into ice")
	}
	if ice.blocks(EntityKindMarajanda, hexg.NewHex(0, 0)) {
		t.Fatal("ice stopped Marajanda")
	}
	outside := GroundTruth(func(hexg.Hex) (Terrain, bool) { return "", false })
	if !outside.blocks(EntityKindMarajanda, hexg.NewHex(0, 0)) {
		t.Fatal("Marajanda stepped beyond the world")
	}
}

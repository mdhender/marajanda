// Copyright (c) 2026 Michael D Henderson.

package compass_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/compass"
	"github.com/mdhender/marajanda/internal/cylinder"
)

// testColumns is an odd column count wide enough that the eastmost and westmost
// columns are not neighbours by accident.
const testColumns = 21

func testCylinder(t *testing.T) cylinder.Cylinder {
	t.Helper()
	c, err := cylinder.New(testColumns)
	if err != nil {
		t.Fatalf("cylinder.New(%d): %v", testColumns, err)
	}
	return c
}

// wantOrder is the order the game rules require. It is written out here rather
// than read from compass.Points, because a test that asked the package what its
// order was could not notice the order changing.
var wantOrder = []compass.Point{compass.NE, compass.E, compass.SE, compass.SW, compass.W, compass.NW}

func TestPointsAreInTheOrderTheRulesRequire(t *testing.T) {
	got := compass.Points()
	if len(got) != 6 {
		t.Fatalf("Points() has %d points, want 6", len(got))
	}
	for index, want := range wantOrder {
		if got[index] != want {
			t.Fatalf("Points()[%d] = %s, want %s", index, got[index], want)
		}
	}
}

// The order is public, so a caller that sorts the slice must not be able to
// change what the next caller reads.
func TestPointsReturnsAFreshSlice(t *testing.T) {
	first := compass.Points()
	slices.Reverse(first)
	if second := compass.Points(); second[0] != compass.NE {
		t.Fatalf("Points()[0] = %s after a caller reversed an earlier result, want NE", second[0])
	}
}

// screenward is where each point must lie on a drawn map: the sign its
// neighbour's pixel offset must have in x and in y, with y increasing
// downward as it does in SVG. Zero means the offset must be zero.
var screenward = map[compass.Point]struct{ x, y int }{
	compass.NE: {+1, -1},
	compass.E:  {+1, 0},
	compass.SE: {+1, +1},
	compass.SW: {-1, +1},
	compass.W:  {-1, 0},
	compass.NW: {-1, -1},
}

// TestEveryPointLiesWhereItsNameSaysOnADrawnMap is the test this package
// exists for. The mapping onto hexg's direction numbers is not restated here;
// it is re-derived by drawing the hex and looking at where it landed, so a
// change to the layout or to hexg's numbering fails here rather than quietly
// sending everyone north-east when they asked to go east.
//
// Both pointy-top offsets are checked. The compass depends on the orientation
// and not on which rows are shoved right: even-r and odd-r differ only in the
// offset conversion, which axial arithmetic never goes through. A flat-top
// layout would need a different compass entirely - it has north and south
// neighbours and no east or west - and there is deliberately no attempt here
// to serve one.
func TestEveryPointLiesWhereItsNameSaysOnADrawnMap(t *testing.T) {
	const epsilon = 0.001
	sign := func(v float64) int {
		switch {
		case v > epsilon:
			return +1
		case v < -epsilon:
			return -1
		}
		return 0
	}
	c := testCylinder(t)
	for _, offset := range []struct {
		name   string
		layout hexg.LayoutOffset
	}{{"even-r", hexg.EvenR}, {"odd-r", hexg.OddR}} {
		layout := hexg.NewLayout(offset.layout, hexg.Point{X: 24, Y: 24}, hexg.Point{})
		origin := hexg.NewHex(0, 0)
		from := layout.HexToPixel(origin)
		for _, p := range compass.Points() {
			to := layout.HexToPixel(compass.Neighbor(c, origin, p))
			x, y := sign(to.X-from.X), sign(to.Y-from.Y)
			want := screenward[p]
			if x != want.x || y != want.y {
				t.Fatalf("%s: %s neighbour is drawn at (%+d, %+d), want (%+d, %+d)",
					offset.name, p.Name(), x, y, want.x, want.y)
			}
		}
	}
}

// The order has to be clockwise as well as correct, because a rule that walks
// neighbours in order walks them around the hex rather than back and forth
// across it.
func TestTheOrderGoesClockwiseAroundTheHex(t *testing.T) {
	layout := hexg.NewLayout(hexg.EvenR, hexg.Point{X: 24, Y: 24}, hexg.Point{})
	c := testCylinder(t)
	origin := hexg.NewHex(0, 0)
	from := layout.HexToPixel(origin)

	for index, p := range compass.Points() {
		to := layout.HexToPixel(compass.Neighbor(c, origin, p))
		// Screen y grows downward, so a growing atan2 is a turn clockwise.
		// Measured from due east and folded into [0, 360), north-east is 300
		// and each following point is another sixth of a turn on.
		angle := math.Mod(math.Atan2(to.Y-from.Y, to.X-from.X)*180/math.Pi+360, 360)
		want := math.Mod(300+60*float64(index), 360)
		if math.Abs(angle-want) > 0.001 {
			t.Fatalf("%s is point %d of the compass at %.1f degrees, want %.1f",
				p.Name(), index+1, angle, want)
		}
	}
}

func TestNeighborsAreSixDistinctHexesOneStepAway(t *testing.T) {
	c := testCylinder(t)
	for _, from := range []hexg.Hex{
		hexg.NewHex(0, 0),
		hexg.NewHex(3, -7),
		hexg.NewHex(-4, 5),
	} {
		around := compass.Neighbors(c, from)
		if len(around) != 6 {
			t.Fatalf("Neighbors(%v) has %d hexes, want 6", from, len(around))
		}
		seen := make(map[hexg.Hex]bool, 6)
		for index, to := range around {
			if seen[to] {
				t.Fatalf("Neighbors(%v)[%d] = %v, already seen", from, index, to)
			}
			seen[to] = true
			if got := c.Distance(from, to); got != 1 {
				t.Fatalf("%s of %v is %v, %d steps away, want 1", compass.Points()[index].Name(), from, to, got)
			}
		}
		// The same six hexes hexg would give, in our order rather than its.
		want := c.Ring(from, 1)
		got := slices.Clone(around)
		slices.SortFunc(got, hexg.Hex.Compare)
		if !slices.Equal(got, want) {
			t.Fatalf("Neighbors(%v) sorted = %v, want the ring %v", from, got, want)
		}
	}
}

// Neighbors must agree with Neighbor point for point, in order. They are two
// ways of asking the same question and a rule may use either.
func TestNeighborsAgreesWithNeighbor(t *testing.T) {
	c := testCylinder(t)
	from := hexg.NewHex(2, -3)
	around := compass.Neighbors(c, from)
	for index, p := range compass.Points() {
		if want := compass.Neighbor(c, from, p); around[index] != want {
			t.Fatalf("Neighbors[%d] = %v, but Neighbor(%s) = %v", index, around[index], p, want)
		}
	}
}

func TestOppositeIsHalfWayRound(t *testing.T) {
	for _, tc := range []struct{ point, want compass.Point }{
		{compass.NE, compass.SW},
		{compass.E, compass.W},
		{compass.SE, compass.NW},
		{compass.SW, compass.NE},
		{compass.W, compass.E},
		{compass.NW, compass.SE},
	} {
		if got := tc.point.Opposite(); got != tc.want {
			t.Fatalf("%s.Opposite() = %s, want %s", tc.point, got, tc.want)
		}
		if got := tc.point.Opposite().Opposite(); got != tc.point {
			t.Fatalf("%s.Opposite().Opposite() = %s, want %s", tc.point, got, tc.point)
		}
	}
	// The zero value has no opposite and must not acquire one.
	var unset compass.Point
	if got := unset.Opposite(); got != unset {
		t.Fatalf("the zero point's Opposite() = %s, want the zero point", got)
	}
}

// Stepping out and back is the property a movement order rests on.
func TestSteppingBackUndoesSteppingOut(t *testing.T) {
	c := testCylinder(t)
	for _, from := range []hexg.Hex{hexg.NewHex(0, 0), hexg.NewHex(10, -2), hexg.NewHex(-10, 4)} {
		for _, p := range compass.Points() {
			there := compass.Neighbor(c, from, p)
			if back := compass.Neighbor(c, there, p.Opposite()); back != from {
				t.Fatalf("%v stepped %s to %v and %s back to %v", from, p, there, p.Opposite(), back)
			}
		}
	}
}

func TestStepsWalksAndRewinds(t *testing.T) {
	c := testCylinder(t)
	from := hexg.NewHex(1, 1)
	for _, p := range compass.Points() {
		if got := compass.Steps(c, from, p, 0); got != from {
			t.Fatalf("Steps(%v, %s, 0) = %v, want %v", from, p, got, from)
		}
		if got, want := compass.Steps(c, from, p, 1), compass.Neighbor(c, from, p); got != want {
			t.Fatalf("Steps(%v, %s, 1) = %v, want %v", from, p, got, want)
		}
		// Walking backwards is walking the other way.
		if got, want := compass.Steps(c, from, p, -3), compass.Steps(c, from, p.Opposite(), 3); got != want {
			t.Fatalf("Steps(%v, %s, -3) = %v, want %v", from, p, got, want)
		}
		// Three steps is three hexes, measured the wrapped way.
		if got := c.Distance(from, compass.Steps(c, from, p, 3)); got != 3 {
			t.Fatalf("three steps %s from %v is %d hexes away, want 3", p, from, got)
		}
	}
}

// A step east from the eastmost column is one hex east, not most of a world
// west. This is the whole reason the functions take a cylinder.
func TestSteppingEastOffTheMeridianWrapsIntoTheWorld(t *testing.T) {
	c := testCylinder(t)
	east := hexg.NewHex(c.HalfWidth(), 0)
	west := hexg.NewHex(-c.HalfWidth(), 0)

	if got := compass.Neighbor(c, east, compass.E); got != west {
		t.Fatalf("east of %v is %v, want %v", east, got, west)
	}
	if got := compass.Neighbor(c, west, compass.W); got != east {
		t.Fatalf("west of %v is %v, want %v", west, got, east)
	}
	// Every hex a step lands on is canonical, or it would seed a private PRNG
	// stream under a name no other route to the same ground would use.
	for _, p := range compass.Points() {
		if to := compass.Neighbor(c, east, p); !c.IsCanonical(to) {
			t.Fatalf("%s of %v is %v, which is not canonical", p, east, to)
		}
	}
	// A whole lap comes home.
	if got := compass.Steps(c, east, compass.E, testColumns); got != east {
		t.Fatalf("a lap east of %v is %v, want %v", east, got, east)
	}
}

// Rows are walls, not a wrap. A walk off the top of the world is reported as a
// row off the top of the world, for the caller to judge against its own bounds.
func TestSteppingNorthLeavesTheRowAloneToBeJudged(t *testing.T) {
	c := testCylinder(t)
	from := hexg.NewHex(0, -4)
	got := compass.Steps(c, from, compass.NE, 10)
	if got.R() != -14 {
		t.Fatalf("ten steps north-east from row %d ends on row %d, want -14", from.R(), got.R())
	}
}

func TestParseAcceptsEverySpellingOfEveryPoint(t *testing.T) {
	for _, tc := range []struct {
		want      compass.Point
		spellings []string
	}{
		{compass.NE, []string{"NE", "ne", "nE", "north-east", "northeast", "North East", " NORTH-EAST "}},
		{compass.E, []string{"E", "e", "east", "East", " EAST "}},
		{compass.SE, []string{"SE", "se", "south-east", "southeast", "South-East"}},
		{compass.SW, []string{"SW", "sw", "south-west", "southwest", "SOUTH WEST"}},
		{compass.W, []string{"W", "w", "west", "West"}},
		{compass.NW, []string{"NW", "nw", "north-west", "northwest", "north_west"}},
	} {
		for _, spelling := range tc.spellings {
			got, err := compass.Parse(spelling)
			if err != nil {
				t.Fatalf("Parse(%q): %v", spelling, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %s, want %s", spelling, got, tc.want)
			}
		}
	}
}

// Whatever a point prints as, it must parse back.
func TestParseRoundTripsEveryPrintedForm(t *testing.T) {
	for _, p := range compass.Points() {
		for _, written := range []string{p.String(), p.Name()} {
			got, err := compass.Parse(written)
			if err != nil {
				t.Fatalf("Parse(%q): %v", written, err)
			}
			if got != p {
				t.Fatalf("Parse(%q) = %s, want %s", written, got, p)
			}
		}
	}
}

// "move north" is the order that started all of this. It has to fail in a way a
// player can act on, and distinguishably from a typo.
func TestParseTellsAPlayerWhyThereIsNoNorth(t *testing.T) {
	for _, tc := range []struct {
		value   string
		bracket []string
	}{
		{"north", []string{"north-west", "north-east"}},
		{"N", []string{"north-west", "north-east"}},
		{"South", []string{"south-west", "south-east"}},
		{"s", []string{"south-west", "south-east"}},
	} {
		got, err := compass.Parse(tc.value)
		if !errors.Is(err, compass.ErrNoNeighbor) {
			t.Fatalf("Parse(%q) error = %v, want ErrNoNeighbor", tc.value, err)
		}
		if errors.Is(err, compass.ErrUnknownPoint) {
			t.Fatalf("Parse(%q) reports a bearing this grid lacks as an unknown word", tc.value)
		}
		if got.IsValid() {
			t.Fatalf("Parse(%q) = %s, want no point", tc.value, got)
		}
		for _, want := range tc.bracket {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Parse(%q) said %q, which does not name %s", tc.value, err, want)
			}
		}
	}
}

func TestParseRejectsWhatIsNotADirection(t *testing.T) {
	for _, value := range []string{"", "   ", "-", "up", "left", "nne", "eastish", "northeasterly", "0", "ne ne"} {
		got, err := compass.Parse(value)
		if !errors.Is(err, compass.ErrUnknownPoint) {
			t.Fatalf("Parse(%q) = (%s, %v), want ErrUnknownPoint", value, got, err)
		}
		if got.IsValid() {
			t.Fatalf("Parse(%q) = %s, want no point", value, got)
		}
	}
}

func TestTheZeroValueIsNotAPoint(t *testing.T) {
	var unset compass.Point
	if unset.IsValid() {
		t.Fatal("the zero value reports itself as a compass point")
	}
	if unset == compass.NE {
		t.Fatal("the zero value is north-east, so an order nobody filled in travels")
	}
	// It prints as something a person can recognize rather than as a direction.
	if got := unset.String(); got != "Point(0)" {
		t.Fatalf("the zero value prints as %q, want Point(0)", got)
	}
	if got := unset.Name(); got != "Point(0)" {
		t.Fatalf("the zero value names itself %q, want Point(0)", got)
	}
	for _, p := range []compass.Point{-1, 0, 7, 100} {
		if p.IsValid() {
			t.Fatalf("Point(%d) reports itself valid", int(p))
		}
	}
}

// Arithmetic on a point nobody set is a bug in the caller, not a direction of
// travel to invent.
func TestVectorPanicsOnAPointNobodySet(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("the zero value returned a vector instead of panicking")
		}
	}()
	var unset compass.Point
	_ = unset.Vector()
}

func TestNamesAndAbbreviations(t *testing.T) {
	for _, tc := range []struct {
		point              compass.Point
		abbreviation, name string
	}{
		{compass.NE, "NE", "north-east"},
		{compass.E, "E", "east"},
		{compass.SE, "SE", "south-east"},
		{compass.SW, "SW", "south-west"},
		{compass.W, "W", "west"},
		{compass.NW, "NW", "north-west"},
	} {
		if got := tc.point.String(); got != tc.abbreviation {
			t.Fatalf("String() = %q, want %q", got, tc.abbreviation)
		}
		if got := tc.point.Name(); got != tc.name {
			t.Fatalf("Name() = %q, want %q", got, tc.name)
		}
	}
}

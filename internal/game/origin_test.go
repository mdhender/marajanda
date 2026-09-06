// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"errors"
	"fmt"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

func TestAssignOriginDeterministic(t *testing.T) {
	seeds := testSeeds()
	world := testWorld(t)
	taken := []Placement{{Coord: hexg.NewHex(16, 0), Race: RaceHuman}}

	first, err := AssignOrigin(seeds, "player@example.com", RaceHuman, world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() error = %v", err)
	}
	second, err := AssignOrigin(seeds, "player@example.com", RaceHuman, world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() repeated error = %v", err)
	}
	if !first.Equals(second) {
		t.Fatalf("AssignOrigin repeated = %v then %v", first, second)
	}
	if distance := world.Cylinder().Distance(hexg.NewHex(0, 0), first); distance <= minimumOriginDistance {
		t.Fatalf("AssignOrigin distance from game origin = %d, want > %d", distance, minimumOriginDistance)
	}
}

// A player origin has to be somewhere a faction can stand, inside the belt the
// placement rules confine it to. Placing one at sea, or under a polar ice sheet
// nothing can enter, is the failure this constraint exists to prevent.
func TestAssignOriginIsLandInsideTheBelt(t *testing.T) {
	seeds := testSeeds()
	world := testWorld(t)
	belt := OriginBelt(world.Height())

	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"} {
		for _, race := range Races() {
			origin, err := AssignOrigin(seeds, email, race, world, nil)
			if err != nil {
				t.Fatalf("AssignOrigin(%q, %q) error = %v", email, race, err)
			}
			if !world.Contains(origin) {
				t.Fatalf("AssignOrigin(%q, %q) = %v, which is outside the world", email, race, origin)
			}
			if origin.R() < -belt || origin.R() > belt {
				t.Fatalf("AssignOrigin(%q, %q) = %v, which is outside the belt |r| <= %d", email, race, origin, belt)
			}
			hex, _ := world.At(origin)
			if !hex.Terrain.IsLand() {
				t.Fatalf("AssignOrigin(%q, %q) = %v, which is %q", email, race, origin, hex.Terrain)
			}
		}
	}
}

// The favored terrain is taken while it has room. Falling through to a less
// favored one is a last resort, not a coin toss.
func TestAssignOriginPrefersTheFavoredTerrain(t *testing.T) {
	world := testWorld(t)
	for _, race := range Races() {
		want := race.TerrainOrder()[0]
		origin, err := AssignOrigin(testSeeds(), "settler@example.com", race, world, nil)
		if err != nil {
			t.Fatalf("AssignOrigin(%q) error = %v", race, err)
		}
		if hex, _ := world.At(origin); hex.Terrain != want {
			t.Fatalf("AssignOrigin(%q) = %v on %q, want %q", race, origin, hex.Terrain, want)
		}
	}
}

// Falling through happens only once the favored pool holds nothing valid. The
// whole of the favored terrain is blocked here, and nothing less.
func TestAssignOriginFallsThroughOnlyWhenTheFavoredPoolIsFull(t *testing.T) {
	world := testWorld(t)
	race := RaceHuman
	favored, next := race.TerrainOrder()[0], race.TerrainOrder()[1]

	var taken []Placement
	for _, hex := range world.Hexes() {
		if hex.Terrain == favored {
			taken = append(taken, Placement{Coord: hex.Coord, Race: race})
		}
	}

	origin, err := AssignOrigin(testSeeds(), "crowded@example.com", race, world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() error = %v", err)
	}
	if hex, _ := world.At(origin); hex.Terrain != next {
		t.Fatalf("AssignOrigin() = %v on %q, want the second choice %q", origin, hex.Terrain, next)
	}
}

// A world with no land in the belt has nowhere to put anybody, however much
// land it has elsewhere.
func TestAssignOriginReportsABeltWithNoLand(t *testing.T) {
	// Height 3 puts the belt at |r| <= 2, and rows -3 and 3 are the ice.
	var hexes []Hex
	for r := -3; r <= 3; r++ {
		for q := -40; q <= 40; q++ {
			terrain := TerrainOcean
			if r == -3 || r == 3 {
				terrain = TerrainIce
			}
			hexes = append(hexes, Hex{Coord: hexg.NewHex(q, r), Terrain: terrain, Elevation: -60})
		}
	}
	world, err := NewWorld(40, 3, hexes)
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := AssignOrigin(testSeeds(), "player@example.com", RaceHuman, world, nil); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("AssignOrigin(no land in the belt) error = %v, want %v", err, ErrNoOrigin)
	}
}

// Land that is all inside the spacing limits is land that cannot be settled.
func TestAssignOriginReportsAWorldWithNoRoomLeft(t *testing.T) {
	world := testWorld(t)

	var taken []Placement
	for _, hex := range world.Hexes() {
		if hex.Terrain.IsLand() {
			taken = append(taken, Placement{Coord: hex.Coord, Race: RaceHuman})
		}
	}
	if _, err := AssignOrigin(testSeeds(), "player@example.com", RaceHuman, world, taken); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("AssignOrigin(full world) error = %v, want %v", err, ErrNoOrigin)
	}
}

// The two spacing limits are the placement contract, and both are inclusive.
// Seating a crowd and measuring every pair is the only way to see them hold
// together rather than one at a time.
func TestAssignOriginKeepsBothSpacingLimits(t *testing.T) {
	world := testWorld(t)
	races := Races()

	var taken []Placement
	for index := range 40 {
		race := races[index%len(races)]
		origin, err := AssignOrigin(testSeeds(), fmt.Sprintf("settler-%d@example.com", index), race, world, taken)
		if err != nil {
			t.Fatalf("AssignOrigin(%d) error = %v", index, err)
		}
		taken = append(taken, Placement{Coord: origin, Race: race})
	}

	terrainAt := func(coord hexg.Hex) Terrain {
		hex, _ := world.At(coord)
		return hex.Terrain
	}
	for i, a := range taken {
		for _, b := range taken[i+1:] {
			want := otherOriginSpacing
			if a.Race == b.Race && terrainAt(a.Coord) == terrainAt(b.Coord) {
				want = sameRaceTerrainSpacing
			}
			if distance := world.Cylinder().Distance(a.Coord, b.Coord); distance < want {
				t.Fatalf("%v (%s on %s) and %v (%s on %s) are %d apart, want at least %d",
					a.Coord, a.Race, terrainAt(a.Coord), b.Coord, b.Race, terrainAt(b.Coord), distance, want)
			}
		}
	}
}

// Exactly at the limit is accepted. Placement that rounded either limit the
// other way would pack the map differently from the documented contract.
func TestAssignOriginAcceptsExactlyTheSpacingLimit(t *testing.T) {
	world := testWorld(t)
	center := hexg.NewHex(0, 0)

	// A land hex on the belt's equator, far enough out to have room around it.
	var anchor hexg.Hex
	var anchorTerrain Terrain
	for _, hex := range world.Hexes() {
		if hex.Coord.R() == 0 && hex.Terrain.IsLand() && world.Cylinder().Distance(center, hex.Coord) > minimumOriginDistance {
			anchor, anchorTerrain = hex.Coord, hex.Terrain
			break
		}
	}
	if anchorTerrain == "" {
		t.Skip("test world has no land row at the equator")
	}

	for _, test := range []struct {
		name     string
		race     Race
		terrain  Terrain
		distance int
	}{
		{name: "same race and terrain", race: RaceHuman, terrain: anchorTerrain, distance: sameRaceTerrainSpacing},
		{name: "everything else", race: RaceElf, terrain: anchorTerrain, distance: otherOriginSpacing},
	} {
		t.Run(test.name, func(t *testing.T) {
			seats := resolveSeats(world, []Placement{{Coord: anchor, Race: RaceHuman}})
			candidate := world.Normalize(hexg.NewHex(anchor.Q()+test.distance, anchor.R()))
			if got := world.Cylinder().Distance(anchor, candidate); got != test.distance {
				t.Fatalf("candidate is %d from the anchor, want %d", got, test.distance)
			}
			if !validOrigin(candidate, test.race, test.terrain, world, seats) {
				t.Fatalf("a candidate exactly %d away was rejected", test.distance)
			}
			closer := world.Normalize(hexg.NewHex(anchor.Q()+test.distance-1, anchor.R()))
			if validOrigin(closer, test.race, test.terrain, world, seats) {
				t.Fatalf("a candidate %d away was accepted, want at least %d", test.distance-1, test.distance)
			}
		})
	}
}

// The exclusion set is what a race spaces against, so a race that shares no
// favored terrain with the crowd is not crowded out by it.
func TestAssignOriginSpacesRacesApartFromEachOther(t *testing.T) {
	world := testWorld(t)

	// Seat dwarves across every mountain hex. A human wants grassland and is
	// unaffected beyond the flat eight-hex limit.
	var taken []Placement
	for _, hex := range world.Hexes() {
		if hex.Terrain == TerrainMountains {
			taken = append(taken, Placement{Coord: hex.Coord, Race: RaceDwarf})
		}
	}
	origin, err := AssignOrigin(testSeeds(), "human@example.com", RaceHuman, world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() error = %v", err)
	}
	if hex, _ := world.At(origin); hex.Terrain != TerrainGrassland {
		t.Fatalf("AssignOrigin() = %v on %q, want grassland", origin, hex.Terrain)
	}
}

// Filling a world seats a stable count, and the account after the last one is
// refused rather than seated somewhere it does not belong.
func TestAssignOriginFillsToAStableExhaustion(t *testing.T) {
	world := mustGenerate(t, testSeeds(), 24, 12)

	fill := func() []Placement {
		var taken []Placement
		for index := range 500 {
			origin, err := AssignOrigin(testSeeds(), fmt.Sprintf("settler-%d@example.com", index), RaceHuman, world, taken)
			if errors.Is(err, ErrNoOrigin) {
				return taken
			}
			if err != nil {
				t.Fatalf("AssignOrigin(%d) error = %v", index, err)
			}
			taken = append(taken, Placement{Coord: origin, Race: RaceHuman})
		}
		t.Fatal("the world never filled up")
		return nil
	}

	first, second := fill(), fill()
	if len(first) == 0 {
		t.Fatal("the world seated nobody at all")
	}
	if len(first) != len(second) {
		t.Fatalf("filling the world seated %d then %d", len(first), len(second))
	}
	for index, placement := range first {
		if !placement.Coord.Equals(second[index].Coord) {
			t.Fatalf("seat %d = %v then %v", index, placement.Coord, second[index].Coord)
		}
	}
}

// An unregistered race has no pools to draw from, so it places nobody rather
// than falling back to somebody else's preferences.
func TestAssignOriginRefusesAnUnknownRace(t *testing.T) {
	if _, err := AssignOrigin(testSeeds(), "player@example.com", Race("wyrm"), testWorld(t), nil); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("AssignOrigin(unknown race) error = %v, want %v", err, ErrNoOrigin)
	}
}

func testSeeds() prng.Seeds {
	seed2 := int64(-98)
	return prng.New(98374, uint64(seed2))
}

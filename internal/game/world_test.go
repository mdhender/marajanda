// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"sync"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// testWorldRadius is small enough to keep the suite quick and large enough to
// grow every feature the generator can produce.
const testWorldRadius = 24

var (
	sharedWorldOnce sync.Once
	sharedWorld     World
)

// testWorld returns a generated world shared by the package's tests. Generation
// is deterministic, so sharing one world between tests cannot couple them.
func testWorld(t *testing.T) World {
	t.Helper()
	sharedWorldOnce.Do(func() {
		sharedWorld = GenerateWorld(testSeeds(), testWorldRadius)
	})
	return sharedWorld
}

func TestGenerateWorldIsDeterministic(t *testing.T) {
	first := GenerateWorld(testSeeds(), 8)
	second := GenerateWorld(testSeeds(), 8)

	firstHexes, secondHexes := first.Hexes(), second.Hexes()
	if len(firstHexes) != len(secondHexes) {
		t.Fatalf("GenerateWorld returned %d then %d hexes", len(firstHexes), len(secondHexes))
	}
	for index, hex := range firstHexes {
		if hex != secondHexes[index] {
			t.Fatalf("GenerateWorld differs at %d: %+v then %+v", index, hex, secondHexes[index])
		}
	}
}

// Different seeds must produce a different world, or the seeds are decorative.
func TestGenerateWorldVariesWithSeeds(t *testing.T) {
	first := GenerateWorld(testSeeds(), 8)
	second := GenerateWorld(prng.New(1, 2), 8)

	same := 0
	for _, hex := range first.Hexes() {
		if other, ok := second.At(hex.Coord); ok && other.Terrain == hex.Terrain {
			same++
		}
	}
	if same == first.Len() {
		t.Fatal("GenerateWorld produced identical terrain for different seeds")
	}
}

func TestGenerateWorldCoversTheDisc(t *testing.T) {
	world := testWorld(t)
	if got := world.Radius(); got != testWorldRadius {
		t.Fatalf("Radius() = %d, want %d", got, testWorldRadius)
	}
	if want := len(disc(testWorldRadius)); world.Len() != want {
		t.Fatalf("world holds %d hexes, want %d", world.Len(), want)
	}
	for _, coord := range disc(testWorldRadius) {
		if !world.Contains(coord) {
			t.Fatalf("world is missing %v", coord)
		}
	}
	if world.Contains(hexg.NewHex(testWorldRadius+1, 0)) {
		t.Fatal("world contains a hex beyond its radius")
	}
}

// The game origin is an ordinary hex. It has to exist, but the generator owes
// it no particular terrain.
func TestGenerateWorldHoldsTheGameOrigin(t *testing.T) {
	hex, ok := testWorld(t).At(hexg.NewHex(0, 0))
	if !ok {
		t.Fatal("world is missing the game origin")
	}
	if !hex.Terrain.Valid() {
		t.Fatalf("game origin is %q, which is not a terrain", hex.Terrain)
	}
}

// The point of the generator is variety: a world of nothing but grassland, or
// nothing but sea, would satisfy every other test here.
func TestGenerateWorldProducesEveryTerrain(t *testing.T) {
	counts := map[Terrain]int{}
	for _, hex := range testWorld(t).Hexes() {
		if !hex.Terrain.Valid() {
			t.Fatalf("hex %v has terrain %q", hex.Coord, hex.Terrain)
		}
		counts[hex.Terrain]++
	}
	for _, terrain := range Terrains() {
		if counts[terrain] == 0 {
			t.Errorf("world has no %q", terrain)
		}
	}
}

func TestGenerateWorldElevationMatchesTerrain(t *testing.T) {
	for _, hex := range testWorld(t).Hexes() {
		switch {
		case hex.Terrain.IsWater():
			if hex.Elevation >= 0 {
				t.Fatalf("%q at %v is %d m, want below sea level", hex.Terrain, hex.Coord, hex.Elevation)
			}
		case hex.Elevation < 0:
			t.Fatalf("%q at %v is %d m, want at or above sea level", hex.Terrain, hex.Coord, hex.Elevation)
		case hex.Terrain == TerrainMountains && hex.Elevation < hillsCeiling:
			t.Fatalf("mountains at %v are %d m, want at least %d", hex.Coord, hex.Elevation, hillsCeiling)
		case hex.Elevation > mountainsCeiling:
			t.Fatalf("%q at %v is %d m, above the highest ground", hex.Terrain, hex.Coord, hex.Elevation)
		}
	}
}

// A lake is water with no way out to sea. This is the property that makes it a
// lake rather than a bay, so it is worth asserting directly rather than
// trusting the flood fill that produced it.
func TestGenerateWorldLakesHaveNoOutletToTheSea(t *testing.T) {
	world := testWorld(t)

	lakes := 0
	for _, hex := range world.Hexes() {
		if hex.Terrain != TerrainLake {
			continue
		}
		lakes++
		if reachesRim(world, hex.Coord) {
			t.Fatalf("lake at %v reaches the edge of the world", hex.Coord)
		}
	}
	if lakes == 0 {
		t.Skip("this world generated no lakes")
	}
}

// Ocean is the other half of that claim: every ocean hex must reach the rim.
func TestGenerateWorldOceanReachesTheRim(t *testing.T) {
	world := testWorld(t)
	for _, hex := range world.Hexes() {
		if hex.Terrain == TerrainOcean && !reachesRim(world, hex.Coord) {
			t.Fatalf("ocean at %v does not reach the edge of the world", hex.Coord)
		}
	}
}

// reachesRim walks water from a hex and reports whether it can get out to the
// edge of the world. It repeats the flood fill independently of the generator's
// own so that a bug in that fill cannot also validate itself.
func reachesRim(world World, start hexg.Hex) bool {
	seen := map[hexg.Hex]bool{start: true}
	queue := []hexg.Hex{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Length() == world.Radius() {
			return true
		}
		for _, direction := range originDirections {
			neighbor := current.Add(direction)
			if seen[neighbor] {
				continue
			}
			hex, ok := world.At(neighbor)
			if !ok || !hex.Terrain.IsWater() {
				continue
			}
			seen[neighbor] = true
			queue = append(queue, neighbor)
		}
	}
	return false
}

// Coherence is the whole reason for the generator: neighbouring hexes should
// usually agree. Independent per-hex rolls over this terrain mix would agree
// only about a quarter of the time.
func TestGenerateWorldTerrainIsCoherent(t *testing.T) {
	world := testWorld(t)

	pairs, matching := 0, 0
	for _, hex := range world.Hexes() {
		for _, direction := range originDirections {
			neighbor, ok := world.At(hex.Coord.Add(direction))
			if !ok {
				continue
			}
			pairs++
			if neighbor.Terrain == hex.Terrain {
				matching++
			}
		}
	}
	if share := float64(matching) / float64(pairs); share < 0.60 {
		t.Fatalf("neighbouring hexes share terrain %.0f%% of the time, want at least 60%%", 100*share)
	}
}

func TestNewWorldRoundTripsHexes(t *testing.T) {
	generated := testWorld(t)
	rebuilt := NewWorld(generated.Radius(), generated.Hexes())

	if rebuilt.Radius() != generated.Radius() || rebuilt.Len() != generated.Len() {
		t.Fatalf("NewWorld = radius %d, %d hexes; want %d, %d",
			rebuilt.Radius(), rebuilt.Len(), generated.Radius(), generated.Len())
	}
	for _, hex := range generated.Hexes() {
		other, ok := rebuilt.At(hex.Coord)
		if !ok || other != hex {
			t.Fatalf("NewWorld lost or changed %v: %+v", hex.Coord, other)
		}
	}
}

func TestWorldIsLand(t *testing.T) {
	world := NewWorld(1, []Hex{
		{Coord: hexg.NewHex(0, 0), Terrain: TerrainGrassland},
		{Coord: hexg.NewHex(1, 0), Terrain: TerrainOcean, Elevation: -60},
	})
	for _, test := range []struct {
		name  string
		coord hexg.Hex
		want  bool
	}{
		{name: "land", coord: hexg.NewHex(0, 0), want: true},
		{name: "water", coord: hexg.NewHex(1, 0), want: false},
		{name: "outside the world", coord: hexg.NewHex(9, 0), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := world.IsLand(test.coord); got != test.want {
				t.Fatalf("IsLand(%v) = %v, want %v", test.coord, got, test.want)
			}
		})
	}
}

func TestGenerateWorldEmptyRadius(t *testing.T) {
	world := GenerateWorld(testSeeds(), -1)
	if world.Len() != 0 {
		t.Fatalf("GenerateWorld(-1) holds %d hexes, want 0", world.Len())
	}
	if got := GenerateWorld(testSeeds(), 0); got.Len() != 1 {
		t.Fatalf("GenerateWorld(0) holds %d hexes, want 1", got.Len())
	}
}

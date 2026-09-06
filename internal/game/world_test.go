// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"sync"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// The test world's half-extents: small enough to keep the suite quick, large
// enough to grow every feature the generator can produce.
//
// Terrain features are a fixed fraction of the world rather than a fixed number
// of hexes, so a very small test world is not a scale model of a real one - its
// continents come out only a few hexes across and nothing about their shape is
// representative.
const (
	testWorldWidth  = 60
	testWorldHeight = 30
)

var (
	sharedWorldOnce sync.Once
	sharedWorld     World
)

// testWorld returns a generated world shared by the package's tests. Generation
// is deterministic, so sharing one world between tests cannot couple them.
func testWorld(t *testing.T) World {
	t.Helper()
	sharedWorldOnce.Do(func() {
		var err error
		sharedWorld, err = GenerateWorld(testSeeds(), testWorldWidth, testWorldHeight)
		if err != nil {
			panic(err)
		}
	})
	return sharedWorld
}

// mustGenerate fails the test rather than returning an error, which keeps the
// generator's own assertions readable.
func mustGenerate(t *testing.T, seeds prng.Seeds, width, height int) World {
	t.Helper()
	world, err := GenerateWorld(seeds, width, height)
	if err != nil {
		t.Fatalf("GenerateWorld(%d, %d): %v", width, height, err)
	}
	return world
}

func TestGenerateWorldIsDeterministic(t *testing.T) {
	first := mustGenerate(t, testSeeds(), 8, 5)
	second := mustGenerate(t, testSeeds(), 8, 5)

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
	first := mustGenerate(t, testSeeds(), 8, 5)
	second := mustGenerate(t, prng.New(1, 2), 8, 5)

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

func TestGenerateWorldCoversTheRectangle(t *testing.T) {
	world := testWorld(t)
	if got, want := world.Columns(), 2*testWorldWidth+1; got != want {
		t.Fatalf("Columns() = %d, want %d", got, want)
	}
	if got, want := world.Rows(), 2*testWorldHeight+1; got != want {
		t.Fatalf("Rows() = %d, want %d", got, want)
	}
	if want := world.Columns() * world.Rows(); world.Len() != want {
		t.Fatalf("world holds %d hexes, want %d", world.Len(), want)
	}
	for r := -testWorldHeight; r <= testWorldHeight; r++ {
		for q := -testWorldWidth; q <= testWorldWidth; q++ {
			if !world.Contains(hexg.NewHex(q, r)) {
				t.Fatalf("world is missing (%d,%d)", q, r)
			}
		}
	}

	// East and west are not edges: a column past the window is the same hex
	// seen from the other side, and the world has it.
	if !world.Contains(hexg.NewHex(testWorldWidth+1, 0)) {
		t.Fatal("world does not wrap east")
	}
	if got, want := world.Normalize(hexg.NewHex(testWorldWidth+1, 0)), hexg.NewHex(-testWorldWidth, 0); got != want {
		t.Fatalf("one column past the east edge normalized to %v, want %v", got, want)
	}

	// North and south are walls.
	if world.Contains(hexg.NewHex(0, testWorldHeight+1)) {
		t.Fatal("world contains a row beyond the south pole")
	}
	if world.Contains(hexg.NewHex(0, -testWorldHeight-1)) {
		t.Fatal("world contains a row beyond the north pole")
	}
}

// The seam is the whole point of the change, and it is the one thing a test
// will not exercise by accident. Terrain either side of the meridian must be no
// less continuous than any other adjacent pair, or the noise is not periodic
// and the map carries a scar down the middle.
func TestGenerateWorldHasNoSeam(t *testing.T) {
	world := testWorld(t)
	cyl := world.Cylinder()

	// Elevation steps across the meridian, against the distribution of steps
	// everywhere else in the same rows.
	var seam, interior []int
	step := func(a, b hexg.Hex) int {
		left, okL := world.At(a)
		right, okR := world.At(b)
		if !okL || !okR {
			t.Fatalf("world is missing %v or %v", a, b)
		}
		d := left.Elevation - right.Elevation
		if d < 0 {
			d = -d
		}
		return d
	}
	for r := -testWorldHeight; r <= testWorldHeight; r++ {
		east := hexg.NewHex(testWorldWidth, r)
		seam = append(seam, step(east, cyl.Normalize(east.Neighbor(0))))
		for q := -testWorldWidth; q < testWorldWidth; q++ {
			interior = append(interior, step(hexg.NewHex(q, r), hexg.NewHex(q+1, r)))
		}
	}

	mean := func(xs []int) float64 {
		total := 0
		for _, x := range xs {
			total += x
		}
		return float64(total) / float64(len(xs))
	}
	seamMean, interiorMean := mean(seam), mean(interior)
	// A discontinuous seam is not subtle: an aperiodic field puts unrelated
	// terrain either side, so the mean step runs many times the interior's.
	if seamMean > 3*interiorMean {
		t.Fatalf("mean elevation step across the seam is %.0f m against %.0f m in the interior: the noise is not periodic",
			seamMean, interiorMean)
	}
}

// An ocean must not be cut in two at the meridian and half of it called a lake.
func TestGenerateWorldClassifiesWaterAcrossTheSeam(t *testing.T) {
	world := testWorld(t)
	cyl := world.Cylinder()
	for r := -testWorldHeight; r <= testWorldHeight; r++ {
		east := hexg.NewHex(testWorldWidth, r)
		west := cyl.Normalize(east.Neighbor(0))
		left, _ := world.At(east)
		right, _ := world.At(west)
		if left.Terrain == TerrainOcean && right.Terrain == TerrainLake {
			t.Fatalf("row %d: ocean at %v meets lake at %v across the seam", r, east, west)
		}
		if left.Terrain == TerrainLake && right.Terrain == TerrainOcean {
			t.Fatalf("row %d: lake at %v meets ocean at %v across the seam", r, east, west)
		}
	}
}

// A world too small to be a world is an error, not a panic and not an empty map.
func TestGenerateWorldRejectsDegenerateDimensions(t *testing.T) {
	for _, tc := range [][2]int{{0, 5}, {5, 0}, {-1, 5}, {5, -1}} {
		if _, err := GenerateWorld(testSeeds(), tc[0], tc[1]); err == nil {
			t.Errorf("GenerateWorld(%d, %d) returned no error", tc[0], tc[1])
		}
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
		// Ice reports the height of the ground under the sheet, which can be
		// anything from an abyssal trench to a mountain top. All that is
		// required of it is that it stays a height of this world.
		case hex.Terrain == TerrainIce:
			if hex.Elevation < -abyssDepth || hex.Elevation > mountainsCeiling {
				t.Fatalf("ice at %v is %d m, outside the world's range", hex.Coord, hex.Elevation)
			}
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

// The polar rows are sheets of ice, whatever the generator put there. This is
// the wall at the edge of the world, so it has to be the whole row and only
// those rows: a gap in it is a way out of the world.
func TestGenerateWorldFreezesThePoles(t *testing.T) {
	world := testWorld(t)
	for _, hex := range world.Hexes() {
		polar := hex.Coord.R() == -testWorldHeight || hex.Coord.R() == testWorldHeight
		if ice := hex.Terrain == TerrainIce; ice != polar {
			t.Fatalf("%v is %q, polar=%v", hex.Coord, hex.Terrain, polar)
		}
		if !polar {
			continue
		}
		if world.IsLand(hex.Coord) || world.IsWater(hex.Coord) {
			t.Fatalf("ice at %v reports as land or water", hex.Coord)
		}
		if world.IsPassable(hex.Coord) {
			t.Fatalf("ice at %v is passable", hex.Coord)
		}
	}
}

// The ice keeps the ground it was laid over rather than a made-up height, so a
// polar row reads as the terrain it covers: sea floor in some columns, high
// ground in others. A convention of one fixed number would show up here as
// every ice hex agreeing.
func TestGenerateWorldIceKeepsTheGroundBeneathIt(t *testing.T) {
	world := testWorld(t)
	above, below := 0, 0
	for _, hex := range world.Hexes() {
		if hex.Terrain != TerrainIce {
			continue
		}
		if hex.Elevation < 0 {
			below++
		} else {
			above++
		}
	}
	if above == 0 || below == 0 {
		t.Fatalf("polar ice is %d hexes above sea level and %d below: it is not reporting the ground under it",
			above, below)
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
		for direction := range 6 {
			neighbor, ok := world.At(world.Normalize(hex.Coord.Neighbor(direction)))
			if ok && neighbor.Terrain == TerrainOcean {
				t.Fatalf("lake at %v touches ocean at %v: it has an outlet to the sea",
					hex.Coord, neighbor.Coord)
			}
		}
	}
	if lakes == 0 {
		t.Skip("this world generated no lakes")
	}
}

// The sea is the other half of that claim. A cylinder has no rim to reach, so
// what makes water the sea is that it is one connected body - the largest one,
// or one that reaches a pole - rather than an enclosed pocket.
func TestGenerateWorldOceanIsOneConnectedSea(t *testing.T) {
	world := testWorld(t)

	var ocean []hexg.Hex
	water := 0
	for _, hex := range world.Hexes() {
		if hex.Terrain.IsWater() {
			water++
		}
		if hex.Terrain == TerrainOcean {
			ocean = append(ocean, hex.Coord)
		}
	}
	if len(ocean) == 0 {
		t.Skip("this world generated no ocean")
	}

	// A polar sea need not connect to the main one, so the sea is not always a
	// single body. What must hold is that no lake is bigger than the sea: a
	// lake is a pocket, and the moment one outgrows the ocean the fill has
	// mistaken the sea for a pocket, which is exactly what seeding from the
	// poles alone used to do.
	biggest := func(want Terrain) int {
		seen := make(map[hexg.Hex]bool)
		largest := 0
		for _, hex := range world.Hexes() {
			if hex.Terrain != want || seen[hex.Coord] {
				continue
			}
			size := 0
			seen[hex.Coord] = true
			queue := []hexg.Hex{hex.Coord}
			for len(queue) > 0 {
				current := queue[0]
				queue = queue[1:]
				size++
				for direction := range 6 {
					neighbor := world.Normalize(current.Neighbor(direction))
					if seen[neighbor] {
						continue
					}
					next, ok := world.At(neighbor)
					if !ok || next.Terrain != want {
						continue
					}
					seen[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
			largest = max(largest, size)
		}
		return largest
	}
	if sea, lake := biggest(TerrainOcean), biggest(TerrainLake); lake >= sea {
		t.Fatalf("largest lake is %d hexes against a sea of %d", lake, sea)
	}

	// The sea should be most of the water. If lakes outnumber it, the fill has
	// mistaken the ocean for a pocket, which is exactly what seeding from the
	// poles alone used to do.
	if share := float64(len(ocean)) / float64(water); share < 0.5 {
		t.Fatalf("ocean is %.0f%% of all water, want most of it", 100*share)
	}
}

// Coherence is the whole reason for the generator: neighbouring hexes should
// usually agree. Independent per-hex rolls over this terrain mix would agree
// only about a quarter of the time.
func TestGenerateWorldTerrainIsCoherent(t *testing.T) {
	world := testWorld(t)

	pairs, matching := 0, 0
	for _, hex := range world.Hexes() {
		for direction := range 6 {
			neighbor, ok := world.At(hex.Coord.Neighbor(direction))
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
	rebuilt, err := NewWorld(generated.Width(), generated.Height(), generated.Hexes())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	if rebuilt.Width() != generated.Width() || rebuilt.Height() != generated.Height() || rebuilt.Len() != generated.Len() {
		t.Fatalf("NewWorld = %dx%d, %d hexes; want %dx%d, %d",
			rebuilt.Width(), rebuilt.Height(), rebuilt.Len(),
			generated.Width(), generated.Height(), generated.Len())
	}
	for _, hex := range generated.Hexes() {
		other, ok := rebuilt.At(hex.Coord)
		if !ok || other != hex {
			t.Fatalf("NewWorld lost or changed %v: %+v", hex.Coord, other)
		}
	}
}

func TestWorldIsLand(t *testing.T) {
	world, err := NewWorld(1, 1, []Hex{
		{Coord: hexg.NewHex(0, 0), Terrain: TerrainGrassland},
		{Coord: hexg.NewHex(1, 0), Terrain: TerrainOcean, Elevation: -60},
		{Coord: hexg.NewHex(-1, 1), Terrain: TerrainIce, Elevation: 220},
	})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for _, test := range []struct {
		name  string
		coord hexg.Hex
		want  bool
	}{
		{name: "land", coord: hexg.NewHex(0, 0), want: true},
		{name: "water", coord: hexg.NewHex(1, 0), want: false},
		{name: "ice", coord: hexg.NewHex(-1, 1), want: false},
		// Rows are the only way out of a world; columns wrap back in.
		{name: "beyond a pole", coord: hexg.NewHex(0, 9), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := world.IsLand(test.coord); got != test.want {
				t.Fatalf("IsLand(%v) = %v, want %v", test.coord, got, test.want)
			}
		})
	}
}

// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"math"
	"sort"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// Field numbers for the generator's noise fields. They are part of the key path
// of every world draw, so they are a compatibility surface: never renumber one,
// and append rather than insert.
const (
	fieldElevation prng.Key = 1
	fieldWarpX     prng.Key = 2
	fieldWarpY     prng.Key = 3
	fieldMoisture  prng.Key = 4
	fieldWind      prng.Key = 5
)

// Generator constants. These are the knobs that decide what a world looks like.
const (
	// waterFraction is the share of the world below sea level. Sea level is
	// taken as a percentile of the elevation field rather than a fixed height,
	// so this fraction holds whatever shape the noise happens to take.
	waterFraction = 0.42

	// hillsRank and mountainsRank split land by elevation rank. Land below
	// hillsRank is lowland, land above mountainsRank is mountains.
	hillsRank     = 0.62
	mountainsRank = 0.88

	// Moisture ranks that turn lowland into wetter terrain, and the rank above
	// which hills grow forest instead of staying bare.
	forestRank      = 0.45
	marshRank       = 0.74
	forestHillsRank = 0.72

	// marshLandRank keeps marsh in genuinely low ground: a wet hex high on the
	// lowland band is forest, not swamp.
	marshLandRank = 0.22

	// Elevation in metres at the band boundaries. Terrain and elevation are
	// derived from one rank, so a hex can never claim to be mountains at 200 m.
	lowlandCeiling   = 400
	hillsCeiling     = 1200
	mountainsCeiling = 4200

	// Ocean depth in metres just off the coast, and in the abyss.
	shelfDepth = 60
	abyssDepth = 6000
	// shelfFalloff is how many hexes from land the abyss is approached over.
	shelfFalloff = 10.0
	// lakeDepth is the deepest an inland lake gets.
	lakeDepth = 90

	// Noise shape.
	elevationOctaves   = 5
	elevationFrequency = 0.055
	warpFrequency      = 0.018
	warpAmplitude      = 9.0
	moistureOctaves    = 4
	moistureFrequency  = 0.045

	// rimFraction is the outer share of the world's radius over which elevation
	// is pulled down, so a bounded world ends in open sea rather than at a wall.
	rimFraction = 0.16
	rimStrength = 1.30

	// Moisture transport.
	windBaselineRate = 0.16
	windOrographicK  = 2.40
	windEvaporation  = 0.22
	windInitialLoad  = 0.60
	// moistureNoiseShare is how much of a hex's moisture comes from the noise
	// field rather than from transported rainfall. Noise gives forests their
	// ragged edges; transport gives them a windward side.
	moistureNoiseShare = 0.45
)

// Hex is one generated hex of the world: its true map coordinate, its terrain,
// and its elevation in metres above sea level. Water hexes carry a negative
// elevation, so a lake bed and a hilltop are the same kind of number.
type Hex struct {
	Coord     hexg.Hex
	Terrain   Terrain
	Elevation int
}

// World is a generated map: every hex within radius of the game origin.
//
// A world is immutable once generated and is the game's terrain of record. It
// is generated exactly once, when the database is created, and persisted; the
// generator's global passes cannot be evaluated one hex at a time, so terrain
// is looked up rather than recomputed.
type World struct {
	radius int
	hexes  map[hexg.Hex]Hex
}

// NewWorld returns a world holding the given hexes. It is how a world is
// rebuilt from storage.
func NewWorld(radius int, hexes []Hex) World {
	world := World{radius: radius, hexes: make(map[hexg.Hex]Hex, len(hexes))}
	for _, hex := range hexes {
		world.hexes[hex.Coord] = hex
	}
	return world
}

// Radius returns how far the world reaches from the game origin.
func (w World) Radius() int { return w.radius }

// Len returns how many hexes the world holds.
func (w World) Len() int { return len(w.hexes) }

// At returns the hex at a true map coordinate.
func (w World) At(coord hexg.Hex) (Hex, bool) {
	hex, ok := w.hexes[coord]
	return hex, ok
}

// Contains reports whether a true map coordinate is part of the world.
func (w World) Contains(coord hexg.Hex) bool {
	_, ok := w.hexes[coord]
	return ok
}

// IsLand reports whether a true map coordinate is a land hex of the world. A
// coordinate outside the world is not land.
func (w World) IsLand(coord hexg.Hex) bool {
	hex, ok := w.hexes[coord]
	return ok && !hex.Terrain.IsWater()
}

// Hexes returns every hex, ordered by q and then r, so that anything derived
// from a world is byte-for-byte stable across runs.
func (w World) Hexes() []Hex {
	hexes := make([]Hex, 0, len(w.hexes))
	for _, coord := range disc(w.radius) {
		if hex, ok := w.hexes[coord]; ok {
			hexes = append(hexes, hex)
		}
	}
	return hexes
}

// GenerateWorld builds the world for a set of game seeds.
//
// The passes run in a fixed order over a fixed, sorted set of coordinates, and
// every draw is addressed rather than consumed, so the same seeds and radius
// produce the same world on any machine. Unlike a per-hex rule, the passes are
// global by necessity: sea level is a percentile of the whole field, an inland
// lake is only inland relative to the whole coastline, and a rain shadow only
// exists downwind of something.
func GenerateWorld(seeds prng.Seeds, radius int) World {
	coords := disc(radius)
	world := World{radius: radius, hexes: make(map[hexg.Hex]Hex, len(coords))}
	if len(coords) == 0 {
		return world
	}

	raw := rawElevation(seeds, coords, radius)
	seaLevel := percentile(values(coords, raw), waterFraction)

	water := make(map[hexg.Hex]bool, len(coords))
	land := make([]hexg.Hex, 0, len(coords))
	for _, coord := range coords {
		if raw[coord] <= seaLevel {
			water[coord] = true
			continue
		}
		land = append(land, coord)
	}

	ocean := reachableFromRim(coords, water, radius)
	depth := distanceToLand(coords, water)
	landRank := rankOf(land, raw)
	moistureRank := rankOf(coords, moisture(seeds, coords, raw, water, radius))

	for _, coord := range coords {
		var hex Hex
		switch {
		case water[coord] && ocean[coord]:
			hex = Hex{Coord: coord, Terrain: TerrainOcean, Elevation: oceanElevation(depth[coord])}
		case water[coord]:
			hex = Hex{Coord: coord, Terrain: TerrainLake, Elevation: lakeElevation(depth[coord])}
		default:
			terrain, elevation := landTerrain(landRank[coord], moistureRank[coord])
			hex = Hex{Coord: coord, Terrain: terrain, Elevation: elevation}
		}
		world.hexes[coord] = hex
	}
	return world
}

// rawElevation evaluates the warped elevation field, pulled down at the rim.
//
// No hex is special-cased, the game origin included: it gets whatever the field
// gives it, the same as everywhere else.
func rawElevation(seeds prng.Seeds, coords []hexg.Hex, radius int) map[hexg.Hex]float64 {
	elevation := newNoiseField(seeds, fieldElevation)
	warpX := newNoiseField(seeds, fieldWarpX)
	warpY := newNoiseField(seeds, fieldWarpY)
	layout := generatorLayout()

	raw := make(map[hexg.Hex]float64, len(coords))
	for _, coord := range coords {
		point := layout.HexToPixel(coord)
		// Domain warp bends the field before it is sampled, which is what turns
		// the smooth blobs of plain noise into coastlines with inlets and
		// peninsulas.
		x := point.X + warpX.at(point.X*warpFrequency, point.Y*warpFrequency)*warpAmplitude
		y := point.Y + warpY.at(point.X*warpFrequency, point.Y*warpFrequency)*warpAmplitude

		value := elevation.fbm(x, y, elevationOctaves, elevationFrequency)
		value -= rim(coord, radius)
		raw[coord] = value
	}
	return raw
}

// rim is how much the edge of a bounded world is pulled under water. It is zero
// across the interior and rises smoothly through the outermost rimFraction of
// the radius.
func rim(coord hexg.Hex, radius int) float64 {
	if radius <= 0 {
		return 0
	}
	distance := float64(coord.Length()) / float64(radius)
	start := 1 - rimFraction
	if distance <= start {
		return 0
	}
	return rimStrength * smoothstep(normalize(distance, start, 1))
}

// moisture returns each hex's rainfall, mixing a coherent noise field with
// moisture carried across the world by a prevailing wind.
//
// Air enters the upwind edge with a fixed load, gains moisture over water, and
// drops it as rain — much harder when climbing. What it drops it no longer
// carries, so the far side of a range is dry. That is the whole of the rain
// shadow, and it is why hexes have to be visited in wind order.
func moisture(seeds prng.Seeds, coords []hexg.Hex, raw map[hexg.Hex]float64, water map[hexg.Hex]bool, radius int) map[hexg.Hex]float64 {
	noise := newNoiseField(seeds, fieldMoisture)
	layout := generatorLayout()

	direction := seeds.Roller(prng.TagWorld, fieldWind).RollRange(0, len(originDirections)-1)
	wind := originDirections[direction]
	windPoint := layout.HexToPixel(wind)

	// Downwind order. A hex's upwind neighbour always projects strictly less
	// along the wind axis, so it is always already computed when we reach it.
	order := make([]hexg.Hex, len(coords))
	copy(order, coords)
	projection := func(coord hexg.Hex) float64 {
		point := layout.HexToPixel(coord)
		return point.X*windPoint.X + point.Y*windPoint.Y
	}
	sort.SliceStable(order, func(i, j int) bool {
		return projection(order[i]) < projection(order[j])
	})

	// Elevation normalized across the world, so "climbing" means the same
	// thing everywhere regardless of the field's absolute range.
	low, high := math.Inf(1), math.Inf(-1)
	for _, value := range raw {
		low, high = min(low, value), max(high, value)
	}

	carried := make(map[hexg.Hex]float64, len(coords))
	rainfall := make(map[hexg.Hex]float64, len(coords))
	for _, coord := range order {
		upwind := coord.Subtract(wind)
		load, ok := carried[upwind]
		if !ok {
			load = windInitialLoad
		}
		if water[coord] {
			load = min(1, load+windEvaporation)
		}
		rise := max(0, normalize(raw[coord], low, high)-normalize(raw[upwind], low, high))
		rain := load * clamp(windBaselineRate+windOrographicK*rise, 0, 1)
		carried[coord] = load - rain
		point := layout.HexToPixel(coord)
		texture := (noise.fbm(point.X, point.Y, moistureOctaves, moistureFrequency) + 1) / 2
		rainfall[coord] = moistureNoiseShare*texture + (1-moistureNoiseShare)*rain
	}
	return rainfall
}

// landTerrain turns a land hex's elevation rank and moisture rank into terrain
// and an elevation in metres. Both come from the same rank, so they always
// agree with each other.
func landTerrain(landRank, moistureRank float64) (Terrain, int) {
	switch {
	case landRank >= mountainsRank:
		return TerrainMountains, band(landRank, mountainsRank, 1, hillsCeiling, mountainsCeiling)
	case landRank >= hillsRank:
		elevation := band(landRank, hillsRank, mountainsRank, lowlandCeiling, hillsCeiling)
		if moistureRank >= forestHillsRank {
			return TerrainForest, elevation
		}
		return TerrainHills, elevation
	default:
		elevation := band(landRank, 0, hillsRank, 0, lowlandCeiling)
		switch {
		case moistureRank >= marshRank && landRank < marshLandRank:
			return TerrainMarsh, elevation
		case moistureRank >= forestRank:
			return TerrainForest, elevation
		default:
			return TerrainGrassland, elevation
		}
	}
}

// band maps a rank within [lowRank, highRank] linearly onto [low, high] metres.
func band(rank, lowRank, highRank float64, low, high int) int {
	return low + int(math.Round(normalize(rank, lowRank, highRank)*float64(high-low)))
}

// oceanElevation grades the sea floor by how far a hex is from land: a shelf at
// the coast falling away to the abyss offshore.
func oceanElevation(distance int) int {
	if distance <= 0 {
		distance = 1
	}
	deepening := 1 - math.Exp(-float64(distance-1)/shelfFalloff)
	return -int(math.Round(shelfDepth + (abyssDepth-shelfDepth)*deepening))
}

// lakeElevation grades a lake bed the same way, over a far shallower range.
func lakeElevation(distance int) int {
	if distance <= 0 {
		distance = 1
	}
	deepening := 1 - math.Exp(-float64(distance-1)/2)
	return -int(math.Round(5 + (lakeDepth-5)*deepening))
}

// reachableFromRim returns the water hexes connected to the edge of the world.
// Those are the ocean; water that the flood fill never reaches has no outlet to
// the sea and is a lake.
func reachableFromRim(coords []hexg.Hex, water map[hexg.Hex]bool, radius int) map[hexg.Hex]bool {
	reached := make(map[hexg.Hex]bool, len(coords))
	queue := make([]hexg.Hex, 0, len(coords))
	for _, coord := range coords {
		if water[coord] && coord.Length() == radius {
			reached[coord] = true
			queue = append(queue, coord)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for direction := range len(originDirections) {
			neighbor := current.Add(originDirections[direction])
			if water[neighbor] && !reached[neighbor] {
				reached[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}
	return reached
}

// distanceToLand returns, for every water hex, how many hexes it lies from the
// nearest land. Land hexes are distance zero.
func distanceToLand(coords []hexg.Hex, water map[hexg.Hex]bool) map[hexg.Hex]int {
	distance := make(map[hexg.Hex]int, len(coords))
	queue := make([]hexg.Hex, 0, len(coords))
	for _, coord := range coords {
		if !water[coord] {
			distance[coord] = 0
			queue = append(queue, coord)
		}
	}
	inWorld := make(map[hexg.Hex]struct{}, len(coords))
	for _, coord := range coords {
		inWorld[coord] = struct{}{}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for direction := range len(originDirections) {
			neighbor := current.Add(originDirections[direction])
			if _, ok := inWorld[neighbor]; !ok {
				continue
			}
			if _, seen := distance[neighbor]; seen {
				continue
			}
			distance[neighbor] = distance[current] + 1
			queue = append(queue, neighbor)
		}
	}
	// A world with no land at all leaves every hex unvisited; treat it as
	// uniformly deep rather than dividing the map into accidental shelves.
	for _, coord := range coords {
		if _, ok := distance[coord]; !ok {
			distance[coord] = len(coords)
		}
	}
	return distance
}

// rankOf returns each coordinate's percentile rank among the given coordinates,
// from 0 for the lowest to 1 for the highest. Ranking rather than thresholding
// raw values is what keeps the share of mountains, or of marsh, the same from
// one set of seeds to the next.
func rankOf(coords []hexg.Hex, value map[hexg.Hex]float64) map[hexg.Hex]float64 {
	rank := make(map[hexg.Hex]float64, len(coords))
	if len(coords) == 0 {
		return rank
	}
	if len(coords) == 1 {
		rank[coords[0]] = 1
		return rank
	}
	sorted := make([]hexg.Hex, len(coords))
	copy(sorted, coords)
	// Ties break on coordinate so the ranking is a function of the world, not
	// of the sort's happenstance.
	sort.SliceStable(sorted, func(i, j int) bool {
		if value[sorted[i]] != value[sorted[j]] {
			return value[sorted[i]] < value[sorted[j]]
		}
		if sorted[i].Q() != sorted[j].Q() {
			return sorted[i].Q() < sorted[j].Q()
		}
		return sorted[i].R() < sorted[j].R()
	})
	for index, coord := range sorted {
		rank[coord] = float64(index) / float64(len(sorted)-1)
	}
	return rank
}

// percentile returns the value at a fraction of the way through a sample.
func percentile(sample []float64, fraction float64) float64 {
	if len(sample) == 0 {
		return 0
	}
	sorted := make([]float64, len(sample))
	copy(sorted, sample)
	sort.Float64s(sorted)
	index := int(clamp(fraction, 0, 1) * float64(len(sorted)-1))
	return sorted[index]
}

func values(coords []hexg.Hex, value map[hexg.Hex]float64) []float64 {
	sample := make([]float64, 0, len(coords))
	for _, coord := range coords {
		sample = append(sample, value[coord])
	}
	return sample
}

// generatorLayout is the layout used to place hexes in the plane for noise
// sampling. It is a generation detail and has nothing to do with how a map is
// eventually drawn: the size is 1 so that noise frequencies are expressed in
// hexes, and it must never change, because it is part of what a set of seeds
// means.
func generatorLayout() hexg.Layout {
	return hexg.NewLayout(hexg.EvenQ, hexg.Point{X: 1, Y: 1}, hexg.Point{})
}

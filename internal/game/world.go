// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"errors"
	"math"
	"sort"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/cylinder"
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

	// Noise shape, expressed as how many periods of each field span the world
	// rather than as a frequency per hex.
	//
	// Per-hex frequencies do not survive a change of world size: the values
	// tuned for a 61-hex disc put six elevation features across the whole of
	// it, and the same numbers on a 511-hex world put forty-nine, which is
	// archipelago soup rather than geography. It also fragments the sea into
	// dozens of separate bodies, most of which are then not the largest and get
	// called lakes.
	//
	// Counting periods instead makes the world's shape scale with the world.
	// These are the counts the disc actually had, so a world looks like the
	// worlds the constants below were tuned against, at any size.
	//
	// A whole number of periods is also what lets the field wrap at all: a
	// period that did not divide the world evenly would not line up with itself
	// at the meridian.
	elevationOctaves = 5
	elevationPeriods = 6
	warpPeriods      = 2
	warpAmplitude    = 9.0
	moistureOctaves  = 4
	moisturePeriods  = 5

	// Moisture transport.
	windBaselineRate = 0.16
	windOrographicK  = 2.40
	windEvaporation  = 0.22
	windInitialLoad  = 0.60
	// moistureNoiseShare is how much of a hex's moisture comes from the noise
	// field rather than from transported rainfall. Noise gives forests their
	// ragged edges; transport gives them a windward side.
	moistureNoiseShare = 0.45
	// windLaps is how many times a purely east-west wind circles the world
	// before its rainfall is recorded. Such a wind has no upwind edge to enter
	// from - it blows around forever - so the first lap starts from an
	// invented load and the later ones wash it out. A wind with any
	// north-south component enters at a pole and needs one pass only.
	windLaps = 3
)

// ErrWorldTooSmall reports world half-extents that cannot make a world.
var ErrWorldTooSmall = errors.New("a world needs a positive width and height")

// frame is the shape of a world: a rectangle of 2*width+1 columns by
// 2*height+1 rows, wrapping east-west, walled north and south.
//
// It exists so the generator and the finished world index hexes the same way.
// A rectangle has a dense index, which a disc did not, and that is worth a type
// of its own: the previous generator carried seven maps keyed by hex over every
// coordinate at once, which cost roughly four hundred bytes a hex and put a
// ceiling on how large a world the server could build.
type frame struct {
	width, height int
	cyl           cylinder.Cylinder
}

func newFrame(width, height int) (frame, error) {
	if width < 1 || height < 1 {
		return frame{}, ErrWorldTooSmall
	}
	cyl, err := cylinder.New(2*width + 1)
	if err != nil {
		return frame{}, err
	}
	return frame{width: width, height: height, cyl: cyl}, nil
}

func (f frame) columns() int { return 2*f.width + 1 }
func (f frame) rows() int    { return 2*f.height + 1 }
func (f frame) size() int    { return f.columns() * f.rows() }

// contains reports whether a coordinate is a hex of the world. Only the row can
// fall outside: every column wraps back in.
func (f frame) contains(coord hexg.Hex) bool {
	return coord.R() >= -f.height && coord.R() <= f.height
}

// index is the dense array position of a canonical coordinate.
func (f frame) index(coord hexg.Hex) int {
	return (coord.R()+f.height)*f.columns() + (coord.Q() + f.width)
}

// lookup normalizes a coordinate and returns its index, reporting whether it is
// a hex of the world at all.
func (f frame) lookup(coord hexg.Hex) (int, bool) {
	canonical := f.cyl.Normalize(coord)
	if !f.contains(canonical) {
		return 0, false
	}
	return f.index(canonical), true
}

// coords returns every hex of the world, row by row from north to south and
// west to east within a row. Everything derived from a world is built in this
// order, so a world is byte-for-byte stable across runs.
func (f frame) coords() []hexg.Hex {
	coords := make([]hexg.Hex, 0, f.size())
	for r := -f.height; r <= f.height; r++ {
		for q := -f.width; q <= f.width; q++ {
			coords = append(coords, hexg.NewHex(q, r))
		}
	}
	return coords
}

// spanX is how far the world reaches east-west in the generator's plane. The
// noise has to repeat over exactly this distance.
func (f frame) spanX() float64 {
	return float64(f.columns()) * math.Sqrt(3)
}

// frequencyFor turns a count of periods across the world into the noise
// frequency that produces exactly that many, which is what makes the field
// repeat over the world's circumference and so wrap without a seam.
func frequencyFor(spanX float64, periods int) float64 {
	return float64(max(1, periods)) / spanX
}

// Hex is one generated hex of the world: its map coordinate, its terrain, and
// its elevation in metres above sea level. Water hexes carry a negative
// elevation, so a lake bed and a hilltop are the same kind of number.
type Hex struct {
	Coord     hexg.Hex
	Terrain   Terrain
	Elevation int
}

// World is the game's terrain of record.
//
// A world is immutable once generated. It is generated exactly once, when the
// database is created, and persisted; the generator's global passes cannot be
// evaluated one hex at a time, so terrain is looked up rather than recomputed.
type World struct {
	frame
	hexes []Hex
}

// NewWorld returns a world holding the given hexes. It is how a world is
// rebuilt from storage.
func NewWorld(width, height int, hexes []Hex) (World, error) {
	f, err := newFrame(width, height)
	if err != nil {
		return World{}, err
	}
	world := World{frame: f, hexes: make([]Hex, f.size())}
	for _, hex := range hexes {
		if index, ok := f.lookup(hex.Coord); ok {
			world.hexes[index] = hex
		}
	}
	return world, nil
}

// Width returns how many columns the world reaches either side of the origin.
func (w World) Width() int { return w.width }

// Height returns how many rows the world reaches above and below the origin.
func (w World) Height() int { return w.height }

// Columns returns the world's total column count.
func (w World) Columns() int { return w.columns() }

// Rows returns the world's total row count.
func (w World) Rows() int { return w.rows() }

// Cylinder returns the world's topology, for callers that need wrapped
// distance or a canonical coordinate.
func (w World) Cylinder() cylinder.Cylinder { return w.cyl }

// Len returns how many hexes the world holds.
func (w World) Len() int { return len(w.hexes) }

// Normalize returns the canonical name of a coordinate.
func (w World) Normalize(coord hexg.Hex) hexg.Hex { return w.cyl.Normalize(coord) }

// At returns the hex at a map coordinate, wrapping the column first.
func (w World) At(coord hexg.Hex) (Hex, bool) {
	index, ok := w.lookup(coord)
	if !ok {
		return Hex{}, false
	}
	return w.hexes[index], true
}

// Contains reports whether a coordinate is part of the world. Only rows can
// fall outside it.
func (w World) Contains(coord hexg.Hex) bool {
	_, ok := w.lookup(coord)
	return ok
}

// IsLand reports whether a coordinate is a land hex of the world. A coordinate
// outside the world is not land.
func (w World) IsLand(coord hexg.Hex) bool {
	hex, ok := w.At(coord)
	return ok && !hex.Terrain.IsWater()
}

// Hexes returns every hex in the world's canonical order.
func (w World) Hexes() []Hex {
	hexes := make([]Hex, len(w.hexes))
	copy(hexes, w.hexes)
	return hexes
}

// GenerateWorld builds the world for a set of game seeds.
//
// The passes run in a fixed order over a fixed set of coordinates, and every
// draw is addressed rather than consumed, so the same seeds and dimensions
// produce the same world on any machine. Unlike a per-hex rule, the passes are
// global by necessity: sea level is a percentile of the whole field, an inland
// lake is only inland relative to the whole coastline, and a rain shadow only
// exists downwind of something.
//
// Every pass wraps east-west. A pass that did not would draw its own seam down
// the meridian even over perfectly periodic noise - an ocean split into an
// ocean and a spurious lake, or a coastal hex reading as abyssal.
func GenerateWorld(seeds prng.Seeds, width, height int) (World, error) {
	f, err := newFrame(width, height)
	if err != nil {
		return World{}, err
	}
	world := World{frame: f, hexes: make([]Hex, f.size())}

	raw := f.rawElevation(seeds)
	seaLevel := percentile(raw, waterFraction)

	water := make([]bool, f.size())
	land := make([]int, 0, f.size())
	for index, value := range raw {
		if value <= seaLevel {
			water[index] = true
			continue
		}
		land = append(land, index)
	}

	ocean := f.seaWater(water)
	depth := f.distanceToLand(water)
	landRank := f.rankOf(land, raw)
	moistureRank := f.rankOf(nil, f.moisture(seeds, raw, water))

	for _, coord := range f.coords() {
		index := f.index(coord)
		switch {
		case water[index] && ocean[index]:
			world.hexes[index] = Hex{Coord: coord, Terrain: TerrainOcean, Elevation: oceanElevation(depth[index])}
		case water[index]:
			world.hexes[index] = Hex{Coord: coord, Terrain: TerrainLake, Elevation: lakeElevation(depth[index])}
		default:
			terrain, elevation := landTerrain(landRank[index], moistureRank[index])
			world.hexes[index] = Hex{Coord: coord, Terrain: terrain, Elevation: elevation}
		}
	}
	return world, nil
}

// rawElevation evaluates the warped elevation field.
//
// No hex is special-cased, the game origin included: it gets whatever the field
// gives it, the same as everywhere else. Nothing pulls the poles under water
// either - the north and south rows are the edge of the world as a game rule,
// not as a shape the terrain has to apologise for.
func (f frame) rawElevation(seeds prng.Seeds) []float64 {
	elevation := newNoiseField(seeds, fieldElevation)
	warpX := newNoiseField(seeds, fieldWarpX)
	warpY := newNoiseField(seeds, fieldWarpY)
	layout := generatorLayout()

	span := f.spanX()
	elevationFreq := frequencyFor(span, elevationPeriods)
	warpFreq := frequencyFor(span, warpPeriods)

	raw := make([]float64, f.size())
	for _, coord := range f.coords() {
		point := layout.HexToPixel(coord)
		// Domain warp bends the field before it is sampled, which is what turns
		// the smooth blobs of plain noise into coastlines with inlets and
		// peninsulas. The warp fields wrap too, so the bend is continuous
		// across the meridian rather than merely similar on both sides.
		wx := point.X * warpFreq
		wy := point.Y * warpFreq
		x := point.X + warpX.at(wx, wy, warpPeriods)*warpAmplitude
		y := point.Y + warpY.at(wx, wy, warpPeriods)*warpAmplitude

		raw[f.index(coord)] = elevation.fbm(x, y, elevationOctaves, elevationFreq, elevationPeriods)
	}
	return raw
}

// moisture returns each hex's rainfall, mixing a coherent noise field with
// moisture carried across the world by a prevailing wind.
//
// Air gains moisture over water and drops it as rain - much harder when
// climbing. What it drops it no longer carries, so the far side of a range is
// dry. That is the whole of the rain shadow, and it is why hexes have to be
// visited in wind order.
func (f frame) moisture(seeds prng.Seeds, raw []float64, water []bool) []float64 {
	noise := newNoiseField(seeds, fieldMoisture)
	layout := generatorLayout()
	moistureFreq := frequencyFor(f.spanX(), moisturePeriods)

	wind := hexg.DirectionVector(seeds.Roller(prng.TagWorld, fieldWind).RollRange(0, 5))
	order := f.windOrder(wind)

	// A wind with a north-south component enters at a pole and leaves at the
	// other, so one pass in downwind order is exact. A purely east-west wind
	// never enters or leaves: it circles the same row forever, so there is no
	// upwind edge to start from and the load has to be spun up instead.
	laps := 1
	if wind.R() == 0 {
		laps = windLaps
	}

	// Elevation normalized across the world, so "climbing" means the same
	// thing everywhere regardless of the field's absolute range.
	low, high := math.Inf(1), math.Inf(-1)
	for _, value := range raw {
		low, high = min(low, value), max(high, value)
	}

	carried := make([]float64, f.size())
	seeded := make([]bool, f.size())
	rainfall := make([]float64, f.size())

	for lap := range laps {
		for _, coord := range order {
			index := f.index(coord)

			load := windInitialLoad
			if upwind, ok := f.lookup(coord.Subtract(wind)); ok && seeded[upwind] {
				load = carried[upwind]
			}
			if water[index] {
				load = min(1, load+windEvaporation)
			}

			upwindRaw := raw[index]
			if upwind, ok := f.lookup(coord.Subtract(wind)); ok {
				upwindRaw = raw[upwind]
			}
			rise := max(0, normalize(raw[index], low, high)-normalize(upwindRaw, low, high))

			rain := load * clamp(windBaselineRate+windOrographicK*rise, 0, 1)
			carried[index] = load - rain
			seeded[index] = true

			if lap == laps-1 {
				point := layout.HexToPixel(coord)
				texture := (noise.fbm(point.X, point.Y, moistureOctaves, moistureFreq, moisturePeriods) + 1) / 2
				rainfall[index] = moistureNoiseShare*texture + (1-moistureNoiseShare)*rain
			}
		}
	}
	return rainfall
}

// windOrder returns the world's hexes in an order where a hex's upwind
// neighbour comes first.
//
// Rows do not wrap, so a wind with a north-south component gives a strict
// ordering by row: its upwind neighbour is always one row back. Within a row
// the order follows the wind so that a purely east-west wind, which has no such
// ordering, still spends only its first hex on an invented load.
func (f frame) windOrder(wind hexg.Hex) []hexg.Hex {
	rows := make([]int, 0, f.rows())
	for r := -f.height; r <= f.height; r++ {
		rows = append(rows, r)
	}
	if wind.R() < 0 {
		// Blowing north: the upwind neighbour is to the south, so go south first.
		sort.Sort(sort.Reverse(sort.IntSlice(rows)))
	}

	coords := make([]hexg.Hex, 0, f.size())
	for _, r := range rows {
		for i := range f.columns() {
			q := -f.width + i
			if wind.Q() < 0 {
				q = f.width - i
			}
			coords = append(coords, hexg.NewHex(q, r))
		}
	}
	return coords
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

// seaWater returns the water hexes that belong to the sea rather than to an
// inland lake.
//
// The rule used to be "water reachable from the rim", which worked because a
// disc's whole boundary was rim. A cylinder has no eastern or western edge at
// all, and only two rows of polar edge, so almost no sea reaches one - seeding
// the fill from the poles alone would classify nearly the whole ocean as an
// inland lake.
//
// What "lake" is meant to mean survives the change of shape: water with no
// outlet to the sea. So the sea is the largest connected body of water, plus
// anything that reaches a pole, and a lake is whatever is left over.
func (f frame) seaWater(water []bool) []bool {
	sea := make([]bool, f.size())
	component := make([]int, 0, 64)
	visited := make([]bool, f.size())

	var largest []int
	for _, start := range f.coords() {
		first := f.index(start)
		if visited[first] || !water[first] {
			continue
		}

		component = component[:0]
		polar := false
		visited[first] = true
		queue := []hexg.Hex{start}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			component = append(component, f.index(current))
			if current.R() == -f.height || current.R() == f.height {
				polar = true
			}
			for direction := range 6 {
				neighbor := f.cyl.Normalize(current.Neighbor(direction))
				index, ok := f.lookup(neighbor)
				if !ok || visited[index] || !water[index] {
					continue
				}
				visited[index] = true
				queue = append(queue, neighbor)
			}
		}

		if polar {
			for _, index := range component {
				sea[index] = true
			}
			continue
		}
		if len(component) > len(largest) {
			largest = append(largest[:0], component...)
		}
	}
	for _, index := range largest {
		sea[index] = true
	}
	return sea
}

// distanceToLand returns, for every water hex, how many hexes it lies from the
// nearest land. Land hexes are distance zero.
func (f frame) distanceToLand(water []bool) []int {
	distance := make([]int, f.size())
	seen := make([]bool, f.size())
	queue := make([]hexg.Hex, 0, f.size())
	for _, coord := range f.coords() {
		index := f.index(coord)
		if !water[index] {
			seen[index] = true
			queue = append(queue, coord)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for direction := range 6 {
			neighbor := current.Neighbor(direction)
			index, ok := f.lookup(neighbor)
			if !ok || seen[index] {
				continue
			}
			seen[index] = true
			distance[index] = distance[f.index(current)] + 1
			queue = append(queue, f.cyl.Normalize(neighbor))
		}
	}
	// A world with no land at all leaves every hex unvisited; treat it as
	// uniformly deep rather than dividing the map into accidental shelves.
	for index := range distance {
		if !seen[index] {
			distance[index] = f.size()
		}
	}
	return distance
}

// rankOf returns each hex's percentile rank among the given subset, from 0 for
// the lowest to 1 for the highest. A nil subset ranks the whole world.
//
// Ranking rather than thresholding raw values is what keeps the share of
// mountains, or of marsh, the same from one set of seeds to the next.
func (f frame) rankOf(subset []int, value []float64) []float64 {
	rank := make([]float64, f.size())
	indices := subset
	if indices == nil {
		indices = make([]int, f.size())
		for i := range indices {
			indices[i] = i
		}
	}
	switch len(indices) {
	case 0:
		return rank
	case 1:
		rank[indices[0]] = 1
		return rank
	}
	sorted := make([]int, len(indices))
	copy(sorted, indices)
	// Ties break on array position, which encodes row then column, so the
	// ranking is a function of the world rather than of the sort's happenstance.
	sort.SliceStable(sorted, func(i, j int) bool {
		if value[sorted[i]] != value[sorted[j]] {
			return value[sorted[i]] < value[sorted[j]]
		}
		return sorted[i] < sorted[j]
	})
	for position, index := range sorted {
		rank[index] = float64(position) / float64(len(sorted)-1)
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

// generatorLayout is the layout used to place hexes in the plane for noise
// sampling. It is a generation detail and has nothing to do with how a map is
// eventually drawn: the size is 1 so that noise frequencies are expressed in
// hexes, and it must never change, because it is part of what a set of seeds
// means.
func generatorLayout() hexg.Layout {
	return hexg.NewLayout(hexg.EvenR, hexg.Point{X: 1, Y: 1}, hexg.Point{})
}

// Copyright (c) 2026 Michael D Henderson.

package game

// Race identifies the people a faction is drawn from. It is required faction
// metadata and defaults to RaceHuman.
//
// Race decides only where a faction is seated. It selects which candidate pool
// placement draws from and which spacing limit applies to an origin already
// taken; nothing else in the game reads it yet.
//
// Race is deliberately not part of any PRNG key path. Addressing a stream by it
// would move every golden vector, and it would make changing a faction's race
// rewrite where that faction already stands.
type Race string

const (
	RaceHuman    Race = "human"
	RaceElf      Race = "elf"
	RaceDwarf    Race = "dwarf"
	RaceOrc      Race = "orc"
	RaceKobold   Race = "kobold"
	RaceHalfling Race = "halfling"
)

// DefaultRace is the race a faction takes when none is chosen.
const DefaultRace = RaceHuman

// raceTerrainOrder is each race's preference over the land terrains, most
// favored first.
//
// An order is total: it covers every land terrain exactly once. A race that
// refused a terrain outright would give placement a fourth way to fail, and no
// game rule asks for one.
var raceTerrainOrder = map[Race][]Terrain{
	RaceHuman:    {TerrainGrassland, TerrainForest, TerrainHills, TerrainMarsh, TerrainMountains},
	RaceElf:      {TerrainForest, TerrainHills, TerrainGrassland, TerrainMarsh, TerrainMountains},
	RaceDwarf:    {TerrainMountains, TerrainHills, TerrainForest, TerrainGrassland, TerrainMarsh},
	RaceOrc:      {TerrainHills, TerrainMountains, TerrainMarsh, TerrainGrassland, TerrainForest},
	RaceKobold:   {TerrainMountains, TerrainHills, TerrainMarsh, TerrainForest, TerrainGrassland},
	RaceHalfling: {TerrainGrassland, TerrainHills, TerrainForest, TerrainMarsh, TerrainMountains},
}

// Races lists every race a faction may take, in the order the product
// reference gives them.
func Races() []Race {
	return []Race{RaceHuman, RaceElf, RaceDwarf, RaceOrc, RaceKobold, RaceHalfling}
}

// Valid reports whether the race is one this game knows.
func (r Race) Valid() bool {
	_, ok := raceTerrainOrder[r]
	return ok
}

// TerrainOrder returns the race's land terrains, most favored first. An
// unregistered race has no order and returns nil, which places nobody.
func (r Race) TerrainOrder() []Terrain {
	order := raceTerrainOrder[r]
	return append(make([]Terrain, 0, len(order)), order...)
}

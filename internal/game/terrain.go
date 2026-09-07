// Copyright (c) 2026 Michael D Henderson.

package game

// Terrain identifies the terrain generated for a map hex.
type Terrain string

const (
	TerrainGrassland Terrain = "grassland"
	TerrainForest    Terrain = "forest"
	TerrainHills     Terrain = "hills"
	TerrainMarsh     Terrain = "marsh"
	TerrainMountains Terrain = "mountains"
	TerrainOcean     Terrain = "ocean"
	TerrainLake      Terrain = "lake"
	TerrainIce       Terrain = "ice"
)

// Terrains lists every terrain a generated hex may hold, in the order the
// terrain reference gives them.
func Terrains() []Terrain {
	return []Terrain{
		TerrainGrassland, TerrainForest, TerrainHills, TerrainMarsh,
		TerrainMountains, TerrainOcean, TerrainLake, TerrainIce,
	}
}

// IsWater reports whether the terrain is water. Water hexes hold no player
// origin and carry a negative elevation.
//
// Ice is not water. The polar sheets are the wall at the edge of the world,
// and a lake that touches one is still a lake: nothing may be classified as
// reaching the sea through an ice sheet.
func (t Terrain) IsWater() bool {
	return t == TerrainOcean || t == TerrainLake
}

// IsLand reports whether the terrain is land. Account origins must be land.
//
// Ice is neither land nor water. It is the third case on purpose: the polar
// sheets hold no origins, so treating them as land would offer players a seat
// their founding entities cannot enter, and treating them as water would let
// the ocean and lake classification run straight through them.
func (t Terrain) IsLand() bool {
	return t != TerrainIce && !t.IsWater()
}

// Passable reports whether an entity of kind may enter a hex of this terrain.
//
// Leaders and hamlets may enter land. Marajanda is not stopped by terrain and
// may also enter water and ice. Impassability is a property of the destination:
// an entity already in water may leave it by stepping onto land.
func (t Terrain) Passable(kind EntityKind) bool {
	return t.IsLand() || kind == EntityKindMarajanda
}

// Valid reports whether the terrain is one this game knows.
func (t Terrain) Valid() bool {
	for _, terrain := range Terrains() {
		if t == terrain {
			return true
		}
	}
	return false
}

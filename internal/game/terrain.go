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
)

// Terrains lists every terrain a generated hex may hold, in the order the
// terrain reference gives them.
func Terrains() []Terrain {
	return []Terrain{
		TerrainGrassland, TerrainForest, TerrainHills, TerrainMarsh,
		TerrainMountains, TerrainOcean, TerrainLake,
	}
}

// IsWater reports whether the terrain is water. Water hexes hold no player
// origin and carry a negative elevation.
func (t Terrain) IsWater() bool {
	return t == TerrainOcean || t == TerrainLake
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

// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// Terrain identifies the terrain initialized for a map hex.
type Terrain string

const (
	TerrainGrassland Terrain = "grassland"
	TerrainForest    Terrain = "forest"
	TerrainHills     Terrain = "hills"
	TerrainMarsh     Terrain = "marsh"
	TerrainMountains Terrain = "mountains"
)

// TerrainAt returns the deterministic terrain at a true map coordinate. The
// game origin is always mountains and does not draw from a stream.
func TerrainAt(seeds prng.Seeds, location hexg.Hex) Terrain {
	if location.Length() == 0 {
		return TerrainMountains
	}
	roll := seeds.Roller(
		prng.TagHex,
		prng.Key(location.Q()),
		prng.Key(location.R()),
		prng.Key(location.S()),
	).RollN(1, 10)
	return terrainForRoll(roll)
}

func terrainForRoll(roll int) Terrain {
	switch roll {
	case 1, 2, 3, 4:
		return TerrainGrassland
	case 5, 6:
		return TerrainForest
	case 7, 8:
		return TerrainHills
	case 9:
		return TerrainMarsh
	case 10:
		return TerrainMountains
	default:
		panic("game: terrain roll must be 1..10")
	}
}

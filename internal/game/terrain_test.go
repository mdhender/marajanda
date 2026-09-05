// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
)

func TestTerrainAtGameOriginIsMountains(t *testing.T) {
	if got := TerrainAt(testSeeds(), hexg.NewHex(0, 0)); got != TerrainMountains {
		t.Fatalf("TerrainAt(game origin) = %q, want %q", got, TerrainMountains)
	}
}

func TestTerrainAtGoldenResult(t *testing.T) {
	if got := TerrainAt(testSeeds(), hexg.NewHex(7, -16)); got != TerrainForest {
		t.Fatalf("TerrainAt(7,-16,9) = %q, want %q", got, TerrainForest)
	}
}

func TestTerrainRollTable(t *testing.T) {
	for roll, want := range []Terrain{
		TerrainGrassland,
		TerrainGrassland,
		TerrainGrassland,
		TerrainGrassland,
		TerrainForest,
		TerrainForest,
		TerrainHills,
		TerrainHills,
		TerrainMarsh,
		TerrainMountains,
	} {
		roll++
		if got := terrainForRoll(roll); got != want {
			t.Fatalf("terrainForRoll(%d) = %q, want %q", roll, got, want)
		}
	}
}

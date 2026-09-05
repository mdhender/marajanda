// Copyright (c) 2026 Michael D Henderson.

package game

import "testing"

func TestTerrainWater(t *testing.T) {
	for _, terrain := range Terrains() {
		want := terrain == TerrainOcean || terrain == TerrainLake
		if got := terrain.IsWater(); got != want {
			t.Fatalf("%q.IsWater() = %v, want %v", terrain, got, want)
		}
	}
}

func TestTerrainValid(t *testing.T) {
	for _, terrain := range Terrains() {
		if !terrain.Valid() {
			t.Fatalf("%q.Valid() = false, want true", terrain)
		}
	}
	for _, terrain := range []Terrain{"", "desert", "Grassland"} {
		if terrain.Valid() {
			t.Fatalf("%q.Valid() = true, want false", terrain)
		}
	}
}

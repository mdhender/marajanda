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

// Ice is the one terrain that is neither land nor water. Every other terrain
// is exactly one of the two, and nothing may quietly become both.
func TestTerrainLandWaterAndIce(t *testing.T) {
	for _, terrain := range Terrains() {
		land, water := terrain.IsLand(), terrain.IsWater()
		if terrain == TerrainIce {
			if land || water {
				t.Fatalf("ice is land=%v water=%v, want neither", land, water)
			}
			continue
		}
		if land == water {
			t.Fatalf("%q is land=%v water=%v, want exactly one", terrain, land, water)
		}
	}
}

// Passability asks both what the destination is and what is trying to enter it.
func TestTerrainPassable(t *testing.T) {
	for _, kind := range []EntityKind{EntityKindLeader, EntityKindHamlet, EntityKindMarajanda} {
		for _, terrain := range Terrains() {
			want := terrain.IsLand() || kind == EntityKindMarajanda
			if got := terrain.Passable(kind); got != want {
				t.Fatalf("%q.Passable(%q) = %v, want %v", terrain, kind, got, want)
			}
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

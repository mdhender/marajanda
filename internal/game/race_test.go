// Copyright (c) 2026 Michael D Henderson.

package game

import "testing"

// A race's order is what placement walks. If it missed a land terrain, that
// terrain would never seat that race however empty the world was; if it
// repeated one, the second pass would be wasted work on a pool already refused.
func TestEveryRaceOrdersEveryLandTerrainOnce(t *testing.T) {
	var land []Terrain
	for _, terrain := range Terrains() {
		if terrain.IsLand() {
			land = append(land, terrain)
		}
	}

	for _, race := range Races() {
		order := race.TerrainOrder()
		if len(order) != len(land) {
			t.Errorf("%s orders %d terrains, want %d", race, len(order), len(land))
			continue
		}
		seen := make(map[Terrain]int, len(order))
		for _, terrain := range order {
			if !terrain.IsLand() {
				t.Errorf("%s prefers %q, which is not land", race, terrain)
			}
			seen[terrain]++
		}
		for _, terrain := range land {
			if seen[terrain] != 1 {
				t.Errorf("%s names %q %d times, want once", race, terrain, seen[terrain])
			}
		}
	}
}

func TestRaceValid(t *testing.T) {
	for _, race := range Races() {
		if !race.Valid() {
			t.Errorf("Races() lists %q, which reports itself invalid", race)
		}
	}
	for _, race := range []Race{"", "Human", "wyrm", "human "} {
		if Race(race).Valid() {
			t.Errorf("%q reports itself valid", race)
		}
	}
	if !DefaultRace.Valid() {
		t.Errorf("DefaultRace %q is not valid", DefaultRace)
	}
}

// The order a race hands out must not be the one placement walks next time.
func TestTerrainOrderIsACopy(t *testing.T) {
	order := RaceHuman.TerrainOrder()
	if len(order) == 0 {
		t.Fatal("human has no terrain order")
	}
	order[0] = TerrainMountains
	if again := RaceHuman.TerrainOrder(); again[0] == TerrainMountains {
		t.Fatal("TerrainOrder returned the stored slice")
	}
}

// The belt is two thirds of the way to each pole, rounded down.
func TestOriginBelt(t *testing.T) {
	for _, test := range []struct{ height, want int }{
		{height: 127, want: 84},
		{height: 30, want: 20},
		{height: 20, want: 13},
		{height: 1, want: 0},
	} {
		if got := OriginBelt(test.height); got != test.want {
			t.Errorf("OriginBelt(%d) = %d, want %d", test.height, got, test.want)
		}
	}
}

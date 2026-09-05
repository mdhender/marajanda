// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"errors"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

func TestAssignOriginDeterministic(t *testing.T) {
	seeds := testSeeds()
	world := testWorld(t)
	taken := []hexg.Hex{hexg.NewHex(16, 0)}

	first, err := AssignOrigin(seeds, "player@example.com", world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() error = %v", err)
	}
	second, err := AssignOrigin(seeds, "player@example.com", world, taken)
	if err != nil {
		t.Fatalf("AssignOrigin() repeated error = %v", err)
	}
	if !first.Equals(second) {
		t.Fatalf("AssignOrigin repeated = %v then %v", first, second)
	}
	if first.Length() <= minimumOriginDistance {
		t.Fatalf("AssignOrigin distance from game origin = %d, want > %d", first.Length(), minimumOriginDistance)
	}
	for _, origin := range taken {
		if distance := first.Distance(origin); distance <= minimumOriginDistance {
			t.Fatalf("AssignOrigin distance from %v = %d, want > %d", origin, distance, minimumOriginDistance)
		}
	}
}

// A player origin has to be somewhere a faction can stand. Placing one at sea
// is the failure this constraint exists to prevent.
func TestAssignOriginIsOnLandInsideTheWorld(t *testing.T) {
	seeds := testSeeds()
	world := testWorld(t)

	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"} {
		origin, err := AssignOrigin(seeds, email, world, nil)
		if err != nil {
			t.Fatalf("AssignOrigin(%q) error = %v", email, err)
		}
		if !world.Contains(origin) {
			t.Fatalf("AssignOrigin(%q) = %v, which is outside the world", email, origin)
		}
		hex, _ := world.At(origin)
		if hex.Terrain.IsWater() {
			t.Fatalf("AssignOrigin(%q) = %v, which is %q", email, origin, hex.Terrain)
		}
	}
}

// A world with no land beyond the exclusion radius must report that it is full
// rather than walk forever looking for a hex that cannot exist.
func TestAssignOriginReportsAFullWorld(t *testing.T) {
	world := NewWorld(2, []Hex{
		{Coord: hexg.NewHex(0, 0), Terrain: TerrainMountains},
		{Coord: hexg.NewHex(1, 0), Terrain: TerrainOcean, Elevation: -60},
		{Coord: hexg.NewHex(0, 1), Terrain: TerrainOcean, Elevation: -60},
	})
	if _, err := AssignOrigin(testSeeds(), "player@example.com", world, nil); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("AssignOrigin(full world) error = %v, want %v", err, ErrNoOrigin)
	}
}

func TestOriginDirectionSlots(t *testing.T) {
	for _, test := range []struct {
		name string
		hex  hexg.Hex
		want int
	}{
		{name: "game origin", hex: hexg.NewHex(0, 0), want: 12},
		{name: "ring corner", hex: hexg.NewHex(2, 0), want: 9},
		{name: "ring edge", hex: hexg.NewHex(2, -1), want: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			slots := len(originDirections)
			for _, direction := range originDirections {
				if test.hex.Add(direction).Length() > test.hex.Length() {
					slots++
				}
			}
			if slots != test.want {
				t.Fatalf("slots at %v = %d, want %d", test.hex, slots, test.want)
			}
		})
	}
}

func TestPlayerRotationGoldenResult(t *testing.T) {
	seeds := testSeeds()
	origin := hexg.NewHex(18, -7)
	first := PlayerRotation(seeds, origin)
	second := PlayerRotation(seeds, origin)
	if first != second {
		t.Fatalf("PlayerRotation repeated = %d then %d", first, second)
	}
	if first != 0 {
		t.Fatalf("PlayerRotation = %d, want 0", first)
	}
}

func testSeeds() prng.Seeds {
	seed2 := int64(-98)
	return prng.New(98374, uint64(seed2))
}

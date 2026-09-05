// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

func TestAssignOriginDeterministic(t *testing.T) {
	seeds := testSeeds()
	initialized := []hexg.Hex{hexg.NewHex(0, 0), hexg.NewHex(16, 0)}

	first := AssignOrigin(seeds, "player@example.com", initialized)
	second := AssignOrigin(seeds, "player@example.com", initialized)
	if !first.Equals(second) {
		t.Fatalf("AssignOrigin repeated = %v then %v", first, second)
	}
	if first.Length() <= minimumOriginDistance {
		t.Fatalf("AssignOrigin distance from game origin = %d, want > %d", first.Length(), minimumOriginDistance)
	}
	for _, origin := range initialized {
		if distance := first.Distance(origin); distance <= minimumOriginDistance {
			t.Fatalf("AssignOrigin distance from %v = %d, want > %d", origin, distance, minimumOriginDistance)
		}
	}
}

func TestAssignOriginGoldenResult(t *testing.T) {
	seeds := testSeeds()
	origin := AssignOrigin(seeds, "player@example.com", []hexg.Hex{hexg.NewHex(0, 0)})

	q, r, s := origin.QRS()
	if q != 7 || r != -16 || s != 9 {
		t.Fatalf("AssignOrigin = (%d,%d,%d), want (7,-16,9)", q, r, s)
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

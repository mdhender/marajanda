// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

// Spacing limits, in wrapped hexes, between an origin and one already taken.
// Both are inclusive: a candidate exactly this far away is accepted.
//
// Two limits rather than one because crowding is about competition for the same
// ground. Two factions of the same race on the same terrain want the same hexes
// and are held further apart than any other pair.
const (
	sameRaceTerrainSpacing = 13
	otherOriginSpacing     = 8
)

// minimumOriginDistance keeps an origin away from the game origin.
//
// It is redundant whenever a main admin exists, because the main admin holds
// the game origin and is itself in the exclusion set at a wider limit. It is
// kept because it costs one comparison and it still holds in a database that
// has no main admin.
const minimumOriginDistance = 15

// ErrNoOrigin reports that the world has no hex left that satisfies the
// placement rules.
var ErrNoOrigin = errors.New("no valid origin remains in the world")

// Placement is an origin already taken, and by whom. Terrain is not carried:
// it is a property of the world, which placement is given anyway, and storing
// it twice would let the two disagree.
type Placement struct {
	Coord hexg.Hex
	Race  Race
}

// seat is a placement with its terrain resolved, so the spacing test does not
// look the same hex up once per candidate.
type seat struct {
	coord   hexg.Hex
	race    Race
	terrain Terrain
}

// OriginBelt returns the largest |r| an origin may hold in a world of the given
// height: the equatorial belt, two thirds of the way to each pole.
//
// Placement is confined to it so that nobody is seated in the thin, cold
// margins against the ice, where a faction would have the world on one side of
// it and a wall on the other.
func OriginBelt(height int) int { return 2 * height / 3 }

// AssignOrigin returns the deterministic origin hex for a normalized account
// email: a land hex inside the equatorial belt, on the most favored terrain
// the race can still be spaced onto.
//
// One candidate pool is built per terrain in the race's preference order. The
// most favored pool is shuffled from the account's placement stream and walked
// in that order, taking the first hex whose spacing holds; if none does, the
// next pool is tried. Running out of pools proves no valid hex exists, so a
// refusal is a fact about the world rather than a walk that gave up.
//
// taken holds the origins existing accounts already hold, with the race that
// holds each. It is not the set of hexes the world contains: every hex of the
// world is generated before any account exists, so an exclusion set drawn from
// the map itself would exclude the whole map.
func AssignOrigin(seeds prng.Seeds, normalizedEmail string, race Race, world World, taken []Placement) (hexg.Hex, error) {
	order := race.TerrainOrder()
	if len(order) == 0 {
		return hexg.Hex{}, ErrNoOrigin
	}

	digest := sha256.Sum256([]byte(normalizedEmail))
	roller := seeds.Roller(
		prng.TagPlayer,
		prng.Key(int64(binary.BigEndian.Uint64(digest[0:8]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[8:16]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[16:24]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[24:32]))),
	)

	seats := resolveSeats(world, taken)
	pools := originPools(world, order)
	for index, terrain := range order {
		pool := pools[index]
		// The shuffle draws the same number of values however full the map is,
		// so a stream's position does not depend on who was seated before.
		roller.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		for _, candidate := range pool {
			if validOrigin(candidate, race, terrain, world, seats) {
				return candidate, nil
			}
		}
	}
	return hexg.Hex{}, ErrNoOrigin
}

// originPools builds one candidate pool per terrain, in the order given.
//
// Pools are filled from World.Hexes, which is row-major from north to south and
// west to east, so the same world always presents the same pool to the shuffle
// and the same seeds always produce the same origin.
func originPools(world World, order []Terrain) [][]hexg.Hex {
	position := make(map[Terrain]int, len(order))
	for index, terrain := range order {
		position[terrain] = index
	}
	pools := make([][]hexg.Hex, len(order))

	belt := OriginBelt(world.Height())
	for _, hex := range world.Hexes() {
		if hex.Coord.R() < -belt || hex.Coord.R() > belt {
			continue
		}
		if index, ok := position[hex.Terrain]; ok {
			pools[index] = append(pools[index], hex.Coord)
		}
	}
	return pools
}

// resolveSeats reads each taken origin's terrain out of the world once.
func resolveSeats(world World, taken []Placement) []seat {
	seats := make([]seat, 0, len(taken))
	for _, placement := range taken {
		coord := world.Normalize(placement.Coord)
		hex, _ := world.At(coord)
		seats = append(seats, seat{coord: coord, race: placement.Race, terrain: hex.Terrain})
	}
	return seats
}

// validOrigin reports whether a candidate is far enough from the game origin
// and from every origin already taken. Both spacing limits are inclusive.
func validOrigin(candidate hexg.Hex, race Race, terrain Terrain, world World, seats []seat) bool {
	cylinder := world.Cylinder()
	if cylinder.Distance(hexg.NewHex(0, 0), candidate) <= minimumOriginDistance {
		return false
	}
	for _, taken := range seats {
		spacing := otherOriginSpacing
		if taken.race == race && taken.terrain == terrain {
			spacing = sameRaceTerrainSpacing
		}
		if cylinder.Distance(candidate, taken.coord) < spacing {
			return false
		}
	}
	return true
}

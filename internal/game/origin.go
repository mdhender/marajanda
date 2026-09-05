// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

const minimumOriginDistance = 15

// originStepBudget bounds the placement walk. The walk is a random walk on a
// bounded disc, so it finds a valid hex almost surely whenever one exists — but
// "almost surely" is not a promise a request handler can wait on. A world with
// no room left, or none whose land is far enough from the origins already
// taken, must fail loudly rather than spin.
const originStepBudget = 1_000_000

// ErrNoOrigin reports that the world has no hex left that satisfies the
// placement rules.
var ErrNoOrigin = errors.New("no valid origin remains in the world")

var originDirections = [...]hexg.Hex{
	hexg.NewHex(1, 0),
	hexg.NewHex(1, -1),
	hexg.NewHex(0, -1),
	hexg.NewHex(-1, 0),
	hexg.NewHex(-1, 1),
	hexg.NewHex(0, 1),
}

// AssignOrigin returns the deterministic origin hex for a normalized account
// email: a land hex of the world, more than minimumOriginDistance hexes from
// the game origin and from every origin already taken.
//
// taken holds the origins of existing accounts. It is not the set of hexes the
// world contains — every hex of the world is generated before any account
// exists, so an exclusion set drawn from the map itself would exclude the whole
// map.
func AssignOrigin(seeds prng.Seeds, normalizedEmail string, world World, taken []hexg.Hex) (hexg.Hex, error) {
	digest := sha256.Sum256([]byte(normalizedEmail))
	roller := seeds.Roller(
		prng.TagPlayer,
		prng.Key(int64(binary.BigEndian.Uint64(digest[0:8]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[8:16]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[16:24]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[24:32]))),
	)

	current := hexg.NewHex(0, 0)
	for range originStepBudget {
		// A direction is offered only when it stays inside the world, so the
		// walk cannot wander off a bounded map. Outward moves are offered twice,
		// which is what pushes the walk away from the crowded centre.
		directions := make([]int, 0, 2*len(originDirections))
		for direction, vector := range originDirections {
			if world.Contains(current.Add(vector)) {
				directions = append(directions, direction)
			}
		}
		for direction, vector := range originDirections {
			neighbor := current.Add(vector)
			if world.Contains(neighbor) && neighbor.Length() > current.Length() {
				directions = append(directions, direction)
			}
		}
		if len(directions) == 0 {
			return hexg.Hex{}, ErrNoOrigin
		}

		direction := directions[roller.RollRange(0, len(directions)-1)]
		current = current.Add(originDirections[direction])
		if validOrigin(current, world, taken) {
			return current, nil
		}
	}
	return hexg.Hex{}, ErrNoOrigin
}

// PlayerRotation returns the deterministic map rotation for an account origin.
func PlayerRotation(seeds prng.Seeds, origin hexg.Hex) int {
	return seeds.Roller(prng.TagPlayer, prng.Key(origin.Q()), prng.Key(origin.R())).RollRange(0, 5)
}

func validOrigin(candidate hexg.Hex, world World, taken []hexg.Hex) bool {
	if candidate.Length() <= minimumOriginDistance {
		return false
	}
	if !world.IsLand(candidate) {
		return false
	}
	for _, origin := range taken {
		if candidate.Distance(origin) <= minimumOriginDistance {
			return false
		}
	}
	return true
}

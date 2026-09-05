// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/prng"
)

const minimumOriginDistance = 15

var originDirections = [...]hexg.Hex{
	hexg.NewHex(1, 0),
	hexg.NewHex(1, -1),
	hexg.NewHex(0, -1),
	hexg.NewHex(-1, 0),
	hexg.NewHex(-1, 1),
	hexg.NewHex(0, 1),
}

// AssignOrigin returns the deterministic origin hex for a normalized account
// email, avoiding the game origin and initialized account origins by more than
// minimumOriginDistance hexes.
func AssignOrigin(seeds prng.Seeds, normalizedEmail string, initialized []hexg.Hex) hexg.Hex {
	digest := sha256.Sum256([]byte(normalizedEmail))
	roller := seeds.Roller(
		prng.TagPlayer,
		prng.Key(int64(binary.BigEndian.Uint64(digest[0:8]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[8:16]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[16:24]))),
		prng.Key(int64(binary.BigEndian.Uint64(digest[24:32]))),
	)

	current := hexg.NewHex(0, 0)
	for {
		directions := make([]int, 0, 12)
		for direction := range len(originDirections) {
			directions = append(directions, direction)
		}
		for direction, vector := range originDirections {
			if current.Add(vector).Length() > current.Length() {
				directions = append(directions, direction)
			}
		}

		direction := directions[roller.RollRange(0, len(directions)-1)]
		current = current.Add(originDirections[direction])
		if validOrigin(current, initialized) {
			return current
		}
	}
}

// PlayerRotation returns the deterministic map rotation for an account origin.
func PlayerRotation(seeds prng.Seeds, origin hexg.Hex) int {
	return seeds.Roller(prng.TagPlayer, prng.Key(origin.Q()), prng.Key(origin.R())).RollRange(0, 5)
}

func validOrigin(candidate hexg.Hex, initialized []hexg.Hex) bool {
	if candidate.Length() <= minimumOriginDistance {
		return false
	}
	for _, origin := range initialized {
		if candidate.Distance(origin) <= minimumOriginDistance {
			return false
		}
	}
	return true
}

// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng

// The root domain-tag registry: the leading element of every root key path
// names the purpose of a draw, providing domain separation so two purposes do
// not accidentally share a stream. Derived Seeds may define a subsystem-local
// registry. This is the single, authoritative place root tags are defined.
//
// Instance keys for map objects are their canonical coordinates, never SQLite
// autoincrement row ids: row ids depend on insertion order, so addressing draws
// by them would weld a game's randomness to the order rows happened to be
// written. Coordinates are intrinsic to the map — a hex's (q, r, s) is the root
// of all draws for a hex. TagPlayer origin-placement draws use the normalized
// email's SHA-256 digest because no player number exists yet; later TagPlayer
// draws use the game-assigned player number. TagFaction draws use the faction
// number. These addresses must always be stable — never database row ids.
//
// SLUSHY/FROZEN SURFACE in beta and alpha. We want to avoid changes but we understand
// that we may have changes in beta and alpha that break determinism and repeatability.
// The block starts at 1 (0 is invalid, so a forgotten tag is an obvious bug rather
// than a silent alias). Never insert or reorder a constant: iota would renumber every
// tag after it and silently rewrite every live game. To add a tag, append it to the
// END of this block and pin a golden vector for its stream.
const (
	_            Key = iota // 0 is invalid — never use as a domain tag
	TagMarajanda            // 1: engine draws
	TagPlayer               // 2: per-player draws, addressed by email digest or player number
	TagFaction              // 3: per-faction draws, addressed by faction number
	TagHex                  // 4: per-hex contents, addressed by (q, r, s)
	TagTile                 // 5: per-tile contents, addressed by (q, r, s, tile_type)
	TagWorld                // 6: world-scale draws, addressed by noise lattice point or field name

	// tagLimit is one past the last registered tag. It is not a tag: it bounds
	// the registry check in validatePath. Append new tags above it so the bound
	// maintains itself.
	tagLimit
)

# Player origin reference

## Coordinate systems

The game map is an unbounded, flat-top hex grid. Marajanda stores map
coordinates as axial `(q, r)` coordinates and may use cube `(q, r, s)`
coordinates for calculations. Cube coordinates satisfy `q + r + s = 0`.

The game origin is the true cube coordinate `(0, 0, 0)`.

Each player has an origin hex at a true cube coordinate `(q, r, s)`. The
origin hex appears to that player as the axial coordinate `(0, 0)`. A player
origin hex is never the game origin.

The true axial components `(q, r)` of the player origin hex are the player
number. Database row identifiers are not player numbers.

Hex coordinates and operations use `github.com/maloquacious/hexg`.

## Placement stream

Player-origin placement uses the account's normalized email address. Email
normalization occurs before hashing and follows the account identity rules.

The normalized UTF-8 email bytes are hashed with SHA-256. The digest is divided,
in order, into four consecutive eight-byte, big-endian words. Each word's bit
pattern is preserved in a `prng.Key`.

The placement stream has this key path:

```text
[
    prng.TagPlayer,
    emailDigestWord0,
    emailDigestWord1,
    emailDigestWord2,
    emailDigestWord3,
]
```

The game seeds and this complete path determine the stream.

## Direction order

Origin placement numbers the six cube direction vectors as follows:

| Direction | `(q, r, s)` |
| ---: | ---: |
| 0 | `(+1, 0, -1)` |
| 1 | `(+1, -1, 0)` |
| 2 | `(0, -1, +1)` |
| 3 | `(-1, 0, +1)` |
| 4 | `(-1, +1, 0)` |
| 5 | `(0, +1, -1)` |

This ordering is part of Marajanda's deterministic placement contract.

## Placement walk

The placement walk starts at the game origin `(0, 0, 0)`.

For each step:

1. Create six selection slots in direction order `0` through `5`, with one
   slot for each direction.
2. For each direction, calculate the neighboring hex. If that neighbor is
   farther from the game origin than the current hex, append a second slot for
   that direction. Append the extra slots in direction order `0` through `5`.
3. Draw one slot uniformly from the placement stream and move one hex in the
   selected direction.
4. Assign the current hex as the player's origin hex if both of these
   conditions hold:
   - its distance from the game origin is greater than 15 hexes; and
   - its distance from every initialized hex is greater than 15 hexes.
5. Otherwise, repeat from step 1 at the current hex.

The direction weighting produces:

- 12 slots at the game origin, where all six moves increase distance;
- 9 slots at a corner of a distance ring, where three moves increase distance;
  and
- 8 slots along an edge of a distance ring, where two moves increase distance.

The assigned origin hex is persisted. For a fixed set of game seeds, normalized
email, direction ordering, and initialized hexes, the walk produces the same
origin hex.

## Player map rotation

Each player map has a persisted rotation in the inclusive range `0` through
`5`. Rotation `N` means `N × 60°` clockwise.

After the origin hex is assigned, its true axial components `(q, r)` form the
player number. The rotation stream has this key path:

```text
[prng.TagPlayer, q, r]
```

Drawing `RollRange(0, 5)` once from that stream determines the rotation. The
result is persisted with the player map.

A 60° clockwise rotation of a cube vector uses the Red Blob Games
transformation:

```text
(q, r, s) -> (-r, -s, -q)
```

To convert a true map coordinate `H` for a player whose origin hex is `P`,
subtract the origin hex and apply the player's clockwise rotation:

```text
playerVisible = axial(rotateClockwise(H - P, rotation))
```

To convert a player-visible coordinate back to a true coordinate, apply the
inverse rotation before adding the origin hex:

```text
trueCoordinate = P + rotateCounterClockwise(cube(playerVisible), rotation)
```

For example, let `P = (0, +1, -1)` and `H = (0, 0, 0)`. The unrotated relative
vector is `(0, -1, +1)`. Its visible axial coordinate by rotation is:

| Rotation | Degrees clockwise | Visible `(q, r)` |
| ---: | ---: | ---: |
| 0 | 0° | `(0, -1)` |
| 1 | 60° | `(+1, -1)` |
| 2 | 120° | `(+1, 0)` |
| 3 | 180° | `(0, +1)` |
| 4 | 240° | `(-1, +1)` |
| 5 | 300° | `(-1, 0)` |

# Player origin reference

## Coordinate systems

The game map is a bounded, flat-top hex grid: every hex within the world radius
of the game origin. Marajanda stores map coordinates as axial `(q, r)`
coordinates and may use cube `(q, r, s)` coordinates for calculations. Cube
coordinates satisfy `q + r + s = 0`. See [Terrain reference](terrain.md) for the
world's radius and contents.

The game origin is the true cube coordinate `(0, 0, 0)`.

Each player has an origin hex at a true cube coordinate `(q, r, s)`. The
origin hex appears to that player as the axial coordinate `(0, 0)`. A player
origin hex is never the game origin, is always a hex of the world, and is
always land.

Every account stores an origin hex and map rotation. The main admin uses the
game origin and rotation `0`, so the main admin's map matches the true map, and
holds it whatever terrain the game origin has. Every later account, including an
assistant admin, uses the placement and rotation rules below, which include the
land rule above.

The true axial components `(q, r)` of the player origin hex are the player
number. Database row identifiers are not player numbers.

Hex coordinates and operations use `github.com/maloquacious/hexg`.

The transform between true and player coordinates, and the maps that apply it,
are described in [Map view reference](map-view.md).

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

1. Create one selection slot for each direction, in direction order `0` through
   `5`, whose neighboring hex is inside the world. A direction whose neighbor
   lies outside the world is not offered.
2. For each of those directions, if the neighboring hex is farther from the
   game origin than the current hex, append a second slot for that direction.
   Append the extra slots in direction order `0` through `5`.
3. Draw one slot uniformly from the placement stream and move one hex in the
   selected direction.
4. Assign the current hex as the player's origin hex if all of these conditions
   hold:
   - its distance from the game origin is greater than 15 hexes;
   - its terrain is not `ocean` or `lake`; and
   - its distance from every origin already assigned to an account is greater
     than 15 hexes.
5. Otherwise, repeat from step 1 at the current hex.

The exclusion set is the origins accounts already hold, not the hexes the world
contains. Every hex of the world exists before the first account is created.

Away from the edge of the world, the direction weighting produces:

- 12 slots at the game origin, where all six moves increase distance;
- 9 slots at a corner of a distance ring, where three moves increase distance;
  and
- 8 slots along an edge of a distance ring, where two moves increase distance.

Within 1 hex of the edge of the world, directions leaving the world are
withheld, and the walk has fewer slots than the counts above.

The walk runs for at most 1,000,000 steps. A walk that assigns no origin within
that budget fails, and the account is not created. A world whose land is fully
taken, or whose land is entirely within 15 hexes of the game origin or of an
assigned origin, produces this failure.

The assigned origin hex is persisted. For a fixed set of game seeds, normalized
email, direction ordering, world, and set of assigned origins, the walk produces
the same origin hex. Creating an account does not create its origin hex: the
world already contains it.

Concurrent account creation may calculate placements from the same set of
assigned origins. The account-origin uniqueness constraint rejects identical
origin hexes. Origins calculated concurrently may be within 15 hexes of each
other, including on neighboring hexes.

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

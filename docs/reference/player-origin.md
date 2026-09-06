# Player origin reference

## Coordinate systems

The game map is a rectangle that wraps east-west and is walled north and south.
Marajanda stores map coordinates as axial `(q, r)` coordinates and may use cube
`(q, r, s)` coordinates for calculations. Cube coordinates satisfy
`q + r + s = 0`. See [Terrain reference](terrain.md) for the world's dimensions
and contents.

The game origin is the true cube coordinate `(0, 0, 0)`.

A seated account has an origin hex at a true cube coordinate `(q, r, s)`. The
origin hex appears to that account as the axial coordinate `(0, 0)`. An origin
hex assigned by placement is never the game origin, is always a hex of the
world, and is always land.

An account stores an origin hex and map rotation once it is seated. The main
admin uses the game origin and rotation `0`, so the main admin's map matches the
true map, and holds it whatever terrain the game origin has. It also occupies a
hex every other account must keep clear of, and counts as race `human` on
whatever terrain `(0, 0)` holds.

Every other account is seated by the placement rules below. When that happens
depends on the role:

| Account | Seated | Race used for placement |
| --- | --- | --- |
| Any other admin | When the account is created | `human` |
| Player | When the account configures its faction | The faction's race |

An admin is seated as it is created because it configures no faction, and so has
no later moment at which a race is known. A player account exists unseated
between creation and faction configuration. Its origin columns are `NULL`, and
no page renders that state: a player without a faction is directed to the
faction form, and the faction form is what seats it.

The true axial components `(q, r)` of an origin hex are the player number.
Database row identifiers are not player numbers.

Hex coordinates and operations use `github.com/maloquacious/hexg`.

The transform between true and player coordinates, and the maps that apply it,
are described in [Map view reference](map-view.md).

## Placement stream

Placement uses the account's normalized email address. Email normalization
occurs before hashing and follows the account identity rules.

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

The game seeds and this complete path determine the stream. Race is not part of
the key path. It selects which candidate pool is drawn from and which spacing
limit applies; it does not address the stream.

## Placement rules

An origin hex assigned by placement satisfies all three of these rules.

**Belt.** The hex lies within the equatorial belt `|r| <= 2 * height / 3`,
using integer division. On the default 511-by-255 world that is rows `-84`
through `84`, or 169 of 255 rows.

**Terrain.** The hex holds a land terrain. Each race orders every land terrain
by preference; see [Terrain reference](terrain.md#race-terrain-preference).
Polar ice never qualifies: it is not land, and the belt does not reach it.

**Spacing.** By wrapped distance from every origin already assigned:

| Pair | Minimum distance |
| --- | ---: |
| Same race on the same terrain | 13 |
| Every other pair | 8 |

Both limits are inclusive. A candidate exactly 13 or exactly 8 hexes away is
accepted.

A candidate is also more than 15 hexes from the game origin. This rule is
redundant whenever a main admin exists, because the main admin holds the game
origin and is in the exclusion set at a wider limit. It still holds in a
database with no main admin.

The main admin is exempt from all three rules.

The exclusion set is the origins accounts already hold, each with the race of
the faction that holds it. It is not the hexes the world contains: every hex of
the world exists before the first account is created. An account with no
faction, which includes every admin, counts as race `human`.

## Placement procedure

Placement builds one candidate pool per terrain, in the race's preference
order, and draws from each without replacement.

1. Take the next terrain in the race's preference order. If the order is
   exhausted, placement fails.
2. Build the pool: every hex of the world whose terrain is that terrain and
   whose row satisfies the belt rule, in the world's canonical order, which is
   row-major from north to south and west to east.
3. Shuffle the pool from the placement stream.
4. Walk the shuffled pool in order. Assign the first hex that satisfies the
   spacing rules.
5. If no hex in the pool satisfies them, repeat from step 1 with the next
   terrain.

Placement fails when every pool is exhausted, which proves no valid hex exists.
There is no step budget and no attempt limit. An account that cannot be seated
is refused: account creation fails for an admin, and faction configuration
fails for a player. Neither leaves a partially built account behind.

Every valid hex of a pool is equally likely to be drawn. The shuffle consumes
the same number of draws whatever the exclusion set holds, so a stream's
position does not depend on how full the map is.

The assigned origin hex is persisted. For a fixed set of game seeds, normalized
email, race, world, and exclusion set, the procedure produces the same origin
hex. Seating an account does not create its origin hex: the world already
contains it.

## Capacity

Pools on the default 511-by-255 world with seeds `98374,-98`, within the belt
`|r| <= 84`:

| Terrain | Hexes in the belt |
| --- | ---: |
| `grassland` | 19,693 |
| `forest` | 9,550 |
| `hills` | 11,497 |
| `marsh` | 1,098 |
| `mountains` | 5,851 |
| **Total** | **47,689** |

Accounts seated before placement fails, by belt:

| Belt | Rows | All `human` | Six races evenly |
| --- | ---: | ---: | ---: |
| `\|r\| <= 63` | 127 | 310 | 498 |
| `\|r\| <= 84` | 169 | 402 | 654 |
| `\|r\| <= 127` | 255 | 638 | 1,019 |

## Concurrency

Placement depends on the exclusion set at the moment it runs, so two accounts
placed concurrently may be calculated from the same set. Origins calculated
concurrently may be closer than the spacing limits above, including on
neighbouring hexes. The account-origin uniqueness constraint rejects identical
origin hexes, and the request fails rather than seating two accounts on one hex.

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

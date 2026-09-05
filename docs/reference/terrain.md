# Terrain reference

## The world

A game has exactly one world: every hex within a fixed radius of the game
origin `(0, 0, 0)`. The world is generated once, when the database is created,
from the game seeds and the world radius, and is stored in full. It does not
change afterwards, and no part of it is recomputed on demand.

A world of radius `R` holds `3R(R + 1) + 1` hexes.

The radius is recorded with the game seeds. Like the seeds, it has no default
in the stored record and cannot be changed once a world exists. Permitted
values are `20` through `120`; `--world-radius` defaults to `30`.

Hexes outside the world do not exist. They hold no terrain and no elevation,
and no account origin may be placed on one.

## Hexes

A hex stores a true map coordinate, its terrain, and its elevation. Coordinates
are stored as axial `(q, r)` values; the cube component is `s = -q-r`. The
axial coordinate pair is the hex's primary key.

Terrain is required and is one of:

| Terrain | Description |
| --- | --- |
| `grassland` | Open lowland |
| `forest` | Wooded lowland or wooded hills |
| `hills` | Rising ground below the mountains |
| `marsh` | Wet, low-lying ground |
| `mountains` | The highest ground |
| `ocean` | Water connected to the edge of the world |
| `lake` | Water with no connection to the edge of the world |

`ocean` and `lake` are water. Every other terrain is land.

Elevation is required and is measured in metres relative to sea level. Land
elevation is zero or greater. Water elevation is negative and gives the depth
of the water at that hex.

Terrain and elevation are derived from the same value and always agree:

| Terrain | Elevation |
| --- | --- |
| `grassland`, `forest`, `marsh` on lowland | `0` to `400` m |
| `hills`, `forest` on hills | `400` to `1200` m |
| `mountains` | `1200` to `4200` m |
| `lake` | `-5` to `-90` m |
| `ocean` | `-60` m at the coast, approaching `-6000` m offshore |

The game origin `(0, 0, 0)` is an ordinary hex. It is part of every world and
receives whatever terrain and elevation the generator produces for it.

## Generation

The generator runs these stages, in this order, over the world's hexes in
ascending `q` then `r` order.

1. **Elevation field.** A five-octave gradient-noise field is sampled at each
   hex's position in the plane. The sampling position is displaced first by a
   second and third noise field, which gives coastlines their irregular shape.
   Elevation is then reduced across the outermost `16%` of the radius, so a
   world ends in water at its edge.
2. **Sea level.** The `42nd` percentile of the elevation field is taken as sea
   level. Hexes at or below it are water; the rest are land. Taking sea level
   as a percentile fixes the proportion of water in every world.
3. **Ocean and lakes.** Water hexes on the outermost ring are flooded inward
   through adjacent water. Water reached by that flood is `ocean`. Water not
   reached by it is `lake`.
4. **Depth.** Each water hex is measured by its hex distance to the nearest
   land, and its elevation is graded from that distance.
5. **Moisture.** One of the six cube directions is drawn as the prevailing
   wind. Hexes are visited in order of their position along that direction.
   Each hex receives moisture carried from its upwind neighbour, gains moisture
   over water, and loses to rainfall what it receives — more when the ground
   rises. Rainfall is combined with a fourth noise field.
6. **Terrain.** Land hexes are ranked by elevation and by moisture among all
   land hexes. The two ranks select terrain and elevation together.

Land terrain follows from the elevation rank and the moisture rank:

| Elevation rank | Moisture rank | Terrain |
| --- | --- | --- |
| `0.88` and above | any | `mountains` |
| `0.62` to `0.88` | `0.72` and above | `forest` |
| `0.62` to `0.88` | below `0.72` | `hills` |
| below `0.22` | `0.74` and above | `marsh` |
| below `0.62` | `0.45` and above | `forest` |
| below `0.62` | below `0.45` | `grassland` |

## Generation stream

World generation draws from streams under the `prng.TagWorld` domain tag. Each
noise field has a field number, which is part of every address it draws from.

| Field | Number |
| --- | ---: |
| Elevation | 1 |
| Horizontal displacement | 2 |
| Vertical displacement | 3 |
| Moisture | 4 |
| Prevailing wind | 5 |

A noise field's lattice point owns a gradient drawn from this key path:

```text
[prng.TagWorld, field, latticeX, latticeY]
```

The prevailing wind is drawn from this key path:

```text
[prng.TagWorld, 5]
```

Field numbers are part of the compatibility surface. They are never renumbered,
and new fields are appended.

The same game seeds and world radius always produce the same world. No stage
depends on the order hexes are visited beyond the orders stated above, and no
address contains a database row identifier.

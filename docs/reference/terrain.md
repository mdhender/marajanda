# Terrain reference

## The world

A game has exactly one world. It is generated once, when the database is
created, from the game seeds and the world's dimensions, and is stored in full.
It does not change afterwards, and no part of it is recomputed on demand.

The world is a rectangle that wraps east-west and is walled north and south.
Its dimensions are stored as half-extents:

```text
columns = 2 * width  + 1
rows    = 2 * height + 1
```

`--width` defaults to `255` and accepts `20` through `511`. `--height`
defaults to `127` and accepts `20` through `255`. The default world is
therefore 511 columns by 255 rows, or 130,305 hexes.

Both values are recorded with the game seeds. Like the seeds, they have no
default in the stored record and cannot be changed once a world exists.

A hex is named by its canonical coordinate, which is the one with
`|q| <= width`. Every row spans the same window of `q`. A column outside that
window names the same hex as the column `2 * width + 1` away from it: the world
has no eastern or western edge. Rows are the only bound. A coordinate with
`|r| > height` is not a hex of the world; it holds no terrain and no elevation,
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
| `ocean` | Water connected to the sea |
| `lake` | Water with no outlet to the sea |
| `ice` | A polar sheet at the northern or southern edge of the world |

Every terrain is land, water, or ice:

| Terrain | Land | Water | Passable |
| --- | --- | --- | --- |
| `grassland`, `forest`, `hills`, `marsh`, `mountains` | yes | no | yes |
| `ocean`, `lake` | no | yes | yes |
| `ice` | no | no | no |

`ice` is neither land nor water. It holds no account origin, and no body of
water is connected to another through it.

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
| `ice` | the elevation of the ground beneath the sheet |

The game origin `(0, 0, 0)` is an ordinary hex. It is part of every world and
receives whatever terrain and elevation the generator produces for it.

## Race terrain preference

Each race orders every land terrain by preference, most favored first. An order
is total: it names each of the five land terrains exactly once, so no race
refuses a terrain outright.

| Race | Terrain order, most favored first |
| --- | --- |
| `human` | `grassland`, `forest`, `hills`, `marsh`, `mountains` |
| `elf` | `forest`, `hills`, `grassland`, `marsh`, `mountains` |
| `dwarf` | `mountains`, `hills`, `forest`, `grassland`, `marsh` |
| `orc` | `hills`, `mountains`, `marsh`, `grassland`, `forest` |
| `kobold` | `mountains`, `hills`, `marsh`, `forest`, `grassland` |
| `halfling` | `grassland`, `hills`, `forest`, `marsh`, `mountains` |

The order decides which candidate pool an account is placed from. See
[Player origin reference](player-origin.md#placement-procedure).

## Polar ice

Row `-height` and row `+height` are `ice` across their full width. They are the
northern and southern edges of the world.

Nothing may enter an ice hex. Impassability is a property of the terrain, not
of the row: a hex is closed because it is ice, not because of where it lies.

An ice hex reports the elevation of the ground beneath the sheet, which is
whatever the generator produced for that hex. It is the only terrain whose
elevation may be either positive or negative.

## Generation

The generator runs these stages, in this order. Stages that visit every hex
visit them row by row from `r = -height` to `r = +height`, and from
`q = -width` to `q = +width` within a row.

Every stage wraps east-west. The noise fields repeat over exactly the width of
the world, and each pass that walks the map follows the wrap, so terrain is
continuous across the meridian.

1. **Elevation field.** A five-octave gradient-noise field is sampled at each
   hex's position in the plane. The sampling position is displaced first by a
   second and third noise field, which gives coastlines their irregular shape.
   No hex is special-cased, and no elevation falloff is applied at any edge.
2. **Sea level.** The `42nd` percentile of the elevation field is taken as sea
   level. Hexes at or below it are water; the rest are land. Taking sea level
   as a percentile fixes the proportion of water in every world.
3. **Ocean and lakes.** Water hexes are grouped into connected bodies. A body
   is `ocean` if it is the largest one or if it reaches row `-height` or row
   `+height`. Every other body is `lake`.
4. **Depth.** Each water hex is measured by its hex distance to the nearest
   land, and its elevation is graded from that distance.
5. **Moisture.** One of the six cube directions is drawn as the prevailing
   wind. Hexes are visited in order of their position along that direction.
   Each hex receives moisture carried from its upwind neighbour, gains moisture
   over water, and loses to rainfall what it receives — more when the ground
   rises. A purely east-west wind has no upwind edge to enter from, so it
   circles the world three times and only the last lap's rainfall is kept.
   Rainfall is combined with a fourth noise field.
6. **Terrain.** Land hexes are ranked by elevation and by moisture among all
   land hexes. The two ranks select terrain and elevation together.
7. **Polar ice.** Row `-height` and row `+height` are set to `ice`. Their
   elevation is left as the earlier stages produced it.

Land terrain follows from the elevation rank and the moisture rank:

| Elevation rank | Moisture rank | Terrain |
| --- | --- | --- |
| `0.88` and above | any | `mountains` |
| `0.62` to `0.88` | `0.72` and above | `forest` |
| `0.62` to `0.88` | below `0.72` | `hills` |
| below `0.22` | `0.74` and above | `marsh` |
| below `0.62` | `0.45` and above | `forest` |
| below `0.62` | below `0.45` | `grassland` |

Stages 1 through 6 see an unbroken world. Sea level is a percentile of every
row, the ranks are taken against every land hex, and the water bodies of stage
3 include the polar rows. The ice of stage 7 is laid over the result, so the
rows between the poles hold the terrain they would hold if the world had no ice
at all.

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

The lattice coordinate is wrapped east-west before it is used as an address,
which is what makes a field repeat over the width of the world.

The prevailing wind is drawn from this key path:

```text
[prng.TagWorld, 5]
```

Field numbers are part of the compatibility surface. They are never renumbered,
and new fields are appended.

The same game seeds and world dimensions always produce the same world. No
stage depends on the order hexes are visited beyond the orders stated above,
and no address contains a database row identifier.

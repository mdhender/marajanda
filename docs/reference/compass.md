# Compass reference

The six directions a hex has a neighbour in, the names they answer to, and the
order the rules walk them in.

Implemented by `internal/compass`. See [#27](https://github.com/mdhender/marajanda/issues/27)
for why it is implemented here rather than requested from HEXG.

## The points

| Order | Point | Long name | Axial step | Offset step (even-r) |
| --- | --- | --- | --- | --- |
| 1 | `NE` | north-east | `q+1, r-1` | one row up, right on even rows |
| 2 | `E` | east | `q+1, r+0` | one column right |
| 3 | `SE` | south-east | `q+0, r+1` | one row down, right on even rows |
| 4 | `SW` | south-west | `q-1, r+1` | one row down, left on odd rows |
| 5 | `W` | west | `q-1, r+0` | one column left |
| 6 | `NW` | north-west | `q+0, r-1` | one row up, left on odd rows |

## There is no north

The world is drawn pointy-top. A hex has flat edges east and west and vertices
at the top and bottom, so no hex borders another due north or due south.

Due north and due south are still real bearings, and a player will write them.
They are rejected as a distinct failure that names the two points either side,
not as unrecognized words. See Parsing below.

## Order

The order above is the order and it is a game rule, not a presentation choice.
Rules that require visiting a hex's neighbours in turn visit them in this order.

It is clockwise on a drawn map, starting from north-east. Measured from due east
with the screen's downward y, the six points are at 300, 0, 60, 120, 180 and 240
degrees.

The order is not HEXG's. HEXG numbers its six directions starting due east and
running anticlockwise. That numbering is an implementation detail of
`internal/compass`, does not leave the package, and must not appear in game
rules, orders, or stored data.

## Names

Parsing accepts, for each point:

- the abbreviation: `NE`, `E`, `SE`, `SW`, `W`, `NW`
- the hyphenated name: `north-east`, `east`, `south-east`, `south-west`, `west`,
  `north-west`
- the same name unhyphenated: `northeast`, `southeast`, `southwest`, `northwest`

Matching is case-insensitive. Surrounding whitespace is ignored, and spaces,
hyphens and underscores inside the word are ignored, so `north east`,
`North-East` and `NORTHEAST` are all north-east.

A point prints as its abbreviation and names itself with its hyphenated long
name. Both forms parse back to the point that produced them.

## Parsing failures

| Input | Result |
| --- | --- |
| `north`, `n`, `south`, `s` | `ErrNoNeighbor`, naming the two points either side |
| anything else that is not a listed name | `ErrUnknownPoint` |

Neither returns a usable point. The zero value of a point is not a compass
point, so an order that failed to parse cannot travel north-east by default.

## Stepping

Every function that lands on a hex takes the world's cylinder and returns a
canonical hex.

- Columns wrap. A step east from the eastmost column is the westmost column, one
  hex east, not most of a world west.
- Rows do not wrap. A step off the top or bottom of the world returns a row
  outside the world. Whether that is impassable ice or a rejected order is a
  game rule the caller applies; the compass reports six neighbours from every
  hex and judges none of them.

Normalizing is not a convenience. Marajanda derives a private PRNG stream from
hex coordinates, so the same ground reached from the east and from the west must
carry the same name. See [Datastore](../DATASTORE.md) and `internal/prng/doc.go`.

## Where it is exercised

The admin map lists the six hexes around its window centre, numbered, in this
order, with the terrain of each. It is a rehearsal of the movement rules rather
than a game feature: it walks the same compass, on the same cylinder, and shows
what a faction standing there could step to. See
[Map view reference](map-view.md#around-the-centre).

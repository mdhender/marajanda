# About the size of the world

A world is generated once from its seeds and its dimensions, and those
dimensions can never change afterwards. So "how big is a world" is a question
that has to be answered before the first database is created, and answering it
badly is expensive: too small and the map is a diorama, too large and it cannot
be generated inside the request that asks for it.

This document explores one possible answer — a world holding roughly as much
land as Earth — and works out what it would take at two hex scales. **It is
informative, not normative.** Nothing here has been decided, nothing in the
code implements it, and no other document is bound by it. It is here so that
the option can be weighed later with the arithmetic already done.

[docs/reference/terrain.md](../reference/terrain.md) describes the world the
generator actually makes, and
[docs/reference/reference-world.md](../reference/reference-world.md) records a
specific instance of it.

## One Earth of land

The target is stated in land rather than in hexes: a world would hold about
**57.5 million square miles** of land, which is Earth's.

Land is the right thing to hold fixed because land is the part players use.
Ocean is a boundary, not a place; doubling the sea doubles the storage and the
generation time and gives nobody anywhere new to walk. Hexes are the wrong
thing to hold fixed for the same reason — a hex count means nothing until it is
multiplied by a scale, and the scale is the thing this document has to pick
twice.

The consequence is worth stating plainly, because it is easy to miss inside the
arithmetic: this is **not** a model of Earth. Earth's land is 57.5 million
square miles out of a total surface of about 197 million, or 29 percent. A
world like this reaches the same land area on a total surface of 99 million —
half of Earth's — because it is 58 percent land. It is Earth's continents with
most of the ocean drained away.

## The assumptions it rests on

| Assumption | Value | Where it comes from |
| --- | --- | --- |
| Earth's land area | 57.5 million sq mi | Measured. |
| Land share of the map | 58% | The generator's `waterFraction` of `0.42`. |
| Hex shape | regular hexagon | The map is a hex grid. |
| Hex scale | 6 or 12 miles across the flats | The two options below. |
| Shape of the rendered map | 2.39 : 1 | Taste. It is the ratio modern widescreen film is shot at, and it looks right. |

Only the first is a measurement. The land share is a generator constant picked
for playability rather than derived from anything, and the last is frankly a
preference — which is a perfectly good reason for it, as long as it is not
mistaken for a derived quantity later.

That the 58 percent is a choice is the load-bearing assumption. It is what
converts 57.5 million square miles of land into 99 million square miles of
world, and a different water fraction moves every number that follows.

## The picture is not the grid

The 2.39 describes the **PNG**, not the hex counts, and the two are not the
same ratio. In a pointy-top layout neighbouring columns sit `2a` apart while
neighbouring rows sit only `sqrt(3) * a` apart, so a row of hexes is wider than
a column of them is tall. `worldmap.Render` lays the image out accordingly:

```text
width  = sqrt(3) * hexSize * (columns + 0.5)
height =           hexSize * (1.5 * rows + 0.5)
```

`hexSize` cancels, which is worth knowing on its own: the shape of the picture
is a property of the world's dimensions and nothing else, so `--hex-size`
changes how big the PNG is and never what shape it is.

Dividing the two gives a picture about `1.1547 * columns / rows` wide, so a
**2.39 : 1 image wants a grid nearer 2.07 : 1**. Applying the 2.39 to the hex
counts directly — the obvious move, and the wrong one — would render at about
2.76 : 1 instead, noticeably wider than intended.

Worth noting where the defaults already sit: 511 x 255 renders at 2.3132 : 1.
The preference is a slightly wider frame than the one we look at today, not a
departure from it.

## Working the numbers back to a rectangle

A hex is measured **across the flats**, so a six-mile hex has an apothem of
three miles. A regular hexagon with apothem `a` covers `2 * sqrt(3) * a^2`:

```text
6-mile hex   a = 3    31.18 square miles
12-mile hex  a = 6   124.71 square miles
```

Earth's land at 58 percent land coverage needs `57,500,000 / 0.58`, or about
**99.14 million square miles** of map, whichever scale draws it. Dividing that
by the two hex areas gives the two hex counts; doubling the scale quarters the
count exactly, so no accuracy is lost between them.

Solving each count against the rendered-shape equation above, and rounding to
odd extents — the datastore stores half-extents, and a world is
`2 * width + 1` by `2 * height + 1`, so an even extent is not expressible —
gives:

| | Six-mile hex | Twelve-mile hex |
| --- | ---: | ---: |
| Across the flats | 6 miles | 12 miles |
| Apothem | 3 miles | 6 miles |
| Area of one hex | 31.18 sq mi | 124.71 sq mi |
| Hexes for 99.14M sq mi | ~3,180,000 | ~795,000 |
| Rectangle | **2565 x 1239** | **1283 x 619** |
| Stored half-extents | 1282, 619 | 641, 309 |
| Total hexes | 3,178,035 | 794,177 |
| Grid ratio | 2.0702 : 1 | 2.0727 : 1 |
| Rendered PNG | 2.3903 : 1 | 2.3930 : 1 |
| Land hexes at 58% | 1,843,260 | 460,623 |
| Land area produced | 57.47M sq mi | 57.44M sq mi |

Both land figures sit within a tenth of a percent of the 57.5 million target
and both pictures within a fifth of a percent of 2.39, which is closer than
either input deserves.

## The same world at two resolutions

The two options are not a large world and a small one. They are one world drawn
at two resolutions, and the miles say so:

| | Six-mile hex | Twelve-mile hex |
| --- | ---: | ---: |
| East-west circumference | 15,390 miles | 15,396 miles |
| North-south extent | 6,438 miles | 6,432 miles |

A leader crossing either world walks the same distance. What changes is how
many hexes that distance is cut into — and therefore how much terrain detail
the map can hold, how far a step goes, and how many rows the database stores.
The choice between them is a choice about granularity, not about scale.

That reframing matters for the rules. Action points are spent per step, so at
six-mile hexes a turn's allowance carries an entity half as far across the
ground as it would at twelve. The two options are not interchangeable once
movement is priced.

## What the generator already does correctly

Two things this exercise needs are already true of the code.

Sea level is taken as a **percentile of the whole elevation field** rather than
as a fixed height, so 42 percent of hexes end up below it whatever shape the
noise happens to take. That is a percentile by cell count, which is the
reliable form: bucketing elevations and calling the lowest 42 buckets water
would only work if the buckets held equal populations, and they do not.

And the generator determines land and water from that percentile rather than
counting toward a target. Small deviations from 58 percent are the expected
result of a percentile applied to real noise, not errors. The reference world
lands at 57.5 percent land — close enough that the arithmetic above survives
using it instead, but a reminder that 58 is a nominal figure. Polar ice takes
its own slice, and belongs to neither side of the split.

## How they compare with the world we generate today

| | Hexes | vs. reference world | vs. current maximum |
| --- | ---: | ---: | ---: |
| Reference world (511 x 255) | 130,305 | 1x | 0.25x |
| Current maximum (1023 x 511) | 522,753 | 4x | 1x |
| Twelve-mile option (1283 x 619) | 794,177 | 6.1x | 1.5x |
| Six-mile option (2565 x 1239) | 3,178,035 | 24.4x | 6.1x |

Neither option fits. The twelve-mile world is half again as large as the
largest world the datastore currently permits; the six-mile world is six times
it. Both also break the ceilings in *both* directions — the twelve-mile world
wants half-extents of 641 and 309 against present caps of 511 and 255, and the
six-mile world 1282 and 619.

Those ceilings exist for a specific reason, and it is not storage. The
datastore's own note says the maxima are set by "what a single core can
generate at database creation without the request that triggered it timing
out". Half a million hexes is about thirteen megabytes on disk, which puts the
twelve-mile world near twenty megabytes and the six-mile world near eighty —
neither troubling. The binding constraint is that world generation happens
inside an HTTP request. Either option means taking generation out of that
request, or making it fast enough to stay in it. That is the real work hiding
behind both numbers, and it is four times as much work for the six-mile world
as for the twelve.

## What it leaves open

**Which resolution, if either.** The six-mile hex is the tabletop convention
and gives terrain somewhere to vary; the twelve-mile hex costs a quarter as
much of everything. Nothing decides between them yet, and the movement rules
have as much say in it as the generator does.

**Whether the noise still looks right at this size.** Terrain features are
counted in *periods across the world*, not in cycles per hex, precisely so the
shape of a world survives a change of size — a bigger world gets bigger
continents rather than more of them. Six elevation periods across 511 columns
puts a feature every 85 hexes; across 1283 it puts one every 214, and across
2565 one every 428. In miles those last two are the same continent, about 2,565
across, which is the point: resolution does not change the shape, and ground
extent does. Whether continents that wide are what anyone wants is the question
tracked in [#20](https://github.com/mdhender/marajanda/issues/20).

**Whether a scale in miles belongs in the code at all.** Nothing in the
generator or the datastore knows how far a hex is; the map is dimensionless,
and distance reaches players as action points rather than as miles. Fixing a
hex scale introduces a unit the rules do not currently have, and it would want
a reference of its own.

## Sources

- [An overview of 6 mile hexes](https://youtu.be/IBvuV9zuXnQ)

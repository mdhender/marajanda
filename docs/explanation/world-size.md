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
| Aspect ratio | 2.39 : 1 | Chosen. Unexplained. |

Only the first is a measurement. The land share is a generator constant that
was picked for playability, not derived from anything; the last has no stated
reason at all.

That the 58 percent is a choice is the load-bearing assumption. It is what
converts 57.5 million square miles of land into 99 million square miles of
world, and a different water fraction moves every number that follows.

## Working the numbers back to a rectangle

A hex is measured **across the flats**, so a six-mile hex has an apothem of
three miles. A regular hexagon with apothem `a` covers `2 * sqrt(3) * a^2`:

```text
6-mile hex   a = 3    31.18 square miles
12-mile hex  a = 6   124.71 square miles
```

Earth's land at 58 percent land coverage needs `57,500,000 / 0.58`, or about
**99.14 million square miles** of map, whichever scale draws it. Dividing that
by the two hex areas gives the two hex counts. Doubling the scale quarters the
count exactly, so the second is a quarter of the first and no accuracy is lost
between them.

Splitting each by the chosen aspect ratio — `width = 2.39 * height` and
`width * height` equal to the count — gives the two rectangles. Both extents
have to be odd: the datastore stores dimensions as half-extents and a world is
`2 * width + 1` by `2 * height + 1`, so 2757 x 1153 and 1379 x 577 are
expressible where 2758 x 1154 and 1380 x 578 would not be.

| | Six-mile hex | Twelve-mile hex |
| --- | ---: | ---: |
| Across the flats | 6 miles | 12 miles |
| Apothem | 3 miles | 6 miles |
| Area of one hex | 31.18 sq mi | 124.71 sq mi |
| Hexes for 99.14M sq mi | ~3,180,000 | ~795,000 |
| Rectangle | **2757 x 1153** | **1379 x 577** |
| Stored half-extents | 1378, 576 | 689, 288 |
| Total hexes | 3,178,821 | 795,683 |
| Aspect ratio | 2.3912 : 1 | 2.3899 : 1 |
| Land hexes at 58% | 1,843,716 | 461,496 |
| Land area produced | 57.48M sq mi | 57.55M sq mi |

Both land figures sit within a tenth of a percent of the 57.5 million target,
so neither rectangle would earn anything by being nudged further.

## The same world at two resolutions

The two options are not a large world and a small one. They are one world drawn
at two resolutions, and the miles say so:

| | Six-mile hex | Twelve-mile hex |
| --- | ---: | ---: |
| East-west circumference | 16,542 miles | 16,548 miles |
| North-south extent | 5,991 miles | 5,996 miles |

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
| Twelve-mile option (1379 x 577) | 795,683 | 6.1x | 1.5x |
| Six-mile option (2757 x 1153) | 3,178,821 | 24.4x | 6.1x |

Neither option fits. The twelve-mile world is half again as large as the
largest world the datastore currently permits; the six-mile world is six times
it, and would need half-extents of 1378 and 576 against present ceilings of 511
and 255.

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

**Where 2.39 : 1 comes from,** and whether it is even the right kind of ratio.
It is a ratio of hex *counts*, which is not the shape of the ground. In a
pointy-top layout columns sit `2a` apart and rows sit `1.5 * a * 2/sqrt(3)`
apart, a ratio of about 1.155 to 1, so both rectangles above are close to
2.76 : 1 on the ground rather than 2.39 : 1. A world stated in miles and a
world stated in hexes are not the same shape, and the target does not say which
one it means.

**Whether the noise still looks right at this size.** Terrain features are
counted in *periods across the world*, not in cycles per hex, precisely so the
shape of a world survives a change of size — a bigger world gets bigger
continents rather than more of them. Six elevation periods across 511 columns
puts a feature every 85 hexes; across 1379 it puts one every 230, and across
2757 one every 459. In miles those last two are the same continent, about 2,760
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

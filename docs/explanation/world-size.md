# About the size of the world

A world is generated once from its seeds and its dimensions, and those
dimensions can never change afterwards. So "how big is a world" is a question
that has to be answered before the first database is created, and answering it
badly is expensive: too small and the map is a diorama, too large and it cannot
be generated inside the request that asks for it.

This document is about one proposed answer — a world holding roughly as much
land as Earth — where its numbers come from, and what adopting it would cost.
It is a proposal, not a description of the world the generator makes today.
[docs/reference/terrain.md](../reference/terrain.md) describes that world, and
[docs/reference/reference-world.md](../reference/reference-world.md) records a
specific instance of it.

## One Earth of land

The target is stated in land rather than in hexes: a world should hold about
**57.5 million square miles** of land, which is Earth's.

Land is the right thing to hold fixed because land is the part players use.
Ocean is a boundary, not a place; doubling the sea doubles the storage and the
generation time and gives nobody anywhere new to walk. Hexes are the wrong
thing to hold fixed for the same reason — a hex count means nothing until it is
multiplied by a scale, and the scale is the thing this document has to pick.

The consequence is worth stating plainly, because it is easy to miss inside the
arithmetic: this is **not** a model of Earth. Earth's land is 57.5 million
square miles out of a total surface of about 197 million, or 29 percent. The
proposed world reaches the same land area on a total surface of 99 million —
half of Earth's — because it is 58 percent land. It is Earth's continents with
most of the ocean drained away.

## The assumptions it rests on

| Assumption | Value | Where it comes from |
| --- | --- | --- |
| Earth's land area | 57.5 million sq mi | Measured. |
| Land share of the map | 58% | The generator's `waterFraction` of `0.42`. |
| Hex shape | regular hexagon | The map is a hex grid. |
| Hex apothem | 6 miles | Chosen. See the open questions below. |
| Aspect ratio | 2.39 : 1 | Chosen. Unexplained. |

Only the first is a measurement. The land share is a generator constant that
was picked for playability, not derived from anything; the other two are
choices, and the last one has no stated reason at all.

That the 58 percent is a choice is the load-bearing assumption. It is what
converts 57.5 million square miles of land into 99 million square miles of
world, and a different water fraction moves the whole result.

## Working the numbers back to a rectangle

A regular hexagon with apothem `a` covers `2 * sqrt(3) * a^2`, so a six-mile
apothem gives a hex of about **124.71 square miles**.

Earth's land at 58 percent land coverage needs
`57,500,000 / 0.58`, or about **99.14 million square miles** of map, which at
124.71 square miles a hex is about **795,000 hexes**.

Splitting that by the chosen aspect ratio — `width = 2.39 * height` and
`width * height = 795,000` — gives a height of about 577 and a width of about
1,378. Rounding to a convenient pair:

```text
1379 x 577 = 795,683 hexes, an aspect ratio of 2.38995 : 1
```

At 58 percent that is about 461,000 land hexes, or **57.55 million square
miles** of land: within a tenth of a percent of the target, so no further
adjustment earns its keep.

Both extents are odd, which matters. The datastore stores dimensions as
half-extents and a world is `2 * width + 1` by `2 * height + 1`, so 1379 x 577
is expressible — it is half-extents of 689 and 288 — where 1380 x 578 would
not be.

## What the generator already does correctly

Two things this target needs are already true of the code, and neither should
be re-decided when it is adopted.

Sea level is taken as a **percentile of the whole elevation field** rather than
as a fixed height, so 42 percent of hexes end up below it whatever shape the
noise happens to take. That is a percentile by cell count, which is the
reliable form: bucketing elevations and calling the lowest 42 buckets water
would only work if the buckets held equal populations, and they do not.

And the generator determines land and water from that percentile rather than
counting toward a target. Small deviations from 58 percent are the expected
result of a percentile applied to real noise, not errors. The reference world
lands at 57.5 percent land — close enough that the arithmetic above survives
using it instead (about 57.05 million square miles, still within one percent),
but a reminder that 58 is a nominal figure. Polar ice takes its own slice, and
belongs to neither side of the split.

## How it compares with the world we generate today

| | Hexes | Relative |
| --- | ---: | ---: |
| Reference world (511 x 255) | 130,305 | 1x |
| Current maximum (1023 x 511) | 522,753 | 4x |
| Proposed target (1379 x 577) | 795,683 | 6.1x |

The target is six times the world the defaults produce and **half again as
large as the largest world the datastore currently permits**. Adopting it is
therefore not a matter of passing bigger numbers to `--width` and `--height`;
the ceilings have to move first.

Those ceilings exist for a specific reason, and it is not storage. The
datastore's own note says the maxima are set by "what a single core can
generate at database creation without the request that triggered it timing
out". Half a million hexes is about thirteen megabytes on disk, so 795,683 is
on the order of twenty — untroubling. The binding constraint is that world
generation happens inside an HTTP request. Reaching this target means taking
generation out of that request, or making it fast enough to stay in it. That
is the real work item hiding behind the number.

## What it leaves open

**Whether a six-mile hex is six miles across or twelve.** The derivation reads
"six-mile hex" as an apothem of six, making each hex twelve miles from flat to
flat. The tabletop tradition the linked overview comes from generally measures
a six-mile hex *across the flats*, which puts the apothem at three miles and
quarters the area to about 31 square miles. Under that reading the same target
needs about 3.2 million hexes, which is out of reach by any route. This single
ambiguity is worth four times the entire map, so it should be settled before
anything is built on it.

**Where 2.39 : 1 comes from,** and whether it is even the right kind of ratio.
It is a ratio of hex *counts*, which is not the shape of the ground. In a
pointy-top layout columns sit `2a` apart and rows sit `1.5 * a * 2/sqrt(3)`
apart — 12 miles against 10.39 — so 1379 x 577 hexes is 16,548 miles around by
5,996 tall, a ground ratio of 2.76 : 1. A world stated in miles and a world
stated in hexes are not the same shape, and the target does not say which one
it means.

**Whether the noise still looks right at this size.** Terrain features are
counted in *periods across the world*, not in cycles per hex, precisely so the
shape of a world survives a change of size — a bigger world gets bigger
continents rather than more of them. Six elevation periods across 511 columns
puts a feature every 85 hexes; across 1379 it puts one every 230. Whether
continents 2,760 miles wide are what anyone wants is the question tracked in
[#20](https://github.com/mdhender/marajanda/issues/20), and this target is the
case that makes it urgent.

**Whether a scale in miles belongs in the code at all.** Nothing in the
generator or the datastore knows how far a hex is; the map is dimensionless,
and distance reaches players as action points rather than as miles. Fixing an
apothem introduces a unit the rules do not currently have, and it would want a
reference of its own.

## Sources

- [An overview of 6 mile hexes](https://youtu.be/IBvuV9zuXnQ)

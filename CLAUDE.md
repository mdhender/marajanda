# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build ./... && go vet ./... && gofmt -l .    # gofmt must print nothing
go test ./...

go test ./internal/generators/ -run TestNoUnpaintedHexes -v   # one test
go test ./internal/hexfield/ -count=3                         # statistical tests: check for flakes

go run ./cmd/hexweb                                        # browser UI, opens localhost:8080
go run ./cmd/hexweb -addr :9000 -open=false -timeout 5m    # scripted/curl checks; -timeout so it cannot leak
go run ./cmd/hexgen -levels 4 -island -ascii               # subdivision straight to the terminal
# generator name is the "gen" query param: /image?gen=tectonic&seed=7&radius=40
go run ./cmd/hexgen -compare out/ -palette gray            # every variant as separate PNGs
```

## Architecture

The point of the layout is that **adding a generator touches one file**. A generator
declares its tunables as `[]mapgen.Param` data; the web form, defaults, parsing,
clamping, and the picker entry are all derived from that declaration. `cmd/hexweb`
never changes.

```
internal/mapgen       registry: Generator, Param, Values. No concrete generators.
internal/generators   the generators, one file each, self-registering in init()
                      plus regions.go: the partition voronoi and tectonic share
internal/hexgrid      shared hex geometry: Coord, Directions, Render, palettes
internal/hexfield     midpoint subdivision only; depends on hexgrid
internal/world        the world datastore: a wrapping cylinder of hexes and
                      its JSON. Depends on maloquacious/hexg, not on hexgrid.
internal/worldgen     fills a world. Fault cuts the sphere with small circles
cmd/hexweb            server; imports generators for its init() side effects
cmd/hexgen            CLI for subdivision specifically
```

**There are two hex worlds in here and they do not meet.** `hexgrid` is the
original: hexagon-*shaped* maps in cube coordinates, rendered pixel-by-pixel,
with no notion of a globe. `world` is where the project is going: a rectangular
cylinder of columns that wraps east to west, which is what a donjon-style world
map is. They share no code, and merging them is not on the table — a hexagon and
a cylinder want different things.

`hexgrid` knows nothing about generation. A generator hands `hexgrid.Render` a
`func(Coord) (color.RGBA, bool)` and gets an image; the bool means "in the map",
and false paints `hexgrid.Background`. Rendering converts each *pixel* back to a
cube coordinate rather than filling polygons, so tile edges land exactly on hex
boundaries. Orientation lives only in `Render` — cube coordinates carry none.

### Constraints that are not obvious from the code

- **`math/rand/v2` sources only.** Both generators seed ChaCha8 from the `seed`
  param (`newRand`); not PCG, because seeds here are frequently small and
  sequential. A previous hand-rolled splitmix64 was removed for this rule.
- **Anything affecting output must be deterministic from the seed.** Go randomises
  map iteration order, so code that ranges over a map and influences the result
  must index by region/id instead and break ties by lowest index. `mergeSlivers`
  in `voronoi.go` is written that way for exactly this reason.
- **Guard output size against `maxPixels`** (`internal/generators/subdivision.go`),
  and return an error rather than attempting the allocation. Levels/radius and hex
  size multiply, so a hand-typed URL can otherwise ask for gigabytes.
- `Values` accessors never fail: unparseable input falls back to the declared
  default and numbers clamp to declared bounds, so generators skip validation.
  `mapgen.FromForm` treats an **absent bool as false** (browsers omit unchecked
  boxes) while `NewValues` still falls back to the default — do not collapse these.
- Declare float params as `Default: 4.0`, not `Default: 4`. An untyped int in an
  `any` field fails the `float64` assertion and silently yields 0.
- `partition` returns `all` in `hexgrid.Hexes` order precisely so callers have a
  deterministic order to walk. `tectonic` indexes every field by position in it
  and never ranges over `owner`; the BFS in `spread` is seeded in that order too,
  so ties break the same way on every run.

- **`internal/world` does its hex trigonometry nowhere.** Layout, pixel
  positions, and the offset/cube conversions all come from
  `github.com/maloquacious/hexg`. What the package keeps is the *frame*
  (flat-top odd-q, origin half a hex west) and the topology (the east-west
  wrap), because those are properties of the world, not of hex geometry. If you
  find yourself writing a `sqrt(3)` in this package, it belongs upstream.
- **A layer is absent or complete, never short.** `world.Layers` is
  struct-of-arrays; each slice is length 0 or exactly `Grid.Len()`, and
  `Validate` rejects anything else so no reader has to invent a meaning for a
  half-filled layer. Index is column-major (`col*Rows + row`) to match
  Worldographer's own `[col][row]` tile array.
- **`worldgen.Options` substitutes nothing.** Every field is used as given,
  zeroes included, and `Defaults` is where a caller gets sensible values. A
  "0 means unset" rule was tried and removed: `Offset: 0` is the great-circle
  case the package exists to contrast with, so the one setting worth asking
  for was the one setting you could not ask for.
- **The world layout is not a parameter, deliberately.** donjon's maps and
  Worldographer's files independently agree on flat-top odd-q columns, so
  offering a choice would only create conversions at both ends.

### Tests

`internal/generators/generators_test.go` runs against **every** registered
generator via `mapgen.All()`, so a new generator inherits coverage by existing:
unpainted hexes, determinism, seed sensitivity, the pixel cap, and param-declaration
soundness. `TestNoUnpaintedHexes` is the regression test for a slice-aliasing bug
where sites shared a backing array with the hex list — prefer extending these
registry-wide tests over per-generator ones.

`hexfield`'s creasing tests are statistical (24 trials); under ~16 the variants do
not separate. If one fails, re-run with `-count=3` before assuming a real change.

## Domain notes

Hexagons are not rep-tiles, so diamond-square has no direct hex analogue. Hex
*centres* form a triangular lattice, which does refine self-similarly. In cube
coordinates the level-k lattice is every hex whose coordinates divide by `2^k`, and
the four parity classes of `Q+R+S == 0` are the coarse lattice plus one midpoint
class per edge direction. Hence one subdivision step per level, not two.

In `tectonic` the boundary normal is taken from the two plate *centroids*, not
from the hex step to the neighbour. The partition is Voronoi in pixel space, so a
margin is perpendicular to the line joining its plates; classifying off the six
hex directions instead makes the class flicker hex to hex and turns every margin
into transform speckle. Relatedly, `shearDominance` is below 1 on purpose —
whichever component is larger winning outright makes half of all margins
transform, and the map comes out nearly flat.

Subdivision creasing is reduced, not eliminated — visible as a hexagonal cell
pattern under `-palette gray`, which the terrain palette's colour banding hides.
Use grayscale when judging lattice artefacts. `Relax` and `SRA` each fail alone and
in opposite directions and only work paired; the measured numbers are in README.md.

### The donjon reference, and the symmetry in it

`docs/downloads/` holds a donjon fantasy world map (`Eglar.png`, plus the
`.html` whose place index carries both grid and pixel coordinates). Two things
were established from it and are worth not re-deriving:

**Its geometry is exactly ours.** Fitting all 32 places gives
`x = 20*col + 20/3` and `y = 23.094*row`, plus half a row on odd columns, with
no outlier above 1e-6. That is flat-top odd-q hexes at circumradius 40/3 px,
200 columns across a 4000px image. donjon's world map *is* a hex map; the PNG
is only its rasterisation. `TestLayoutMatchesDonjonSample` pins us to it.

**Great-circle faulting is antipodally antisymmetric, and that is the "weird
symmetry".** Every cut raises one hemisphere and lowers the other, so each cut
is odd under p -> -p and so is any sum of them: `h(-p) = -h(p)`, exactly. A
replication of `mdhender/mapgen`'s Olsson port measures `corr(H, antipode) =
-1.000` at every fault count. Every continent is guaranteed an ocean at its
antipode. Consequences:

- Olsson's `I have only calculated faults for 1/2 the image` trick is *not* the
  cause. Running the sweep over the full width and deleting the mirror produces
  statistically identical output; the mirror exploits the symmetry, it does not
  create it.
- donjon has it too: land/sea against its own antipode scores -0.683 on the
  sample, against -0.24..-0.38 for control shifts.
- To break it, the cut must not be odd under the antipodal map. The smallest
  change is a **small circle** — offset the cutting plane from the centre —
  which is what `worldgen.Fault` does, through `Options.Offset`.

Measured on `worldgen.Fault` itself, averaged over seeds and on a grid where
every hex has an exact antipode (see below), `corr(h, antipode)` against the
offset:

| offset | 0.0 | 0.1 | 0.25 | 0.5 | 0.75 | 0.9 |
|---|---|---|---|---|---|---|
| corr | **-1.00** | -0.83 | -0.63 | **-0.47** | -0.41 | -0.41 |

It plateaus around -0.4 rather than reaching 0, and that is inherent: for a cut
at offset d, the even (symmetric) part of the field lives only in the band
where `|p·n| < |d|`, so displacing the plane shrinks the odd part without
removing it. This is not worth chasing — Earth's own land is famously
antipodal to water, and donjon scores -0.68, so the 0.5 default is already
less mirrored than the reference it is imitating.

Generating on the sphere means there is **no seam**: the wrap's mean step
measures 0.98x the interior mean, i.e. indistinguishable. Regression tested
over eight seeds, because one map's seam is 60 differences against an interior
of 7,000 and a single trial is noise.

### Latitude, and why hex centres sit half a band off the poles

`Grid.LatLon` measures latitude between the poles as `Grid.poles` defines
them — half a row beyond the outermost hex *centres* — not between the edges
of the image. A hex covers a band of latitude and sits in the middle of it, so
the northernmost centre is at +88.971 on a 200x87 grid and the southernmost at
exactly -88.971.

The earlier version divided by the image height, which put row 0 on the pole
at +90.000 while the last row stopped 1.03 degrees short of -90. Two things
went wrong with that, both fixed by the half-band offset:

- The hemispheres were not mirror images, so anything sized off latitude — an
  ice cap, a climate band — came out different at the two ends of the map.
  `TestLatitudeIsSymmetric` now checks the whole sampled set, not just the
  extremes.
- Every even column of row 0 landed on the pole *exactly*, so all 100 of them
  were one point sampled a hundred times and held one identical elevation.
  They now sit on a small circle just short of it and carry 7 distinct values.

Two parity rules follow from the stagger, and neither is optional:

- **A wrapping grid needs an even column count.** Odd columns are the ones
  pushed half a row south, so the cylinder only closes if the last column and
  column 0 have opposite parity. `Validate` rejects the rest.
- **Exact antipodes need `Cols = 2 (mod 4)`.** A mirrored latitude lives in
  the other parity class, so the antipodal column `col + Cols/2` must have the
  opposite parity to `col`. On 48 columns the nearest hex to an antipode is
  half a band away and the measured correlation blurs by about 0.06 — enough
  to hide the property. The tests use 50.

### Upstream: hexg

`maloquacious/hexg` (the standalone module, v1.0.1) is the one to use. The copy
vendored inside `maloquacious/wxx` as `wxx/hexg` is a stub: `OddQLayout` panics
in 12 of 13 methods, and every coordinate type has unexported fields with no
accessors, so `wxx.Tile_t.Coords` marshals as `{}`. Filed as maloquacious/wxx
[#52](https://github.com/maloquacious/wxx/issues/52) (replace the vendored copy),
[#53](https://github.com/maloquacious/wxx/issues/53) and
[#54](https://github.com/maloquacious/wxx/issues/54). Add the `wxx` dependency
back when the Worldographer exporter is written, not before.

`docs/` holds RPG source PDFs (gitignored), the donjon samples above, and a
reconstructed changelog. `out/` is generated images (gitignored).

---

An OpenAI Codex config exists at `~/.codex/config.toml`. To bring anything across,
reply `/import` to scan and list what's importable, then `/import --yes=<digest>`
using the digest that scan prints. If `/import` is unavailable here, run
`claude import` from a terminal.

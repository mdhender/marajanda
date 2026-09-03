# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build ./... && go vet ./... && gofmt -l .    # gofmt must print nothing
go test ./...

go test ./internal/generators/ -run TestNoUnpaintedHexes -v   # one test
go test ./internal/hexfield/ -count=3                         # statistical tests: check for flakes

go run ./cmd/hexweb                             # browser UI, opens localhost:8080
go run ./cmd/hexweb -addr :9000 -open=false     # for scripted/curl checks
go run ./cmd/hexgen -levels 4 -island -ascii    # subdivision straight to the terminal
go run ./cmd/hexgen -compare out/ -palette gray # every variant as separate PNGs
```

The local directory is `marjanda`; the module and GitHub repo are `marajanda`. The
directory name is a typo, deliberately left alone — nothing depends on it.

## Architecture

The point of the layout is that **adding a generator touches one file**. A generator
declares its tunables as `[]mapgen.Param` data; the web form, defaults, parsing,
clamping, and the picker entry are all derived from that declaration. `cmd/hexweb`
never changes.

```
internal/mapgen       registry: Generator, Param, Values. No concrete generators.
internal/generators   the generators, one file each, self-registering in init()
internal/hexgrid      shared hex geometry: Coord, Directions, Render, palettes
internal/hexfield     midpoint subdivision only; depends on hexgrid
cmd/hexweb            server; imports generators for its init() side effects
cmd/hexgen            CLI for subdivision specifically
```

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

Subdivision creasing is reduced, not eliminated — visible as a hexagonal cell
pattern under `-palette gray`, which the terrain palette's colour banding hides.
Use grayscale when judging lattice artefacts. `Relax` and `SRA` each fail alone and
in opposite directions and only work paired; the measured numbers are in README.md.

`docs/` holds RPG source PDFs (gitignored) and a reconstructed changelog. `out/` is
generated images (gitignored).

---

An OpenAI Codex config exists at `~/.codex/config.toml`. To bring anything across,
reply `/import` to scan and list what's importable, then `/import --yes=<digest>`
using the digest that scan prints. If `/import` is unavailable here, run
`claude import` from a terminal.

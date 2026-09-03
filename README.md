# marajanda

Hex map generators.

```
cmd/hexweb              browser front end: pick a generator, adjust, render
cmd/hexgen              CLI for the subdivision generator
internal/mapgen         the registry generators plug into
internal/generators     the generators themselves, one file each
internal/hexgrid        shared hex geometry: cube coords, layout, rendering
internal/hexfield       midpoint subdivision on a hex grid
```

## Generators

**Midpoint subdivision** produces a continuous height field: recursive midpoint
displacement on the triangular lattice of hex centres, the hex analogue of
diamond-square. See below.

**Voronoi regions** produces discrete areas instead: sites are scattered across
the map and every hex goes to its nearest one. Lloyd relaxation evens out the
sizes, and the distance metric decides whether borders follow hex steps or run
straight. The shape wanted for realms, faction territory or biome patches
rather than terrain.

`internal/hexgrid` holds what both need — cube coordinates, the six directions,
palettes, and rendering a hexagon-shaped map by converting each pixel back to a
coordinate. It knows nothing about how a map is generated; a generator supplies
a function from coordinate to colour.

## Running it

```sh
go run ./cmd/hexweb              # opens a browser on localhost:8080
go run ./cmd/hexweb -addr :9000 -open=false
```

Pick a generator, adjust the parameters, hit Render. The image opens in a new
tab as a plain `GET /image?...` URL, so it is shareable and bookmarkable. Seeds
default to a fresh random value on every page load; the **New** button asks the
server for another.

## Adding a generator

Implement `mapgen.Generator` in a new file under `internal/generators` and
register it in `init`. Declare the tunables as `mapgen.Param` values and the
web form, defaults, parsing, clamping and the picker entry all follow. The
server does not change.

```go
func init() { mapgen.Register(myGen{}) }

func (myGen) Params() []mapgen.Param {
    return []mapgen.Param{
        {Name: "seed", Label: "Seed", Kind: mapgen.KindSeed},
        {Name: "levels", Label: "Levels", Kind: mapgen.KindInt,
         Default: 7, Min: 1, Max: 9},
    }
}

func (myGen) Generate(v mapgen.Values) (image.Image, error) {
    // v is already clamped to the declared bounds.
    return render(v.Int("levels"), v.Uint64("seed"))
}
```

Values accessors never fail: missing or unparseable input falls back to the
declared default and numbers are clamped, so a hand-typed URL cannot push a
generator outside the range it said it handles. Guard the output size, though
— `internal/generators` caps the rendered image at 40 megapixels, since levels
and hex size multiply.

Randomness comes from `math/rand/v2` sources only.

---

# internal/hexfield

Fractal terrain generation on a hex grid, by recursive midpoint displacement.

This is the hex analogue of the diamond-square algorithm. It is not a direct
port, because there isn't one.

## Why it isn't diamond-square

Hexagons are not rep-tiles: a hexagon cannot be divided into smaller hexagons.
Split one and you get three rhombi or six triangles, never hexagons. Looking
for the analogue of "split the square into four squares" is a dead end.

The way through is to stop thinking about tiles and look at the lattice of
*centres*. Hex centres form a **triangular lattice**, and triangular lattices
do refine self-similarly: insert a point at every edge midpoint and you get a
triangular lattice at half the spacing, with each triangle becoming four. That
is the recursion here, and it is Loop subdivision's topology.

In cube coordinates it falls out exactly. Define the **level-k lattice** as
every hex whose `Q`, `R` and `S` are all divisible by `2^k`. The midpoint of
two adjacent level-k points lands precisely on the level-(k−1) lattice:

```
p = (0,0,0)   p' = (2,-2,0)   midpoint = (1,-1,0)     integer, and sums to 0
```

Because `Q+R+S == 0`, coordinate parities fall into exactly four classes:

| parity of (Q,R,S)  | role                                     |
|--------------------|------------------------------------------|
| (even, even, even) | the coarse lattice — already has values  |
| (odd, odd, even)   | midpoints along direction (1,−1,0)       |
| (odd, even, odd)   | midpoints along direction (1,0,−1)       |
| (even, odd, odd)   | midpoints along direction (0,1,−1)       |

Three new classes, one per edge direction, so each pass adds exactly three
points per existing point and quadruples the density. Nothing is missed and
nothing is written twice.

**This needs only one step per level.** Square grids need a diamond phase and
a square phase because their midpoints come in two incompatible flavours; the
triangular lattice refines uniformly.

## Grid shape

A **hexagon of radius 2^K**. It is convex in the hex metric, so midpoints of
in-bounds points stay in bounds; it has no preferred axis; and its coarsest
lattice is exactly seven points — origin plus six at radius N — which refines
with no leftovers.

```
Levels 0: radius  1 ->    7 hexes   (the seed)
Levels 1: radius  2 ->   19 hexes
Levels 2: radius  4 ->   61 hexes
Levels 6: radius 64 -> 12481 hexes      3N^2 + 3N + 1
```

Those seven seed values are the caller's control over the large-scale shape.
Pinning them is the reason to reach for subdivision rather than summed noise
octaves in the first place.

If you want a **wrapping** world instead, use a rhombus of side `2^K` seeded
at four corners; a rhombus tiles the plane by translation, so `Q` and `R`
modulo the side length give a torus. Not implemented here.

## Orientation

Cube coordinates carry no orientation. Pointy-top versus flat-top is entirely
the layout matrix applied at render time — a 30° rotation — so every
adjacency, distance and midpoint in the algorithm is identical either way.
Orientation is confined to `Field.Image`.

## Creasing, and what actually helps

Subdivision writes a point once and never revisits it, so the few values fixed
while the displacement amplitude was largest act as frozen scaffolding that
shows through as ridges along the lattice axes. Two knobs address it, and
**they fail in opposite directions on their own**:

- `Relax` — the Loop **vertex mask**, repositioning existing points towards
  their neighbourhood average (5/8 point + 3/8 neighbours) before each round
  of midpoints. A naive port omits this half of Loop subdivision entirely.
- `SRA` — successive random additions (Voss): perturb every point at every
  level, not just newly inserted ones.

Measured as the mean deviation of coarse-lattice points from their
neighbourhood, divided by the same for finest-level points. **1.0 means no
creasing signature**; above 1 the old points spike, below 1 they sit in flat
spots. 40 trials, standard error ≈ 0.02–0.05:

| variant                  | H=0.5 | H=0.7 | H=0.9 |
|--------------------------|-------|-------|-------|
| midpoint, bare           | 0.507 | 0.628 | 0.900 |
| loop, bare               | 0.834 | 1.335 | 2.423 |
| loop + SRA only          | 1.802 | 2.467 | 3.805 |
| loop + relax only        | 0.406 | 0.430 | 0.490 |
| **loop + relax + SRA**   | 1.125 | 1.195 | 1.324 |
| **midpoint + relax + SRA** | 1.052 | 1.079 | 1.131 |

Read off it:

- **SRA alone makes creasing worse**, badly. A point present at coarse levels
  accumulates independent noise from every level while its later-inserted
  neighbours get only the smaller, finer noise, so old points spike.
- **Relax alone over-corrects**, leaving old points flatter than the surface
  around them.
- **Together they cancel.** Both default to on.
- Bare `loop` degrades sharply as H rises: fine at H=0.5, badly creased at
  H=0.9.
- The four-point Loop edge mask scores *worse* than the plain two-point
  midpoint once relax is in play, consistently and at every H. Loop remains
  the default as the better interpolant, but `-stencil midpoint` is a
  defensible choice and the numbers say so.

**None of this eliminates the artefact.** Rendered in grayscale, a faint
hexagonal cell pattern remains visible in every variant. That is inherent to
subdivision — it is Miller's 1986 critique, and the reason summed noise
octaves displaced these methods for most terrain work. If you don't need to
pin exact heights at specific points, fBm over Perlin/simplex noise has no
lattice memory and no creasing.

Note that `Relax` repositions the seven seed points too, so `-island` bias is
weakened by it. Exact control and crease suppression are in tension.

## CLI

```sh
go run ./cmd/hexgen -levels 7 -hurst 0.7 -seed 42 -out terrain.png
go run ./cmd/hexgen -levels 4 -island -ascii            # ASCII to stdout
go run ./cmd/hexgen -palette gray -out raw.png          # shows artefacts
go run ./cmd/hexgen -compare out/ -palette gray         # the full variant set
```

`-hurst` is the roughness knob: 1.0 smooth rolling hills, ~0.5 Brownian and
natural-looking, near 0 violently jagged. The displacement amplitude is
multiplied by `2^-H` after each level, which is what makes the result
fractional Brownian motion rather than white noise or a flat plane.

`-compare` writes one PNG per variant, named for its settings, sweeping H and
the creasing knobs.

## Library

```go
f := hexfield.Generate(hexfield.Params{
    Levels: 7, Hurst: 0.7, Seed: 42,
    Stencil: hexfield.Loop, Relax: true, SRA: true,
})
f.Normalize()
for c, h := range f.All() {
    // c.Q, c.R, c.S and a height in [0,1]
}
```

Displacements are drawn as a stream from a `math/rand/v2` ChaCha8 source in the
order the lattice is walked. That order is fixed, so a seed reproduces a field
exactly.

An earlier version hashed `(seed, coordinate, level)` so any region could be
regenerated independently of traversal, and so the coarse structure stayed put
while you tuned `levels`. That meant hand-rolling a mixer, and the project
standardises on `math/rand/v2` sources; the property was traded away
deliberately. ChaCha8 rather than PCG because seeds here are frequently small
and sequential, and ChaCha8 has no short-seed correlation to reason about.

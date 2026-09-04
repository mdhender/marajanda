// Package worldgen fills a world with generated data.
//
// The generator here is the one donjon's fantasy world maps descend from, by
// way of John Olsson's fractal worldmap: cut the sphere with a random circle,
// raise everything on one side of the cut and lower everything on the other,
// and repeat. A thousand cuts sum to a plausible height field, and because the
// work is done on the sphere rather than on a rectangle, the east-west wrap
// costs nothing -- there is no edge in a sphere to join up.
//
// The one deliberate departure from that lineage is the cutting plane's
// offset. See Options.Offset: it is the whole reason this package exists
// rather than a faithful port.
package worldgen

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/mdhender/marajanda/internal/world"
)

// Name is what a generated world records as its Generator.
const Name = "fault"

// Defaults for Options. Faults is high because the field is a sum: too few
// cuts and the map is a handful of enormous steps rather than a coastline.
const (
	DefaultFaults = 1000
	DefaultOffset = 0.5
	DefaultOcean  = 0.65
)

// maxHexes caps what a caller can ask to allocate. Cols and Rows multiply, so
// a hand-typed pair can otherwise ask for tens of gigabytes; refusing is
// better than attempting it.
const maxHexes = 4 << 20

// Options configures a generated world.
type Options struct {
	Cols, Rows int
	Seed       uint64
	Name       string

	// Faults is how many times the sphere is cut. The height field is the sum
	// of the cuts, so this is the roughness knob: more cuts, finer coastline.
	Faults int

	// Offset is how far a cutting plane may sit from the centre of the
	// sphere, as a fraction of its radius, and it is the point of this
	// generator.
	//
	// At 0 every cut is a great circle, which is what Olsson and donjon do.
	// A great circle raises one hemisphere and lowers the other, so each cut
	// is an odd function under p -> -p, and so is any sum of them: the
	// finished field satisfies h(-p) = -h(p) exactly, and every continent is
	// guaranteed an ocean at its antipode. Measured on a replication of the
	// Olsson port, the correlation between the field and its own antipode is
	// -1.000 at every fault count, and donjon's own output scores -0.683.
	//
	// Above 0 the plane is displaced, so the cut is a small circle and the
	// two sides are unequal. That breaks the oddness and with it the
	// antipodal symmetry. Larger values break it harder, at the cost of cuts
	// that carve out small caps rather than dividing the world.
	Offset float64

	// Ocean is the fraction of hexes to put at or below sea level. It sets
	// Grid.SeaLevel by quantile rather than fixing a height, because the
	// spread of a fault field varies with the seed and a fixed sea level
	// would give a different amount of water on every map.
	Ocean float64
}

// Defaults returns a usable set of options to adjust from.
//
// Fault substitutes nothing, so every field means exactly what it says --
// including the zeroes. That matters here: Offset 0 is the great-circle case
// this generator exists to contrast with, and a "0 means unset" rule would
// make the one setting worth asking for the one setting you cannot ask for.
func Defaults(cols, rows int, seed uint64) Options {
	return Options{
		Cols:   cols,
		Rows:   rows,
		Seed:   seed,
		Faults: DefaultFaults,
		Offset: DefaultOffset,
		Ocean:  DefaultOcean,
	}
}

func (o Options) check() error {
	switch {
	case o.Cols < 1 || o.Rows < 1:
		return fmt.Errorf("worldgen: %dx%d: both dimensions must be positive", o.Cols, o.Rows)
	case o.Cols%2 != 0:
		// Fault always makes a wrapping world, and world.Grid only closes on
		// an even column count -- odd columns are the ones pushed half a row
		// south, so the parity has to alternate all the way round.
		return fmt.Errorf("worldgen: %d columns: a world wraps, and an odd column count cannot close", o.Cols)
	case o.Cols > maxHexes || o.Rows > maxHexes || o.Cols*o.Rows > maxHexes:
		return fmt.Errorf("worldgen: %dx%d is more than %d hexes", o.Cols, o.Rows, maxHexes)
	case o.Faults < 0:
		return fmt.Errorf("worldgen: %d faults", o.Faults)
	case o.Offset < 0 || o.Offset >= 1:
		return fmt.Errorf("worldgen: offset %v is outside 0..1", o.Offset)
	case o.Ocean < 0 || o.Ocean > 1:
		return fmt.Errorf("worldgen: ocean %v is outside 0..1", o.Ocean)
	}
	return nil
}

// Fault generates a world by cutting the sphere. It fills the elevation layer
// and sets the sea level; the climate and terrain layers are left absent for
// a later stage to add.
//
// Every field of opts is used as given; start from Defaults for a world that
// looks like anything.
func Fault(opts Options) (*world.World, error) {
	if err := opts.check(); err != nil {
		return nil, err
	}

	w := world.New(opts.Cols, opts.Rows)
	w.Name, w.Seed, w.Generator = opts.Name, opts.Seed, Name
	g := w.Grid

	// Where every hex sits on the sphere, computed once. Each cut below tests
	// every hex against a plane, so this is the only part worth hoisting.
	px := make([]float64, g.Len())
	py := make([]float64, g.Len())
	pz := make([]float64, g.Len())
	for col := range g.Cols {
		for row := range g.Rows {
			i := g.Index(col, row)
			px[i], py[i], pz[i] = g.Unit(col, row)
		}
	}

	h := make([]float64, g.Len())
	rng := newRand(opts.Seed)
	for range opts.Faults {
		nx, ny, nz := direction(rng)
		// The plane's signed distance from the centre. Zero is a great circle.
		d := opts.Offset * (2*rng.Float64() - 1)
		// Which side rises is a coin flip, so the field is a random walk
		// rather than a climb.
		up := 1.0
		if rng.Uint64()&1 == 0 {
			up = -1
		}
		for i := range h {
			if px[i]*nx+py[i]*ny+pz[i]*nz > d {
				h[i] += up
			} else {
				h[i] -= up
			}
		}
	}

	normalize(h)
	w.Layers.Elevation = h
	w.Grid.SeaLevel = seaLevel(h, opts.Ocean)
	return w, nil
}

// direction draws the cutting plane's normal uniformly from the sphere.
// Uniform in z and in angle: picking a latitude and longitude uniformly
// instead would crowd the cuts around the poles.
func direction(rng *rand.Rand) (x, y, z float64) {
	z = 2*rng.Float64() - 1
	r := math.Sqrt(1 - z*z)
	a := 2 * math.Pi * rng.Float64()
	return r * math.Cos(a), r * math.Sin(a), z
}

// elevationScale rounds elevations to 1/10000. That is far finer than any
// renderer resolves, and it keeps the JSON to short decimals rather than the
// seventeen digits an unrounded float64 needs. Rounding here rather than at
// save time means the stored world is the world that was generated.
const elevationScale = 1e4

// normalize rescales the raw fault sums onto 0..1 in place.
func normalize(h []float64) {
	lo, hi := slices.Min(h), slices.Max(h)
	if lo == hi {
		// No cuts, or every cut cancelled. A flat world sits at mid-height
		// rather than at 0, so that a sea level of 0.5 does not drown it.
		for i := range h {
			h[i] = 0.5
		}
		return
	}
	span := hi - lo
	for i, e := range h {
		h[i] = math.Round((e-lo)/span*elevationScale) / elevationScale
	}
}

// seaLevel is the elevation that puts the requested fraction of the world at
// or below it.
func seaLevel(h []float64, ocean float64) float64 {
	switch k := int(math.Round(ocean * float64(len(h)))); {
	case k <= 0:
		// Elevations are never negative, so this drowns nothing.
		return -1
	case k >= len(h):
		return 1
	default:
		return slices.Sorted(slices.Values(h))[k-1]
	}
}

// newRand mirrors the choice made elsewhere in this repo: math/rand/v2 only,
// and ChaCha8 rather than PCG because seeds are frequently small and
// sequential.
func newRand(seed uint64) *rand.Rand {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	return rand.New(rand.NewChaCha8(key))
}

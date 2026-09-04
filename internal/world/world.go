// Package world is the datastore for a generated world: a cylinder of hexes
// that wraps east to west, the layers of data laid over it, and the JSON it
// is saved as.
//
// The hex layout is not a free choice, which is why it is not a parameter.
// donjon's world maps -- the reference this project is chasing -- place
// features at x = 20*col + 20/3 and y = 23.094*row, plus half a row on odd
// columns. That is flat-top hexes in odd-q offset coordinates at a 40/3
// pixel circumradius, and it fits every place in the sample exactly.
// Worldographer, the eventual export target, stores its tiles as columns
// (wxx.Tiles_t.Tiles is indexed [col][row]). Both ends of the pipeline
// already agree on one layout, so this package agrees with them and converts
// at neither end.
//
// This package is about storage, not generation. It knows the shape of a
// world and how to persist it; what fills the layers lives elsewhere.
package world

import (
	"fmt"
	"time"
)

// Schema identifies the on-disk format. It is stored in every file and
// checked on load, so a future format change is a rejected file rather than
// a silently misread one.
const Schema = "marajanda.world/1"

// World is a generated world and everything known about it.
type World struct {
	Schema string `json:"schema"`
	// Name is the world's own name, as it would appear on the map.
	Name string `json:"name,omitempty"`
	// Seed and Generator are provenance: together they should be enough to
	// produce this world again.
	Seed      uint64    `json:"seed"`
	Generator string    `json:"generator,omitempty"`
	Created   time.Time `json:"created"`

	Grid Grid `json:"grid"`

	// Terrains is the palette Layers.Terrain indexes into, and it is stored
	// with the world so a file explains itself. Use Worldographer's own
	// terrain names here: export is then a lookup into its TerrainMap rather
	// than a translation table that has to be kept in step.
	Terrains []string `json:"terrains,omitempty"`

	Layers Layers `json:"layers"`
}

// Grid is the shape of the hex cylinder.
type Grid struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// WrapEastWest closes the grid into a cylinder, making column Cols-1 the
	// western neighbour of column 0. A world wraps; a region cropped out of
	// one does not, so this is recorded rather than assumed.
	WrapEastWest bool `json:"wrapEastWest"`
	// SeaLevel is the elevation at or below which a hex is water. It lives
	// with the map because it is the single number that decides where the
	// coastline falls, and the same height field at a different sea level is
	// a different world.
	SeaLevel float64 `json:"seaLevel"`
}

// Layers holds one flat slice per datum. A slice is either empty, meaning
// that layer was never generated, or exactly Grid.Len() long and indexed by
// Grid.Index.
//
// Struct-of-arrays rather than a slice of per-hex structs: a later stage adds
// a layer by adding a field, an early one pays nothing for the layers it does
// not have, and the JSON stays one array per concept rather than repeating
// every key once per hex.
type Layers struct {
	// Elevation is normalised to 0..1 across the world, so Grid.SeaLevel and
	// any colour ramp mean the same thing on every map.
	//
	// Values are written exactly. encoding/json emits the shortest text that
	// round-trips a float64, so a generator that quantises its output (say to
	// four decimals) gets a small file for free, and one that does not gets a
	// faithful one.
	Elevation []float64 `json:"elevation,omitempty"`
	// Temperature and Rainfall are the climate inputs a terrain
	// classification needs, also normalised to 0..1. They are stored rather
	// than recomputed because the rules that read them will change.
	Temperature []float64 `json:"temperature,omitempty"`
	Rainfall    []float64 `json:"rainfall,omitempty"`
	// Terrain indexes World.Terrains. It is stored even though it is derived,
	// because the Worldographer export needs a terrain per tile and should
	// not have to re-run a classifier to find one.
	Terrain []int `json:"terrain,omitempty"`
	// Icy marks permanent ice, which Worldographer carries as its own flag
	// on a tile rather than as a terrain.
	Icy []bool `json:"icy,omitempty"`
}

// New returns an empty world of the given size. It allocates no layers; a
// generator allocates the ones it fills, with Alloc.
func New(cols, rows int) *World {
	return &World{
		Schema:  Schema,
		Created: time.Now().UTC().Round(0),
		Grid: Grid{
			Cols:         cols,
			Rows:         rows,
			WrapEastWest: true,
			SeaLevel:     DefaultSeaLevel,
		},
	}
}

// DefaultSeaLevel puts the coastline at the middle of the elevation range.
// It is a starting point, not a law; generators are expected to set their own.
const DefaultSeaLevel = 0.5

// Alloc returns s when it is already the right length for g, and a zeroed
// slice of Grid.Len() otherwise. Generators use it to fill in a layer without
// caring whether the world arrived empty or is being regenerated in place.
func Alloc[T any](g Grid, s []T) []T {
	if len(s) == g.Len() {
		return s
	}
	return make([]T, g.Len())
}

// Hex is one hex's data gathered from across the layers: the array-of-structs
// view that an exporter or a renderer wants, over storage that is
// struct-of-arrays. A layer that does not exist reads as its zero value.
type Hex struct {
	Col, Row    int
	Elevation   float64
	Temperature float64
	Rainfall    float64
	Terrain     int
	Icy         bool
}

// Hex gathers the data at (col, row). It panics if the hex is outside the
// grid, since that is a caller bug rather than a condition to handle.
func (w *World) Hex(col, row int) Hex {
	if !w.Grid.Contains(col, row) {
		panic(fmt.Sprintf("world: hex (%d,%d) is outside a %dx%d grid", col, row, w.Grid.Cols, w.Grid.Rows))
	}
	i := w.Grid.Index(col, row)
	h := Hex{Col: col, Row: row}
	if len(w.Layers.Elevation) != 0 {
		h.Elevation = w.Layers.Elevation[i]
	}
	if len(w.Layers.Temperature) != 0 {
		h.Temperature = w.Layers.Temperature[i]
	}
	if len(w.Layers.Rainfall) != 0 {
		h.Rainfall = w.Layers.Rainfall[i]
	}
	if len(w.Layers.Terrain) != 0 {
		h.Terrain = w.Layers.Terrain[i]
	}
	if len(w.Layers.Icy) != 0 {
		h.Icy = w.Layers.Icy[i]
	}
	return h
}

// IsWater reports whether the hex at (col, row) is at or below sea level.
func (w *World) IsWater(col, row int) bool {
	if len(w.Layers.Elevation) == 0 {
		return false
	}
	return w.Layers.Elevation[w.Grid.Index(col, row)] <= w.Grid.SeaLevel
}

package world

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Validate reports the first thing wrong with a world. Load runs it, so a
// file that decodes into a shape later stages cannot use is rejected at the
// point it was read rather than as a panic three stages downstream.
func (w *World) Validate() error {
	if w.Schema != Schema {
		return fmt.Errorf("schema %q: want %q", w.Schema, Schema)
	}
	if w.Grid.Cols < 1 || w.Grid.Rows < 1 {
		return fmt.Errorf("grid %dx%d: both dimensions must be positive", w.Grid.Cols, w.Grid.Rows)
	}
	// Odd columns are pushed half a row south, so a cylinder closes only when
	// the last column and column 0 have opposite parity. An odd column count
	// leaves two unstaggered columns against each other at the join: the hexes
	// there do not tile, and no amount of care downstream can fix it.
	if w.Grid.WrapEastWest && w.Grid.Cols%2 != 0 {
		return fmt.Errorf("grid wraps east to west with %d columns: an odd count cannot close", w.Grid.Cols)
	}
	n := w.Grid.Len()
	// A layer is either absent or complete. Anything else would leave every
	// reader to decide what a short layer means.
	for _, l := range []struct {
		name string
		got  int
	}{
		{"elevation", len(w.Layers.Elevation)},
		{"temperature", len(w.Layers.Temperature)},
		{"rainfall", len(w.Layers.Rainfall)},
		{"terrain", len(w.Layers.Terrain)},
		{"icy", len(w.Layers.Icy)},
	} {
		if l.got != 0 && l.got != n {
			return fmt.Errorf("%s layer has %d values: want 0 or %d", l.name, l.got, n)
		}
	}
	for i, t := range w.Layers.Terrain {
		if t < 0 || t >= len(w.Terrains) {
			col, row := w.Grid.ColRow(i)
			return fmt.Errorf("terrain %d at (%d,%d): outside a palette of %d", t, col, row, len(w.Terrains))
		}
	}
	return nil
}

// Encode writes the world as JSON. Not named WriteTo, which io reserves for
// a signature that reports a byte count.
func (w *World) Encode(dst io.Writer) error {
	enc := json.NewEncoder(dst)
	enc.SetIndent("", "  ")
	return enc.Encode(w)
}

// Decode reads a world and validates it.
func Decode(src io.Reader) (*World, error) {
	var w World
	if err := json.NewDecoder(src).Decode(&w); err != nil {
		return nil, err
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// Save writes the world to a file, replacing anything already there.
func (w *World) Save(path string) error {
	if err := w.Validate(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := w.Encode(f); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", path, err)
	}
	return f.Close()
}

// Load reads a world from a file.
func Load(path string) (*World, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w, err := Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return w, nil
}

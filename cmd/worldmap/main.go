// Copyright (c) 2026 Michael D Henderson.

// Command worldmap generates a world from a set of seeds and writes it out as
// PNGs, so the generator can be reviewed without a server, a sign-in or a
// browser.
//
// It shares the generator and the meridian cut with the running game, so what
// it draws is what the admin map draws.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mdhender/marajanda/internal/game"
	"github.com/mdhender/marajanda/internal/prng"
	"github.com/mdhender/marajanda/internal/worldmap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worldmap:", err)
		os.Exit(1)
	}
}

func run() error {
	seed := flag.String("game-seed", "98374,-98", "two comma-separated int64 PRNG seeds")
	width := flag.Int("width", 255, "columns either side of the origin; the world is 2*width+1 wide")
	height := flag.Int("height", 127, "rows above and below the origin; the world is 2*height+1 tall")
	hexSize := flag.Int("hex-size", 4, "pixel radius of one hexagon at full scale")
	center := flag.Int("center", 0, "column to place at the centre of the page; use half the width to bring the meridian into view")
	out := flag.String("out", "out", "directory to write the PNGs into")
	flag.Parse()

	seed1, seed2, err := parseSeeds(*seed)
	if err != nil {
		return err
	}

	world, err := game.GenerateWorld(prng.New(uint64(seed1), uint64(seed2)), *width, *height)
	if err != nil {
		return err
	}
	fmt.Printf("world %dx%d, %d hexes, seeds %d,%d\n",
		world.Columns(), world.Rows(), world.Len(), seed1, seed2)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	suffix := ""
	if *center != 0 {
		suffix = fmt.Sprintf("-c%d", *center)
	}
	full := worldmap.Render(world, float64(*hexSize), *center)
	// 100%, 50% and 25%: the same image reduced, so the three views cannot
	// disagree about the world, only about how much of it you can make out.
	for _, scale := range []struct {
		percent int
		factor  int
	}{{100, 1}, {50, 2}, {25, 4}} {
		img := worldmap.Downscale(full, scale.factor)
		name := filepath.Join(*out, fmt.Sprintf("world-%d%s.png", scale.percent, suffix))
		file, err := os.Create(name)
		if err != nil {
			return err
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		fmt.Printf("  %-16s %4d x %4d\n", name, img.Bounds().Dx(), img.Bounds().Dy())
	}

	counts, land, coherence := worldmap.Census(world)
	fmt.Printf("land %.1f%%, neighbours agree %.1f%%\n", 100*land, 100*coherence)
	for _, terrain := range slices.Sorted(maps.Keys(counts)) {
		fmt.Printf("  %-10s %7d  %5.1f%%\n",
			terrain, counts[terrain], 100*float64(counts[terrain])/float64(world.Len()))
	}
	return nil
}

func parseSeeds(text string) (int64, int64, error) {
	first, second, ok := strings.Cut(text, ",")
	if !ok {
		return 0, 0, fmt.Errorf("game seed %q is not two comma-separated int64s", text)
	}
	seed1, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("game seed %q: %w", text, err)
	}
	seed2, err := strconv.ParseInt(strings.TrimSpace(second), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("game seed %q: %w", text, err)
	}
	return seed1, seed2, nil
}

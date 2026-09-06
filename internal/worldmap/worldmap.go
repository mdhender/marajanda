// Copyright (c) 2026 Michael D Henderson.

// Package worldmap draws a generated world as a raster image.
//
// It exists so the world can be inspected without a running server, a browser
// or a session: reviewing what the generator produced is a tight loop, and
// putting a sign-in and an SVG page in that loop makes it a slow one. The
// server draws the same world as SVG, and shares this package's [Cut] so the
// two never disagree about where the meridian falls.
package worldmap

import (
	"image"
	"image/color"
	"math"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/game"
)

// offsetEven selects even-r offset conversion, matching the layout the world is
// generated in.
const offsetEven = true

// Palette is the terrain colouring, matching the map in the web UI so a review
// here and a review in the browser are looking at the same world.
var Palette = map[game.Terrain]color.RGBA{
	game.TerrainGrassland: {0x7f, 0x9c, 0x5a, 0xff},
	game.TerrainForest:    {0x3f, 0x6b, 0x46, 0xff},
	game.TerrainHills:     {0xa9, 0x8a, 0x4e, 0xff},
	game.TerrainMarsh:     {0x5b, 0x7d, 0x78, 0xff},
	game.TerrainMountains: {0x8a, 0x83, 0x78, 0xff},
	game.TerrainOcean:     {0x1d, 0x4a, 0x63, 0xff},
	game.TerrainLake:      {0x2f, 0x7d, 0x95, 0xff},
	game.TerrainIce:       {0xdc, 0xe6, 0xeb, 0xff},
}

// background is what shows through beyond the poles, where there is no world.
var background = color.RGBA{0x0b, 0x14, 0x16, 0xff}

// Cut returns the copy of a hex whose offset column falls in the drawing
// window, which is what draws the world as an upright rectangle.
//
// center is the column placed in the middle of the page. Zero puts the game
// origin there, which is the ordinary view; any other value slides the map
// round the cylinder. That is the only honest way to inspect the meridian: it
// is not an edge, so the only way to see whether it looks like one is to move
// it into the middle and find you cannot pick it out.
//
// Canonical coordinates fix q rather than the offset column, so plotting them
// straight from their axial position leans the map sideways by half a row per
// row - a parallelogram, not a map. Where the meridian falls on the page is a
// drawing decision, not a property of a cylinder, which has no edge to put it
// at.
func Cut(halfWidth, center int, coord hexg.Hex) hexg.Hex {
	offset := coord.CubeToROffset(offsetEven)
	columns := 2*halfWidth + 1
	col := ((offset.Col-center+halfWidth)%columns+columns)%columns - halfWidth
	return hexg.NewOffsetCoord(col, offset.Row).ROffsetToCube(offsetEven)
}

// Render draws the whole world with hexagons of the given pixel radius.
//
// It works backwards, from pixels to hexes rather than from hexes to polygons.
// That costs nothing here and buys exactness: every pixel belongs to whichever
// hex actually encloses it, so the tiling has no seams, no gaps and no overdraw
// however small the hexagons get.
func Render(world game.World, hexSize float64, center int) *image.RGBA {
	halfWidth, halfHeight := world.Width(), world.Height()

	// The display rectangle in pixels, before any shift. Even rows sit at
	// sqrt(3)*size*col and odd rows half a hex to the west of that.
	spanX := math.Sqrt(3) * hexSize
	minX := -spanX*(float64(halfWidth)+0.5) - spanX/2
	maxX := spanX*float64(halfWidth) + spanX/2
	minY := -1.5*hexSize*float64(halfHeight) - hexSize
	maxY := 1.5*hexSize*float64(halfHeight) + hexSize

	layout := hexg.NewLayout(hexg.EvenR,
		hexg.Point{X: hexSize, Y: hexSize},
		hexg.Point{X: -minX, Y: -minY})

	bounds := image.Rect(0, 0, int(math.Ceil(maxX-minX)), int(math.Ceil(maxY-minY)))
	img := image.NewRGBA(bounds)

	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			// The hex under this pixel is a display hex: its offset column is
			// already inside the cut window, so converting back through the
			// offset gives the world hex it stands for.
			display := layout.PixelToHexRounded(hexg.Point{X: float64(x) + 0.5, Y: float64(y) + 0.5})
			offset := display.CubeToROffset(offsetEven)

			paint := background
			if offset.Col >= -halfWidth && offset.Col <= halfWidth {
				coord := world.Normalize(hexg.NewOffsetCoord(offset.Col+center, offset.Row).ROffsetToCube(offsetEven))
				if hex, ok := world.At(coord); ok {
					if rgba, known := Palette[hex.Terrain]; known {
						paint = rgba
					}
				}
			}
			img.SetRGBA(x, y, paint)
		}
	}
	return img
}

// Downscale shrinks an image by an integer factor, averaging each block.
//
// Averaging rather than sampling is the whole point: terrain features run five
// to ten hexes across, so a reduction that picked one pixel per block would
// alias a coherent world into speckle and hide the very thing being reviewed.
func Downscale(src *image.RGBA, factor int) *image.RGBA {
	if factor <= 1 {
		return src
	}
	width := max(1, src.Bounds().Dx()/factor)
	height := max(1, src.Bounds().Dy()/factor)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			var r, g, b, n int
			for dy := range factor {
				for dx := range factor {
					sx, sy := x*factor+dx, y*factor+dy
					if sx >= src.Bounds().Dx() || sy >= src.Bounds().Dy() {
						continue
					}
					c := src.RGBAAt(sx, sy)
					r, g, b, n = r+int(c.R), g+int(c.G), b+int(c.B), n+1
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 0xff})
		}
	}
	return dst
}

// Census counts the world's terrain, which is the numeric half of a review: the
// picture shows whether the shapes are right, this shows whether the mix is.
func Census(world game.World) (counts map[game.Terrain]int, land, coherence float64) {
	counts = make(map[game.Terrain]int, len(Palette))
	dry := 0
	for _, hex := range world.Hexes() {
		counts[hex.Terrain]++
		if hex.Terrain.IsLand() {
			dry++
		}
	}
	if world.Len() == 0 {
		return counts, 0, 0
	}

	// How often neighbouring hexes agree. Independent per-hex rolls over this
	// terrain mix would agree about a quarter of the time, so this is the
	// number that says whether the generator produced geography or noise.
	pairs, matching := 0, 0
	for _, hex := range world.Hexes() {
		for direction := range 6 {
			neighbor, ok := world.At(world.Normalize(hex.Coord.Neighbor(direction)))
			if !ok {
				continue
			}
			pairs++
			if neighbor.Terrain == hex.Terrain {
				matching++
			}
		}
	}
	if pairs > 0 {
		coherence = float64(matching) / float64(pairs)
	}
	return counts, float64(dry) / float64(world.Len()), coherence
}

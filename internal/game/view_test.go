// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"testing"

	"github.com/maloquacious/hexg"
)

func TestWindowViewCoversTheWindow(t *testing.T) {
	world := testWorld(t)
	center := hexg.NewHex(4, -3)

	tiles := WindowView(world, center, 9, 7)
	if want := 9 * 7; len(tiles) != want {
		t.Fatalf("WindowView returned %d tiles, want %d", len(tiles), want)
	}

	seen := make(map[hexg.Hex]struct{}, len(tiles))
	found := false
	for _, tile := range tiles {
		if !tile.Visible || tile.Terrain == "" {
			t.Fatalf("WindowView left %v visible=%v terrain=%q", tile.Coord, tile.Visible, tile.Terrain)
		}
		if _, duplicate := seen[tile.Coord]; duplicate {
			t.Fatalf("WindowView repeated %v", tile.Coord)
		}
		seen[tile.Coord] = struct{}{}
		found = found || tile.Coord.Equals(center)
	}
	if !found {
		t.Fatalf("WindowView(%v) does not contain its own centre", center)
	}
}

// Rows are the one thing a cylinder does not wrap, so a window against a pole
// is short rather than full of hexes that do not exist.
func TestWindowViewClampsRowsAtThePoles(t *testing.T) {
	world := testWorld(t)
	center := hexg.NewHex(0, testWorldHeight)

	tiles := WindowView(world, center, 5, 9)
	if len(tiles) == 0 {
		t.Fatal("WindowView at the pole returned nothing")
	}
	rows := make(map[int]struct{})
	for _, tile := range tiles {
		if !world.Contains(tile.Coord) {
			t.Fatalf("WindowView returned %v, which is not a hex of the world", tile.Coord)
		}
		rows[tile.Coord.R()] = struct{}{}
	}
	// Five of the nine requested rows lie beyond the south pole.
	if len(rows) != 5 {
		t.Fatalf("WindowView at the pole spans %d rows, want 5", len(rows))
	}
}

// Columns do wrap, so a window centred on the meridian is an ordinary full
// window that happens to hold hexes from both ends of the canonical range.
func TestWindowViewWrapsAcrossTheSeam(t *testing.T) {
	world := testWorld(t)
	center := hexg.NewHex(testWorldWidth, 0)

	tiles := WindowView(world, center, 9, 3)
	if want := 9 * 3; len(tiles) != want {
		t.Fatalf("WindowView at the seam returned %d tiles, want %d", len(tiles), want)
	}
	east, west := false, false
	for _, tile := range tiles {
		if !world.Contains(tile.Coord) {
			t.Fatalf("WindowView returned %v, which is not a hex of the world", tile.Coord)
		}
		if !tile.Coord.Equals(world.Normalize(tile.Coord)) {
			t.Fatalf("WindowView returned %v, which is not canonical", tile.Coord)
		}
		east = east || tile.Coord.Q() > testWorldWidth-3
		west = west || tile.Coord.Q() < -testWorldWidth+3
	}
	if !east || !west {
		t.Fatalf("WindowView at the seam reached east=%v west=%v, want both", east, west)
	}
}

// A window wider than the world would draw the same hex twice.
func TestWindowViewNeverRepeatsAHex(t *testing.T) {
	world := mustGenerate(t, testSeeds(), 3, 2)

	tiles := WindowView(world, hexg.NewHex(0, 0), world.Columns()+8, world.Rows())
	seen := make(map[hexg.Hex]struct{}, len(tiles))
	for _, tile := range tiles {
		if _, duplicate := seen[tile.Coord]; duplicate {
			t.Fatalf("WindowView repeated %v", tile.Coord)
		}
		seen[tile.Coord] = struct{}{}
	}
	if len(tiles) != world.Len() {
		t.Fatalf("WindowView returned %d tiles, want the whole world's %d", len(tiles), world.Len())
	}
}

func TestWindowViewRejectsAnEmptyWindow(t *testing.T) {
	for _, size := range [][2]int{{0, 5}, {5, 0}, {-1, 5}} {
		if got := WindowView(testWorld(t), hexg.NewHex(0, 0), size[0], size[1]); got != nil {
			t.Errorf("WindowView(%d, %d) returned %d tiles, want none", size[0], size[1], len(got))
		}
	}
}

func TestPlayerViewRevealsOnlyVisibleHexes(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(7, -6)
	neighbor := origin.Add(hexg.NewHex(1, 0))

	tiles := PlayerView(world, origin, 2, []hexg.Hex{origin, neighbor})
	// Two hexes side by side in one row, plus two hexes of fog on every side.
	if want := 6 * 5; len(tiles) != want {
		t.Fatalf("PlayerView returned %d tiles, want %d", len(tiles), want)
	}

	visible := make(map[hexg.Hex]Terrain)
	for _, tile := range tiles {
		if tile.Visible {
			visible[tile.Coord] = tile.Terrain
			continue
		}
		if tile.Terrain != "" {
			t.Fatalf("fog tile %v leaked terrain %q", tile.Coord, tile.Terrain)
		}
	}
	if len(visible) != 2 {
		t.Fatalf("PlayerView revealed %d tiles, want 2", len(visible))
	}

	// Coordinates are the world's own now, not a per-account frame.
	originHex, _ := world.At(origin)
	if got, ok := visible[origin]; !ok || got != originHex.Terrain {
		t.Fatalf("PlayerView origin tile = %q ok=%v, want %q", got, ok, originHex.Terrain)
	}
	neighborHex, _ := world.At(neighbor)
	if got, ok := visible[world.Normalize(neighbor)]; !ok || got != neighborHex.Terrain {
		t.Fatalf("PlayerView neighbor tile at %v = %q ok=%v", neighbor, got, ok)
	}
}

// An account that can see one hex is sent that hex and its close neighbours,
// and nothing else. This is the size of the whole map, not the size of a
// viewport onto a larger one: there is no larger one to scroll to.
func TestPlayerViewIsBoundedByWhatTheAccountSees(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(7, -6)

	for margin := range 4 {
		tiles := PlayerView(world, origin, margin, []hexg.Hex{origin})
		side := 2*margin + 1
		if want := side * side; len(tiles) != want {
			t.Fatalf("PlayerView with margin %d returned %d tiles, want %d", margin, len(tiles), want)
		}
	}
}

// The map a player is sent must be the same size wherever they are. A window
// that shrank near a pole, or against any other feature of the world, would
// tell them where they stand without their ever having gone there.
func TestPlayerViewIsTheSameSizeEverywhere(t *testing.T) {
	world := testWorld(t)
	want := len(PlayerView(world, hexg.NewHex(0, 0), 2, []hexg.Hex{hexg.NewHex(0, 0)}))

	for _, origin := range []hexg.Hex{
		hexg.NewHex(0, -testWorldHeight),  // the north pole
		hexg.NewHex(0, testWorldHeight),   // the south pole
		hexg.NewHex(testWorldWidth, 0),    // the meridian
		hexg.NewHex(-testWorldWidth, 4),   // the other side of it
		hexg.NewHex(3, testWorldHeight-1), // one row in from the ice
		hexg.NewHex(-6, -testWorldHeight+1),
	} {
		if got := len(PlayerView(world, origin, 2, []hexg.Hex{origin})); got != want {
			t.Fatalf("PlayerView at %v returned %d tiles, want %d everywhere", origin, got, want)
		}
	}
}

// A visible hex beyond a pole has no terrain to show. It must read as fog,
// exactly like ground the player has never seen, rather than drawing a hole in
// the map. Only rows can be outside: there is no eastern or western edge.
func TestPlayerViewFogsHexesBeyondAPole(t *testing.T) {
	world := mustGenerate(t, testSeeds(), 4, 3)
	origin := hexg.NewHex(0, 3)
	outside := hexg.NewHex(0, 5)

	for _, tile := range PlayerView(world, origin, 2, []hexg.Hex{outside}) {
		if tile.Visible {
			t.Fatalf("PlayerView revealed %v from outside the world", tile.Coord)
		}
	}
}

// An account with nothing visible still gets a map, drawn around its origin.
// Falling back to nothing would render an empty frame on the dashboard of a
// player whose visibility has not been recorded yet.
func TestPlayerViewFallsBackToTheOrigin(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(7, -6)

	tiles := PlayerView(world, origin, 2, nil)
	if want := 5 * 5; len(tiles) != want {
		t.Fatalf("PlayerView with nothing visible returned %d tiles, want %d", len(tiles), want)
	}
	for _, tile := range tiles {
		if tile.Visible {
			t.Fatalf("PlayerView revealed %v with nothing visible", tile.Coord)
		}
	}
}

// A player standing against the meridian sees across it. Their eastern
// neighbour is canonically the westmost column of the world, and the view must
// carry it as an ordinary neighbouring hex rather than dropping it.
func TestPlayerViewSeesAcrossTheSeam(t *testing.T) {
	world := testWorld(t)
	origin := hexg.NewHex(testWorldWidth, 0)
	east := world.Normalize(origin.Neighbor(0))

	if east.Q() != -testWorldWidth {
		t.Fatalf("east of the seam = %v, want q = %d", east, -testWorldWidth)
	}

	found := false
	for _, tile := range PlayerView(world, origin, 2, []hexg.Hex{origin, east}) {
		if tile.Coord.Equals(east) {
			found = true
			if !tile.Visible || tile.Terrain == "" {
				t.Fatalf("tile across the seam at %v is visible=%v terrain=%q", east, tile.Visible, tile.Terrain)
			}
		}
	}
	if !found {
		t.Fatalf("PlayerView from %v never returned %v across the seam", origin, east)
	}
}

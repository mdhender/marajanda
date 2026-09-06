// Copyright (c) 2026 Michael D Henderson.

package game

import "github.com/mdhender/marajanda/internal/prng"

// Setting is one generator knob: a name, its value, and what it does.
type Setting struct {
	Name  string
	Value any
	Note  string
}

// Settings returns every constant that shapes a world.
//
// Seeds and dimensions alone do not reproduce a map. Terrain is a pure function
// of those *and* of every knob below, so a record that omits them is not a
// record of anything: change one and the same seeds give a different world.
//
// This is generated from the constants rather than transcribed alongside them,
// which is the point. A hand-kept list of twenty-five numbers drifts from the
// code the first time one of them is tuned, and drifts silently.
func Settings() []Setting {
	return []Setting{
		{"layout", "even-r pointy-top, size 1", "how hexes are placed in the plane for noise sampling"},
		{"waterFraction", waterFraction, "share of the world below sea level"},

		{"elevationOctaves", elevationOctaves, "octaves summed for the elevation field"},
		{"elevationPeriods", elevationPeriods, "elevation features across the world, not cycles per hex"},
		{"warpPeriods", warpPeriods, "domain-warp features across the world"},
		{"warpAmplitude", warpAmplitude, "how far the warp bends the field, in hexes"},
		{"moistureOctaves", moistureOctaves, "octaves summed for the moisture field"},
		{"moisturePeriods", moisturePeriods, "moisture features across the world"},

		{"hillsRank", hillsRank, "elevation rank above which land is hills"},
		{"mountainsRank", mountainsRank, "elevation rank above which land is mountains"},
		{"forestRank", forestRank, "moisture rank above which lowland is forest"},
		{"marshRank", marshRank, "moisture rank above which low lowland is marsh"},
		{"marshLandRank", marshLandRank, "elevation rank below which wet ground can be marsh"},
		{"forestHillsRank", forestHillsRank, "moisture rank above which hills grow forest"},

		{"lowlandCeiling", lowlandCeiling, "metres at the top of the lowland band"},
		{"hillsCeiling", hillsCeiling, "metres at the top of the hills band"},
		{"mountainsCeiling", mountainsCeiling, "metres at the top of the mountains band"},
		{"shelfDepth", shelfDepth, "metres deep just off the coast"},
		{"abyssDepth", abyssDepth, "metres deep in the open ocean"},
		{"shelfFalloff", shelfFalloff, "hexes from land over which the abyss is approached"},
		{"lakeDepth", lakeDepth, "metres at the deepest of an inland lake"},

		{"windBaselineRate", windBaselineRate, "share of carried moisture dropped on level ground"},
		{"windOrographicK", windOrographicK, "extra rain per unit of climb"},
		{"windEvaporation", windEvaporation, "moisture picked up crossing water"},
		{"windInitialLoad", windInitialLoad, "moisture the wind enters the world carrying"},
		{"windLaps", windLaps, "laps a purely east-west wind circles before its rain is kept"},
		{"moistureNoiseShare", moistureNoiseShare, "share of moisture from noise rather than transport"},

		{"tagWorld", int(tagWorldForSettings()), "PRNG domain tag for world-scale draws"},
		{"fieldElevation", int(fieldElevation), "PRNG field number for the elevation lattice"},
		{"fieldWarpX", int(fieldWarpX), "PRNG field number for the x warp lattice"},
		{"fieldWarpY", int(fieldWarpY), "PRNG field number for the y warp lattice"},
		{"fieldMoisture", int(fieldMoisture), "PRNG field number for the moisture lattice"},
		{"fieldWind", int(fieldWind), "PRNG field number for the prevailing wind draw"},
	}
}

// tagWorldForSettings names the PRNG domain every world draw is addressed
// under. It is a compatibility surface: renumbering it rewrites every world.
func tagWorldForSettings() prng.Key {
	return prng.TagWorld
}

// generatorLayoutOffset pins the layout named in Settings to the one actually
// used, so the record cannot claim a layout the generator does not use.
var _ = func() bool {
	if generatorLayout().IsEvenR() {
		return true
	}
	panic("game: Settings names even-r but generatorLayout is not")
}()

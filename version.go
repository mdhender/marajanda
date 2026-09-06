// Copyright (c) 2026 Michael D Henderson.

package marajanda

import (
	"github.com/maloquacious/semver"
)

func Version() semver.Version {
	return semver.Version{
		Major:      0,
		Minor:      9,
		Patch:      0,
		PreRelease: "beta",
		Build:      semver.Commit(),
	}
}

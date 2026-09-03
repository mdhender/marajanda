// Package mapgen is the registry that hex map generators plug into.
//
// A generator declares its parameters as data. Defaults, form rendering,
// parsing, clamping and the web UI are all derived from that one declaration,
// so adding a generator means adding one file and never touching the server.
package mapgen

import (
	"fmt"
	"image"
	"sort"
	"sync"
)

// Kind is the type of a parameter, and drives how it is rendered and parsed.
// It is a string so templates can compare against it directly.
type Kind string

const (
	KindInt    Kind = "int"
	KindFloat  Kind = "float"
	KindBool   Kind = "bool"
	KindChoice Kind = "choice"
	// KindSeed is an integer that defaults to a fresh random value on every
	// form render, so reloading the page offers a new map rather than the
	// same one.
	KindSeed Kind = "seed"
)

// Param declares one tunable of a generator.
type Param struct {
	Name    string // form key; must be unique within a generator
	Label   string
	Kind    Kind
	Default any      // int, float64, bool or string, matching Kind
	Min     float64  // KindInt and KindFloat: inclusive bounds
	Max     float64  //
	Step    float64  // form input granularity; 0 means pick a sane default
	Choices []string // KindChoice only
	Help    string
}

// Generator produces a hex map image from a set of parameter values.
type Generator interface {
	// Name is the stable, URL-safe identifier.
	Name() string
	// Title is the human-readable name.
	Title() string
	// Description is one or two sentences shown alongside the form.
	Description() string
	// Params declares the tunables, in the order they should be shown.
	Params() []Param
	// Generate renders a map. Values are already clamped to each Param's
	// bounds, so a generator can trust them.
	Generate(Values) (image.Image, error)
}

var (
	mu         sync.RWMutex
	registered = map[string]Generator{}
)

// Register adds a generator to the default registry. It panics on a duplicate
// or malformed name, since registration happens at init time and a clash is a
// programming error rather than something to recover from.
func Register(g Generator) {
	mu.Lock()
	defer mu.Unlock()

	name := g.Name()
	if name == "" {
		panic("mapgen: generator with empty name")
	}
	if _, dup := registered[name]; dup {
		panic(fmt.Sprintf("mapgen: generator %q registered twice", name))
	}
	seen := map[string]bool{}
	for _, p := range g.Params() {
		if seen[p.Name] {
			panic(fmt.Sprintf("mapgen: generator %q declares parameter %q twice", name, p.Name))
		}
		seen[p.Name] = true
	}
	registered[name] = g
}

// All returns every registered generator, ordered by title so the UI is
// stable across runs regardless of init order.
func All() []Generator {
	mu.RLock()
	defer mu.RUnlock()

	out := make([]Generator, 0, len(registered))
	for _, g := range registered {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title() < out[j].Title() })
	return out
}

// Get returns the named generator.
func Get(name string) (Generator, bool) {
	mu.RLock()
	defer mu.RUnlock()

	g, ok := registered[name]
	return g, ok
}

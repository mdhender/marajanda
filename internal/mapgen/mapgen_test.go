package mapgen

import (
	"image"
	"net/url"
	"testing"
)

type fake struct{ name string }

func (f fake) Name() string        { return f.name }
func (f fake) Title() string       { return "Fake " + f.name }
func (f fake) Description() string { return "" }

func (fake) Params() []Param {
	return []Param{
		{Name: "seed", Kind: KindSeed},
		{Name: "levels", Kind: KindInt, Default: 7, Min: 1, Max: 9},
		{Name: "hurst", Kind: KindFloat, Default: 0.7, Min: 0.05, Max: 1.0},
		{Name: "relax", Kind: KindBool, Default: true},
		{Name: "quiet", Kind: KindBool, Default: false},
		{Name: "palette", Kind: KindChoice, Default: "terrain", Choices: []string{"terrain", "gray"}},
	}
}

func (fake) Generate(Values) (image.Image, error) { return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil }

var g = fake{"fake"}

// A generator must be able to trust its values, so out-of-range input is
// clamped rather than rejected or passed through.
func TestValuesClamp(t *testing.T) {
	v := NewValues(g, map[string]string{"levels": "9999", "hurst": "-4"})
	if got := v.Int("levels"); got != 9 {
		t.Errorf("levels clamped to %d, want 9", got)
	}
	if got := v.Float("hurst"); got != 0.05 {
		t.Errorf("hurst clamped to %v, want 0.05", got)
	}
}

func TestValuesFallBackToDefaults(t *testing.T) {
	v := NewValues(g, nil)
	if got := v.Int("levels"); got != 7 {
		t.Errorf("levels = %d, want the default 7", got)
	}
	if got := v.Float("hurst"); got != 0.7 {
		t.Errorf("hurst = %v, want the default 0.7", got)
	}
	if !v.Bool("relax") {
		t.Error("relax = false, want the default true")
	}
	if got := v.String("palette"); got != "terrain" {
		t.Errorf("palette = %q, want the default terrain", got)
	}
}

func TestValuesRejectGarbage(t *testing.T) {
	v := NewValues(g, map[string]string{"levels": "seven", "hurst": "", "palette": "chartreuse"})
	if got := v.Int("levels"); got != 7 {
		t.Errorf("unparseable levels gave %d, want the default 7", got)
	}
	if got := v.Float("hurst"); got != 0.7 {
		t.Errorf("empty hurst gave %v, want the default 0.7", got)
	}
	if got := v.String("palette"); got != "terrain" {
		t.Errorf("off-list palette gave %q, want the default terrain", got)
	}
}

// A browser omits unchecked boxes entirely, so on a submitted form an absent
// bool must read as false and not silently revert to a true default.
func TestFromFormTreatsAbsentBoolAsFalse(t *testing.T) {
	v := FromForm(g, url.Values{"quiet": {"true"}})
	if v.Bool("relax") {
		t.Error("relax = true for an unchecked box, want false")
	}
	if !v.Bool("quiet") {
		t.Error("quiet = false for a checked box, want true")
	}
}

// Absent bools are only false for a form. A partial map is a different thing
// and should still fall back.
func TestNewValuesKeepsBoolDefaults(t *testing.T) {
	if v := NewValues(g, map[string]string{}); !v.Bool("relax") {
		t.Error("relax = false, want the default true outside form parsing")
	}
}

func TestDefaultsGiveFreshSeeds(t *testing.T) {
	a, b := Defaults(g)["seed"], Defaults(g)["seed"]
	if a == "" || b == "" {
		t.Fatal("no seed default produced")
	}
	if a == b {
		t.Errorf("two Defaults calls produced the same seed %q; each page load should differ", a)
	}
	if got := Defaults(g)["levels"]; got != "7" {
		t.Errorf("levels default formatted as %q, want \"7\"", got)
	}
}

func TestSeedIsNotClamped(t *testing.T) {
	const big = "18446744073709551615" // math.MaxUint64
	if got := NewValues(g, map[string]string{"seed": big}).Uint64("seed"); got != 1<<64-1 {
		t.Errorf("seed = %d, want the full uint64 range preserved", got)
	}
}

// registerForTest registers g and takes it out again when the test ends.
//
// The registry is package-level state that Register deliberately refuses to
// overwrite, so a test that registered and walked away left an entry behind:
// harmless within one run, fatal on the second, which made every test here
// fail under -count=2 or higher. TestRegisterRejectsDuplicates failed
// especially quietly, still passing while recovering from the wrong panic.
func registerForTest(t *testing.T, g Generator) {
	t.Helper()
	Register(g)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		delete(registered, g.Name())
	})
}

func TestRegisterAndGet(t *testing.T) {
	registerForTest(t, fake{"registry-test"})
	got, ok := Get("registry-test")
	if !ok {
		t.Fatal("registered generator not found")
	}
	if got.Name() != "registry-test" {
		t.Errorf("got %q", got.Name())
	}
	if _, ok := Get("nope"); ok {
		t.Error("unregistered name resolved")
	}
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	registerForTest(t, fake{"dup-test"})
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate name did not panic")
		}
	}()
	// The second registration is the one under test, and it must not be
	// cleaned up: it never lands, and deleting the name twice would take the
	// first one out from under the cleanup above.
	Register(fake{"dup-test"})
}

// All must be ordered, or the picker shuffles between runs.
func TestAllIsOrdered(t *testing.T) {
	registerForTest(t, fake{"zzz-order"})
	registerForTest(t, fake{"aaa-order"})
	all := All()
	for i := 1; i < len(all); i++ {
		if all[i-1].Title() > all[i].Title() {
			t.Fatalf("All not ordered: %q before %q", all[i-1].Title(), all[i].Title())
		}
	}
}

package mapgen

import (
	"math/rand/v2"
	"net/url"
	"strconv"
)

// Values holds parameter values for one Generate call.
//
// Accessors never fail: a missing or unparseable value falls back to the
// Param's default, and numbers are clamped to the declared bounds. That keeps
// generators free of validation code and means a hand-typed URL cannot push a
// generator outside the range it declared it could handle.
type Values struct {
	params map[string]Param
	raw    map[string]string
}

// NewValues builds Values for g from raw string values, which may be partial.
func NewValues(g Generator, raw map[string]string) Values {
	v := Values{params: map[string]Param{}, raw: raw}
	if v.raw == nil {
		v.raw = map[string]string{}
	}
	for _, p := range g.Params() {
		v.params[p.Name] = p
	}
	return v
}

// FromForm builds Values from a submitted form.
//
// An unchecked checkbox is omitted by the browser entirely, so for a submitted
// form an absent bool means false rather than "use the default". Every other
// kind falls back to its default when absent.
func FromForm(g Generator, form url.Values) Values {
	raw := map[string]string{}
	for _, p := range g.Params() {
		if got, ok := form[p.Name]; ok && len(got) > 0 {
			raw[p.Name] = got[0]
			continue
		}
		if p.Kind == KindBool {
			raw[p.Name] = "false"
		}
	}
	return NewValues(g, raw)
}

// Defaults returns form-ready default values for g. Seed parameters get a
// fresh random value on every call, so each page load offers a new map.
func Defaults(g Generator) map[string]string {
	out := map[string]string{}
	for _, p := range g.Params() {
		if p.Kind == KindSeed {
			out[p.Name] = strconv.FormatUint(rand.Uint64(), 10)
			continue
		}
		out[p.Name] = format(p.Default)
	}
	return out
}

// Raw returns the underlying string values, for round-tripping into a form.
func (v Values) Raw() map[string]string { return v.raw }

// Int returns a bounded integer parameter.
func (v Values) Int(name string) int {
	p := v.params[name]
	n, err := strconv.Atoi(v.raw[name])
	if err != nil {
		n, _ = p.Default.(int)
	}
	if p.Min != 0 || p.Max != 0 {
		n = min(max(n, int(p.Min)), int(p.Max))
	}
	return n
}

// Float returns a bounded floating-point parameter.
func (v Values) Float(name string) float64 {
	p := v.params[name]
	f, err := strconv.ParseFloat(v.raw[name], 64)
	if err != nil {
		f, _ = p.Default.(float64)
	}
	if p.Min != 0 || p.Max != 0 {
		f = min(max(f, p.Min), p.Max)
	}
	return f
}

// Bool returns a boolean parameter. Anything unparseable is the default.
func (v Values) Bool(name string) bool {
	b, err := strconv.ParseBool(v.raw[name])
	if err != nil {
		b, _ = v.params[name].Default.(bool)
	}
	return b
}

// String returns a string parameter, restricted to the declared choices.
func (v Values) String(name string) string {
	p := v.params[name]
	s := v.raw[name]
	if len(p.Choices) > 0 {
		for _, c := range p.Choices {
			if c == s {
				return s
			}
		}
		d, _ := p.Default.(string)
		return d
	}
	if s == "" {
		d, _ := p.Default.(string)
		return d
	}
	return s
}

// Uint64 returns a seed parameter. Seeds are not clamped: the whole range is
// meaningful, and an out-of-range value is better wrapped than rejected.
func (v Values) Uint64(name string) uint64 {
	n, err := strconv.ParseUint(v.raw[name], 10, 64)
	if err != nil {
		return rand.Uint64()
	}
	return n
}

func format(v any) string {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case string:
		return t
	case nil:
		return ""
	default:
		return ""
	}
}

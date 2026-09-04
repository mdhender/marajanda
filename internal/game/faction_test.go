// Copyright (c) 2026 Michael D Henderson.

package game

import "testing"

func TestNormalizeFactionName(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "The Cartographers", want: "The Cartographers"},
		{name: "spaces", input: "  The   Cartographers  ", want: "The Cartographers"},
		{name: "Unicode", input: "星の民", want: "星の民"},
		{name: "rune length", input: "🐉🐉🐉", want: "🐉🐉🐉"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeFactionName(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("NormalizeFactionName(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestNormalizeFactionNameRejectsInvalidNames(t *testing.T) {
	for _, input := range []string{
		"ab",
		"123456789012345678901234567890123",
		"line\nbreak",
		"tab\tseparated",
		"non" + string(rune(0xa0)) + "breaking",
		"non" + string(rune(16)) + "printing",
		string([]byte{0xff}),
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := NormalizeFactionName(input); err == nil {
				t.Fatalf("NormalizeFactionName(%q) succeeded, want error", input)
			}
		})
	}
}

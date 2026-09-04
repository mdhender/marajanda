// Copyright (c) 2026 Michael D Henderson.

// Package game implements Marajanda game rules.
package game

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidFactionName = errors.New("faction name must contain 3 to 32 printable characters, using spaces only between words")

// NormalizeFactionName validates a faction name and collapses runs of spaces.
func NormalizeFactionName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrInvalidFactionName
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return "", ErrInvalidFactionName
		}
	}
	name = strings.Join(strings.Fields(name), " ")
	if length := utf8.RuneCountInString(name); length < 3 || length > 32 {
		return "", ErrInvalidFactionName
	}
	return name, nil
}

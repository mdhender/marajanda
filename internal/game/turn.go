// Copyright (c) 2026 Michael D Henderson.

package game

// A turn is the game's clock. It is an integer, starts at FirstTurn, and only
// ever increases. There is one game per database, so there is one current turn.
//
// Facts about the world are dated in turns over the half-open period
// [effective_from, effective_through), so a fact is true on turn t when
//
//	effective_from <= t && t < effective_through
//
// The clock is the turn, and turn processing is what moves it. An order issued
// during turn 3 executes when turn 3 is processed, and the fact it writes is
// effective from turn 4: on turn 3 the settlement is still named Mudville, and
// every turn-3 report line says Mudville. Turn processing therefore only ever
// closes an open row at turn+1 - replacing EndOfTimeTurn with turn+1 - and
// opens the replacement running from turn+1 to EndOfTimeTurn, so the world a
// report describes never changes underneath the report.
//
// A faction configured during turn T is the exception that proves it: its
// entities' founding facts are effective from T, because nothing about them is
// waiting on a turn to be processed.
//
// A period that has not ended runs to EndOfTimeTurn rather than to nothing.
// Sentinels rather than an absent end because every read of a fact is that one
// between-test, and an open end written as NULL drags three-valued logic into
// the predicate that has to be right. With a sentinel there is no IS NULL
// branch, no COALESCE, and an index that behaves the same for an open period as
// for a closed one; a row missing its end becomes a constraint violation
// instead of an open period nobody meant to write.
const (
	// StartOfTimeTurn dates a fact that was already true before the game's
	// first turn. Nothing writes it yet - a faction's founding facts are
	// effective from the turn it was configured - and it is named so that the
	// bounds of a period are named at both ends rather than at one.
	StartOfTimeTurn = 0

	// FirstTurn is the turn a new game sits on.
	FirstTurn = 1

	// EndOfTimeTurn ends a period that has not ended: the fact is still true.
	//
	// It is not MaxTurn. That name describes the range of a type, which invites
	// reading it as a turn the game might one day reach; this one says what the
	// value means in a period, which is the only place it is ever written.
	EndOfTimeTurn = 99_999_999
)

// ValidTurn reports whether turn is a turn the game can actually be on.
// Neither sentinel is one, so neither can collide with a real turn.
func ValidTurn(turn int) bool {
	return turn >= FirstTurn && turn < EndOfTimeTurn
}
